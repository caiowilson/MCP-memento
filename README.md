# memento-mcp

[![Server Release](https://img.shields.io/github/v/tag/caiowilson/MCP-memento?filter=server%2Fv*&label=server)](https://github.com/caiowilson/MCP-memento/releases)
[![Latest Binary Tag](https://img.shields.io/badge/tag-server%2Flatest-blue)](https://github.com/caiowilson/MCP-memento/releases/tag/server%2Flatest)
[![VS Code Extension Release](https://img.shields.io/github/v/tag/caiowilson/MCP-memento?filter=extension%2Fv*&label=extension)](https://github.com/caiowilson/MCP-memento/releases)
[![Go Version](https://img.shields.io/badge/Go-1.25.5-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

[![Support on Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/caiowilson)

A local-first MCP server that gives AI agents durable, high-signal memory for your repository: indexed code context, semantic relationships, fast search, and explicit notes that persist across sessions.

fun easy tl;dr version of the change logs: [`Nomit Memento`](https://nomit.dev/caiowilson/MCP-memento)

## Languages

- English: `README.md`
- Brazilian Portuguese: [`README.pt-BR.md`](./README.pt-BR.md)

## Documentation

- Project docs: [`docs/README.md`](./docs/README.md)
- Canonical client configuration and LLM guidance for Claude, ChatGPT/Codex, VS Code, and other MCP clients: [`docs/clients.md`](./docs/clients.md)
- VS Code usage: [`docs/vscode.md`](./docs/vscode.md)
- Opt-in local aggregate feedback and privacy controls: [`docs/feedback.md`](./docs/feedback.md)
- macOS release signing and notarization: [`docs/macos-signing.md`](./docs/macos-signing.md)
- VS Code extension: [`vscode-extension/README.md`](./vscode-extension/README.md)
- Credential-free Claude Code workflows: [`plugins/memento-workflows/README.md`](./plugins/memento-workflows/README.md)
- ADR guide: [`docs/adr/README.md`](./docs/adr/README.md)
- ADR index and decisions: [`docs/adr/ADRs.md`](./docs/adr/ADRs.md)

## Installation

### Claude Code plugin (recommended)

Add this repository as a Claude Code marketplace, install Memento, then reload active plugins:

```text
/plugin marketplace add caiowilson/MCP-memento
/plugin install memento@memento-mcp
/reload-plugins
/mcp
```

The enabled plugin starts Memento automatically for each Claude Code project. On first start it downloads the version-pinned prebuilt binary for x64 or arm64 macOS, Linux, or Windows, verifies the release SHA-256 checksum, and caches it in Claude Code's persistent plugin data directory. The first start requires GitHub access; later starts verify the cache and work offline.

### Claude Code workflows plugin (no native binary)

If you cannot or do not want to run the native MCP server, install the companion skills-only plugin instead:

```text
/plugin marketplace add caiowilson/MCP-memento
/plugin install memento-workflows@memento-mcp
/reload-plugins
```

It provides bounded repository orientation, local change review, and user-confirmed handoffs through Claude Code's built-in tools. It does not provide indexed retrieval, semantic vectors, MCP resources or prompts, background indexing, cross-client support, or structured durable memory. See the [capability matrix](./plugins/memento-workflows/README.md#capability-matrix).

### Standalone binary

Install the latest prebuilt server to `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/caiowilson/MCP-memento/main/install.sh | sh
```

Set `MEMENTO_INSTALL_DIR` to choose another directory. The installer supports x64 and arm64 macOS, Linux, and Windows environments with a POSIX shell. Ensure the selected directory is on `PATH`, then verify the binary:

```bash
memento-mcp help
```

Update a standalone installation in place:

```bash
memento-mcp update
```

Use `memento-mcp update --check` to check without installing. Release builds also perform a silent, throttled check at server startup and write only an update-available notice to stderr; they never write update messages to MCP stdout. Set `MEMENTO_UPDATE_CHECK=false` to disable that check. Claude Code plugin installations remain version-pinned and must be updated through `/plugin` commands.

### Build from source

Building requires Go 1.25.5 or newer:

```bash
git clone https://github.com/caiowilson/MCP-memento.git
cd MCP-memento
make build
./bin/memento-mcp help
```

## Configure an MCP client

The canonical setup and LLM-usage guide is [`docs/clients.md`](./docs/clients.md). It covers Claude Code and Desktop, ChatGPT/Codex, VS Code, generic stdio clients, workspace detection, runtime settings, semantic retrieval, output limits, native resources, and the current tool-call guidance.

For a standalone build, let Memento detect and configure installed clients, or preview the writes first:

```bash
memento-mcp setup
memento-mcp setup --print-only
```

Use `memento-mcp print-config` for a generic JSON entry and `memento-mcp print-guidance` for the canonical agent instructions. Claude Code plugin users need no separate MCP configuration; verify the automatically started server with `/mcp`.

For manual Claude Code or Codex setup, the shortest local commands are:

```bash
claude mcp add memento -- /absolute/path/to/memento-mcp
codex mcp add memento -- /absolute/path/to/memento-mcp
```

Run `claude mcp list` or `codex mcp list` to verify the registration. Memento then uses `CLAUDE_PROJECT_DIR` or the client's MCP workspace root automatically; the canonical guide covers shared configuration and troubleshooting.

## What It Does

- Exposes MCP tools for repository context, structure, search, diffs, and routing, including `repo_context`, `repo_outline`, `repo_search`, `repo_related_files`, `repo_diff_context`, and `repo_switch_workspace`
- Maintains an on-disk code index per repository for fast, bounded context retrieval
- Stores lifecycle-aware repo-scoped notes with `memory_upsert`, `memory_search`, `memory_list`, `memory_verify`, and explicit maintenance tools
- Can record strictly aggregate, local-only helpfulness feedback after explicit opt-in
- Supports a companion VS Code extension that installs and configures the server

## How It Works

1. The server starts over stdio JSON-RPC and registers MCP tools.
2. It auto-detects the workspace root (`--root`, `CLAUDE_PROJECT_DIR`, MCP `roots/list`, then cwd) and builds a local chunk index under `~/.memento-mcp/`.
3. Change detection is incremental:
   - Default (`auto`): filesystem watcher first, fallback to `git status` polling for git repos if watcher fails
   - Configurable via `MEMENTO_CHANGE_DETECTOR` (`auto` / `fs` / `git`)
4. Context tools combine:
   - Indexed chunks and scoring
   - Declaration-aligned Go, JavaScript/JSX, TypeScript/TSX, Python, and Rust chunk boundaries from a pinned pure-Go tree-sitter runtime, with bounded deterministic fallback
   - Language-aware relationships (Go type analysis, TS/JS and Python imports, and PHP Composer/symbol/Laravel references)
   - Hard byte and line limits for LLM context safety
5. Explicit notes are stored separately as durable, repo-scoped memory.

## Project Structure

- `cmd/server/` - entrypoint
- `internal/mcp/` - MCP server and tool handlers
- `internal/indexing/` - chunking, manifest, search, incremental indexing
- `internal/parsing/` - bounded tree-sitter queries and shared declaration extents
- `internal/app/` - app lifecycle wiring
- `vscode-extension/` - companion extension (installer and MCP config UX)
- `docs/` - usage docs and ADRs

## Contributing

### Prerequisites

- Go `1.25.5`
- Node.js (if working on `vscode-extension/` or the Claude Code plugin)

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
./bin/memento-mcp claude-md    # writes ./CLAUDE.local.md in the current project
```

### Run Tests

```bash
make test
```

### Check Internal Package Coverage

Run the same uncached package coverage commands used by CI:

```bash
make coverage-internal
```

The blocking floors are `63.0%` for `./internal/indexing` and `45.0%` for `./internal/mcp`. Update the tests when a change would lower either package below its floor; the canonical CI implementation and thresholds live in [`.github/workflows/coverage-internal.yml`](./.github/workflows/coverage-internal.yml).

### VS Code Extension Development

```bash
cd vscode-extension
npm install
npm run build
```

### Contribution Flow

1. Create a branch from `main`.
2. Make focused changes with tests and docs updates.
3. Run `make test` (and extension build/tests when applicable).
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

## Recommended workflow

Use `repo_context` with an intent for bounded implementation context, `repo_outline` when signatures are enough, and `repo_diff_context` for worktree review. Save durable decisions with anchored `memory_upsert` and recall them with `memory_search` or `memory_list`; verify stale notes before changing them, and reserve `memory_gc`, `memory_delete`, and `memory_clear` for explicit destructive maintenance.

Run `memento-mcp print-guidance` for the current, copyable agent instructions, or see the canonical [LLM guidance](./docs/clients.md#recommended-llm-guidance). For Claude Code projects, `memento-mcp claude-md` writes or updates the same managed guidance in `./CLAUDE.local.md`; use `--print-only` to preview it.
