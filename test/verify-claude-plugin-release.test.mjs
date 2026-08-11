import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const verifier = join(root, "scripts", "verify-claude-plugin-release.mjs");

async function writeReleaseFixture(root, versions = {}) {
  const version = "1.2.3";
  await mkdir(join(root, ".claude-plugin"), { recursive: true });
  await mkdir(join(root, "plugins", "memento", ".claude-plugin"), { recursive: true });
  await mkdir(join(root, "plugins", "memento", "bin"), { recursive: true });
  await writeFile(join(root, ".claude-plugin", "marketplace.json"), JSON.stringify({
    plugins: [{ name: "memento", version: versions.marketplace || version }],
  }));
  await writeFile(join(root, "plugins", "memento", ".claude-plugin", "plugin.json"), JSON.stringify({
    name: "memento",
    version: versions.plugin || version,
  }));
  await writeFile(join(root, "plugins", "memento", "bin", "memento-launcher.cjs"),
    `const RELEASE_VERSION = "${versions.launcher || version}";\n`);
}

function verifyFixture(fixture, refName = "server/v1.2.3") {
  return spawnSync(process.execPath, [verifier, "--root", fixture, "--ref", refName], { encoding: "utf8" });
}

test("release contract accepts an aligned native Claude plugin", async (t) => {
  const fixture = await mkdtemp(join(tmpdir(), "memento-plugin-release-contract-"));
  t.after(() => rm(fixture, { recursive: true, force: true }));
  await writeReleaseFixture(fixture);

  const result = verifyFixture(fixture);

  assert.equal(result.status, 0, result.stderr);
});

for (const [source, versions, expectedPath] of [
  ["marketplace", { marketplace: "1.2.2" }, ".claude-plugin/marketplace.json"],
  ["plugin", { plugin: "1.2.2" }, "plugins/memento/.claude-plugin/plugin.json"],
  ["launcher", { launcher: "1.2.2" }, "plugins/memento/bin/memento-launcher.cjs"],
]) {
  test(`release contract rejects stale ${source} version`, async (t) => {
    const fixture = await mkdtemp(join(tmpdir(), "memento-plugin-release-contract-"));
    t.after(() => rm(fixture, { recursive: true, force: true }));
    await writeReleaseFixture(fixture, versions);

    const result = verifyFixture(fixture);

    assert.equal(result.status, 1);
    assert.match(result.stderr, new RegExp(`${expectedPath.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}.*expected 1\\.2\\.3`, "s"));
  });
}

test("release contract rejects a non-server semantic tag", async (t) => {
  const fixture = await mkdtemp(join(tmpdir(), "memento-plugin-release-contract-"));
  t.after(() => rm(fixture, { recursive: true, force: true }));
  await writeReleaseFixture(fixture);

  const result = verifyFixture(fixture, "server/latest");

  assert.equal(result.status, 1);
  assert.match(result.stderr, /expected server\/vX\.Y\.Z tag/);
});
