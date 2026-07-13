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
- ADR 0006: Opt-in local hybrid retrieval with Ollama (Accepted, 2026-07-10)
- ADR 0007: Standalone extractive repository outlines (Accepted; superseded in part by ADR 0011, 2026-07-10)
- ADR 0008: Anchored memory lifecycle and conservative eviction (Accepted, 2026-07-10)
- ADR 0009: Native MCP resources and prime prompt (Accepted, 2026-07-11)
- ADR 0010: Claude Code plugin distribution through verified release binaries (Accepted, 2026-07-11)
- ADR 0011: Pure-Go tree-sitter as the shared structural parser (Accepted, 2026-07-13)

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
- PHP: include/require graph, Composer PSR-4 namespace imports/importers, symbol references, and common Laravel view/config references.
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

## ADR 0006: Opt-in local hybrid retrieval with Ollama

- Status: Accepted
- Date: 2026-07-10

### Context

The persisted chunk index uses exact substring scoring. It is fast and predictable, but it misses conceptually related code that does not share query tokens. Adding embeddings must preserve the project's local-first guarantee, avoid making the default installation substantially larger, and leave the server useful when no model runtime is available.

### Decision

- Keep lexical retrieval as the default and gate semantic retrieval behind `MEMENTO_SEMANTIC_ENABLED`.
- Use a separately installed local Ollama process through its `/api/embed` endpoint. Accept only loopback HTTP URLs and reject redirects.
- Default to the explicitly tagged `nomic-embed-text:v1.5` model. Memento does not install Ollama or pull models.
- Prefer this compact general-purpose model over a larger code-specific model for the initial release; use retrieval fixtures to justify any future default change.
- Embed redacted chunks in batches and store normalized float32 vectors as binary `.vec` sidecars beside the JSONL chunk files.
- Fingerprint the embedding input format and model. Remove incompatible sidecars and re-embed unchanged chunks when the fingerprint changes.
- Rank with a configurable weighted combination of normalized lexical score and cosine similarity. Preserve the previous lexical score and ordering when semantic retrieval is disabled or unavailable.
- Apply hybrid focus retrieval to `repo_context`; retain literal substring semantics as the `repo_search` default, with regex matching available only through explicit opt-in.
- Treat embedding failures as degradations: record the error, fall back to lexical retrieval, and use a short retry backoff.

### Consequences

- The Memento binary and default startup remain model-free; enabling the feature adds the separately managed Ollama runtime and an approximately 274 MB default model.
- Source chunks and queries stay on the machine. Redaction occurs before document embedding.
- Vector storage grows with chunk count and embedding dimension, and semantic queries scan the local sidecars linearly in this first implementation.
- Model upgrades require vector regeneration but do not require rebuilding or deleting the chunk index.
- Retrieval gains can be measured with the existing fixture harness by running it once lexically and once with semantic retrieval enabled.

### Alternatives considered

- Bundle an ONNX or fastembed runtime and model: tighter process integration, but materially increases binary/distribution complexity and makes model lifecycle Memento's responsibility.
- Use a hosted embedding API: simpler local setup, but violates the local-first source handling guarantee.
- Replace lexical scoring entirely: discards exact-match strength and makes provider outages disruptive.

### References

