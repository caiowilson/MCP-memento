"use strict";

const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const fsp = require("node:fs/promises");
const http = require("node:http");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");
const { EventEmitter } = require("node:events");

const {
  RELEASE_VERSION,
  hasLegacyPluginCache,
  initializeUpdateState,
  isVerified,
  marketplaceUpdateStatePath,
  parseChecksum,
  resolveBinary,
  resolveTarget,
  run,
  runMarketplaceUpdateCheck,
  startServerProcess,
} = require("../bin/memento-launcher.cjs");

async function writeSetupPreference(home, autoUpdate) {
  const filename = path.join(home, ".memento-mcp", "marketplace-update.json");
  await fsp.mkdir(path.dirname(filename), { recursive: true });
  await fsp.writeFile(filename, `${JSON.stringify({ autoUpdate })}\n`, { mode: 0o600 });
}

function recordingRunner(responses, calls) {
  return async (command, args, options) => {
    calls.push({ command, args, timeoutMs: options.timeoutMs });
    const response = responses.shift();
    if (response instanceof Error) throw response;
    return response || { stdout: "", stderr: "" };
  };
}

test("new marketplace installs persist an enabled update policy while legacy installs remain off", async (t) => {
  const root = await fsp.mkdtemp(path.join(os.tmpdir(), "memento-plugin-update-policy-"));
  t.after(() => fsp.rm(root, { recursive: true, force: true }));
  const newPluginData = path.join(root, "new-plugin-data");
  const legacyPluginData = path.join(root, "legacy-plugin-data");
  const home = path.join(root, "home");
  await fsp.mkdir(newPluginData);
  await fsp.mkdir(legacyPluginData);
  await fsp.mkdir(path.join(legacyPluginData, "bin"));

  const fresh = await initializeUpdateState({ pluginData: newPluginData, home, pluginDataExisted: await hasLegacyPluginCache(newPluginData) });
  const legacy = await initializeUpdateState({ pluginData: legacyPluginData, home, pluginDataExisted: await hasLegacyPluginCache(legacyPluginData) });

  assert.equal(fresh.effectivePolicy, true);
  assert.equal(fresh.policyProvenance, "new-marketplace-install");
  assert.equal(legacy.effectivePolicy, false);
  assert.equal(legacy.policyProvenance, "legacy-marketplace-install");
  assert.equal((await fsp.stat(marketplaceUpdateStatePath(newPluginData))).mode & 0o777, 0o600);

  await writeSetupPreference(home, true);
  const optedIn = await initializeUpdateState({ pluginData: legacyPluginData, home, pluginDataExisted: await hasLegacyPluginCache(legacyPluginData) });
  assert.equal(optedIn.effectivePolicy, true);
  assert.equal(optedIn.policyProvenance, "setup-preference");
});

test("only an existing server cache classifies plugin data as legacy", async (t) => {
  const root = await fsp.mkdtemp(path.join(os.tmpdir(), "memento-plugin-update-provenance-"));
  t.after(() => fsp.rm(root, { recursive: true, force: true }));
  const emptyPluginData = path.join(root, "empty-plugin-data");
  const cachedPluginData = path.join(root, "cached-plugin-data");
  await fsp.mkdir(emptyPluginData);
  await fsp.mkdir(path.join(cachedPluginData, "bin"), { recursive: true });

  assert.equal(await hasLegacyPluginCache(emptyPluginData), false);
  assert.equal(await hasLegacyPluginCache(cachedPluginData), true);
});

