# Documentation

Memento is a local-first MCP server that gives AI agents durable, high-signal memory for your repository: indexed code context, semantic relationships, fast search, and explicit notes that persist across sessions.

This directory collects the main project documentation, including client setup, VS Code usage, and architecture decisions.

## Contents

- Getting started: `../README.md`
- Claude Code and Claude Desktop setup: `clients.md#claude-code`
- Generic MCP clients: `clients.md`
- VS Code usage: `vscode.md`
- Canonical backlog: `../TODO.md`
- VS Code extension: `../vscode-extension/README.md`
- Architecture decisions (ADRs): `adr/README.md`

## MCP tools (current)

- `repo_list_files` — list files under workspace root
- `repo_read_file` — read redacted file content (optionally line-bounded)
- `repo_search` — substring search across files with redacted snippets
- `repo_related_files` — related files for a given path (Go/TS/JS/PHP-aware)
- `repo_context` — indexed chunks for a file + related files, with intent-aware routing for `navigate`, `implement`, and `review`
- `repo_switch_workspace` — switch active workspace root at runtime without restarting MCP
- `repo_index_status` — background indexer status
- `repo_reindex` — trigger full re-index
- `repo_clear_index` — delete all indexed chunks and manifest
- `repo_index_debug` — index debug info (filters, counts, last error)
- `memory_upsert` — store/update repo-scoped notes
- `memory_search` — search repo-scoped notes
- `memory_clear` — delete all repo-scoped notes

## Automatic indexing

On startup the server resolves the workspace root in this order: explicit `--root`/config, `CLAUDE_PROJECT_DIR` (set by Claude Code), MCP client `roots/list`, then the current working directory. It then builds a best-effort, on-disk index of that repo under `~/.memento-mcp/` so tools like `repo_context` can return useful chunks quickly. `repo_switch_workspace` remains available as a manual override during a session.

By default (`MEMENTO_CHANGE_DETECTOR=auto`) the indexer uses a filesystem watcher to detect changes; if the watcher fails to start and the repo is a git repo, it falls back to `git status` polling. You can force a specific strategy with `MEMENTO_CHANGE_DETECTOR=fs` (filesystem watcher first) or `MEMENTO_CHANGE_DETECTOR=git` (git polling first). See `docs/adr/ADRs.md`.

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

## LLM usage

- Prefer `repo_context` with `intent` for normal workflows.
- Use `intent: "navigate"` for lighter outlines and `intent: "implement"` or `intent: "review"` for mixed full+outline context.
- Omit `mode` unless you need to force `full`, `outline`, or `summary`.
- Existing callers that already send `mode` are unchanged.

## Output limits

Defaults are sized to stay below Claude Code's roughly 10k-token MCP result warning in normal use:

- `repo_context` defaults to `maxTokens: 7000`, using a conservative `ceil(UTF-8 bytes / 4)` estimate, with `maxTotalBytes: 32000` retained as a hard ceiling.
- `repo_read_file` defaults to `maxBytes: 32000`.
- `repo_search` caps each returned snippet to `maxSnippetBytes: 500`.

Set `MEMENTO_CONTEXT_MAX_TOKENS` to change the server default, or pass `maxTokens` on an individual `repo_context` call. The token budget is the primary packing constraint; full-mode candidates are ordered by weighted relevance per estimated token, and an oversized chunk is skipped so smaller later candidates can still fit. Callers can still change the hard byte ceiling with `maxTotalBytes`.

The `repo_context`, `repo_read_file`, and `repo_search` tool definitions advertise `_meta["anthropic/maxResultSizeChars"] = 500000` so Claude Code can handle intentional large reads without its smaller default persistence threshold surprising the caller. Client-side settings such as `MAX_MCP_OUTPUT_TOKENS` may still impose a stricter display/context budget; lower the tool arguments when you want compact responses, or raise the client setting when you intentionally need larger results.

Run `go test ./internal/mcp -run '^$' -bench BenchmarkContextPacking -benchmem` to compare the previous byte-only accounting baseline with token-primary packing overhead.

## Retrieval evaluation

Run `make retrieval-eval` to index this repository and print macro-averaged precision@k, recall@k, MRR, and nDCG@k. `make test` runs the Go suite and then prints the same report. The initial CI job is non-blocking so ranking changes are visible before metric thresholds are established.

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

A path-only judgment matches the first retrieved chunk from that file. A line-bounded judgment matches a retrieved chunk whose line range overlaps it. Keep queries representative of real repository navigation, use repo-relative slash-separated paths, and prefer narrow ranges around the relevant symbol or passage. Each judgment can match only once, so duplicate chunks do not inflate recall. After adding or changing a fixture, run `go test ./evaluation -count=1` for fixture/metric coverage and `make retrieval-eval` to inspect the ranking report.

Default include/exclude rules (configurable in code):

- Include by extension: `.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.php`, `.md`, `.json`, `.yaml`, `.yml`
- Include by high-signal path: `go.mod`, `go.sum`, `README*`, `Makefile`, `Dockerfile`, `.github/workflows/*`, `Taskfile.yml`
- Exclude by pattern: `.env*`, `*.key`, `*.pem`, `*.p12`, `*.pfx`, `*.crt`, `*.der`, `*.ppk`, `id_rsa`, `id_ed25519`, `*.sqlite`, `*.db`, `*.bin`, `*.exe`

## Repository layout (current)

- `cmd/server/` — executable entrypoint
- `internal/app/` — app lifecycle wiring
- `internal/mcp/` — MCP server implementation (stdio JSON-RPC + tools)
- `internal/indexing/` — automatic code indexing (chunk store)
