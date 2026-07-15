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
- ADR 0012: First-class PHP parsing and Composer resolution (Accepted, 2026-07-13)
- ADR 0013: Deterministic term-aware focus retrieval (Accepted, 2026-07-13)
- ADR 0014: Declaration-level PHP retrieval evaluation (Accepted, 2026-07-13)
- ADR 0015: Structural query-intent retrieval (terms-v4 through terms-v8) (Accepted, 2026-07-13)
- ADR 0016: Candidate-bounded relationship ranking (terms-v9) (Accepted, 2026-07-15)

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

## ADR 0012: First-class PHP parsing and Composer resolution

- Status: Accepted
- Date: 2026-07-13

### Context

PHP was indexed and outlined through a local scanner while the other primary languages shared exact tree-sitter declaration extents. Composer resolution supported only a subset of PSR-4, and PHP compatibility had no versioned or framework-shaped measurement corpus. That left chunk boundaries, note anchors, namespace resolution, and autoload relationships vulnerable to silent drift.

### Decision

- Add the embedded PHP grammar from the existing pinned pure-Go `gotreesitter v0.32.0` dependency and include `grammar_subset_php` in local, CI, evaluation, and release commands.
- Use strict PHP syntax trees for outlines, declaration-aligned chunks, namespace-scoped class relationships, and complete hidden note-anchor extents. Preserve the local scanner as the parse-error fallback.
- Index Composer classmap `.inc` files and common PHP-bearing extensions used by templates and Drupal modules.
- Resolve root-package `autoload` and `autoload-dev` PSR-4, PSR-0, classmap, exclusion, and files entries without executing Composer or traversing ignored `vendor` dependencies.
- Keep Composer mappings case-sensitive and ordered, make configured-prefix misses fail closed, and keep all path resolution inside the repository root.
- Maintain an original, dependency-free PHP 7.4–8.4 and Composer/Laravel/Symfony/WordPress/Drupal fixture suite with explicit structural, relationship, retrieval, and negative expectations.
- Change the chunking fingerprint so existing indexes rebuild with PHP declaration boundaries.

### Consequences

- PHP declarations now share exact parser extents across outlines, indexing, and durable-note verification.
- Bracketed namespaces and reused aliases resolve independently instead of relying on one file-wide regex map.
- Composer graph coverage is broader without requiring PHP, Composer, framework installation, or network access during tests.
- Blade and valid syntax rejected by the pinned grammar degrade to the scanner. The known grouped-import trailing-comma gap remains a measured upgrade canary.
- Framework configuration and template relationships remain bounded conventions rather than full framework containers or runtime interpretation.

### Alternatives considered

- Keep the PHP scanner: smaller change, but it cannot provide trustworthy syntax extents or namespace-scoped reference resolution.
- Execute Composer and framework tooling: highest runtime fidelity, but violates dependency-free, local-first indexing and introduces arbitrary project-code execution.
- Index installed `vendor` trees: resolves third-party classes, but adds noise, latency, and a large retrieval surface that is intentionally ignored today.

### References