test("a pre-created empty plugin-data directory remains a new installation at runtime", async (t) => {
  const root = await fsp.mkdtemp(path.join(os.tmpdir(), "memento-plugin-update-runtime-provenance-"));
  t.after(() => fsp.rm(root, { recursive: true, force: true }));
  const pluginData = path.join(root, "plugin-data");
  await fsp.mkdir(pluginData);
  const child = new EventEmitter();
  child.killed = false;
  child.kill = () => { child.killed = true; };
  let commands = 0;
  let safetyExit;

  await run({
    env: { MEMENTO_PLUGIN_DATA: pluginData },
    home: path.join(root, "home"),
    resolveBinary: async () => "/verified/memento-mcp",
    spawnServer: () => {
      queueMicrotask(() => {
        child.emit("spawn");
        safetyExit = setTimeout(() => child.emit("exit", 0, null), 100);
      });
      return child;
    },
    commandRunner: async () => {
      commands += 1;
      if (commands === 2) {
        clearTimeout(safetyExit);
        queueMicrotask(() => child.emit("exit", 0, null));
      }
      return { stdout: "Updated memento@memento-mcp to version 1.0.4", stderr: "" };
    },
    forwardSignals: false,
  });

  assert.equal(commands, 2);
  const state = JSON.parse(await fsp.readFile(marketplaceUpdateStatePath(pluginData), "utf8"));
  assert.equal(state.policyProvenance, "new-marketplace-install");
});

test("Claude stages marketplace updates once per day after a persisted attempt", async (t) => {
  const root = await fsp.mkdtemp(path.join(os.tmpdir(), "memento-plugin-update-claude-"));
  t.after(() => fsp.rm(root, { recursive: true, force: true }));
  const pluginData = path.join(root, "plugin-data");
  await initializeUpdateState({ pluginData, home: path.join(root, "home"), pluginDataExisted: false });
  const calls = [];
  const now = new Date("2026-08-10T00:00:00.000Z");
  let persistedBeforeCommand = false;

  const first = await runMarketplaceUpdateCheck({
    pluginData,
    now: () => now,
    commandRunner: async (command, args, options) => {
      calls.push({ command, args, timeoutMs: options.timeoutMs });
      const state = JSON.parse(await fsp.readFile(marketplaceUpdateStatePath(pluginData), "utf8"));
      persistedBeforeCommand ||= state.lastCheckedAt === now.toISOString();
      return { stdout: "Updated memento@memento-mcp to version 1.0.4", stderr: "" };
    },
  });

  assert.equal(first.result, "staged");
  assert.equal(persistedBeforeCommand, true);
  assert.deepEqual(calls.map(({ command, args }) => [command, ...args]), [
    ["claude", "plugin", "marketplace", "update", "memento-mcp"],
    ["claude", "plugin", "update", "memento@memento-mcp"],
  ]);
  assert.ok(calls.every(({ timeoutMs }) => timeoutMs > 0 && timeoutMs <= 30_000));

  const second = await runMarketplaceUpdateCheck({
    pluginData,
    now: () => new Date("2026-08-10T23:59:59.000Z"),
    commandRunner: async () => { throw new Error("daily throttle did not hold"); },
  });
  assert.equal(second.result, "staged");
  assert.equal(calls.length, 2);
});

test("a live update lock and update failures leave the active server available", async (t) => {
  const root = await fsp.mkdtemp(path.join(os.tmpdir(), "memento-plugin-update-lock-"));
  t.after(() => fsp.rm(root, { recursive: true, force: true }));
  const pluginData = path.join(root, "plugin-data");
  await initializeUpdateState({ pluginData, home: path.join(root, "home"), pluginDataExisted: false });
  const lock = path.join(pluginData, "marketplace-update-state.lock");
  await fsp.writeFile(lock, JSON.stringify({ pid: process.pid, createdAt: new Date().toISOString() }), { mode: 0o600 });
  let called = false;
  await runMarketplaceUpdateCheck({
    pluginData,
    now: () => new Date("2026-08-10T00:00:00.000Z"),
    commandRunner: async () => { called = true; },
  });
  assert.equal(called, false);

  await fsp.rm(lock);
  const offline = Object.assign(new Error("offline"), { code: "ENETUNREACH" });
  const state = await runMarketplaceUpdateCheck({
    pluginData,
    now: () => new Date("2026-08-11T01:00:00.000Z"),
    commandRunner: async () => { throw offline; },
  });
  assert.equal(state.result, "offline");
  assert.equal(state.currentVersion, RELEASE_VERSION);
});

