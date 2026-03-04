# Architecture Decision Records (ADRs)

This document consolidates all ADRs for this repository.

## How to add a new ADR

1. Add a new `## ADR NNNN: Title` section at the end of this file.
2. Update the **Index** below.
3. Use the **ADR Template** section for structure.

## Index

- ADR 0001: Record architecture decisions (Accepted, 2026-01-17)
- ADR 0002: VS Code codebase memory MVP (Accepted, 2026-01-17)
- ADR 0003: Multi-language related-file analysis (Accepted, 2026-01-17)
- ADR 0004: Automatic codebase indexing (“memorization”) (Accepted, 2026-01-17)
- ADR 0005: Git-first incremental indexing (with filesystem fallback) (Proposed, 2026-01-17)

---

## ADR 0001: Record architecture decisions

- Status: Accepted
- Date: 2026-01-17

### Context

This codebase is early-stage and still being scaffolded. Several upcoming choices (transport, persistence, indexing, API boundaries) will have long-lived impact and are easy to lose in chat or commit history.

### Decision

Adopt Architecture Decision Records (ADRs) for capturing significant technical decisions. Use the **ADR Template** section in this document for consistency.

### Consequences

- Decisions become discoverable and reviewable without reading git history.
- We add small process overhead for decision-worthy changes.
- Future ADRs can supersede earlier ones as the project evolves.

### Alternatives considered

- Keep decisions in PR descriptions only (harder to find later).
- Rely on git history (high effort to reconstruct rationale).

---

## ADR 0002: VS Code codebase memory MVP

- Status: Accepted
- Date: 2026-01-17

### Context

The primary client target is VS Code. The MCP server’s job is to provide persistent, tool-accessible “external context” for a single local codebase, not general-purpose user notes or cross-project memory.

We need something useful immediately for answering questions about a repo, even before we add heavier features like embeddings, semantic chunking, or advanced indexing.

### Decision

Start with an MVP MCP stdio server that exposes repository context tools and a small persistent, repo-scoped note store:

#### Repo context tools (MVP)

- `repo_list_files` — list files under the workspace root (with basic ignores)
- `repo_read_file` — read file content (optionally line-bounded)
- `repo_search` — substring search across files (optionally glob-bounded)
- `repo_related_files` — fetch related files for a given path (same folder, imports, importers; heuristics)
- `repo_context` — fetch indexed chunks for a file plus its related files

#### Repo-scoped memory (MVP)

- `memory_upsert` — store/update a note keyed to the repo (optionally associated with a path and tags)
- `memory_search` — search stored notes by substring/tags

The server runs over stdio JSON-RPC 2.0 using MCP methods `initialize`, `tools/list`, and `tools/call`.

### Consequences

- Immediate usefulness in VS Code clients that can call MCP tools for “read/search/list” without any external services.
- “Memory” is initially explicit (notes saved by the model/user) rather than implicit (auto-ingested full-repo embeddings).
- Future work can add:
  - better ignore handling (gitignore)
  - incremental indexing and change detection
  - embeddings and semantic retrieval (opt-in)

### Alternatives considered

- Start with embeddings-only: higher complexity, API keys, and unclear privacy story.
- Persist full repo content in a DB: redundant with the filesystem and increases storage and invalidation complexity.

---

## ADR 0003: Multi-language related-file analysis

- Status: Accepted
- Date: 2026-01-17

### Context

This MCP server is intended to act as an external “context window” for agents working in VS Code across mixed-language repositories (Go, TypeScript/Node.js, PHP). A core workflow is: “given a file I’m editing, fetch all related files”.

A naive approach (same-folder + substring search) is often too noisy and misses important edges like importers/callers.

### Decision

Implement `repo_related_files` using a layered, language-aware approach that stays decoupled:

- Shared heuristics:
  - same-directory files
  - generic “mentions” fallback for unknown languages
- Go:
  - use `go/packages` + `go/types` to infer file-to-file relationships via referenced definitions
  - include import edges and importers
- TypeScript/JavaScript (Node):
  - build an import graph by parsing `import` / `export from` / `require()` / `import()`
  - resolve only relative specifiers (`./` / `../`) to repo-local files
- PHP:
  - build an include graph by parsing `require` / `include` (and `_once`) with string literal paths
  - resolve only simple relative includes

Each language implementation is isolated in its own file so we can iterate independently.

### Consequences

- Go “semantic” relations are substantially more accurate (type-aware) than plain text search.
- TS/JS and PHP semantics are initially import/include based; deeper symbol-level mapping can be added later without changing the tool shape.
- Graphs and indexes are cached per repo root for the lifetime of the server process; changes may require a restart for perfect freshness (MVP).

### Alternatives considered

