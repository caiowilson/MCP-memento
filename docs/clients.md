# Generic MCP Clients

This page covers non-VS Code usage for `memento-mcp`.

## Quick start

Build the binary from the repo root:

```bash
go build -o ./bin/memento-mcp ./cmd/server
```

Print a generic MCP config snippet:

```bash
./bin/memento-mcp print-config
```

Print copyable LLM guidance:

```bash
./bin/memento-mcp print-guidance
```

Show built-in help:

```bash
./bin/memento-mcp help
```

## Recommended client config

`print-config` emits a generic `mcpServers` map that uses:

- stdio transport
- the current binary path as `command`
- `${workspaceFolder}` as `cwd`
- the default indexing environment variables

If your MCP client expects a different JSON shape, reuse the same values for `command`, `args`, `cwd`, `env`, and stdio transport.

## Workspace root detection

The server resolves its workspace root in this order:

1. Explicit `--root DIR` or client config
2. `CLAUDE_PROJECT_DIR` from Claude Code
3. MCP client `roots/list`, when the client advertises Roots support
4. Current working directory

Claude Code sessions therefore index the launched project without a manual `repo_switch_workspace` call. Use `repo_switch_workspace` only when you intentionally retarget the same MCP session to another repository.

## Recommended LLM guidance

Use the output of `print-guidance` directly, or paste the following into client instructions:

```text
When using memento-mcp, start with repo_context and set intent to navigate, implement, or review.
Omit mode unless you need to force a low-level output such as full, outline, or summary.
If repo_context returns suggestedNextCall, prefer following it for a deeper read without repeating context.
When you change repositories in the same MCP session, call repo_switch_workspace with the new root path instead of restarting.
Existing explicit mode calls still work, but new callers should prefer intent.
```

## Claude Code output limits

`repo_context`, `repo_read_file`, and `repo_search` default to compact responses to avoid Claude Code's MCP result warning near 10k tokens:

- `repo_context`: `maxTotalBytes` defaults to `32000`.
- `repo_read_file`: `maxBytes` defaults to `32000`.
- `repo_search`: each snippet is capped by `maxSnippetBytes`, default `500`.

The same three tools advertise `_meta["anthropic/maxResultSizeChars"] = 500000` for intentional large reads. Claude Code's own `MAX_MCP_OUTPUT_TOKENS` setting can still be lower than the server-side caps, so tune the tool arguments and the client setting together when you need larger context.

## Example tool calls

Navigate:

```json
{
  "name": "repo_context",
  "arguments": {
    "path": "internal/mcp/context_tool.go",
    "intent": "navigate"
  }
}
```

Implement:

```json
{
  "name": "repo_context",
  "arguments": {
    "path": "internal/mcp/context_tool.go",
    "intent": "implement",
    "focus": "repoContextOutputSchema"
  }
}
```

Review:

```json
{
  "name": "repo_context",
  "arguments": {
    "path": "internal/mcp/context_tool.go",
    "intent": "review"
  }
}
```

Switch workspace without restart:

```json
{
  "name": "repo_switch_workspace",
  "arguments": {
    "path": "/absolute/path/to/another/repo",
    "reindexNow": true
  }
}
```
