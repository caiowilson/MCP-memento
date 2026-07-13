# Documentation

Memento is a local-first MCP server that gives AI agents durable, high-signal memory for your repository: indexed code context, semantic relationships, fast search, and explicit notes that persist across sessions.

This directory collects the main project documentation, including client setup, VS Code usage, and architecture decisions.

## Contents

- Getting started: `../README.md`
- Claude Code plugin, manual setup, and Claude Desktop: `clients.md#claude-code`
- Generic MCP clients: `clients.md`
- VS Code usage: `vscode.md`
- Retrieval and agent helpfulness evaluation: `evaluation.md`
- Opt-in local aggregate feedback and privacy controls: `feedback.md`
- Canonical backlog: `../TODO.md`
- VS Code extension: `../vscode-extension/README.md`
- Architecture decisions (ADRs): `adr/README.md`

## MCP tools (current)

- `repo_list_files` — list files under workspace root
- `repo_read_file` — read redacted file content (optionally line-bounded)
- `repo_search` — literal substring search by default, with explicit regex mode and redacted snippets
- `repo_related_files` — related files for a given path (Go/TS/JS/PHP-aware; PHP resolves Composer PSR-4 imports, symbol references, and common Laravel view/config conventions)
- `repo_outline` — compact structured signatures, documentation, imports, and line ranges for one file
- `repo_context` — indexed chunks, related files, and path-matching durable memories, with intent-aware routing for `navigate`, `implement`, and `review`
- `repo_diff_context` — compact exact-file chunks and a bounded, redacted unified diff summary for auto-detected Git worktree changes or an explicit ordered path override
- `repo_switch_workspace` — switch active workspace root at runtime without restarting MCP
- `repo_index_status` — background indexer status
- `repo_reindex` — trigger full re-index
- `repo_clear_index` — delete all indexed chunks, vectors, and manifest
- `repo_index_debug` — index debug info (filters, counts, last error)
- `memory_upsert` — store/update repo-scoped notes, optionally anchored to code
- `memory_search` — search active notes; stale results remain visible and downranked
- `memory_list` — enumerate active, stale, and tombstoned notes
- `memory_mark_stale` — confirm a contradiction and record adjudication evidence
- `memory_verify` — refresh anchors and reset a reconciled note to fresh
- `memory_tombstone` — soft-evict an obsolete note without deleting it
- `memory_gc` — conservatively delete eligible tombstones
- `memory_delete` — explicitly delete one note
- `memory_clear` — delete all repo-scoped notes
- `feedback_submit` — when explicitly enabled, store one fixed-choice helpful/not-helpful/unsure signal locally

## Automatic indexing

On startup the server resolves the workspace root in this order: explicit `--root`/config, `CLAUDE_PROJECT_DIR` (set by Claude Code), MCP client `roots/list`, then the current working directory. It then builds a best-effort, on-disk index of that repo under `~/.memento-mcp/` so tools like `repo_context` can return useful chunks quickly. `repo_switch_workspace` remains available as a manual override during a session.

`memory_search` and `memory_list` fail closed when the only available scope is the server's launch directory, because that directory may not be the client's active workspace. Pass the tool's `root` argument, configure `--root` or `CLAUDE_PROJECT_DIR`, use a client with MCP Roots support, or call `repo_switch_workspace` before reading durable notes. When a Roots-capable client reports a workspace change, Memento also blocks tool results until the refreshed `roots/list` response is processed.

By default (`MEMENTO_CHANGE_DETECTOR=auto`) the indexer uses a filesystem watcher to detect changes; if the watcher fails to start and the repo is a git repo, it falls back to `git status` polling. You can force a specific strategy with `MEMENTO_CHANGE_DETECTOR=fs` (filesystem watcher first) or `MEMENTO_CHANGE_DETECTOR=git` (git polling first). See `docs/adr/ADRs.md`.

Memento never indexes or returns untracked paths ignored by Git. It honors nested `.gitignore` files, `.git/info/exclude`, and the user's global `core.excludesFile`; tracked files remain available, matching Git semantics. `.mementoignore` can exclude additional paths but cannot re-include a Git-ignored path. The same boundary applies to list, read, search, outline, context, related-file, resource, and language-relationship operations.