- [Ollama embedding API](https://docs.ollama.com/api/embed)
- [Ollama embedding guidance](https://docs.ollama.com/capabilities/embeddings)
- [nomic-embed-text model](https://registry.ollama.com/library/nomic-embed-text)

---

## ADR 0007: Standalone extractive repository outlines

- Status: Accepted (superseded in part by ADR 0011)
- Date: 2026-07-10

### Context

Agents often need a file's declarations and signatures before they need implementation bodies. `repo_context` already has string-based outline modes, but those modes are hidden behind an enum and mix structural navigation with related-file context assembly. Tool Search works best when the capability is named directly, and callers need stable structured fields to follow line references programmatically.

### Decision

- Add a standalone read-only `repo_outline` tool for one repository file.
- Return package/module metadata, imports, bounded headers, and structured symbols containing name, kind, signature, documentation, container, and line range.
- Parse Go with `go/ast`; use local comment/string-aware structural scanning for TypeScript/JavaScript and PHP; degrade to bounded headers and declaration heuristics for other languages.
- Exclude function and method bodies in every supported parser and sanitize fallback declaration/header lines.
- Preserve source order, cap symbol and documentation output, report source/outline byte counts, and apply repository redaction to every returned text field.
- Keep cross-file relationship edges in `repo_related_files`. Callers compose relationship discovery with per-file outlines instead of receiving a second graph representation.
- Keep existing `repo_context` outline and summary modes for compatibility.

### Consequences

- Tool Search can surface structural retrieval directly, and callers can inspect signatures without spending a full context budget.
- The output is machine-readable and line-addressable instead of a single language-specific string.
- TypeScript/JavaScript and PHP extraction remains intentionally conservative and dependency-free; unusual syntax degrades by omitting a symbol rather than risking implementation-body leakage.
- A new tool and schema must be kept synchronized across leaf and broker registrations and server instructions.

### Alternatives considered

- Add only a `granularity` flag to `repo_context`: smaller API surface, but poor Tool Search discoverability and continued coupling to relationship assembly.
- Replace existing context outline modes: cleaner long term, but breaks established callers without improving the standalone tool.
- Add tree-sitter: richer cross-language parsing, but adds native/runtime distribution complexity that is disproportionate to extractive signatures.

---

## ADR 0008: Anchored memory lifecycle and conservative eviction

- Status: Accepted
- Date: 2026-07-10

### Context

Durable notes can become actively misleading when code changes but the note continues to appear as fresh fact. Elapsed-time expiry alone cannot establish whether a note is wrong, and deleting on each change would disproportionately destroy useful knowledge in frequently edited files. The system needs deterministic evidence, model adjudication, recoverable eviction, and a deletion policy whose false-positive cost matches the irreversibility of hard deletion.

### Decision

- Extend notes with optional code anchors containing a repo-relative path, optional symbol or line range, content hash, Git commit, and branch identity. Preserve legacy notes as unanchored fresh notes.
- Snapshot anchors on `memory_upsert` and `memory_verify`. Symbol anchors resolve line metadata and hash the full declaration extent, including function or method bodies.
- Reconcile affected anchors from filesystem/Git change callbacks and reconcile all active notes before search or list operations.
- Model the lifecycle as `fresh`, `stale`, and `tombstoned`. Content drift marks a note stale; stale notes remain searchable with reasons and rank after fresh notes.
- Expose `memory_mark_stale` and `memory_verify` as discoverable adjudication tools. Count only explicitly declared failed adjudications, never raw deterministic flags.
- Use Git branch and ancestry identity before declaring orphaning. Follow working-tree and committed renames; absence on another branch, detached checkout, or non-descendant history is not orphaning.
- Soft-evict high-confidence orphaned referents as recoverable tombstones. Keep tombstones in `memory_list` and omit them from active search.
- Hard-delete through `memory_gc` only when a note is tombstoned, orphaned, aged out, not recently used, below the retrieval threshold, and above the failed-adjudication threshold. Default to 90 days, two failed adjudications, and fewer than three retrievals; enforce a 30-day minimum age.
- Keep `memory_delete` and `memory_clear` as explicit destructive overrides.

### Consequences

- Anchored notes can no longer silently survive code drift as fresh facts, while unanchored notes remain backward compatible.
- Change-heavy files produce review prompts rather than deletion pressure. Usage is a hard protection signal for automatic GC.
- Git commands add local reconciliation work for missing anchors, but only when disappearance needs lineage or rename adjudication.
- Conservative scanners may omit unusual symbols during anchor creation; callers can use path or explicit line-range anchors instead.
- Notes continue to use the existing local JSON store, with additive fields that older files can omit.

### Alternatives considered

- Time-to-live expiration: simple, but age does not establish falsity and would evict stable decisions.
- Delete after a fixed stale-flag count: conflates code churn with obsolescence and targets valuable hot-path notes.
- Hide stale notes: removes useful historical evidence and makes reconciliation harder.
- Treat every missing path as orphaned: fails on renames, branch switches, detached checkouts, and rewritten history.
- Require model adjudication for every file change: precise but expensive and unavailable without an active caller.

---

## ADR 0009: Native MCP resources and prime prompt

- Status: Accepted
- Date: 2026-07-11

### Context

Memento tools make repository context available to a model, but users cannot explicitly attach durable notes or repository files through native client context pickers. Session priming also requires users to remember a sequence of tool calls. MCP resources and prompts are user-controlled protocol surfaces designed for those workflows, and Claude Code maps them to `@` mentions and slash commands.

### Decision

- Advertise `resources` and `prompts` capabilities under the existing MCP 2024-11-05 protocol negotiation.
- Expose active notes as direct `note://memory/<key>` resources and high-signal project files as direct `repo://file/<path>` resources.
- Expose a `repo://file/{path}` resource template for bounded, redacted, repo-relative UTF-8 text reads.
- Omit tombstoned notes from resource discovery and reads; retain stale notes with explicit warning metadata.
- Count note resource reads as usage in the anchored memory lifecycle.
- Add one user-controlled `prime` prompt with optional active-file path and focus arguments. Embed bounded fresh-then-stale notes, high-signal project files, and an optional body-free outline.
- Serve resources and prompts directly against the broker's current workspace so they follow roots discovery and manual workspace switching without requiring a separate child protocol proxy.
- Do not advertise resource subscriptions or list-changed notifications until the stdio notification path covers every workspace and note mutation source.

### Consequences

- Claude Code users can attach Memento context through native `@` autocomplete and invoke `/mcp__<server-name>__prime` at session start.
- Resource URIs remain stable within a workspace scope and are fuzzy-searchable by note key or file path.
- Arbitrary file resources require stricter path, type, size, and redaction checks than explicit low-level file tools.
- Resource and prompt lists may be refreshed by clients, but this release does not push dynamic list-change notifications.
- Prime context is deliberately bounded; task-specific depth remains the responsibility of `repo_context` and `repo_outline`.

### Alternatives considered

- Add tools named `attach_note` and `prime`: callable by the model, but not visible in native `@` or `/` client interfaces.
- Proxy resources/prompts through child processes: duplicates protocol transport and lifecycle logic without needing the child index.
- Expose every repository file as a direct resource: large discovery payloads; a small direct set plus a template is cheaper.
- Include tombstones in autocomplete: improves recoverability but makes obsolete context too easy to attach accidentally.
- Advertise list-changed immediately: incomplete unless every mutation and workspace transition emits notifications reliably.

---

## ADR 0010: Claude Code plugin distribution through verified release binaries

- Status: Accepted
- Date: 2026-07-11

### Context

Manual `claude mcp add` and project `.mcp.json` setup require every user to install a binary and maintain a machine-specific path. Claude Code plugins can auto-start MCP servers, but marketplace installs copy only the plugin directory into a cache. Committing six platform binaries would bloat the repository, while building on first start would require a matching Go toolchain and delay team onboarding.

### Decision

- Keep the plugin and marketplace catalog in this repository under `plugins/memento` and `.claude-plugin/marketplace.json`.
- Register the MCP server through the plugin's `.mcp.json` and invoke a Node launcher from `${CLAUDE_PLUGIN_ROOT}`. Claude Code already supplies the Node runtime needed to run the launcher.
- Publish prebuilt x64 and arm64 server binaries for macOS, Linux, and Windows through the existing versioned server release workflow.
- Pin each plugin version to the same server version. On first start, download that release's platform binary into `${CLAUDE_PLUGIN_DATA}`, verify its published SHA-256 sidecar, and cache both files.
- Rehash the cached executable on every start. Redownload it when the binary or checksum is missing or invalid; allow later starts to work offline after verification.
- Keep manual MCP configuration and the standalone installer as supported alternatives.
- Validate the marketplace and plugin with Claude Code's strict validator in CI, test launcher download/cache/failure behavior, and exercise an MCP initialize exchange through the launcher.

### Consequences

- Plugin installation is one marketplace command plus one install command, and enabled plugins start Memento automatically after session start or `/reload-plugins`.
- The repository does not carry generated binaries, and plugin users do not need Go.
- First start needs HTTPS access to the pinned GitHub release. Existing cached installations remain usable offline.
- A plugin release and its server release must use the same version, and raw release assets must retain their `.sha256` sidecars.
- Plugin-provided MCP names include Claude Code's plugin and server namespace rather than the shorter names used by manually configured servers.

### Alternatives considered

- Commit every prebuilt binary into the plugin directory: fully self-contained, but adds large generated files and multiplies repository churn on each release.
- Build from source on first start: avoids binary release downloads, but requires Go 1.25.5 on every user machine and makes startup slower and less predictable.
- Depend on a globally installed `memento-mcp`: small plugin package, but preserves the installation ritual and machine-specific PATH failures that the plugin is intended to remove.
- Use a companion repository: isolates release assets, but splits ownership and duplicates version coordination before the plugin has independent lifecycle needs.

### References

- [Claude Code plugin reference](https://code.claude.com/docs/en/plugins-reference)
- [Claude Code plugin marketplaces](https://code.claude.com/docs/en/plugin-marketplaces)
- [Claude Code plugin-provided MCP servers](https://code.claude.com/docs/en/mcp#plugin-provided-mcp-servers)

---

## ADR 0011: Pure-Go tree-sitter as the shared structural parser

- Status: Accepted
- Date: 2026-07-13

### Context

Chunking, standalone outlines, repository-context summaries, and durable-note anchors previously used separate Go AST, JavaScript/TypeScript scanner, brace-matching, and generic-regex paths. Their language coverage and declaration extents could drift. ADR 0007 rejected tree-sitter because the then-considered bindings introduced native build and runtime distribution complexity, but a maintained pure-Go runtime now supports pinned embedded grammars and grammar-subset build tags.

### Decision

- Pin `github.com/odvcencio/gotreesitter` at `v0.32.0` and use its pure-Go parser, embedded grammars, and query API. Do not introduce cgo or runtime grammar downloads.
- Select only Go, JavaScript, TypeScript, TSX, Python, and Rust grammars in local, CI, evaluation, and release commands. JSX uses the TSX grammar; public language labels remain JavaScript or TypeScript.
- Parse at most 1 MiB with a 500 ms parser-pool timeout. Reject syntax-error, missing, partial, or panicking parses and use the existing bounded deterministic fallback.
- Extract declarations through per-language tree-sitter queries, exclude symbols nested inside callable bodies, and keep both body-free signature ranges and full declaration extents.
- Use the shared analysis for syntax-aligned index chunks, `repo_outline`, `repo_context` outline/summary modes, and symbol-anchor hashing. Preserve the richer Go and JavaScript/TypeScript renderers after successful parsing; render Python and Rust directly from shared symbols; retain the PHP scanner.
- Add Python and Rust to default indexed extensions and change the chunking fingerprint so persisted indexes rebuild under the new boundary algorithm.

### Consequences

- Structural retrieval now covers six grammar families consistently, including JSX/TSX, Python, and Rust, while release binaries remain cross-compilable with `CGO_ENABLED=0`.
- Note anchors for supported languages hash exact parser extents, including bodies, decorators, and attributes, without exposing those bodies in outline results.
- Parsing is serialized behind a bounded gate because `gotreesitter v0.32.0` shares GLR scratch state across parser instances; the race suite protects this constraint until an upgrade removes it.
- Grammar subset tags are part of every supported Go command; bypassing the Makefile or workflows without them produces unnecessarily large binaries.
- Valid syntax not accepted by the pinned parser version degrades safely through the local renderer or generic declaration fallback. Upgrading the dependency requires rerunning malformed-source, body-omission, retrieval, and six-target cross-build checks.

### Alternatives considered

- `smacker/go-tree-sitter` or the official Go binding: mature native runtimes, but cgo complicates the six-target static release matrix and macOS packaging.
- Keep independent language-specific paths: no dependency, but perpetuates coverage gaps and inconsistent anchor extents.
- Ship every embedded grammar: simplest build command, but roughly doubles the tested binary size without improving Memento's supported language contract.

### References

- [gotreesitter repository](https://github.com/odvcencio/gotreesitter)
- [gotreesitter v0.32.0 release](https://github.com/odvcencio/gotreesitter/releases/tag/v0.32.0)
- [Tree-sitter query syntax](https://tree-sitter.github.io/tree-sitter/using-parsers/queries/1-syntax.html)

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
