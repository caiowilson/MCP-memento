#!/usr/bin/env node
"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const fsp = require("node:fs/promises");
const http = require("node:http");
const https = require("node:https");
const os = require("node:os");
const path = require("node:path");
const { spawn } = require("node:child_process");

const RELEASE_VERSION = "1.0.3";
const RELEASE_TAG = `server/v${RELEASE_VERSION}`;
const RELEASE_BASE_URL = `https://github.com/caiowilson/MCP-memento/releases/download/${encodeURIComponent(RELEASE_TAG)}`;
const MAX_BINARY_BYTES = 128 * 1024 * 1024;
const MAX_CHECKSUM_BYTES = 4096;
const MAX_REDIRECTS = 5;
const DOWNLOAD_TIMEOUT_MS = 30_000;
const LOCK_WAIT_MS = 75_000;
const LOCK_RETRY_MS = 200;
const UPDATE_CHECK_INTERVAL_MS = 24 * 60 * 60 * 1000;
const UPDATE_CHECK_DEADLINE_MS = 30_000;
const UPDATE_STATE_FILE = "marketplace-update-state.json";

function marketplaceUpdateStatePath(pluginData) {
  return path.join(pluginData, UPDATE_STATE_FILE);
}

async function readJSON(filename) {
  try {
    return JSON.parse(await fsp.readFile(filename, "utf8"));
  } catch (error) {
    if (error && error.code === "ENOENT") return null;
    throw error;
  }
}

async function writeJSONAtomic(filename, value) {
  await fsp.mkdir(path.dirname(filename), { recursive: true, mode: 0o700 });
  const temporary = `${filename}.tmp-${process.pid}-${crypto.randomBytes(6).toString("hex")}`;
  try {
    await fsp.writeFile(temporary, `${JSON.stringify(value, null, 2)}\n`, { flag: "wx", mode: 0o600 });
    await fsp.rename(temporary, filename);
  } catch (error) {
    await fsp.rm(temporary, { force: true });
    throw error;
  }
}

function normalizedUpdateState(value = {}) {
  const state = {
    currentVersion: typeof value.currentVersion === "string" ? value.currentVersion : RELEASE_VERSION,
    effectivePolicy: value.effectivePolicy === true,
    policyProvenance: typeof value.policyProvenance === "string" ? value.policyProvenance : "legacy-marketplace-install",
    result: typeof value.result === "string" ? value.result : "never",
  };
  for (const key of ["availableVersion", "lastCheckedAt"]) {
    if (typeof value[key] === "string" && value[key] !== "") state[key] = value[key];
  }
  return state;
}

async function persistUpdateState(pluginData, value) {
  const state = normalizedUpdateState(value);
  await writeJSONAtomic(marketplaceUpdateStatePath(pluginData), state);
  return state;
}

async function initializeUpdateState({ pluginData, home = os.homedir(), pluginDataExisted }) {
  const preference = await readJSON(path.join(home, ".memento-mcp", "marketplace-update.json"));
  const persisted = await readJSON(marketplaceUpdateStatePath(pluginData));
  let effectivePolicy;
  let policyProvenance;
  if (preference && typeof preference.autoUpdate === "boolean") {
    effectivePolicy = preference.autoUpdate;
    policyProvenance = "setup-preference";
  } else if (persisted && typeof persisted.effectivePolicy === "boolean") {
    effectivePolicy = persisted.effectivePolicy;
    policyProvenance = persisted.policyProvenance;
  } else if (pluginDataExisted) {
    effectivePolicy = false;
    policyProvenance = "legacy-marketplace-install";
  } else {
    effectivePolicy = true;
    policyProvenance = "new-marketplace-install";
  }
  return persistUpdateState(pluginData, {
    ...(persisted || {}),
    currentVersion: RELEASE_VERSION,
    effectivePolicy,
    policyProvenance,
  });
}