test("the verified server starts before the background update check", async () => {
  const events = [];
  const child = new EventEmitter();
  child.killed = false;
  child.kill = () => { child.killed = true; };
  const completed = startServerProcess("/verified/memento-mcp", {
    spawnServer(binary) {
      events.push(`spawn:${binary}`);
      queueMicrotask(() => child.emit("spawn"));
      return child;
    },
    backgroundCheck: async () => {
      events.push("check");
      throw new Error("offline");
    },
    notice: (message) => events.push(`notice:${message}`),
  });
  queueMicrotask(() => queueMicrotask(() => child.emit("exit", 0, null)));
  await completed;
  await new Promise((resolve) => setImmediate(resolve));

  assert.deepEqual(events.slice(0, 2), ["spawn:/verified/memento-mcp", "check"]);
  assert.match(events[2], /^notice:memento plugin update check failed: offline$/);
  assert.equal(child.killed, false);
});

test("plugin, marketplace, and launcher versions stay aligned", async () => {
  const pluginRoot = path.resolve(__dirname, "..");
  const plugin = JSON.parse(await fsp.readFile(path.join(pluginRoot, ".claude-plugin", "plugin.json"), "utf8"));
  const marketplace = JSON.parse(await fsp.readFile(path.join(pluginRoot, "..", "..", ".claude-plugin", "marketplace.json"), "utf8"));
  const entry = marketplace.plugins.find((candidate) => candidate.name === plugin.name);
  assert.equal(plugin.version, RELEASE_VERSION);
  assert.equal(entry.version, RELEASE_VERSION);
});

test("resolveTarget maps every published platform", () => {
  assert.deepEqual(resolveTarget("darwin", "x64"), {
    asset: "memento-mcp_darwin_x64",
    extension: "",
    os: "darwin",
    arch: "x64",
  });
  assert.equal(resolveTarget("linux", "arm64").asset, "memento-mcp_linux_arm64");
  assert.equal(resolveTarget("win32", "x64").asset, "memento-mcp_windows_x64.exe");
  assert.throws(() => resolveTarget("freebsd", "x64"), /unsupported platform/);
  assert.throws(() => resolveTarget("linux", "ia32"), /unsupported platform/);
});

test("parseChecksum validates the digest and optional filename", () => {
  const digest = "a".repeat(64);
  assert.equal(parseChecksum(`${digest}  memento-mcp_linux_x64\n`, "memento-mcp_linux_x64"), digest);
  assert.equal(parseChecksum(digest, "memento-mcp_linux_x64"), digest);
  assert.throws(() => parseChecksum("not-a-digest", "asset"), /invalid checksum/);
  assert.throws(() => parseChecksum(`${digest}  another`, "asset"), /expected asset/);
});

