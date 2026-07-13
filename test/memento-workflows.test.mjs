import assert from "node:assert/strict";
import { constants as fsConstants } from "node:fs";
import { access, lstat, readFile, readdir } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const pluginRoot = path.join(root, "plugins", "memento-workflows");
const expectedSkills = ["handoff", "prime", "review-changes"];

async function walk(directory, prefix = "") {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const relative = path.posix.join(prefix, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await walk(path.join(directory, entry.name), relative)));
    } else {
      files.push(relative);
    }
  }
  return files.sort();
}

function frontmatter(markdown) {
  const match = markdown.match(/^---\n([\s\S]*?)\n---\n/);
  assert.ok(match, "skill must start with YAML frontmatter");
  return Object.fromEntries(
    match[1].split("\n").map((line) => {
      const separator = line.indexOf(":");
      assert.notEqual(separator, -1, `invalid frontmatter line: ${line}`);
      return [line.slice(0, separator).trim(), line.slice(separator + 1).trim()];
    }),
  );
}

test("marketplace exposes separate native and credential-free plugins", async () => {
  const marketplace = JSON.parse(await readFile(path.join(root, ".claude-plugin", "marketplace.json"), "utf8"));
  const names = marketplace.plugins.map((plugin) => plugin.name);
  assert.deepEqual(names, ["memento", "memento-workflows"]);
  assert.equal(new Set(names).size, names.length);

  const manifest = JSON.parse(await readFile(path.join(pluginRoot, ".claude-plugin", "plugin.json"), "utf8"));
  const entry = marketplace.plugins.find((plugin) => plugin.name === manifest.name);
  assert.equal(entry.source, "./plugins/memento-workflows");
  assert.equal(entry.version, manifest.version);
  assert.equal(manifest.mcpServers, undefined);
});

test("workflow plugin contains only portable, non-executable content", async () => {
  const files = await walk(pluginRoot);
  assert.deepEqual(
    files.filter((file) => file.endsWith("/SKILL.md")),
    expectedSkills.map((name) => `skills/${name}/SKILL.md`),
  );

  const forbiddenPath = /(^|\/)(?:\.mcp\.json|hooks|agents|commands|bin|scripts|node_modules)(?:\/|$)|\.(?:exe|dll|dylib|so)$/i;
  for (const file of files) {
    assert.doesNotMatch(file, forbiddenPath);
    const metadata = await lstat(path.join(pluginRoot, file));
    assert.ok(metadata.isFile(), `${file} must be a regular file`);
    assert.equal(metadata.mode & 0o111, 0, `${file} must not be executable`);
  }

  await assert.rejects(access(path.join(pluginRoot, ".mcp.json"), fsConstants.F_OK));
});

test("skills keep narrow metadata and credential-free tool boundaries", async () => {
  for (const name of expectedSkills) {
    const file = path.join(pluginRoot, "skills", name, "SKILL.md");
    const markdown = await readFile(file, "utf8");
    const metadata = frontmatter(markdown);
    assert.equal(metadata.name, name);
    assert.ok(metadata.description.length > 20 && metadata.description.length < 500);
    assert.match(metadata.description, /Use when/);
    assert.ok(metadata["allowed-tools"]);
    assert.doesNotMatch(metadata["allowed-tools"], /\b(?:Write|Edit|WebFetch|WebSearch)\b/);
    assert.doesNotMatch(metadata["allowed-tools"], /Bash\(git (?:add|apply|checkout|clean|commit|merge|mv|pull|push|rebase|reset|restore|rm|switch|tag)\b/);
    assert.ok(markdown.split("\n").length < 500);
    assert.doesNotMatch(markdown, /\b(?:curl|wget|npm install|npx|pip install|brew install|gh release download)\b/i);
  }
});

test("workflow contracts stay bounded, read-only, and explicit", async () => {
  const prime = await readFile(path.join(pluginRoot, "skills", "prime", "SKILL.md"), "utf8");
  assert.match(prime, /Never write files/);
  assert.match(prime, /up to four root instruction files/);
  assert.match(prime, /up to six root manifests/);
  assert.match(prime, /about 200 lines/);

  const review = await readFile(path.join(pluginRoot, "skills", "review-changes", "SKILL.md"), "utf8");
  assert.match(review, /Perform a read-only review/);
  assert.match(review, /staged and unstaged/);
  assert.match(review, /untracked files directly/);
  assert.match(review, /Discover nearby tests/);

  const handoff = await readFile(path.join(pluginRoot, "skills", "handoff", "SKILL.md"), "utf8");
  assert.match(handoff, /MEMENTO_HANDOFF\.md/);
  assert.match(handoff, /Ask the user to confirm that exact write/);
  assert.match(handoff, /Do not invoke Write, Edit, Bash redirection/);
  assert.match(handoff, /abort if the target is a symbolic link/);
  assert.match(handoff, /repeat the link and regular-file checks immediately before writing/);
  assert.match(handoff, /Otherwise write only the approved Markdown/);
});
