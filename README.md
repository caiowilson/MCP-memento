# memento-mcp

A **local-first, durability-focused Model Context Protocol (MCP) server** that empowers AI agents with a comprehensive, indexed "external memory" of your codebase.

`memento-mcp` connects your LLM (via VS Code, Claude Desktop, or other MCP clients) to your local source code, providing fast semantic-aware context retrieval, full-text search, and persistent memory across sessions.

Primary target: VS Code’s native MCP integration.

Status: experimental / WIP (tooling and behavior may evolve).

---

## 🚀 Key Features

- **⚡️ High-Performance Indexing**
  - Automatically indexes your repository into bounded, line-based chunks.
  - **Smart Polling:**
    - **Git Repos:** Uses `git status` to detect changes instantly (2s polling by default).
    - **Standard Folders:** Falls back to efficient filesystem monitoring (debounce support).
  - **Persistence:** Indexes are stored locally in `~/.memento-mcp/`, surviving restarts.

- **🧠 Intelligent Context & Relationships**
  - **`repo_context`**: One-shot retrieval of the most relevant code chunks for a given file, automatically including related files.
  - **Language Awareness**:
    - **Go**: Resolves imports, importers, and semantic type references.
    - **TypeScript/JavaScript**: Resolves imports and importers using a graph.
    - **PHP**: Resolves `include`/`require` relationships.
    - **Universal**: Fallback text-based matching for all other languages.

- **📝 Explicit Memory**
  - Store and retrieve persistent notes (`memory_upsert`, `memory_search`) scoped to the repository.
  - Agents can "remember" architectural decisions, TODOs, or user instructions between chat sessions.

- **🔍 Comprehensive Toolset**
  - List files, read content, search codebase, debug index status, and more.

---

## 🧠 Indexing & Memory Model

`memento-mcp` maintains two complementary "memory" systems:

- An **automatic code index** that continuously scans your workspace (via Git status or filesystem events) and stores bounded chunks under `~/.memento-mcp/`. Tools like `repo_context`, `repo_related_files`, and `repo_search` read from this index.
- An **explicit note store** backed by `notes.json`, managed via `memory_upsert`, `memory_search`, and `memory_clear`, for things that don’t naturally live in source files (architecture notes, gotchas, runbooks, etc.).

Most of the time you rely on the background indexer. When you need a deterministic fresh snapshot, you can:

- Call **`repo_reindex`** as a "Memory Start Index" operation to force a full re-scan and rebuild of the code index.
- Use **`repo_index_status`** and **`repo_index_debug`** to check readiness and active rules, and **`repo_clear_index`** to wipe the on-disk index for the current repo.

Clients like VS Code can choose to eagerly warm up small workspaces (for example, calling `repo_reindex` once when the index is empty and the repo is under ~10MB) and otherwise rely on background indexing. See `docs/README.md` for the full indexing/memory reference and `docs/vscode.md` for VS Code-specific guidance.

---

## 🛠️ Installation & Quickstart

### Prerequisites

- **Go 1.25.5** (see `go.mod`).

### Build from Source

```bash
# Clone the repository
git clone https://github.com/caiowilson/MCP-memento.git
cd MCP-memento

# Build the binary
go build -o ./bin/memento-mcp ./cmd/server

# Run it directly (stdio mode)
./bin/memento-mcp
```

### Smoke Test (Raw Stdio)

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"repo_index_debug","arguments":{}}}' | ./bin/memento-mcp
```

### Install System-Wide (Optional)

On macOS, `make install` uses the Homebrew prefix when available, otherwise `/usr/local`.

```bash
# Install (sudo may be required on Linux/macOS)
make install

