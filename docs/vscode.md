## VS Code

This server is designed to be launched as an MCP stdio server with the working directory set to the repository root. It exposes tools for reading/searching the repo and storing repo-scoped notes.

### Option 1: VS Code extension (WIP)

This repo includes a companion VS Code extension under `vscode-extension/` that can:

- Download/install a `memento-mcp` binary into VS Code extension storage
- Generate an MCP config snippet for your `mcp.json`

See `vscode-extension/README.md`.

### Build a local binary

From the repo root:

```bash
go build -o ./bin/memento-mcp ./cmd/server
```

### Run locally (binary)

From the repo root:

```bash
./bin/memento-mcp
```

### Configure in VS Code (client-agnostic)

Use any VS Code extension that supports MCP stdio servers and configure it to run the binary with the workspace root as its CWD. A generic config looks like:

```json
{
  "name": "memento-mcp",
  "transport": "stdio",
  "command": "${workspaceFolder}/bin/memento-mcp",
  "args": [],
  "cwd": "${workspaceFolder}",
  "env": {
    "MEMENTO_INDEX_POLL_SECONDS": "10"
  }
}
```

If your MCP client uses different field names, map them to the same concepts: **command**, **args**, **cwd**, **env**, and **stdio transport**.

### Smoke test (raw stdio)

You can verify the server responds to MCP JSON-RPC over stdio:

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | ./bin/memento-mcp
```

To call a tool, use `tools/call`:

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"repo_index_debug","arguments":{}}}' | ./bin/memento-mcp
```

### What it provides

- Repo context tools (`repo_*`) for listing, reading, and searching files.
- `repo_related_files` to fetch “nearby” context for a file (same folder + Go/TS/JS/PHP import/semantic analysis).
- `repo_context` to fetch a single “context window” (uses automatic indexing in the background).
- Repo-scoped explicit memory (`memory_*`) persisted under `~/.memento-mcp/`.

### Index tuning (optional)

- `MEMENTO_INDEX_POLL_SECONDS` (default `10`)
- `MEMENTO_INDEX_MAX_TOTAL_BYTES` (default `20971520`)
- `MEMENTO_INDEX_MAX_FILE_BYTES` (default `1048576`)
- `MEMENTO_GIT_POLL_SECONDS` (default `2`)
- `MEMENTO_GIT_DEBOUNCE_MS` (default `500`)
- `MEMENTO_FS_DEBOUNCE_MS` (default `500`)