async function acquireUpdateLock(lockFilename) {
  for (let attempt = 0; attempt < 2; attempt += 1) {
    try {
      const handle = await fsp.open(lockFilename, "wx", 0o600);
      await handle.writeFile(JSON.stringify({ pid: process.pid, createdAt: new Date().toISOString() }));
      return handle;
    } catch (error) {
      if (!error || error.code !== "EEXIST") throw error;
      if (await lockOwnerAlive(lockFilename)) return null;
      await fsp.rm(lockFilename, { force: true });
    }
  }
  return null;
}

function commandResult(command, args, { timeoutMs }) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { stdio: ["ignore", "pipe", "pipe"], windowsHide: true });
    const stdout = [];
    const stderr = [];
    const timer = setTimeout(() => {
      child.kill("SIGTERM");
      reject(Object.assign(new Error("marketplace update deadline exceeded"), { code: "ETIMEDOUT" }));
    }, timeoutMs);
    child.stdout.on("data", (chunk) => stdout.push(chunk));
    child.stderr.on("data", (chunk) => stderr.push(chunk));
    child.once("error", (error) => {
      clearTimeout(timer);
      reject(error);
    });
    child.once("exit", (code, signal) => {
      clearTimeout(timer);
      if (code === 0) {
        resolve({ stdout: Buffer.concat(stdout).toString("utf8"), stderr: Buffer.concat(stderr).toString("utf8") });
        return;
      }
      reject(Object.assign(new Error(`${command} exited ${code === null ? signal : code}`), { code: "COMMAND_FAILED" }));
    });
  });
}

function withDeadline(operation, timeoutMs) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(Object.assign(new Error("marketplace update deadline exceeded"), { code: "ETIMEDOUT" })), timeoutMs);
    Promise.resolve().then(operation).then(
      (value) => { clearTimeout(timer); resolve(value); },
      (error) => { clearTimeout(timer); reject(error); },
    );
  });
}

function versionFromText(text) {
  return (String(text || "").match(/\b\d+\.\d+\.\d+\b/g) || []).at(-1) || RELEASE_VERSION;
}

function updateFailureResult(error) {
  if (error && error.code === "ETIMEDOUT") return "deadline";
  if (error && ["ENETUNREACH", "ENOTFOUND", "ECONNREFUSED", "ECONNRESET", "EHOSTUNREACH"].includes(error.code)) return "offline";
  return "failed";
}

async function runMarketplaceUpdateCheck({ pluginData, now = () => new Date(), commandRunner = commandResult, deadlineMs = UPDATE_CHECK_DEADLINE_MS }) {
  let state = normalizedUpdateState(await readJSON(marketplaceUpdateStatePath(pluginData)) || {});
  if (!state.effectivePolicy) return state;
  const currentTime = now();
  const lastChecked = Date.parse(state.lastCheckedAt || "");
  if (Number.isFinite(lastChecked) && currentTime.getTime() - lastChecked < UPDATE_CHECK_INTERVAL_MS) return state;

  const lockFilename = path.join(pluginData, "marketplace-update-state.lock");
  const lock = await acquireUpdateLock(lockFilename);
  if (!lock) return state;
  try {
    state = normalizedUpdateState(await readJSON(marketplaceUpdateStatePath(pluginData)) || state);
    const lockedLastChecked = Date.parse(state.lastCheckedAt || "");
    if (Number.isFinite(lockedLastChecked) && currentTime.getTime() - lockedLastChecked < UPDATE_CHECK_INTERVAL_MS) return state;

    const next = await persistUpdateState(pluginData, {
      ...state,
      currentVersion: RELEASE_VERSION,
      lastCheckedAt: currentTime.toISOString(),
    });
    const deadline = Date.now() + deadlineMs;
    const invoke = (command, args) => {
      const timeoutMs = Math.max(1, deadline - Date.now());
      return withDeadline(() => commandRunner(command, args, { timeoutMs }), timeoutMs);
    };
    try {
      const refresh = await invoke("claude", ["plugin", "marketplace", "update", "memento-mcp"]);
      const staged = await invoke("claude", ["plugin", "update", "memento@memento-mcp"]);
      return persistUpdateState(pluginData, {
        ...next,
        availableVersion: versionFromText(`${refresh.stdout}\n${refresh.stderr}\n${staged.stdout}\n${staged.stderr}`),
        result: "staged",
      });
    } catch (error) {
      return persistUpdateState(pluginData, { ...next, result: updateFailureResult(error) });
    }
  } finally {
    await lock.close().catch(() => {});
    await fsp.rm(lockFilename, { force: true });
  }
}

