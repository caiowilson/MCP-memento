import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const require = createRequire(import.meta.url);

async function readJSON(...parts) {
  return JSON.parse(await readFile(join(root, ...parts), "utf8"));
}

test("Codex marketplace exposes the shared Memento plugin source", async () => {
  const marketplace = await readJSON(".agents", "plugins", "marketplace.json");
  const entry = marketplace.plugins.find((candidate) => candidate.name === "memento");

  assert.equal(marketplace.name, "memento-mcp");
  assert.equal(entry.source.source, "local");
  assert.equal(entry.source.path, "./plugins/memento");
  assert.deepEqual(entry.policy, {
    installation: "AVAILABLE",
    authentication: "ON_INSTALL",
  });
  assert.equal(entry.category, "Productivity");
});

test("Codex manifest declares a local stdio launcher with Codex plugin paths", async () => {
  const manifest = await readJSON("plugins", "memento", ".codex-plugin", "plugin.json");
  const server = manifest.mcpServers.memento;

  assert.equal(manifest.name, "memento");
  assert.match(manifest.version, /^\d+\.\d+\.\d+$/);
  assert.equal(server.command, "node");
  assert.deepEqual(server.args, ["${PLUGIN_ROOT}/bin/memento-launcher.cjs"]);
  assert.deepEqual(server.env, {
    MEMENTO_PLUGIN_DATA: "${PLUGIN_DATA}",
    MEMENTO_PLUGIN_PROVIDER: "codex",
  });
});

test("Claude and Codex manifests keep the shared launcher release aligned", async () => {
  const claude = await readJSON("plugins", "memento", ".claude-plugin", "plugin.json");
  const codex = await readJSON("plugins", "memento", ".codex-plugin", "plugin.json");
  const launcher = require(join(root, "plugins", "memento", "bin", "memento-launcher.cjs"));

  assert.equal(codex.name, claude.name);
  assert.equal(codex.version, claude.version);
  assert.equal(codex.version, launcher.RELEASE_VERSION);
});

test("Claude keeps its provider-specific MCP declaration", async () => {
  const mcp = await readJSON("plugins", "memento", ".mcp.json");
  const server = mcp.mcpServers.memento;

  assert.deepEqual(server.args, ["${CLAUDE_PLUGIN_ROOT}/bin/memento-launcher.cjs"]);
  assert.equal(server.env.MEMENTO_PLUGIN_DATA, "${CLAUDE_PLUGIN_DATA}");
});

test("installed Codex discovers and registers the repository plugin", async (t) => {
  const manifest = await readJSON("plugins", "memento", ".codex-plugin", "plugin.json");
  const probe = spawnSync("codex", ["--version"], { encoding: "utf8" });
  if (probe.error?.code === "ENOENT") {
    assert.notEqual(
      process.env.MEMENTO_REQUIRE_CODEX_CLI,
      "1",
      "Codex CLI is required for this test but was not found on PATH",
    );
    t.skip("Codex CLI is not installed");
    return;
  }
  assert.equal(probe.status, 0, probe.stderr);

  const codexHome = await mkdtemp(join(tmpdir(), "memento-codex-plugin-test-"));
  t.after(() => rm(codexHome, { recursive: true, force: true }));
  const env = { ...process.env, CODEX_HOME: codexHome };
  const runCodex = (...args) => execFileSync("codex", args, { encoding: "utf8", env });

  runCodex("plugin", "marketplace", "add", root);
  const catalog = JSON.parse(runCodex("plugin", "list", "--json", "--available"));
  const available = catalog.available.find((entry) => entry.pluginId === "memento@memento-mcp");
  assert.equal(available.version, manifest.version);
  assert.equal(available.installPolicy, "AVAILABLE");

  runCodex("plugin", "add", "memento@memento-mcp");
  runCodex("plugin", "add", "memento@memento-mcp");
  const installed = JSON.parse(runCodex("mcp", "get", "memento", "--json"));
  assert.equal(installed.enabled, true);
  assert.equal(installed.transport.type, "stdio");
  assert.equal(installed.transport.command, "node");
  assert.deepEqual(installed.transport.args, ["${PLUGIN_ROOT}/bin/memento-launcher.cjs"]);
  assert.equal(installed.transport.env.MEMENTO_PLUGIN_DATA, "${PLUGIN_DATA}");
  assert.equal(installed.transport.env.MEMENTO_PLUGIN_PROVIDER, "codex");
});