- Use language servers (gopls/tsserver/intelephense) as the source of truth: higher operational complexity and more external dependencies.
- Parse everything with a universal AST engine (e.g., tree-sitter): more consistent but still needs name resolution to be truly semantic.

---

## ADR 0004: Automatic codebase indexing (“memorization”)

- Status: Accepted
- Date: 2026-01-17

### Context

The MCP server should automatically “memorize” the codebase so an agent can pull relevant context quickly without manually reading dozens of files each session.

We want a solution that:

- Works offline and locally (no external services required).
- Supports mixed-language repos (Go, TypeScript/Node.js, PHP).
- Is decoupled from language-server setups.
- Scales by indexing “everything if small enough” and “most relevant files” when larger.

### Decision

Implement a background indexer that:

- Scans the workspace root at startup and periodically thereafter.
- Indexes preferred source/document formats into line-based chunks.
- Persists chunks on disk under `~/.memento-mcp/` scoped to the repo root.
- Enforces byte budgets (max total bytes indexed, max file size, chunk size).

Expose a tool (`repo_context`) that returns indexed chunks for the active file plus its related files.

### Consequences

- Context retrieval becomes fast and “one-call” for common workflows.
- Index contents can lag behind changes depending on polling interval; worst case restart fixes it (MVP).
- Chunking is structural-but-shallow (line based); future work can add AST-aware chunking without changing the on-disk storage model.

---

## ADR 0005: Git-first incremental indexing (with filesystem fallback)

- Status: Proposed
- Date: 2026-01-17

### Context

This MCP server acts as an external context window for agents working inside VS Code. To be useful, it needs to “memorize” the repo automatically and stay fresh as files change, without burning CPU on repeated full scans.

Most workspaces are git repositories. Git already tracks what changed; using that signal is usually cheaper than repeatedly walking the filesystem.

### Objectives

- Cold start indexing for ~5k files: **< 30s** for the “fast path” (Tier 1).
- Automatic, offline, local-first operation (no external services required).
- Incremental updates on create/modify/delete/rename.
- Prefer **git-derived change lists** when available; fall back to **filesystem events** when not.
- Go-first MVP, but keep the change detection + indexing pipeline decoupled from language semantics.
- Avoid indexing likely secrets by default (e.g. `.env`, keys, certs).

### Non-goals (MVP)

- Patch-level/hunk-level indexing (“diff chunks”) as the primary mechanism.
- Perfect real-time semantic graph updates for Go; best-effort background refresh is acceptable.
- Full language-server integration (gopls/tsserver/intelephense).

### Decision

Adopt a tiered index + controller architecture:

#### Tier 1: Chunk index (fast path)

- Index selected repo files into line-based chunks persisted on disk (repo-scoped).
- Update per-file on change events.
- Provide tools that can retrieve relevant chunks for a file and its neighbors (imports/importers/etc.).

#### Tier 2: Language semantics (slow/background)

- Build Go semantic relationships (`go/packages` + `go/types`) in the background after startup.
- Invalidate semantic caches on relevant changes (at minimum: `*.go`, `go.mod`, `go.sum`).

#### Change detection strategy

1. If inside a git worktree:
   - Watch `.git/` metadata cheaply (at minimum `.git/index`).
   - On change, run `git status --porcelain -z --untracked-files=all` and derive changed paths.
2. Otherwise:
   - Use a filesystem watcher on the workspace root with debounce/batching.

### Architecture

#### Components

- `IndexController`
  - Owns lifecycle and scheduling (startup indexing, debounced incremental updates, semantic refresh triggers).
  - Chooses a change source: Git-first, else filesystem.
- `ChangeSource` (pluggable)
  - `GitChangeSource`: produces changed paths by parsing `git status --porcelain -z`.
  - `FSChangeSource`: produces changed paths from fs events (create/write/remove/rename).
- `FileSelector`
  - Decides which files are indexable:
    - Go code: `**/*.go`, `**/*_test.go`
    - “High-signal” non-code whitelist: `go.mod`, `go.sum`, `README*`, `Makefile`, `Dockerfile`, `.github/workflows/*`
    - Ignore sensitive/unhelpful files and directories (`.git`, `node_modules`, `.env`, binaries, etc.).
- `ChunkIndexer` + `ChunkStore`
  - Reads file bytes, chunks into bounded segments, writes to persistent store.
  - Maintains a manifest for incremental updates (size/modtime/hash and chunk count).
- `SemanticIndexer` (Go)
  - Produces semantic edges used by `repo_related_files` / `repo_context`.
  - Runs in background; can be restarted/invalidate-on-change.

#### Data flow (high level)

Startup:

1. Tier 1 full scan (bounded by byte budgets; “index everything if small enough”).
2. Start change source (git-first or fs).
3. Start Tier 2 Go semantic indexing in background.

On change:

