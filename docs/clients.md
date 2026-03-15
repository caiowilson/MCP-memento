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

## Recommended LLM guidance

Use the output of `print-guidance` directly, or paste the following into client instructions:

```text
When using memento-mcp, start with repo_context and set intent to navigate, implement, or review.
Omit mode unless you need to force a low-level output such as full, outline, or summary.
If repo_context returns suggestedNextCall, prefer following it for a deeper read without repeating context.
Existing explicit mode calls still work, but new callers should prefer intent.
```

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