async function withReleaseServer(binary, checksum, callback) {
  const requests = [];
  const server = http.createServer((request, response) => {
    requests.push(request.url);
    if (request.url.endsWith(".sha256")) {
      response.writeHead(200, { "content-type": "text/plain" });
      response.end(checksum);
      return;
    }
    response.writeHead(200, { "content-type": "application/octet-stream" });
    response.end(binary);
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  try {
    const address = server.address();
    await callback(`http://127.0.0.1:${address.port}`, requests);
  } finally {
    await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
  }
}

test("resolveBinary downloads, verifies, and caches a release binary", async () => {
  const root = await fsp.mkdtemp(path.join(os.tmpdir(), "memento-plugin-test-"));
  const pluginData = path.join(root, "data");
  const pluginRoot = path.join(root, "plugin");
  const binary = Buffer.from("test memento executable");
  const asset = resolveTarget().asset;
  const digest = crypto.createHash("sha256").update(binary).digest("hex");

  try {
    await withReleaseServer(binary, `${digest}  ${asset}\n`, async (baseUrl, requests) => {
      const first = await resolveBinary({ pluginData, pluginRoot, baseUrl, allowHTTP: true });
      assert.deepEqual(await fsp.readFile(first), binary);
      assert.equal(await isVerified(first, `${first}.sha256`, asset), true);
      assert.equal(requests.length, 2);

      const second = await resolveBinary({ pluginData, pluginRoot, baseUrl, allowHTTP: true });
      assert.equal(second, first);
      assert.equal(requests.length, 2, "cached binary should not hit the release server");
    });
  } finally {
    await fsp.rm(root, { recursive: true, force: true });
  }
});

test("resolveBinary replaces a corrupted cached binary", async () => {
  const root = await fsp.mkdtemp(path.join(os.tmpdir(), "memento-plugin-test-"));
  const pluginData = path.join(root, "data");
  const pluginRoot = path.join(root, "plugin");
  const binary = Buffer.from("valid executable");
  const asset = resolveTarget().asset;
  const digest = crypto.createHash("sha256").update(binary).digest("hex");

  try {
    await withReleaseServer(binary, `${digest}  ${asset}\n`, async (baseUrl, requests) => {
      const installed = await resolveBinary({ pluginData, pluginRoot, baseUrl, allowHTTP: true });
      await fsp.writeFile(installed, "corrupted");
      const replaced = await resolveBinary({ pluginData, pluginRoot, baseUrl, allowHTTP: true });
      assert.equal(replaced, installed);
      assert.deepEqual(await fsp.readFile(replaced), binary);
      assert.equal(requests.length, 4);
    });
  } finally {
    await fsp.rm(root, { recursive: true, force: true });
  }
});

test("concurrent starts share one verified download", async () => {
  const root = await fsp.mkdtemp(path.join(os.tmpdir(), "memento-plugin-test-"));
  const pluginData = path.join(root, "data");
  const pluginRoot = path.join(root, "plugin");
  const binary = Buffer.from("concurrent executable");
  const asset = resolveTarget().asset;
  const digest = crypto.createHash("sha256").update(binary).digest("hex");

  try {
    await withReleaseServer(binary, `${digest}  ${asset}\n`, async (baseUrl, requests) => {
      const [first, second] = await Promise.all([
        resolveBinary({ pluginData, pluginRoot, baseUrl, allowHTTP: true }),
        resolveBinary({ pluginData, pluginRoot, baseUrl, allowHTTP: true }),
      ]);
      assert.equal(second, first);
      assert.deepEqual(await fsp.readFile(first), binary);
      assert.equal(requests.length, 2, "only the lock owner should download release files");
    });
  } finally {
    await fsp.rm(root, { recursive: true, force: true });
  }
});

test("resolveBinary rejects a mismatched checksum without caching", async () => {
  const root = await fsp.mkdtemp(path.join(os.tmpdir(), "memento-plugin-test-"));
  const pluginData = path.join(root, "data");
  const pluginRoot = path.join(root, "plugin");
  const binary = Buffer.from("tampered executable");
  const asset = resolveTarget().asset;

  try {
    await withReleaseServer(binary, `${"0".repeat(64)}  ${asset}\n`, async (baseUrl) => {
      await assert.rejects(
        resolveBinary({ pluginData, pluginRoot, baseUrl, allowHTTP: true }),
        /checksum mismatch/,
      );
    });
    const expected = path.join(pluginData, "bin", `server-v${RELEASE_VERSION}`, asset);
    await assert.rejects(fsp.stat(expected), { code: "ENOENT" });
  } finally {
    await fsp.rm(root, { recursive: true, force: true });
  }
});

test("resolveBinary supports an explicit local binary for development", async () => {
  const root = await fsp.mkdtemp(path.join(os.tmpdir(), "memento-plugin-test-"));
  const binary = path.join(root, "memento-mcp");
  try {
    await fsp.writeFile(binary, "local");
    assert.equal(await resolveBinary({ override: binary }), binary);
    await assert.rejects(resolveBinary({ override: path.join(root, "missing") }), /is not a file/);
  } finally {
    await fsp.rm(root, { recursive: true, force: true });
  }
});

test("resolveBinary rejects an unexpanded plugin data path", async () => {
  await assert.rejects(
    resolveBinary({ pluginData: "${CLAUDE_PLUGIN_DATA}", pluginRoot: "/tmp/plugin" }),
    /did not resolve to an absolute path/,
  );
});