1. Change source emits paths (debounced).
2. Re-index only those files (and delete removed ones).
3. If changes include Go-related inputs, invalidate/refresh the Go semantic cache.

### Notes on using diffs

Git diffs are still valuable as a *change detector* (file list), but Tier 1 indexing does not require patch hunks: per-file re-chunking is simpler and typically fast enough for ~5k files. Hunk-level updates can be added later as an optimization.

### Tool contracts

This section pins the expected behavior and shapes of the primary “automatic memory” tools.

All tool outputs should be machine-readable JSON objects (even if transported as MCP `text` content).

#### `repo_context`

**Purpose**

Return a single “context window” for an active file by combining:

- the active file’s indexed chunks
- indexed chunks from related files (imports/importers/semantic edges, depending on language and options)

**Input**

- `path` (string, required): repo-relative file path.
- `focus` (string, optional): a symbol/term to prioritize chunk selection (e.g., `StartServer`, `Handler`, `IndexController`).
- `maxFiles` (int, optional, default 10): cap on how many files to include (including the active file).
- `maxChunksPerFile` (int, optional, default 2): cap on chunks per file.
- `includeSameDir` / `includeImports` / `includeImporters` / `includeReferences` (bool, optional): forwarded to the related-file strategy.

**Output**

```json
{
  "path": "internal/mcp/server.go",
  "focus": "StartBackgroundIndexing",
  "files": [
    {
      "path": "internal/mcp/server.go",
      "chunks": [
        {
          "path": "internal/mcp/server.go",
          "language": "go",
          "startLine": 1,
          "endLine": 120,
          "content": "..."
        }
      ]
    }
  ]
}
```

**Guarantees**

- The active `path` must be validated (exists, is a file, is within workspace root).
- The first entry in `files` should be the active file when available.
- The server may clamp `maxFiles` and `maxChunksPerFile` to safe bounds.
- The server should prefer chunks that match `focus`, falling back to the top-of-file chunk if needed.

**Failure modes**

- If `path` is missing/invalid/unreadable: return a tool error.
- If indexing is incomplete: return best-effort `files` based on what is already indexed.

#### `repo_related_files`

**Purpose**

Return an ordered list of repo-relative files related to a given file. Relationships are language-aware when possible:

- Go: imports, importers, and semantic edges (go/types).
- TS/JS: import graph (relative specifiers).
- PHP: include/require graph (simple relative literals).
- Other: best-effort mention/same-directory heuristics.

**Input**

- `path` (string, required)
- `max` (int, optional, default 50)
- `includeSameDir` / `includeImports` / `includeImporters` / `includeReferences` (bool, optional)

**Output**

```json
{
  "path": "internal/mcp/server.go",
  "count": 3,
  "related": [
    { "path": "internal/mcp/context_tool.go", "score": 17, "reasons": ["go_types_refs_target"] }
  ]
}
```

**Guarantees**

- Sorted by descending `score`, then path.
- `reasons` provides stable, inspectable justification for why a file was included.

#### `repo_index_status`

**Purpose**

Expose whether automatic indexing is ready and provide basic freshness/size indicators.

**Output**

```json
{
  "ready": true,
  "lastIndexedAt": "2026-01-17T01:23:45Z",
  "filesIndexed": 1234,
  "bytesIndexed": 4567890,
  "partial": false,
  "error": ""
}
```

#### `repo_reindex`

**Purpose**

Trigger a full re-index (useful for recovery if change detection misses events). Returns the same shape as `repo_index_status`.

#### `repo_clear_index`

**Purpose**

Delete all persisted index data for the repo (chunks + manifest).

**Output**

Returns the same shape as `repo_index_status`.

#### `repo_index_debug`

**Purpose**

Return index debug information to help diagnose filter/coverage issues.

**Output**

```json
{
  "root": "...",
  "storeDir": "...",
  "filesIndexed": 123,
  "totalBytes": 456789,
  "preferredExts": [".go", ".md"],
  "allowGlobs": ["go.mod", "README*"],
  "denyGlobs": ["*.key"],
  "extraIgnoreDirs": [],
  "extraIgnoreGlobs": [],
  "lastError": ""
}
```

#### `memory_clear`

**Purpose**

Delete all repo-scoped notes.

**Output**

```json
{ "cleared": true }
```

### Open questions

- Which git paths are sufficient to watch reliably across platforms (`.git/index` vs refs/HEAD)?
- Should we include `go.sum` and `.github/workflows/*` in the default whitelist, or keep them configurable?
- How do we expose “index freshness” to tools (timestamps, indexed file count, partial indexing reason)?

---

## ADR Template

## ADR NNNN: Title

- Status: Proposed
- Date: YYYY-MM-DD

### Context

### Decision

### Consequences

### Alternatives considered

### References
