# memento-mcp

> **Local-first MCP server** — gives AI agents a durable, searchable "external context window" for your codebase, persisted entirely on your machine.

[![Status: Experimental](https://img.shields.io/badge/status-experimental-orange.svg)](#)
[![Go version](https://img.shields.io/badge/go-1.25%2B-00ADD8?logo=go)](#prerequisites)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey)](#)
[![VS Code MCP](https://img.shields.io/badge/VS%20Code-MCP%20stdio-007ACC?logo=visualstudiocode)](#vs-code-integration)
[![License: TBD](https://img.shields.io/badge/license-TBD-red)](#license)

---

## TL;DR — Quick start

> **Requirements:** Go 1.25+ installed, a terminal, and VS Code with MCP support.

```bash
# 1. Clone and build
git clone https://github.com/caiowilson/MCP-memento.git
cd MCP-memento
go build -o ./bin/memento-mcp ./cmd/server

# 2. (Optional) install system-wide
sudo make install        # installs to /usr/local/bin (macOS/Linux)

# 3. Configure VS Code — add this to your .vscode/mcp.json
# {
#   "servers": {
#     "memento-mcp": {
#       "type": "stdio",
#       "command": "${workspaceFolder}/bin/memento-mcp",
#       "args": [],
#       "cwd": "${workspaceFolder}"
#     }
#   }
# }

# 4. Smoke-test in a terminal
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | ./bin/memento-mcp
```

That's it. Reload VS Code and your MCP client will discover the tools automatically.

---

## What is memento-mcp?

memento-mcp is a **stdio MCP server** (JSON-RPC 2.0) written in Go. It indexes your repository into bounded, line-based chunks stored locally under `~/.memento-mcp/` and exposes them through a set of structured tools that any MCP-compatible AI client can call.

### Core capabilities

| Capability | How it works |
|---|---|
| **Automatic indexing** | On startup the server builds an on-disk index of your repo. Chunks are bounded by both line count and byte size. |
| **Incremental updates — git repos** | Polls `git status --porcelain` and re-indexes only changed paths (debounced, default 500 ms). |
| **Incremental updates — non-git repos** | Falls back to a filesystem watcher; re-indexes touched paths (also debounced). |
| **Semantic related-file resolution** | For Go files uses `go/packages`; for TypeScript/JavaScript and PHP uses import-graph analysis. |
| **Explicit memory** | Stores and retrieves repo-scoped notes (key/value) alongside the auto-index. |
| **Context window assembly** | `repo_context` scores and ranks chunks (focus > semantic edges > imports > same-dir) and returns them within hard byte limits. |

---

## Prerequisites

| Requirement | Notes |
|---|---|
| **Go 1.25+** | See `go.mod` for the exact version. Install from [go.dev/dl](https://go.dev/dl). |
| **Git** (optional) | Required for the faster, git-status-based incremental indexing path. |
| **VS Code** (optional) | Required only for the VS Code integration. Needs an MCP-compatible extension or VS Code >= 1.99. |

---

## Installation

### Option A — Build from source (recommended)

```bash
git clone https://github.com/caiowilson/MCP-memento.git
cd MCP-memento
go build -o ./bin/memento-mcp ./cmd/server
```

The binary is placed at `./bin/memento-mcp`. Run it directly from there or proceed to Option B.

### Option B — Install system-wide via `make`

After building (Option A above), run:

```bash
# macOS / Linux — installs to $(brew --prefix)/bin or /usr/local/bin
sudo make install

# Uninstall
make uninstall
```

The `PREFIX` variable can be overridden:

```bash
sudo make install PREFIX=/opt/homebrew
```

### Option C — VS Code extension (WIP)

A companion VS Code extension under `vscode-extension/` can download a pre-built binary and generate an `mcp.json` snippet automatically. See [`vscode-extension/README.md`](vscode-extension/README.md) for instructions.

> Warning: The extension is a work-in-progress. Pre-built binaries are not yet published as GitHub Releases.

---

## Running the server

The server reads JSON-RPC requests from **stdin** and writes responses to **stdout** (standard MCP stdio transport). You do not run it directly in a terminal during normal usage — your MCP client (VS Code, etc.) launches it automatically.

To verify it works before wiring it into a client:

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"repo_index_debug","arguments":{}}}' \
  | ./bin/memento-mcp
```

A successful run prints a JSON response for each request, ending with the index debug info.

---

## VS Code integration

> Important: The server uses its **process working directory** as the workspace root. Always set `cwd` to `${workspaceFolder}`.

### 1. Add an entry to `.vscode/mcp.json`

```json
{
  "servers": {
    "memento-mcp": {
      "type": "stdio",
      "command": "${workspaceFolder}/bin/memento-mcp",
      "args": [],
      "cwd": "${workspaceFolder}",
      "env": {
        "MEMENTO_GIT_POLL_SECONDS": "2"
      }
    }
  }
}
```

If your MCP client uses different field names (e.g. `transport` instead of `type`), map them to the same concepts: **command**, **args**, **cwd**, **env**, **stdio transport**.

### 2. Bias Copilot Chat toward these tools (optional)

This repo includes `/.github/copilot-instructions.md`, which nudges GitHub Copilot Chat to prefer memento-mcp tools when answering codebase questions.

### 3. Detailed VS Code docs

See [`docs/vscode.md`](docs/vscode.md) for additional config options and a raw stdio smoke test.

---

## MCP tools reference

Tool names use **underscore style** — some clients reject dots.

### Repository tools

| Tool | Description |
|---|---|
| `repo_list_files` | List all indexed files under the workspace root |
| `repo_read_file` | Read a file's content (supports optional `start_line` / `end_line` bounds) |
| `repo_search` | Substring search across all indexed files |
| `repo_related_files` | Return files related to a given path (Go/TS/JS/PHP import- and semantic-aware) |
| `repo_context` | Return scored, bounded chunks for a file + its related files — the main "context window" tool |

### Index management tools

| Tool | Description |
|---|---|
| `repo_index_status` | Show background indexer status (`ready`, file counts, last run time) |
| `repo_index_debug` | Detailed debug info: filter rules, chunk counts, last error |
| `repo_reindex` | Trigger a full re-index of the workspace |
| `repo_clear_index` | Delete all indexed chunks and the manifest |

### Explicit memory tools

| Tool | Description |
|---|---|
| `memory_upsert` | Store or update a repo-scoped note (key/value) |
| `memory_search` | Search repo-scoped notes by keyword |
| `memory_clear` | Delete all repo-scoped notes |

---

## Indexing behavior

Index selection follows a **"code-first + high-signal files"** strategy. The index is stored per-repo under `~/.memento-mcp/`.

### Included by default

| Rule type | Patterns |
|---|---|
| **Extensions** | `.go` `.ts` `.tsx` `.js` `.jsx` `.php` `.md` `.json` `.yaml` `.yml` |
| **High-signal paths** | `go.mod`, `go.sum`, `README*`, `Makefile`, `Dockerfile`, `.github/workflows/*`, `Taskfile.yml` |

### Excluded by default

| Rule type | Patterns |
|---|---|
| **Secrets / keys** | `*.key`, `*.pem`, `*.p12`, `*.pfx`, `*.crt`, `*.der`, `*.ppk`, `id_rsa`, `id_ed25519` |
| **Binaries / databases** | `*.sqlite`, `*.db`, `*.bin`, `*.exe` |

> Note: `.env` files are indexed by default (local-first assumption). If you don't want that, add the pattern to the denylist in `internal/indexing/`.

---

## Configuration

All settings are controlled via environment variables. Pass them in the `env` block of your MCP client config, or export them in your shell before running the binary.

| Variable | Default | Description |
|---|---|---|
| `MEMENTO_INDEX_MAX_TOTAL_BYTES` | `20971520` (20 MB) | Maximum total size of all indexed chunks |
| `MEMENTO_INDEX_MAX_FILE_BYTES` | `1048576` (1 MB) | Maximum size of a single indexed file |
| `MEMENTO_INDEX_POLL_SECONDS` | `10` | Non-git fallback poll interval (seconds) |
| `MEMENTO_GIT_POLL_SECONDS` | `2` | Git-status poll interval (seconds) |
| `MEMENTO_GIT_DEBOUNCE_MS` | `500` | Debounce delay after git changes (ms) |
| `MEMENTO_FS_DEBOUNCE_MS` | `500` | Debounce delay after filesystem events (ms) |

---

## Development

### Run tests and vet

```bash
go test ./...
go vet ./...
```

### Build

```bash
make build          # outputs ./bin/memento-mcp
```

### Repository layout

```
cmd/server/          — executable entry point
internal/app/        — application lifecycle wiring
internal/mcp/        — MCP server (stdio JSON-RPC + tool registry)
internal/indexing/   — automatic code indexing (chunking, chunk store)
docs/                — project docs, VS Code guide, ADRs
vscode-extension/    — companion VS Code extension (WIP)
```

---

## Documentation

| Document | Description |
|---|---|
| [`docs/README.md`](docs/README.md) | Project overview and repo layout |
| [`docs/vscode.md`](docs/vscode.md) | VS Code integration guide |
| [`docs/adr/ADRs.md`](docs/adr/ADRs.md) | Architecture decision records |
| [`TODO.md`](TODO.md) | Roadmap and work-in-progress slices |
| [`vscode-extension/README.md`](vscode-extension/README.md) | VS Code extension development guide |

---

## Notes & limitations

- **Local-only storage.** The index and notes live under `~/.memento-mcp/`. Nothing is sent to any remote service. Use `repo_clear_index` or `memory_clear` to reset, or delete the directory manually.
- **Experimental status.** Tool names, config variables, and on-disk formats may change between versions without a migration path.
- **Client compatibility.** Some MCP clients enforce tool-name restrictions. This server uses underscore names everywhere and maps dot-style names to underscores at runtime for legacy clients.
- **No pre-built binaries yet.** You must build from source. Pre-built releases (used by the VS Code extension) are not yet published.

---

## License

No license file is included yet. Before making this repository public or sharing it with others, add a `LICENSE` file so that contributors and users know the terms under which they can use or contribute to the project.
