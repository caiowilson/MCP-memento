# memento-mcp

[![Server Release](https://img.shields.io/github/v/tag/caiowilson/MCP-memento?filter=server%2Fv*&label=server)](https://github.com/caiowilson/MCP-memento/releases)
[![Latest Binary Tag](https://img.shields.io/badge/tag-server%2Flatest-blue)](https://github.com/caiowilson/MCP-memento/releases/tag/server%2Flatest)
[![VS Code Extension Release](https://img.shields.io/github/v/tag/caiowilson/MCP-memento?filter=extension%2Fv*&label=extension)](https://github.com/caiowilson/MCP-memento/releases)
[![Go Version](https://img.shields.io/badge/Go-1.25.5-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

A local-first MCP server that gives AI agents durable, high-signal memory for your repository: indexed code context, semantic relationships, fast search, and explicit notes that persist across sessions.

## Languages

- English: `README.md`
- Brazilian Portuguese: [`README.pt-BR.md`](./README.pt-BR.md)

## Documentation

- Project docs: [`docs/README.md`](./docs/README.md)
- Generic MCP clients: [`docs/clients.md`](./docs/clients.md)
- VS Code usage: [`docs/vscode.md`](./docs/vscode.md)
- VS Code extension: [`vscode-extension/README.md`](./vscode-extension/README.md)
- ADR guide: [`docs/adr/README.md`](./docs/adr/README.md)
- ADR index and decisions: [`docs/adr/ADRs.md`](./docs/adr/ADRs.md)

## What It Does

- Exposes MCP tools for repo operations: `repo_list_files`, `repo_read_file`, `repo_search`, `repo_related_files`, `repo_context`
- Maintains an on-disk code index per repository for fast, bounded context retrieval
- Stores explicit repo-scoped notes: `memory_upsert`, `memory_search`, `memory_clear`
- Supports a companion VS Code extension that installs and configures the server

## How It Works

1. The server starts over stdio JSON-RPC and registers MCP tools.
2. It builds and updates a local chunk index under `~/.memento-mcp/`.
3. Change detection is incremental:
   - Default (`auto`): filesystem watcher first, fallback to `git status` polling for git repos if watcher fails
   - Configurable via `MEMENTO_CHANGE_DETECTOR` (`auto` / `fs` / `git`)
4. Context tools combine:
   - Indexed chunks and scoring
   - Language-aware relationships (Go, TS/JS, PHP)
   - Hard byte and line limits for LLM context safety
5. Explicit notes are stored separately as durable, repo-scoped memory.

## Project Structure

- `cmd/server/` - entrypoint
- `internal/mcp/` - MCP server and tool handlers
- `internal/indexing/` - chunking, manifest, search, incremental indexing
- `internal/app/` - app lifecycle wiring
- `vscode-extension/` - companion extension (installer and MCP config UX)
- `docs/` - usage docs and ADRs

## Contributing

### Prerequisites

- Go `1.25.5`
- Node.js (only if working on `vscode-extension/`)

### Local Development

```bash
git clone https://github.com/caiowilson/MCP-memento.git
cd MCP-memento
make build
./bin/memento-mcp
```

### Generic Client Onboarding

```bash
./bin/memento-mcp print-config
./bin/memento-mcp print-guidance
```

### Run Tests

```bash
go test ./...
```

### VS Code Extension Development

```bash
cd vscode-extension
npm install
npm run build
```

### Contribution Flow

1. Create a branch from `main`.
2. Make focused changes with tests and docs updates.
3. Run `go test ./...` (and extension build/tests when applicable).
4. Open a PR with:
   - Problem statement
   - Approach
   - Validation steps
   - Any tool or behavior changes

## Roadmap Themes

- Better context quality and ranking
- Broader semantic language support
- Extension UX and install reliability
- Release automation and operational tooling
