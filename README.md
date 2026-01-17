# memento-mcp

Local-first MCP (Model Context Protocol) server that gives agents a durable, tool-accessible “external context window” for a codebase.

Primary target: VS Code’s native MCP integration.

Status: experimental / WIP (tooling and behavior may evolve).

## What it does

- Runs as an MCP **stdio** server (JSON-RPC 2.0).
- Automatically indexes a repository into bounded, line-based chunks (persisted per repo under `~/.memento-mcp/`).
- Keeps the index fresh:
  - Git repos: polls `git status` and re-indexes only changed paths (debounced).
  - Non-git repos: filesystem watcher fallback (debounced).
- Exposes repo tools for listing/reading/searching files and fetching a single “context window” for the active file.
- Stores optional repo-scoped notes (“explicit memory”).

## Tools (current)

Tool names use underscore style because some clients reject dots.

- `repo_list_files` — list files under workspace root
- `repo_read_file` — read file content (optionally line-bounded)
- `repo_search` — substring search across files
- `repo_related_files` — related files for a given path (Go/TS/JS/PHP-aware)
- `repo_context` — indexed chunks for a file + related files (with explicit limits metadata)
- `repo_index_status` — background indexer status
- `repo_index_debug` — debug info (filters, counts, last error)
- `repo_reindex` — trigger full re-index
- `repo_clear_index` — delete all indexed chunks and manifest
- `memory_upsert` — store/update repo-scoped notes
- `memory_search` — search repo-scoped notes
- `memory_clear` — delete all repo-scoped notes

## Quickstart

### Prereqs

- Go (see `go.mod` for the required version)

### Build & run (local)

```bash
go build -o ./bin/memento-mcp ./cmd/server
./bin/memento-mcp
```

### Smoke test (raw stdio)

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"repo_index_debug","arguments":{}}}' | ./bin/memento-mcp
```

### Install to `/usr/local/bin` (optional)

```bash
sudo make install
```

Uninstall:

```bash
make uninstall
```

## VS Code

See `docs/vscode.md` for a client-agnostic config snippet and a raw stdio smoke test.

To bias Copilot Chat toward using these tools, this repo includes `/.github/copilot-instructions.md`.

Important: the server uses its process working directory as the workspace root, so VS Code should set the MCP server `cwd` to `${workspaceFolder}`.

## Indexing behavior (defaults)

Index selection is “code-first + high-signal files” and is stored locally per repo.

- Include by extension: `.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.php`, `.md`, `.json`, `.yaml`, `.yml`
- Include by high-signal path: `go.mod`, `go.sum`, `README*`, `Makefile`, `Dockerfile`, `.github/workflows/*`, `Taskfile.yml`
- Exclude by pattern: `*.key`, `*.pem`, `*.p12`, `*.pfx`, `*.crt`, `*.der`, `*.ppk`, `id_rsa`, `id_ed25519`, `*.sqlite`, `*.db`, `*.bin`, `*.exe`

Note: `.env` files are indexed by default (local-first assumption). If you don’t want that, add them to the denylist in code.

## Configuration (env vars)

- `MEMENTO_INDEX_MAX_TOTAL_BYTES` (default `20971520`)
- `MEMENTO_INDEX_MAX_FILE_BYTES` (default `1048576`)
- `MEMENTO_INDEX_POLL_SECONDS` (default `10`; ignored for git repos by default)
- `MEMENTO_GIT_POLL_SECONDS` (default `2`)
- `MEMENTO_GIT_DEBOUNCE_MS` (default `500`)
- `MEMENTO_FS_DEBOUNCE_MS` (default `500`)

## Development

```bash
go test ./...
go vet ./...
```

## Documentation

- Project docs: `docs/README.md`
- ADRs: `docs/adr/ADRs.md`
- To do / roadmap: `TODO.md`

## Notes & limitations

- The index is local and persisted under `~/.memento-mcp/`. Use `repo_clear_index` / `memory_clear` to reset, or delete the directory manually.
- Some clients enforce tool name restrictions; this server uses underscore tool names and also maps dot names to underscores when possible.

## License

No license is included yet. Add a `LICENSE` file before making the repository public if you want others to use/contribute under explicit terms.
