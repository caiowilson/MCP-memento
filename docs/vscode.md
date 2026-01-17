## VS Code

This server is designed to be launched as an MCP stdio server with the working directory set to the repository root. It exposes tools for reading/searching the repo and storing repo-scoped notes.

### Run locally

From the repo root:

```bash
go run ./cmd/server
```

### Configure in VS Code

Use any VS Code extension that supports MCP stdio servers and configure it to run:

- Command: `go`
- Args: `run`, `./cmd/server`
- CWD: your workspace root

### What it provides

- Repo context tools (`repo.*`) for listing, reading, and searching files.
- `repo.related_files` to fetch “nearby” context for a file (same folder + Go/TS/JS/PHP import/semantic analysis).
- `repo.context` to fetch a single “context window” (uses automatic indexing in the background).
- Repo-scoped explicit memory (`memory.*`) persisted under `~/.memento-mcp/`.

### Index tuning (optional)

- `MEMENTO_INDEX_POLL_SECONDS` (default `10`)
- `MEMENTO_INDEX_MAX_TOTAL_BYTES` (default `20971520`)
- `MEMENTO_INDEX_MAX_FILE_BYTES` (default `1048576`)
