# VS Code

This server is designed to be launched as an MCP stdio server with the working directory set to the repository root. It exposes tools for reading/searching the repo and storing repo-scoped notes.

## Option 1: VS Code extension (WIP)

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

## Build a local binary

From the repo root:

```bash
go build -o ./bin/memento-mcp ./cmd/server
```

## Run locally (binary)

From the repo root:

```bash
./bin/memento-mcp
```

## Configure in VS Code (client-agnostic)

Use the server's setup command to configure a detected VS Code-compatible client, or preview the change first:

```bash
./bin/memento-mcp setup --client=vscode
./bin/memento-mcp setup --client=vscode --print-only
```

For clients with a different configuration shape, use `./bin/memento-mcp print-config` and map its command, arguments, working directory, environment, and stdio transport. See the canonical [client configuration guide](./clients.md#recommended-client-config).

## Smoke test (raw stdio)

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

## What it provides

- Repo context tools (`repo_*`) for listing, reading, and searching files.
- `repo_related_files` to fetch “nearby” context for a file (same folder + Go/TS/JS/PHP import/semantic analysis).
- `repo_outline` to inspect symbols, signatures, documentation, and line ranges without function bodies.
- `repo_context` to fetch a single “context window” with intent-aware routing (uses automatic indexing in the background).
- `repo_diff_context` to fetch exact-file chunks and a bounded, redacted unified diff summary for auto-detected staged, unstaged, and untracked Git changes; a non-empty ordered `paths` list overrides detection, and results never expand to related files.
- `repo_switch_workspace` to retarget the server to another repository/workspace root at runtime (no process restart).
- Repo-scoped explicit memory (`memory_*`) persisted under `~/.memento-mcp/`.
- Optional code anchors that flag stale notes, preserve them for verification, and protect renamed or branch-specific referents from accidental eviction.
- MCP resources for active notes and bounded repository text files, discoverable through client `@`-mention interfaces.
- A `prime` MCP prompt for bounded session-start context, exposed by Claude Code as `/mcp__<server-name>__prime`.

## Switch workspace without restart

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
- Every actual workspace change triggers a full index refresh. Fresh child processes perform their startup index; cached worktree/repository children are explicitly reindexed when selected again.
- `reindexNow: true` blocks until the refresh completes. The default triggers it asynchronously.

## LLM usage recipe

Run `./bin/memento-mcp print-guidance` for the copyable source of truth. The canonical [LLM guidance and tool-call examples](./clients.md#recommended-llm-guidance) cover intent routing, outlines, changed-file review, anchored memory, native resources, and progressive follow-up calls without duplicating them here.

## Index lifecycle & VS Code behavior

The server maintains a background code index on disk, but clients can still control when a full reindex happens:

- On activation, call `tools/call` for `repo_index_status` to see whether the index is already warm for the current workspace.
- If the index is effectively empty and the workspace is small (for example, under ~10MB of source), you can eagerly call `repo_reindex` once to "warm up" the index for that VS Code window.
- For larger workspaces or when an index already exists, rely on the background indexer (Git polling or filesystem watcher) and use `repo_index_status` / `repo_index_debug` only for UI status or diagnostics.
- Expose a command such as **“Memento: Force Reindex”** that calls `repo_reindex` (optionally preceded by `repo_clear_index`) against the current workspace when the user wants a deterministic fresh snapshot.
- Explicit memory (`memory_*`) is independent of the code index: notes remain available even while the index is building or being rebuilt.

## Index tuning (optional)

The canonical [runtime configuration reference](./clients.md#runtime-configuration) lists every supported environment variable and default. Keep VS Code entries minimal unless a workspace needs a specific limit or opt-in feature.