# Uninstall
make uninstall
```

---

## 📦 Tool Reference

The server exposes the following MCP tools. Note that tool names use **underscores** (e.g., `repo_list_files`) for broad client compatibility.

### Repository Tools

| Tool                     | Description                                                                                                                      |
| :----------------------- | :------------------------------------------------------------------------------------------------------------------------------- |
| **`repo_list_files`**    | Lists all files in the workspace (respects ignores). Supports glob filtering.                                                    |
| **`repo_read_file`**     | Reads file content. Supports line ranges (`startLine`, `endLine`) to save tokens.                                                |
| **`repo_search`**        | Performs a fast substring search across the entire indexed codebase.                                                             |
| **`repo_related_files`** | Returns a ranked list of files related to a given path (imports, importers, same dir).                                           |
| **`repo_context`**       | **The Power Tool.** Returns optimized code chunks for a file + its related context (imports/definitions) to fit context windows. |

### Indexing Management

| Tool                    | Description                                                        |
| :---------------------- | :----------------------------------------------------------------- |
| **`repo_index_status`** | Checks if the background indexer is ready, partial, or has errors. |
| **`repo_index_debug`**  | Detailed stats: file counts, total bytes, active ignore rules.     |
| **`repo_reindex`**      | Forces a complete re-scan and re-index of the repository.          |
| **`repo_clear_index`**  | Deletes the local index and manifest for the current repo.         |

### Explicit Memory

| Tool                | Description                                                  |
| :------------------ | :----------------------------------------------------------- |
| **`memory_upsert`** | Saves a text note with a specific `key` and optional `tags`. |
| **`memory_search`** | Finds saved notes by query string or tags.                   |
| **`memory_clear`**  | Wipes all explicit memory notes for the current repo.        |

---

## ⚙️ Configuration

You can configure `memento-mcp` using environment variables.

### Indexing Limits

| Variable                        | Default           | Description                               |
| :------------------------------ | :---------------- | :---------------------------------------- |
| `MEMENTO_INDEX_MAX_TOTAL_BYTES` | `52428800` (50MB) | Max total size of the index per repo.     |
| `MEMENTO_INDEX_MAX_FILE_BYTES`  | `4194304` (4MB)   | Max size for a single file to be indexed. |

### Polling & Watchers

| Variable                     | Default | Description                                        |
| :--------------------------- | :------ | :------------------------------------------------- |
| `MEMENTO_INDEX_POLL_SECONDS` | `10`    | Full index scan interval (disabled for Git repos). |
| `MEMENTO_GIT_POLL_SECONDS`   | `2`     | How often to check `git status` for changes.       |
| `MEMENTO_GIT_DEBOUNCE_MS`    | `500`   | Wait time (ms) before processing Git changes.      |
| `MEMENTO_FS_DEBOUNCE_MS`     | `500`   | Wait time (ms) for filesystem events.              |
| `MEMENTO_MCP_DEV_LOG`        | `0`     | Log tool calls to stderr when set to `1`.          |

### Included / Excluded Files (Default Behavior)

The indexer uses an "allowlist + denylist" strategy:

- **Included Extensions:**
  `.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.php`, `.md`, `.json`, `.yaml`, `.yml`
- **Always Included (Globs):**
  `go.mod`, `go.sum`, `README*`, `Makefile`, `Dockerfile`, `.github/workflows/*`, `Taskfile.yml`, `Taskfile.yaml`
- **Always Excluded (Globs):**
  `*.key`, `*.pem`, `*.p12`, `*.pfx`, `*.crt`, `*.der`, `*.ppk`, `id_rsa`, `id_ed25519`, `*.sqlite`, `*.db`, `*.bin`, `*.exe`
- **Always Ignored (Dirs):**
  `.git`, `node_modules`, `vendor`, `dist`, `build`, `out`, `.vscode`, `.idea`, `.memento-mcp`

_Note: `.env` files are currently **indexed** by default to support local development contexts. Add them to the denylist code if this is a security concern._

---

## 💻 VS Code Integration

To use this with VS Code's MCP client (e.g., via Copilot):

- **Extension:** The companion VS Code extension in `vscode-extension/` can install the server binary, open/create `mcp.json`, and generate config snippets.
- **Manual config:** Add an MCP stdio entry to your settings file (often `~/.vscode/mcp.json`):

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

If your client requires a different schema (for example `{ "servers": [ ... ] }` or `{ "mcpServers": { ... } }`), see `docs/vscode.md` or use the extension snippet builder.

Release tags are namespaced: server releases use `server/vX.Y.Z` (with `server/latest` kept up to date), and the VS Code extension uses `extension/vA.B.C`.

**Important:** The `cwd` must be set to `${workspaceFolder}` so `memento-mcp` knows which repository to index.

---

## 🏗 Architecture

1.  **Server:** Runs as a standard JSON-RPC 2.0 application over Stdio.
2.  **Storage:**
    - Indices are stored in `~/.memento-mcp/repos/<REPO_HASH>/index/v1/`.
    - Explicit notes are stored in `~/.memento-mcp/repos/<REPO_HASH>/notes.json`.
3.  **Language Graphs:**
    - Built on-the-fly or cached in memory to support `repo_related_files`.
    - Currently supports AST-based parsing for Go, regex/AST-lite for TS/JS/PHP.

---

## 🧪 Development

```bash
go test ./...
go vet ./...
```

To log tool calls while developing:

```bash
MEMENTO_MCP_DEV_LOG=1 ./bin/memento-mcp 2> /tmp/memento-mcp-dev.log
```

## 📚 Documentation

- Project docs: `docs/README.md`
- ADRs: `docs/adr/ADRs.md`
- To do / roadmap: `TODO.md`

## 📝 Notes & Limitations

- The index is local and persisted under `~/.memento-mcp/`. Use `repo_clear_index` / `memory_clear` to reset, or delete the directory manually.
- Some clients enforce tool name restrictions; this server uses underscore tool names and also maps dot names to underscores when possible.

---

## 📜 License

This project is licensed under the **MIT License**. See the [LICENSE](LICENSE) file for details.