function resolveTarget(platform = process.platform, arch = process.arch) {
  const os = { darwin: "darwin", linux: "linux", win32: "windows" }[platform];
  const cpu = { x64: "x64", arm64: "arm64" }[arch];
  if (!os || !cpu) {
    throw new Error(`unsupported platform: ${platform}/${arch}`);
  }
  const extension = os === "windows" ? ".exe" : "";
  return {
    asset: `memento-mcp_${os}_${cpu}${extension}`,
    extension,
    os,
    arch: cpu,
  };
}

function parseChecksum(text, asset) {
  const match = text.trim().match(/^([a-fA-F0-9]{64})(?:\s+\*?(.+))?$/);
  if (!match) {
    throw new Error(`invalid checksum file for ${asset}`);
  }
  if (match[2] && path.basename(match[2].trim()) !== asset) {
    throw new Error(`checksum names ${match[2].trim()}, expected ${asset}`);
  }
  return match[1].toLowerCase();
}

function requestBuffer(url, options = {}, redirects = 0) {
  const maxBytes = options.maxBytes || MAX_BINARY_BYTES;
  const parsed = new URL(url);
  const allowHTTP = options.allowHTTP === true;
  if (parsed.protocol !== "https:" && !(allowHTTP && parsed.protocol === "http:")) {
    return Promise.reject(new Error(`refusing non-HTTPS download: ${parsed.protocol}`));
  }
  if (redirects > MAX_REDIRECTS) {
    return Promise.reject(new Error("too many release download redirects"));
  }

  const client = parsed.protocol === "https:" ? https : http;
  return new Promise((resolve, reject) => {
    const request = client.get(parsed, {
      headers: {
        Accept: "application/octet-stream",
        "User-Agent": `memento-claude-plugin/${RELEASE_VERSION}`,
      },
    }, (response) => {
      const status = response.statusCode || 0;
      if ([301, 302, 303, 307, 308].includes(status)) {
        const location = response.headers.location;
        response.resume();
        if (!location) {
          reject(new Error(`release download redirect ${status} had no location`));
          return;
        }
        const next = new URL(location, parsed).toString();
        requestBuffer(next, options, redirects + 1).then(resolve, reject);
        return;
      }
      if (status !== 200) {
        response.resume();
        reject(new Error(`release download failed with HTTP ${status}`));
        return;
      }

      const declared = Number(response.headers["content-length"] || 0);
      if (declared > maxBytes) {
        response.destroy();
        reject(new Error(`release asset exceeds ${maxBytes} bytes`));
        return;
      }

      const chunks = [];
      let size = 0;
      response.on("data", (chunk) => {
        size += chunk.length;
        if (size > maxBytes) {
          response.destroy(new Error(`release asset exceeds ${maxBytes} bytes`));
          return;
        }
        chunks.push(chunk);
      });
      response.on("end", () => resolve(Buffer.concat(chunks)));
      response.on("error", reject);
    });
    request.setTimeout(options.timeoutMs || DOWNLOAD_TIMEOUT_MS, () => {
      request.destroy(new Error("release download timed out"));
    });
    request.on("error", reject);
  });
}

async function isFile(filename) {
  try {
    return (await fsp.stat(filename)).isFile();
  } catch (error) {
    if (error && error.code === "ENOENT") return false;
    throw error;
  }
}

function hashFile(filename) {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash("sha256");
    const input = fs.createReadStream(filename);
    input.on("error", reject);
    input.on("data", (chunk) => hash.update(chunk));
    input.on("end", () => resolve(hash.digest("hex")));
  });
}

async function isVerified(filename, checksumFilename, asset) {
  if (!(await isFile(filename)) || !(await isFile(checksumFilename))) return false;
  try {
    const expected = parseChecksum(await fsp.readFile(checksumFilename, "utf8"), asset);
    return (await hashFile(filename)) === expected;
  } catch (error) {
    if (error && error.code === "ENOENT") return false;
    return false;
  }
}