- [tree-sitter-php](https://github.com/tree-sitter/tree-sitter-php)
- [Composer autoload schema](https://getcomposer.org/doc/04-schema.md#autoload)

---

## ADR 0013: Deterministic term-aware focus retrieval

- Status: Accepted
- Date: 2026-07-13

### Context

`repo_search` deliberately provides literal substring and explicit regular-expression search, but natural-language `repo_context` focus queries previously needed an embedding runtime to discover files outside the active file's relationship graph. The PHP compatibility suite included 19 natural-language retrieval judgments that literal full-phrase matching could not measure meaningfully. Default retrieval must remain local, deterministic, redacted, and inexpensive while preserving the exact-search contract.

### Decision

- Keep `SearchContext` and MCP `repo_search` exact-substring behavior unchanged.
- Add an opt-in, versioned `terms-v1` indexer path (superseded by `terms-v3` in ADR 0014) that splits punctuation, snake case, camel case, and acronym boundaries; removes common query glue; and applies a small explicit set of canonical forms and conservative inflections.
- Score each meaningful concept once across content and path, reward multi-term coverage, use path matches only as a tie boost, and sort equal results by path and declaration start line.
- Use the existing redacted trigram index as a conservative candidate filter and return one highest-ranked chunk per path for focused repository orientation.
- Apply the term-aware scorer to `repo_context` focus queries by default. When local semantic retrieval is enabled, combine the same lexical score with embeddings instead of replacing it.
- Evaluate every checked-in PHP retrieval judgment independently within its corpus at `k=5`, gate recall, MRR, and nDCG per corpus and overall, and keep query IDs and ranked paths out of aggregate JSON reports.

### Consequences

- Natural-language focus can discover relevant unconnected files without Ollama, while explicit exact and regex search remain predictable.
- The scorer is intentionally narrower than stemming or fuzzy search; unsupported synonyms still miss instead of creating broad false positives.
- File-level relevance cannot prove that the right declaration chunk ranked first. The next accuracy slice should add line-bounded judgments and minimized production misses.
- Changes to tokenization, canonical forms, or weights require a new adapter fingerprint and rerunning the compatibility and general retrieval baselines.

### Alternatives considered

- Make `repo_search` term-aware: improves recall but breaks its literal contract and makes exact repository inspection less predictable.
- Require embeddings for natural-language focus: better synonym coverage, but adds a runtime dependency and makes default behavior unavailable offline.
- Use broad stemming, edit distance, or prefix matching: simpler recall gains, but produced ambiguous code-identifier matches and weaker precision than explicit conservative forms.
- Evaluate all framework roots as one corpus: easier to run, but allows similarly named files in one framework to hide misses in another.

---

## ADR 0014: Declaration-level PHP retrieval evaluation

- Status: Accepted
- Date: 2026-07-13

### Context

File-only relevance could pass when retrieval selected a namespace header or an
unrelated member from the correct PHP file. The original 19 queries were also a
single tuning surface, so aggregate gains could hide corpus-specific misses and
did not measure distractors explicitly.

### Decision

- Preserve successful parser-backed PHP namespace, type, and member units as
  distinct chunks; keep bounded line fallback for malformed input and hard-split
  only declarations that exceed configured limits.
- Version the resulting persisted boundary identity as
  `treesitter-php-members-v3` so existing indexes rebuild safely.
- Store exact answer-bearing and hard-negative line ranges in suite v2. Promote
  measured misses into a 30-query training split, retain 11 validation queries,
  and author a fresh holdout query for each corpus only after freezing the
  scorer.
- Gate recall@5, MRR, nDCG@5, and hard-negative ordering per corpus for training
  and validation. Report the post-freeze holdout against the same targets as an
  advisory baseline. Keep query and path detail behind the local details flag.
- Version scorer changes as `terms-v3`: reward content and PHP
  declaration-header evidence, make path evidence a bounded fallback, preserve
  targeted imports, normalize measured forms, and ignore explicit
  `instead of`/`rather than` contrast clauses during positive lexical scoring.

### Consequences

- Selecting the correct file but the wrong declaration no longer earns credit.
- The checked-in suite now contains 52 queries, 57 relevance judgments, and a
  post-freeze 11-query holdout with explicit distractors across all supported
  PHP and framework corpus shapes.
- The first unseen `terms-v3` holdout measurement is recall@5 `0.909`, MRR
  `0.773`, nDCG@5 `0.808`, and one hard-negative win. Keeping it advisory
  preserves the evidence needed for the next scorer generation.
- PHP files produce more, smaller chunks. This improves member precision but can
  increase index records; configured byte and line limits remain hard bounds.
- Future scorer tuning must use the training split. Production misses should be
  minimized into original fixtures and introduced as a new holdout generation
  before their rankings are inspected.

### Alternatives considered

- Keep file-level relevance: simpler, but it masks the exact failure this slice
  is intended to measure.
- Tune on one combined query set: maximizes the visible metric but provides no
  credible estimate of behavior on unseen cases.
- Copy public framework applications into the repository: broader surface area,
  but adds licensing, size, update, and answer-leakage risks compared with
  original minimized reproductions.

---

## ADR 0015: Structural query-intent retrieval (terms-v4 through terms-v8)

- Status: Accepted
- Date: 2026-07-13

### Context

The first post-freeze terms-v3 holdout preserved perfect validation but exposed
four role-sensitive ranking gaps: declaration metadata versus a reference,
first-class callable construction versus the referenced method, configuration
definition versus its consumer, and entity-to-repository mapping versus service
injection. Directly fitting those query strings would invalidate the holdout,
while requiring embeddings would weaken the deterministic offline default.

### Decision

- Version the deterministic scorer as `terms-v4` and derive structural evidence
  at query time without changing persisted chunks or the exact `SearchContext`
  contract.
- Classify only bounded programming-role cues for attributes, callable
  construction, configuration definitions, and relationship declarations.
  Require independent lexical evidence before any structural adjustment.
- Isolate a definition clause from trailing consumer context for explicit
  definition requests, while continuing to ignore `instead of` and `rather
  than` contrast clauses.
- Treat PHP `#[...]` lines as declaration metadata rather than comments. Reward
  syntax and conventional definition paths, downrank explicit consumers, and
  cap the total adjustment between minus one and plus three existing exact-term
  score units.
- Develop against new synthetic and production-shaped training identifiers.
  Freeze the scorer at `cffc091` before opening the existing holdout, then use an
  isolated author with no scorer, suite, query, history, or evaluator access for
  the next holdout generation.
- When those new files exposed a path-order tie in the existing PHP 8.1
  `never`/throw training query, promote the measured training miss and version
  the fix as `terms-v5`. Activate the additional signal only when the query asks
  for never-returning termination and the candidate contains both a `: never`
  declaration and a throw expression.
- The independently authored post-terms-v5 generation paraphrased callable
  construction as a value packaged for passing around and later execution. Its
  one hard-negative win was promoted to training, and `terms-v6` recognizes a
  bounded set of deferred-execution phrases before applying the existing
  first-class-callable syntax bonus.
- The independently authored post-terms-v6 generation described an ORM
  association without framework terms. Promote only that miss and version the
  fix as `terms-v7`: paired parent and dependent-collection roles activate the
  intent, while only concrete `hasMany` or equivalent ORM syntax receives the
  model-path bonus. Model class shells therefore cannot win on path alone.
- The post-terms-v7 generation exposed three unrelated definition-versus-
  consumer gaps. Promote only those misses under `terms-v8`, using separate
  intents whose candidate rewards require a string- or integer-backed `enum`,
  `register_shutdown_function`, or `register_uninstall_hook` syntax. Penalize
  an early-exit consumer and a deactivation registration only within their
  corresponding intents.
- Keep independently authored Composer mini-projects in a retrieval-only
  advisory corpus. Required splits are enforced suite-wide, while a corpus with
  no blocking queries is not failed for missing train or validation data. This
  prevents equally valid package mappings from corrupting base-package labels.

### Consequences

- Neutral queries preserve terms-v3 term extraction and chunk scores exactly;
  structural requests gain deterministic role-aware ordering without a chunk
  migration or network dependency.
- Training scores recall@5 `1.000`, MRR `1.000`, nDCG@5 `0.998`, and zero
  hard-negative wins; validation scores `1.000` on all three metrics with zero
  hard-negative wins. The earlier 11-query advisory generation improves from
  recall@5 `0.909`, MRR `0.773`, nDCG@5 `0.808`, and one hard-negative win to
  recall@5 `1.000`, MRR `0.955`, nDCG@5 `0.966`, and zero hard-negative wins.
- The untouched terms-v4 scorer ranks all eight independently authored
  post-freeze cases first in isolation, producing recall@5, MRR, and nDCG@5
  `1.000` with zero hard-negative wins. Indexing the new files alongside the
  earlier corpus also creates a training distractor that terms-v5 resolves
  without changing the terms-v4 evidence.
- The six-query post-terms-v5 generation records recall@5 `1.000`, MRR `0.917`,
  nDCG@5 `0.938`, and one hard-negative win. Terms-v6 treats that measured miss
  as training.
- The four-query post-terms-v6 generation records recall@5 `0.750`, MRR `0.625`,
  nDCG@5 `0.658`, and one hard-negative win. Deferred behavior and Composer
  mapping rank first, a never-returning routine ranks second, and
  framework-neutral parent-to-collection wording misses the Eloquent
  relationship method. This remains immutable terms-v6 evidence; the one miss
  is promoted under terms-v7, whose 36-query training gate ranks every relevant
  answer first with zero hard-negative wins.
- The five-query post-terms-v7 generation records recall@5 `0.800`, MRR `0.567`,
  nDCG@5 `0.626`, and two hard-negative wins. The independently phrased
  Doctrine association ranks first, confirming the collection relationship
  intent generalizes. Shutdown registration, backed-enum definition, and
  WordPress uninstall registration remain immutable terms-v7 misses. Terms-v8
  promotes those three judgments, and all 39 training queries rank relevant
  answers first with zero hard-negative wins.
- The final six-query post-terms-v8 generation records recall@5 `1.000`, MRR
  `0.722`, nDCG@5 `0.794`, and three hard-negative wins. Full advisory holdout
  recall is `1.000`, MRR is `0.924`, and nDCG@5 is `0.944`, with three
  hard-negative wins. The remaining problem is ordering, not candidate recall.
- Cue vocabulary remains intentionally narrow. Further accuracy work should use
  an injected intent or relationship provider rather than adding a terms-v9
  synonym list; deterministic terms-v8 remains the offline fallback.

### Alternatives considered

- Tune directly against the first holdout: likely improves the visible metric,
  but destroys its value as unseen evidence.
- Add an indexing-package dependency on MCP relationship graphs: supplies richer
  evidence, but creates layering and evaluator-parity problems. A future batch
  provider may add bounded direct-edge reranking through an injected interface.
- Require semantic retrieval: may cover broader paraphrases, but adds a local
  runtime dependency and cannot replace the deterministic offline baseline.
- Persist symbol and relationship roles in chunk files: avoids query-time source
  inspection, but requires a format migration and reindex for evidence that can
  be derived cheaply from existing chunks.

---

## ADR 0016: Candidate-bounded relationship ranking (terms-v9)

- Status: Accepted
- Date: 2026-07-15

### Context

The final post-terms-v8 holdout preserved recall@5 `1.000`, but three explicit
provider requests ranked a consumer first: an enum presenter ahead of the
backed enum, a bootstrap caller ahead of shutdown-function registration, and a
WordPress purge implementation ahead of the uninstall-hook binding. Memento's
PHP analysis already produced directional parser-, Composer-, and
framework-backed relationships, but term-aware ranking could see only chunk
content, paths, and declaration headers.

### Decision

- Promote the three measured provider-versus-consumer misses before changing
  the scorer and identify the resulting production/evaluator adapter as
  `terms-v9+php-relationships-v1`.
- Expand the existing syntax-gated intents only for the measured semantic roles:
  canonical serialized enum values, shutdown callback attachment at process
  termination, and binding permanent cleanup to plugin deletion.
- Inject a language-neutral `RelationshipProvider` into indexing rather than
  importing MCP graph internals. The production server adapts its cached PHP
  graph, and the evaluator receives the same provider through `ExecuteConfig`.
- Limit relationship input to the 20-100 highest-ranked distinct paths, based
  on four times the requested result count. Only candidates with independent
  lexical evidence participate; relationships cannot add a file or convert a
  semantic-only match into a lexical match.
- Accept only direct candidate-to-candidate edges and cap each path's graph
  adjustment at one exact-term unit (`+20`), regardless of edge count. Prefer
  the edge target for explicit provider/configuration and shutdown-attachment
  intents; prefer the edge source for ORM and uninstall-binding intents.
- Apply the bounded adjustment before lexical normalization so hybrid retrieval
  uses the same adjusted score. Provider absence or error preserves the
  deterministic lexical result; cancellation still terminates the request.
- Expose the effective term-search version and relationship-ranking state in
  index debug output. Require every provider to expose a non-empty fingerprint
  so a missing or different provider cannot satisfy the evaluator's adapter
  guard. Keep literal `SearchContext` and `repo_search` behavior unchanged.
- Freeze the scorer before opening an independently authored post-terms-v9
  holdout and record that first result without tuning.

### Consequences

- Production and evaluation now share the same relationship-aware ranking path
  without making indexing depend on MCP or persisting new chunk metadata.
- Against the unchanged 85-query pre-promotion suite, terms-v9 preserves
  recall@5 `1.000`, raises MRR from `0.969` to `0.982` and nDCG@5 from `0.976`
  to `0.986`, and reduces hard-negative wins from three to zero. The original
  35-query holdout improves from MRR `0.924` and nDCG@5 `0.944` to MRR `0.957`
  and nDCG@5 `0.968`, with recall unchanged and zero hard-negative wins.
- A structural query can pay the PHP graph's first-build cost. The graph is
  cached, singleflight-built, and already bounded by repository ignore rules
  and file-size limits; cached queries inspect adjacency only for at most 100
  distinct lexical paths. Successful reindex, incremental index, removal, and
  clear operations invalidate provider state.
- Path-level relationships cannot identify a specific declaration when several
  chunks in one file are plausible. Existing declaration scoring still chooses
  the file's best chunk, and line-bounded judgments continue to detect errors.
- The independently authored six-query post-terms-v9 generation records
  recall@5 `1.000`, MRR `0.917`, nDCG@5 `0.938`, and one hard-negative win. Five
  controls rank first; a serialized enum definition ranks second behind its
  consumer. That miss remains advisory and terms-v9 is unchanged. Across the
  expanded suite, training and validation retain zero hard-negative wins while
  the 38-query holdout records recall@5 `1.000`, MRR `0.961`, and nDCG@5
  `0.971` with the one preserved miss.

### Alternatives considered

- Continue adding phrase-specific structural bonuses: closes visible cases but
  does not use the stable provider/consumer direction already available.
- Move the PHP graph into indexing: avoids injection plumbing but reverses the
  package boundary and makes evaluator parity harder to enforce.
- Let relationships introduce candidates: can improve recall, but risks hub
  bias and makes graph quality override explicit lexical evidence.
- Penalize consumers: creates larger ranking swings and can harm queries asking
  for use sites. A capped positive adjustment is easier to reason about and
  preserves unrelated scores.

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
