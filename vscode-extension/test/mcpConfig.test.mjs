import test from "node:test";
import assert from "node:assert/strict";

import { buildSnippetMarkdown } from "../src/mcpConfig.js";

test("buildSnippetMarkdown includes intent guidance and migration note", () => {
  const markdown = buildSnippetMarkdown("/tmp/memento-mcp");

  assert.match(markdown, /## Recommended LLM guidance/);
  assert.match(markdown, /repo_context` and set `intent` to `navigate`, `implement`, or `review`/);
  assert.match(markdown, /Omit `mode` unless you need to force a low-level output shape/);
  assert.match(markdown, /New callers should prefer `repo_context` with `intent`/);
});
