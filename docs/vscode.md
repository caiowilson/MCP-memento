## VS Code

This server is designed to be launched as an MCP stdio server with the working directory set to the repository root. It exposes tools for reading/searching the repo and storing repo-scoped notes.

### Option 1: VS Code extension (WIP)

This repo includes a companion VS Code extension under `vscode-extension/` that can:

- Download/install a `memento-mcp` binary into VS Code extension storage
- Generate an MCP config snippet for your `mcp.json`
- Configure MCP by writing/merging an entry into either a workspace `mcp.json` or a user/global config file
- Best-effort auto-call `repo_switch_workspace` when workspace folders change (configurable in extension settings)

See `vscode-extension/README.md`.

Defaults:

- GitHub repo: `caiowilson/MCP-memento`
- Release tag: `server/latest` (server releases are `server/vX.Y.Z`)
- Install behavior: tries latest release tags first; if `repo_switch_workspace` is still unavailable, the extension opens source-build instructions from README.

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
- `repo_context` to fetch a single “context window” with intent-aware routing (uses automatic indexing in the background).
- `repo_switch_workspace` to retarget the server to another repository/workspace root at runtime (no process restart).
- Repo-scoped explicit memory (`memory_*`) persisted under `~/.memento-mcp/`.

### Switch workspace without restart

Call `repo_switch_workspace` with a new root path:

```json
{
  "name": "repo_switch_workspace",
  "arguments": {
    "path": "/absolute/path/to/another/repo",
    "reindexNow": true
  }
}
```

- `path` can be absolute or relative to the current process working directory.
- `reindexNow: true` blocks until the first full index pass completes.

### LLM usage recipe

Prefer `repo_context` with `intent` for new callers. Keep explicit `mode` only for advanced overrides.

Navigate or explain:

```json
{
  "name": "repo_context",
  "arguments": {
    "path": "internal/mcp/context_tool.go",
    "intent": "navigate"
  }
}
```

Implement or edit:

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

Review or debug:

```json
{
  "name": "repo_context",
  "arguments": {
    "path": "internal/mcp/context_tool.go",
    "intent": "review"
  }
}
```

Advanced explicit mode override:

```json
{
  "name": "repo_context",
  "arguments": {
    "path": "internal/mcp/context_tool.go",
    "mode": "full",
    "excludePaths": ["internal/mcp/server.go"]
  }
}
```

Migration rule:

- Existing callers that already send `mode` are unchanged.
- New callers should prefer `intent`.
- If `suggestedNextCall` is returned, it is the preferred progressive follow-up.

### Index lifecycle & VS Code behavior

The server maintains a background code index on disk, but clients can still control when a full reindex happens:

- On activation, call `tools/call` for `repo_index_status` to see whether the index is already warm for the current workspace.
- If the index is effectively empty and the workspace is small (for example, under ~10MB of source), you can eagerly call `repo_reindex` once to "warm up" the index for that VS Code window.
- For larger workspaces or when an index already exists, rely on the background indexer (Git polling or filesystem watcher) and use `repo_index_status` / `repo_index_debug` only for UI status or diagnostics.
- Expose a command such as **“Memento: Force Reindex”** that calls `repo_reindex` (optionally preceded by `repo_clear_index`) against the current workspace when the user wants a deterministic fresh snapshot.
- Explicit memory (`memory_*`) is independent of the code index: notes remain available even while the index is building or being rebuilt.

### Index tuning (optional)

- `MEMENTO_INDEX_POLL_SECONDS` (default `10`)
- `MEMENTO_INDEX_MAX_TOTAL_BYTES` (default `20971520`)
- `MEMENTO_INDEX_MAX_FILE_BYTES` (default `1048576`)
- `MEMENTO_GIT_POLL_SECONDS` (default `2`)
- `MEMENTO_GIT_DEBOUNCE_MS` (default `500`)
- `MEMENTO_FS_DEBOUNCE_MS` (default `500`)
- `MEMENTO_MCP_DEV_LOG` (default `0`, set to `1` to log tool calls)
