# MCP Tooling Policy (VS Code)

Use MCP tools to ground answers in the repo:

- For any code question, call `repo_context` for the active file first.
- If more context is needed, call `repo_related_files`, then `repo_read_file` or `repo_search`.
- If unsure, say so and call a tool rather than guessing.
- If results look stale or empty, call `repo_index_status` or `repo_index_debug`, then `repo_reindex`.

Tool names use underscore style (e.g., `repo_context`, `repo_read_file`, `memory_search`).

## Project architecture (big picture)

- Entrypoint is [cmd/server/server.go](cmd/server/server.go#L1): `app.Run()` wires up the MCP server.
- The MCP server lives in [internal/mcp/server.go](internal/mcp/server.go#L1): JSON-RPC over stdio, tool registry, and dot-to-underscore name mapping for clients.
- Indexing is a background worker in [internal/indexing/indexer.go](internal/indexing/indexer.go#L1) with on-disk chunk storage under `~/.memento-mcp/`.
- File chunks are line-based and bounded by bytes/lines in [internal/indexing/chunk.go](internal/indexing/chunk.go#L1).
- Related-files resolution is in [internal/mcp/related_tools.go](internal/mcp/related_tools.go#L1): Go uses `go/packages` semantic refs; JS/TS and PHP use import/include graphs.
- Context windows are assembled in [internal/mcp/context_tool.go](internal/mcp/context_tool.go#L1) (chunk scoring + limits).

## Critical workflows

- Build server: `go build -o ./bin/memento-mcp ./cmd/server` (see [README.md](README.md#L20)).
- Run server: `./bin/memento-mcp` (stdio JSON-RPC).
- Tests & vet: `go test ./...` and `go vet ./...` (see [README.md](README.md#L73)).
- VS Code extension (WIP): in [vscode-extension/README.md](vscode-extension/README.md#L1) use `npm install` + `npm run build`.

## Project-specific conventions

- The server’s workspace root is its process working directory; VS Code must set MCP `cwd` to `${workspaceFolder}` (see [README.md](README.md#L41)).
- Indexing differs for git vs non-git repos: git uses `git status` polling + debounce; non-git uses fs watcher (see [internal/mcp/server.go](internal/mcp/server.go#L60)).
- Tool names are underscore-only; dot names are mapped to underscores at runtime (see [internal/mcp/server.go](internal/mcp/server.go#L141)).

## Tool notes

- `repo_index_status` confirms background indexing state; if `ready` is false, check `repo_index_debug` and then `repo_reindex`.
