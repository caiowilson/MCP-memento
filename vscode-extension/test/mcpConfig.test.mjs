import test from "node:test";
import assert from "node:assert/strict";

import {
  buildConfigEntry,
  buildSnippetMarkdown,
  upsertIntoKnownSchema,
} from "../.test-dist/mcpConfig.mjs";

test("buildConfigEntry includes adaptive Git polling defaults", () => {
  const entry = buildConfigEntry("/tmp/memento-mcp");

  assert.equal(entry.env.MEMENTO_GIT_POLL_SECONDS, "2");
  assert.equal(entry.env.MEMENTO_GIT_MAX_POLL_SECONDS, "30");
  assert.equal(entry.env.MEMENTO_GIT_ERROR_MAX_POLL_SECONDS, "60");
});

test("buildSnippetMarkdown includes intent guidance and migration note", () => {
  const markdown = buildSnippetMarkdown("/tmp/memento-mcp");

  assert.match(markdown, /## Recommended LLM guidance/);
  assert.match(markdown, /repo_context` and set `intent` to `navigate`, `implement`, or `review`/);
  assert.match(markdown, /repo_outline` when you need signatures/);
  assert.match(markdown, /Anchor durable notes to code/);
  assert.match(markdown, /`prime` MCP prompt at session start/);
  assert.match(markdown, /Omit `mode` unless you need to force a low-level output shape/);
  assert.match(markdown, /New callers should prefer `repo_context` with `intent`/);
});

test("upsertIntoKnownSchema merges into an existing mcpServers map", () => {
  const original = {
    inputs: [{ id: "api-token", type: "promptString" }],
    mcpServers: {
      existing: { command: "/opt/existing-server" },
    },
  };
  const entry = buildConfigEntry("/new/memento-mcp");

  const updated = upsertIntoKnownSchema(original, entry);

  assert.deepEqual(updated, {
    inputs: original.inputs,
    mcpServers: {
      existing: original.mcpServers.existing,
      "memento-mcp": entry,
    },
  });
  assert.deepEqual(original.mcpServers, {
    existing: { command: "/opt/existing-server" },
  });
});

test("upsertIntoKnownSchema replaces only the named entry in an existing servers array", () => {
  const existing = { name: "existing", command: "/opt/existing-server" };
  const original = {
    version: 1,
    servers: [
      existing,
      { name: "memento-mcp", command: "/old/memento-mcp" },
    ],
  };
  const entry = buildConfigEntry("/new/memento-mcp");

  const updated = upsertIntoKnownSchema(original, entry);

  assert.deepEqual(updated, { version: 1, servers: [existing, entry] });
});
