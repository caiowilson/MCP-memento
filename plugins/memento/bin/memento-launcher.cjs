#!/usr/bin/env node
"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const fsp = require("node:fs/promises");
const http = require("node:http");
const https = require("node:https");
const path = require("node:path");
const { spawn } = require("node:child_process");

const RELEASE_VERSION = "0.9.0";
const RELEASE_TAG = `server/v${RELEASE_VERSION}`;
const RELEASE_BASE_URL = `https://github.com/caiowilson/MCP-memento/releases/download/${encodeURIComponent(RELEASE_TAG)}`;
const MAX_BINARY_BYTES = 128 * 1024 * 1024;
const MAX_CHECKSUM_BYTES = 4096;
const MAX_REDIRECTS = 5;
const DOWNLOAD_TIMEOUT_MS = 30_000;
const LOCK_WAIT_MS = 75_000;
const LOCK_RETRY_MS = 200;

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

async function run() {
  const binary = await resolveBinary();
  const child = spawn(binary, [], {
    cwd: process.env.CLAUDE_PROJECT_DIR || process.cwd(),
    env: process.env,
    stdio: "inherit",
    windowsHide: true,
  });

  for (const signal of ["SIGINT", "SIGTERM"]) {
    process.on(signal, () => {
      if (!child.killed) child.kill(signal);
    });
  }

  await new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => {
      process.exitCode = code === null ? (signal ? 1 : 0) : code;
      resolve();
    });
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
  isVerified,
  parseChecksum,
  requestBuffer,
  resolveBinary,
  resolveTarget,
  run,
};
