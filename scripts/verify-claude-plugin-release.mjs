import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const require = createRequire(import.meta.url);

function expectedVersion(refName) {
  const match = /^server\/v(\d+\.\d+\.\d+)$/.exec(refName || "");
  if (!match) throw new Error(`expected server/vX.Y.Z tag, got ${refName || "(empty)"}`);
  return match[1];
}

async function readJSON(root, relativePath) {
  return JSON.parse(await readFile(resolve(root, relativePath), "utf8"));
}

export async function validateClaudePluginRelease({ root, refName }) {
  const expected = expectedVersion(refName);
  const marketplacePath = ".claude-plugin/marketplace.json";
  const claudePluginPath = "plugins/memento/.claude-plugin/plugin.json";
  const codexPluginPath = "plugins/memento/.codex-plugin/plugin.json";
  const launcherPath = "plugins/memento/bin/memento-launcher.cjs";
  const [marketplace, claudePlugin, codexPlugin] = await Promise.all([
    readJSON(root, marketplacePath),
    readJSON(root, claudePluginPath),
    readJSON(root, codexPluginPath),
  ]);
  const launcher = require(resolve(root, launcherPath));
  const entry = marketplace.plugins?.find((candidate) => candidate.name === "memento");
  const mismatches = [
    [marketplacePath, entry?.version],
    [claudePluginPath, claudePlugin.version],
    [codexPluginPath, codexPlugin.version],
    [launcherPath, launcher.RELEASE_VERSION],
  ].filter(([, actual]) => actual !== expected);

  if (mismatches.length > 0) {
    throw new Error(mismatches.map(([path, actual]) => `${path}: found ${actual || "missing"}; expected ${expected}`).join("\n"));
  }
}

function option(name) {
  const index = process.argv.indexOf(name);
  return index === -1 ? undefined : process.argv[index + 1];
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  validateClaudePluginRelease({
    root: option("--root") || process.cwd(),
    refName: option("--ref") || process.env.GITHUB_REF_NAME,
  }).catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}
