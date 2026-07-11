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
- Claude Code, Claude Desktop, ChatGPT/Codex, and other MCP clients: [`docs/clients.md`](./docs/clients.md)
- VS Code usage: [`docs/vscode.md`](./docs/vscode.md)
- Opt-in local aggregate feedback and privacy controls: [`docs/feedback.md`](./docs/feedback.md)
- VS Code extension: [`vscode-extension/README.md`](./vscode-extension/README.md)
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

### Standalone binary

Install the latest prebuilt server to `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/caiowilson/MCP-memento/main/install.sh | sh
```

Set `MEMENTO_INSTALL_DIR` to choose another directory. The installer supports x64 and arm64 macOS, Linux, and Windows environments with a POSIX shell. Ensure the selected directory is on `PATH`, then verify the binary:

```bash
memento-mcp help
```

### Build from source

Building requires Go 1.25.5 or newer:

```bash
git clone https://github.com/caiowilson/MCP-memento.git
cd MCP-memento
make build
./bin/memento-mcp help
```

## Use with Claude Code

Plugin users need no separate MCP configuration. Verify the automatically started server with `/mcp`. Update with `/plugin marketplace update memento-mcp`, followed by `/plugin update memento@memento-mcp` and `/reload-plugins`.

Plugin MCP names are scoped: for example, the prime prompt is `/mcp__plugin_memento_memento__prime`. See the [client setup guide](./docs/clients.md#plugin-installation-recommended) for lifecycle and troubleshooting details.

### Manual MCP setup

If you installed the standalone binary or built from source, run this from the project you want Claude Code to index. Replace the executable path when it is not available on `PATH`:

```bash
claude mcp add memento -- memento-mcp
```

Claude Code passes the active project through `CLAUDE_PROJECT_DIR`, so memento indexes it without a manual `repo_switch_workspace` call. Verify the connection with `claude mcp list` or `/mcp` inside Claude Code.

For a shared, committable manual setup, add this `.mcp.json` to the project root:

```json
{
  "mcpServers": {
    "memento": {
      "type": "stdio",
      "command": "${CLAUDE_PROJECT_DIR:-.}/bin/memento-mcp",
      "args": [],
      "env": {}
    }
  }
}
```

This form expects an executable at `bin/memento-mcp` in every checkout. Claude Code asks each user to approve project-scoped servers before first use.

### Claude Desktop

Add the equivalent stdio entry to `claude_desktop_config.json` and restart Claude Desktop:

```json
{
  "mcpServers": {
    "memento": {
      "command": "/absolute/path/to/MCP-memento/bin/memento-mcp",
      "args": [],
      "env": {}
    }
  }
}
```

On macOS or WSL, `claude mcp add-from-claude-desktop` can import this server into Claude Code. See the [client setup guide](./docs/clients.md) for details.

## Use with ChatGPT / Codex

After building memento, add it to Codex with the absolute path to the executable:

```bash
codex mcp add memento -- /absolute/path/to/MCP-memento/bin/memento-mcp
```

This configuration is shared by the ChatGPT desktop app, Codex CLI, and the Codex IDE extension. Memento uses the MCP workspace root supplied by the client, so it automatically indexes the repository open in Codex. Verify the connection with `codex mcp list` or `/mcp` inside Codex.

You can also add it from the ChatGPT desktop app:

1. Open **Settings → MCP servers**.
2. Select **Add server** and choose **STDIO**.
3. Name it `memento` and use `/absolute/path/to/MCP-memento/bin/memento-mcp` as the command.
4. Save, then restart the app. Use `/mcp` to confirm that memento is connected.

For manual or project-scoped setup, add this to `~/.codex/config.toml` or to `.codex/config.toml` in a trusted project:

```toml
[mcp_servers.memento]
command = "/absolute/path/to/MCP-memento/bin/memento-mcp"
args = []
```

ChatGPT on the web does not load local Codex configuration or launch local STDIO servers. This local-first server therefore works directly with the ChatGPT desktop app and Codex clients; web use would require hosting a remote MCP endpoint instead.

## What It Does

- Exposes MCP tools for repo operations: `repo_list_files`, `repo_read_file`, `repo_search`, `repo_related_files`, `repo_context`, `repo_switch_workspace`
- Maintains an on-disk code index per repository for fast, bounded context retrieval
- Stores explicit repo-scoped notes: `memory_upsert`, `memory_search`, `memory_clear`
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
   - Language-aware relationships (Go type analysis, TS/JS imports, and PHP Composer/symbol/Laravel references)
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

## Recommended workflow (memory + lean context)

Treat Memento as the default for both memory and context in a repository:

- **Prefer Memento memory over any other memory store.** Persist durable decisions and handoffs with `memory_upsert` (anchored to code); recall with `memory_search` / `memory_list` before re-deriving. `memory_gc` / `memory_delete` / `memory_clear` are destructive — only on explicit instruction.
- **Prime the codebase index for leaner context and lower tokens.** Lead with `repo_context` on the active file, `repo_outline` for signatures, `repo_search` for symbols, and `repo_related_files` for imports — reach for `repo_read_file` only for the exact path you need. Querying the index first (and reading whole files last) is the main lever for lower token usage.

To make this automatic in a project, run `memento-mcp claude-md` in its root: it writes this section into `./CLAUDE.local.md` so the guidance loads every session. Rerun it to update the block in place; use `--print-only` to preview.
