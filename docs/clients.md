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

## Claude Code

After building or installing the server, the fastest global setup is:

```bash
claude mcp add memento-mcp -- /absolute/path/to/memento-mcp
```

Then verify it from Claude Code:

```bash
claude mcp list
```

You can also use `/mcp` inside Claude Code to inspect connected MCP servers.

### Project-scoped `.mcp.json`

For a project-local setup, commit a `.mcp.json` file at the project root and point `command` at the server binary you want the project to use:

```json
{
  "mcpServers": {
    "memento-mcp": {
      "type": "stdio",
      "command": "${CLAUDE_PROJECT_DIR:-.}/bin/memento-mcp",
      "args": [],
      "env": {
        "MEMENTO_CHANGE_DETECTOR": "auto",
        "MEMENTO_FS_DEBOUNCE_MS": "500",
        "MEMENTO_GIT_DEBOUNCE_MS": "500",
        "MEMENTO_GIT_POLL_SECONDS": "2",
        "MEMENTO_INDEX_POLL_SECONDS": "10"
      }
    }
  }
}
```

Project-scoped MCP servers may prompt for approval the first time Claude Code sees them in a workspace. Keep the binary path stable so the approval and config remain useful across sessions.

### Claude Desktop

Claude Desktop uses the same stdio server values in `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "memento-mcp": {
      "command": "/absolute/path/to/memento-mcp",
      "args": [],
      "env": {
        "MEMENTO_CHANGE_DETECTOR": "auto",
        "MEMENTO_FS_DEBOUNCE_MS": "500",
        "MEMENTO_GIT_DEBOUNCE_MS": "500",
        "MEMENTO_GIT_POLL_SECONDS": "2",
        "MEMENTO_INDEX_POLL_SECONDS": "10"
      }
    }
  }
}
```

On macOS and WSL, Claude Code can import Claude Desktop MCP servers:

```bash
claude mcp add-from-claude-desktop
```

Use `./bin/memento-mcp print-config` as the source of truth for the current generic `mcpServers` shape and environment defaults.

## Recommended LLM guidance

Use the output of `print-guidance` directly, or paste the following into client instructions:

```text
When using memento-mcp, start with repo_context and set intent to navigate, implement, or review.
Omit mode unless you need to force a low-level output such as full, outline, or summary.
If repo_context returns suggestedNextCall, prefer following it for a deeper read without repeating context.
When you change repositories in the same MCP session, call repo_switch_workspace with the new root path instead of restarting.
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