## Native MCP resources and prompt

Memento advertises MCP `resources` and `prompts` alongside tools. Active durable notes are direct `note://memory/<key>` resources, while high-signal project files such as `AGENTS.md`, `README.md`, `go.mod`, and `package.json` are direct `repo://file/<path>` resources. The `repo://file/{path}` resource template supports other repo-relative UTF-8 text files on demand.

Resource reads default to `32000` bytes; set `MEMENTO_RESOURCE_MAX_BYTES` to change that cap. File resources stay inside the active workspace after symlink resolution, reject ignored directories, binary files, `.env*`, private-key/certificate/database formats, and other sensitive paths, and apply the repository redactor. Tombstoned notes are omitted from discovery and cannot be read as active resources; stale notes remain discoverable with a warning in their description. Reading a note resource records usage for the conservative memory lifecycle.

The `prime` prompt front-loads up to eight fresh-then-stale durable notes and bounded high-signal project files. Optional `path` and `focus` arguments add an active-file `repo_outline` and task focus. The complete prompt defaults to `24000` bytes, configurable with `MEMENTO_PRIME_MAX_BYTES`, and ends with a truncation marker when capped. Clients discover it through `prompts/list`; Claude Code presents it as `/mcp__<server-name>__prime`.

## Optional semantic retrieval