function sleep(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

async function lockOwnerAlive(lockFilename) {
  try {
    const raw = await fsp.readFile(lockFilename, "utf8");
    const owner = JSON.parse(raw);
    if (!Number.isSafeInteger(owner.pid) || owner.pid <= 0) return false;
    try {
      process.kill(owner.pid, 0);
      return true;
    } catch (error) {
      return error && error.code !== "ESRCH";
    }
  } catch (error) {
    if (error && error.code === "ENOENT") return false;
    const stat = await fsp.stat(lockFilename).catch(() => null);
    return Boolean(stat && Date.now() - stat.mtimeMs < 2000);
  }
}

async function acquireInstallLock(lockFilename, installed, installedChecksum, asset) {
  const deadline = Date.now() + LOCK_WAIT_MS;
  while (Date.now() < deadline) {
    try {
      const handle = await fsp.open(lockFilename, "wx", 0o600);
      await handle.writeFile(JSON.stringify({ pid: process.pid, createdAt: new Date().toISOString() }));
      return handle;
    } catch (error) {
      if (!error || error.code !== "EEXIST") throw error;
      if (await isVerified(installed, installedChecksum, asset)) return null;
      if (!(await lockOwnerAlive(lockFilename))) {
        await fsp.rm(lockFilename, { force: true });
        continue;
      }
      await sleep(LOCK_RETRY_MS);
    }
  }
  throw new Error(`timed out waiting for another Memento download: ${asset}`);
}

async function resolveBinary(options = {}) {
  const override = options.override || process.env.MEMENTO_PLUGIN_BINARY;
  if (override) {
    const absolute = path.resolve(override);
    if (!(await isFile(absolute))) {
      throw new Error(`MEMENTO_PLUGIN_BINARY is not a file: ${absolute}`);
    }
    return absolute;
  }

  const pluginRoot = options.pluginRoot || path.resolve(__dirname, "..");
  const pluginData = options.pluginData || process.env.MEMENTO_PLUGIN_DATA;
  if (!pluginData) {
    throw new Error("CLAUDE_PLUGIN_DATA was not expanded by Claude Code");
  }
  if (!path.isAbsolute(pluginData) || pluginData.includes("${")) {
    throw new Error("CLAUDE_PLUGIN_DATA did not resolve to an absolute path");
  }

  const target = resolveTarget(options.platform, options.arch);
  const bundled = path.join(pluginRoot, "dist", target.asset);
  if (await isFile(bundled)) {
    return bundled;
  }

  const installDir = path.join(pluginData, "bin", `server-v${RELEASE_VERSION}`);
  const installed = path.join(installDir, target.asset);
  const installedChecksum = `${installed}.sha256`;
  if (await isVerified(installed, installedChecksum, target.asset)) {
    return installed;
  }

  await fsp.mkdir(installDir, { recursive: true, mode: 0o700 });
  const lockFilename = `${installed}.lock`;
  const lock = await acquireInstallLock(lockFilename, installed, installedChecksum, target.asset);
  if (!lock) return installed;
  try {
    if (await isVerified(installed, installedChecksum, target.asset)) return installed;
    await Promise.all([
      fsp.rm(installed, { force: true }),
      fsp.rm(installedChecksum, { force: true }),
    ]);
    const baseUrl = options.baseUrl || RELEASE_BASE_URL;
    const download = options.requestBuffer || requestBuffer;
    const requestOptions = { allowHTTP: options.allowHTTP, timeoutMs: options.timeoutMs };
    const checksumURL = `${baseUrl}/${target.asset}.sha256`;
    const assetURL = `${baseUrl}/${target.asset}`;

    process.stderr.write(`memento plugin: downloading ${RELEASE_TAG} for ${target.os}/${target.arch}\n`);
    const checksumBody = await download(checksumURL, {
      ...requestOptions,
      maxBytes: MAX_CHECKSUM_BYTES,
    });
    const expected = parseChecksum(checksumBody.toString("utf8"), target.asset);
    const binary = await download(assetURL, {
      ...requestOptions,
      maxBytes: MAX_BINARY_BYTES,
    });
    const actual = crypto.createHash("sha256").update(binary).digest("hex");
    if (actual !== expected) {
      throw new Error(`checksum mismatch for ${target.asset}`);
    }
    if (binary.length === 0) {
      throw new Error(`release asset is empty: ${target.asset}`);
    }

    const temporary = `${installed}.tmp-${process.pid}-${crypto.randomBytes(6).toString("hex")}`;
    const temporaryChecksum = `${temporary}.sha256`;
    try {
      await fsp.writeFile(temporary, binary, { mode: 0o700, flag: "wx" });
      await fsp.writeFile(temporaryChecksum, `${expected}  ${target.asset}\n`, { mode: 0o600, flag: "wx" });
      if (target.os !== "windows") {
        await fsp.chmod(temporary, 0o700);
      }
      await fsp.rename(temporary, installed);
      await fsp.rename(temporaryChecksum, installedChecksum);
    } catch (error) {
      await Promise.all([
        fsp.rm(temporary, { force: true }),
        fsp.rm(temporaryChecksum, { force: true }),
      ]);
      throw error;
    }
    return installed;
  } finally {
    await lock.close().catch(() => {});
    await fsp.rm(lockFilename, { force: true });
  }
}

function startServerProcess(binary, options = {}) {
  const child = (options.spawnServer || spawn)(binary, [], {
    cwd: options.cwd || process.env.CLAUDE_PROJECT_DIR || process.cwd(),
    env: options.env || process.env,
    stdio: "inherit",
    windowsHide: true,
  });
  child.once("spawn", () => {
    Promise.resolve()
      .then(() => options.backgroundCheck && options.backgroundCheck())
      .catch((error) => (options.notice || ((message) => process.stderr.write(`${message}\n`)))(`memento plugin update check failed: ${error.message}`));
  });
  const forwarders = new Map();
  if (options.forwardSignals !== false) {
    for (const signal of ["SIGINT", "SIGTERM"]) {
      const forward = () => {
        if (!child.killed) child.kill(signal);
      };
      forwarders.set(signal, forward);
      process.on(signal, forward);
    }
  }
  return new Promise((resolve, reject) => {
    const cleanup = () => {
      for (const [signal, forward] of forwarders) process.off(signal, forward);
    };
    child.once("error", (error) => {
      cleanup();
      reject(error);
    });
    child.once("exit", (code, signal) => {
      cleanup();
      process.exitCode = code === null ? (signal ? 1 : 0) : code;
      resolve();
    });
  });
}

async function hasLegacyPluginCache(pluginData) {
  try {
    return (await fsp.stat(path.join(pluginData, "bin"))).isDirectory();
  } catch (error) {
    if (error && error.code === "ENOENT") return false;
    throw error;
  }
}

async function run(options = {}) {
  const env = options.env || process.env;
  const pluginData = options.pluginData || env.MEMENTO_PLUGIN_DATA;
  const override = options.override || env.MEMENTO_PLUGIN_BINARY;
  const pluginDataExisted = pluginData ? await hasLegacyPluginCache(pluginData) : false;
  const binary = await (options.resolveBinary || resolveBinary)({ ...options, pluginData, override });
  return startServerProcess(binary, {
    ...options,
    env,
    backgroundCheck: override || !pluginData ? undefined : async () => {
      await initializeUpdateState({ pluginData, home: options.home, pluginDataExisted });
      await runMarketplaceUpdateCheck({
        pluginData,
        now: options.now,
        commandRunner: options.commandRunner,
        deadlineMs: options.deadlineMs,
      });
    },
  });
}

if (require.main === module) {
  run().catch((error) => {
    process.stderr.write(`memento plugin launcher: ${error.message}\n`);
    process.exitCode = 1;
  });
}

module.exports = {
  RELEASE_TAG,
  RELEASE_VERSION,
  acquireInstallLock,
  hasLegacyPluginCache,
  initializeUpdateState,
  isVerified,
  marketplaceUpdateStatePath,
  parseChecksum,
  requestBuffer,
  resolveBinary,
  resolveTarget,
  run,
  runMarketplaceUpdateCheck,
  startServerProcess,
};
