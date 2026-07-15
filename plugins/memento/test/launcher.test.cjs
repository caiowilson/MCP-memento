"use strict";

const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const fsp = require("node:fs/promises");
const http = require("node:http");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const {
  RELEASE_VERSION,
  isVerified,
  parseChecksum,
  resolveBinary,
  resolveTarget,
} = require("../bin/memento-launcher.cjs");

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
    const expected = path.join(pluginData, "bin", "server-v0.11.0", asset);
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