Semantic retrieval is opt-in. The default remains deterministic substring scoring and does not require a model runtime. When enabled, Memento asks a local [Ollama](https://docs.ollama.com/) process for embeddings, stores one normalized vector beside each redacted chunk, and combines lexical and cosine scores. A focused `repo_context` call can then include conceptually related chunks even when they do not share the query text. `repo_search` remains literal by default; pass `regex: true` for explicit regular-expression matching.

Memento defaults to `nomic-embed-text:v1.5`, a roughly 274 MB general-purpose embedding model with enough context for the current 8 KiB chunk ceiling. Install Ollama separately and pull the model explicitly:

```bash
ollama pull nomic-embed-text:v1.5
MEMENTO_SEMANTIC_ENABLED=true ./bin/memento-mcp
```

Memento never downloads a model. Ollama owns its model cache; after the explicit pull, retrieval works offline. Memento accepts only unauthenticated loopback HTTP endpoints and does not follow redirects, so indexed source is not sent to a remote embedding service. Embeddings are created from already-redacted chunk content.

Configuration:

- `MEMENTO_SEMANTIC_ENABLED` (default `false`)
- `MEMENTO_EMBEDDING_MODEL` (default `nomic-embed-text:v1.5`)
- `MEMENTO_OLLAMA_URL` (default `http://127.0.0.1:11434`; loopback HTTP only)
- `MEMENTO_HYBRID_SEMANTIC_WEIGHT` (default `0.65`; greater than `0` and at most `1`)
- `MEMENTO_EMBEDDING_BATCH_SIZE` (default `32`)
- `MEMENTO_EMBEDDING_TIMEOUT_SECONDS` (default `30`)

Vector sidecars live under `~/.memento-mcp/repos/<repo-id>/index/v1/files/*.vec`. The embedding fingerprint is recorded in the manifest; changing the model removes incompatible vectors and re-embeds unchanged chunks during the next index pass. The manifest also fingerprints the chunking algorithm and effective size limits. Upgrading to a release with new chunk boundaries triggers a one-time chunk rebuild and, when semantic retrieval is enabled, re-embedding. Prefer explicit model tags so cache invalidation is predictable.

If Ollama is stopped or the model is absent, indexing and queries fall back to lexical retrieval. `repo_index_status` and `repo_index_debug` expose the embedding error, and Memento waits 30 seconds before retrying the unavailable runtime. Chunk indexing and all non-semantic tools continue to work.

## Secret redaction

Memento redacts likely credentials before writing indexed chunks and before returning source content from `repo_read_file`, `repo_search`, and `repo_context`. Detection combines common credential patterns with high-entropy token detection. Private-key files and `.env*` files are excluded from indexing by default; `.env*` files are also omitted from listing and search, while an explicit `repo_read_file` call returns redacted content.

Redaction is a defense-in-depth safeguard, not a replacement for keeping credentials out of repositories. Provider token formats can change, and entropy detection can produce false positives.

Configuration:

- `MEMENTO_REDACTION_ENABLED` (default `true`; set to `false` to opt out)
- `MEMENTO_REDACTION_ENTROPY_ENABLED` (default `true`)
- `MEMENTO_REDACTION_ENTROPY_THRESHOLD` (default `4.3`)
- `MEMENTO_REDACTION_HEX_ENTROPY_THRESHOLD` (default `3.5`)
- `MEMENTO_REDACTION_MIN_TOKEN_LENGTH` (default `24`)
- `MEMENTO_REDACTION_ADDITIONAL_PATTERNS` (JSON array of Go regular expressions to redact)
- `MEMENTO_REDACTION_ALLOW_PATTERNS` (JSON array of Go regular expressions exempted from redaction for known fixtures or placeholders)

Example: `MEMENTO_REDACTION_ADDITIONAL_PATTERNS='["INTERNAL-[A-Z0-9]{16}"]'`. Invalid JSON or regular expressions fail server startup rather than silently weakening protection.

The redaction configuration is fingerprinted in the index manifest. On the first startup after this feature is installed, or whenever these settings change, Memento automatically removes the old chunk index and rebuilds it so previously persisted unredacted chunks are not retained. Explicit memory notes are unaffected.

## Durable memory lifecycle

`memory_upsert` accepts optional `anchors`. A code anchor uses a repo-relative `path` and can narrow the referent with `symbol` or `startLine`/`endLine`; a commit-only anchor marks a note stale when the same branch advances. On save, Memento captures the current content hash, Git commit, branch, and resolved symbol lines. The legacy top-level `path` remains descriptive metadata; use `anchors` when deterministic drift detection is required.

Filesystem and Git change notifications reconcile affected anchors. Search and list also reconcile before returning, so missed watcher events do not silently preserve fresh status. Content changes mark a note `stale`; stale notes remain in `memory_search`, carry `staleReason`, and rank after fresh matches. Repeated code churn does not increment `failedAdjudications` and never deletes a note.

`repo_context` automatically queries the active repository's note store with its normalized target-file path and returns up to eight matching fresh-then-stale notes under `memories` in every output mode. Surfaced note text is capped at 1,200 bytes with `textTruncated` reported, tombstoned notes stay hidden, and surfaced notes count as retrievals for the conservative memory lifecycle.

Git lineage makes orphaning deliberately strict. Memento follows working-tree and committed renames, searches for a uniquely moved anchored symbol when Git similarity is inconclusive, does not orphan notes when their anchor is absent on another branch or detached checkout, and only confirms disappearance when the anchor commit remains in the current branch lineage. Confirmed disappearance of a note's single referent produces a recoverable `tombstoned` note; loss of one anchor from a multi-anchor note remains stale for adjudication. Tombstones are visible through `memory_list` but omitted from active search.

Use `memory_mark_stale` when current context contradicts a note. Set `failedAdjudication: true` only after attempting and failing to reconcile it; raw stale flags are not deletion evidence. Use `memory_verify` after correction to refresh anchors and reset the lifecycle to `fresh`. `memory_tombstone` is explicit soft eviction, while `memory_delete` is the explicit hard-delete override.

`memory_gc` hard-deletes only notes that are all of: tombstoned, confirmed orphaned, older than the aging window, not recently used, below the retrieval threshold, and above the failed-adjudication threshold. Defaults are 90 days, two failed adjudications, and fewer than three retrievals; the aging window cannot be lowered below 30 days. Frequently retrieved notes therefore survive regardless of stale-flag count. Notes remain stored in the existing backward-compatible `notes.json` file under `~/.memento-mcp/repos/<repo-id>/`.

Example anchored upsert:

```json
{
  "key": "context-packing-contract",
  "text": "Oversized chunks are skipped so smaller candidates can still fit.",
  "anchors": [
    { "path": "internal/mcp/token_budget.go", "symbol": "contextBudget.tryAdd" }
  ]
}
```

## LLM usage

- Use `repo_outline` when you need a file's structure before deciding which source ranges to read.
- Prefer `repo_context` with `intent` for normal workflows.
- Use `repo_diff_context` without `paths` to auto-detect staged, unstaged, and untracked Git changes. A supplied non-empty path list overrides detection, preserves caller order, and continues to work outside Git. Auto mode chooses at most 20 deterministic safe changes and reports overflow; deleted and rename-source paths are summarized and evicted but never chunk-loaded. A clean Git worktree succeeds with an empty result, while omitting `paths` outside Git is an error. Results never expand to related files.
- Use `intent: "navigate"` for lighter outlines and `intent: "implement"` or `intent: "review"` for mixed full+outline context.
- Omit `mode` unless you need to force `full`, `outline`, or `summary`.
- Existing callers that already send `mode` are unchanged.
- In clients with MCP resource/prompt support, use note/file `@`-mentions for explicit context and the `prime` prompt at session start.

## Extractive outlines

`repo_outline` is the discoverable, low-cost structural view. It returns package/module metadata and an ordered `symbols` array. Each symbol includes `name`, `kind`, `signature`, `startLine`, `endLine`, and optional `documentation` and `container`. Function and method bodies are never returned. `repo_context` outline and summary modes consume this same structural result, so their language coverage and symbol names stay aligned.

Go, JavaScript/JSX, TypeScript/TSX, Python, and Rust first pass through pinned `github.com/odvcencio/gotreesitter` queries. The runtime is pure Go and release builds select only those six grammars, preserving `CGO_ENABLED=0` cross-compilation. Parsing is strict, capped at 1 MiB and 500 ms, and yields body-free signatures plus hidden full declaration extents used by syntax chunking and durable-note anchors. Go and JavaScript/TypeScript retain their richer renderers after the shared parse; Python and Rust render query results directly. PHP retains its local structural scanner. Malformed supported files and other languages return a bounded, sanitized declaration fallback with `fallback: true` instead of failing the call.

Input options:

- `includeDocumentation` (default `true`)
- `includeImports` (default `true`)
- `maxSymbols` (default `200`, maximum `1000`; preserves source order)

The response reports `sourceBytes` and `outlineBytes` so clients and tests can verify the structural view is materially smaller than the source. Individual documentation fields are capped at 2048 bytes, source files default to a 1 MiB safety limit configurable through `MEMENTO_OUTLINE_MAX_FILE_BYTES`, and all returned text passes through the same redactor as repository reads.

Cross-file edges intentionally remain in `repo_related_files`; use that tool to choose neighboring files, then call `repo_outline` on the files whose shape you need. Use `repo_read_file` or `repo_context` with full content only after the outline identifies the relevant symbols.

## Output limits

Defaults are sized to stay below Claude Code's roughly 10k-token MCP result warning in normal use:

- `repo_context` defaults to `maxTokens: 7000`, using a conservative `ceil(UTF-8 bytes / 4)` estimate, with `maxTotalBytes: 32000` retained as a hard ceiling.
- `repo_diff_context` defaults to three chunks per file, `maxTokens: 4000`, `maxTotalBytes: 16000`, `maxDiffBytes: 12000`, `diffContextLines: 3`, and at most 20 resolved paths. Its bounded, redacted unified diff covers staged, unstaged, and untracked changes; the response reports indexed, included, skipped, omitted, deleted, and overflow counts instead of silently expanding scope.
- `repo_read_file` defaults to `maxBytes: 32000`.
- `repo_search` caps each returned snippet to `maxSnippetBytes: 500`.

Set `MEMENTO_CONTEXT_MAX_TOKENS` to change the server default, or pass `maxTokens` on an individual `repo_context` call. The token budget is the primary packing constraint; full-mode candidates are ordered by weighted relevance per estimated token, and an oversized chunk is skipped so smaller later candidates can still fit. Callers can still change the hard byte ceiling with `maxTotalBytes`.

The `repo_context`, `repo_diff_context`, `repo_read_file`, and `repo_search` tool definitions advertise `_meta["anthropic/maxResultSizeChars"] = 500000` so Claude Code can handle intentional large reads without its smaller default persistence threshold surprising the caller. Client-side settings such as `MAX_MCP_OUTPUT_TOKENS` may still impose a stricter display/context budget; lower the tool arguments when you want compact responses, or raise the client setting when you intentionally need larger results.

Run `go test -tags='grammar_subset,grammar_subset_go,grammar_subset_javascript,grammar_subset_typescript,grammar_subset_tsx,grammar_subset_python,grammar_subset_rust' ./internal/mcp -run '^$' -bench BenchmarkContextPacking -benchmem` to compare the previous byte-only accounting baseline with token-primary packing overhead.

Run `go test -tags='grammar_subset,grammar_subset_go,grammar_subset_javascript,grammar_subset_typescript,grammar_subset_tsx,grammar_subset_python,grammar_subset_rust' ./internal/indexing -run '^$' -bench '^BenchmarkIndexerSearch1000Files$' -benchmem` to compare linear chunk scanning with trigram candidate filtering on a generated 1,000-file repository. On an Apple M4 Pro, the final three-run sample improved from 12.7–13.0 ms/op to 71.1–71.8 µs/op while reducing allocations from about 4.56 MB to 292 KB per search. `BenchmarkTrigramIndexHighEntropy1MiB` separately exercises the compact per-file representation against a high-entropy input.

## Retrieval evaluation

Run `make retrieval-eval` to index this repository and print macro-averaged precision@k, recall@k, MRR, and nDCG@k. `make test` runs the Go suite and then prints the same lexical report. The standalone retrieval job remains non-blocking for visibility; `make evaluation-ci` reproduces the unified helpfulness workflow, whose fingerprint-matched retrieval-baseline comparison is blocking while newer outcome, floor, efficiency, and rating targets remain advisory.

After pulling the configured Ollama model, run `MEMENTO_SEMANTIC_ENABLED=true make retrieval-eval` to measure hybrid retrieval against the same fixtures. Compare it with the default command to separate semantic gains from fixture or scoring changes. Unlike the production server's lexical fallback, the semantic evaluator fails when vector creation or query embedding is unavailable so it cannot mislabel fallback metrics as hybrid results.

Fixtures live in `evaluation/fixtures/retrieval.json`. The top-level `k` is the ranking cutoff, and each query contains a stable `id`, the exact query text, and one or more relevance judgments:

```json
{
  "id": "descriptive-id",
  "query": "exact search text",
  "relevant": [
    { "path": "path/to/file.go" },
    { "path": "path/to/other.go", "startLine": 40, "endLine": 55 }
  ]
}
```

A path-only judgment matches the first retrieved chunk from that file. A line-bounded judgment matches a retrieved chunk whose line range overlaps it. Keep queries representative of real repository navigation, use repo-relative slash-separated paths, and prefer narrow ranges around the relevant symbol or passage. Each judgment can match only once, so duplicate chunks do not inflate recall. After adding or changing a fixture, run `make test` for fixture/metric coverage and `make retrieval-eval` to inspect the ranking report.

Default include/exclude rules (configurable in code):

- Include by extension: `.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.php`, `.md`, `.json`, `.yaml`, `.yml`
- Include by high-signal path: `go.mod`, `go.sum`, `README*`, `Makefile`, `Dockerfile`, `.github/workflows/*`, `Taskfile.yml`
- Exclude by pattern: `.env*`, `*.key`, `*.pem`, `*.p12`, `*.pfx`, `*.crt`, `*.der`, `*.ppk`, `id_rsa`, `id_ed25519`, `*.sqlite`, `*.db`, `*.bin`, `*.exe`

## Repository layout (current)

- `cmd/server/` — executable entrypoint
- `internal/app/` — app lifecycle wiring
- `internal/mcp/` — MCP server implementation (stdio JSON-RPC + tools)
- `internal/indexing/` — automatic code indexing (chunk store)
