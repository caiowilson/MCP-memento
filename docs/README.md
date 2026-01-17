## Documentation

This repository is a work-in-progress Go implementation of an MCP server. The codebase is currently a scaffold (some packages are placeholders/stubs).

### Contents

- Getting started: `../README.md`
- VS Code usage: `vscode.md`
- Architecture decisions (ADRs): `adr/README.md`

### MCP tools (current)

- `repo.list_files` — list files under workspace root
- `repo.read_file` — read file content (optionally line-bounded)
- `repo.search` — substring search across files
- `repo.related_files` — related files for a given path (Go/TS/JS/PHP-aware)
- `repo.context` — indexed chunks for a file + related files
- `repo.index_status` — background indexer status
- `repo.reindex` — trigger full re-index
- `memory.upsert` — store/update repo-scoped notes
- `memory.search` — search repo-scoped notes

### Automatic indexing

On startup the server builds a best-effort, on-disk index of the repo under `~/.memento-mcp/` so tools like `repo.context` can return useful chunks quickly. For git repos it prefers `git status` to detect changes; otherwise it falls back to periodic scanning. See `docs/adr/ADRs.md`.

Default include/exclude rules (configurable in code):

- Include by extension: `.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.php`, `.md`, `.json`, `.yaml`, `.yml`
- Include by high-signal path: `go.mod`, `go.sum`, `README*`, `Makefile`, `Dockerfile`, `.github/workflows/*`, `Taskfile.yml`
- Exclude by pattern: `*.key`, `*.pem`, `*.p12`, `*.pfx`, `*.crt`, `*.der`, `*.ppk`, `id_rsa`, `id_ed25519`, `*.sqlite`, `*.db`, `*.bin`, `*.exe`

### Repository layout (current)

- `cmd/server/` — executable entrypoint
- `internal/app/` — app lifecycle wiring (WIP)
- `internal/mcp/` — MCP server implementation (stdio JSON-RPC + tools)
- `internal/indexing/` — automatic code indexing (chunk store)
