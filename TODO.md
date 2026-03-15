# To Do

Vertical slices (ship small, end-to-end improvements).

## Tracking

- Status: `todo` | `in-progress` | `done` | `blocked`
- Owner: GitHub handle (e.g. `@caiowilson`) or name
- Convention: if a step is `done`, mark it `[x]` and set `(status: done)`

## Slice 1 — VS Code happy path

- Status: done
- Owner: codex

### Steps

- [x] Add a concrete MCP configuration snippet to `docs/vscode.md` (owner: codex) (status: done)
- [x] Add a “try these tool calls” smoke section for `repo.context` (owner: codex) (status: done)

## Slice 2 — Indexer safety + file selection

- Status: done
- Owner: codex

### Steps

- [x] Implement explicit allowlist (Go + high-signal files) and denylist (secrets/binaries) (owner: codex) (status: done)
- [x] Document default include/exclude rules (owner: codex) (status: done)

## Slice 3 — Git-first incremental reindex

- Status: done
- Owner: codex

### Steps

- [x] Detect git worktree and use `git status --porcelain -z --untracked-files=all` (owner: codex) (status: done)
- [x] Debounce and re-index only changed paths (owner: codex) (status: done)

## Slice 4 — Filesystem watcher fallback

- Status: done
- Owner: codex

### Steps

- [x] Add fs watcher for non-git repos (create/modify/delete/rename) (owner: codex) (status: done)
- [x] Debounce and re-index touched paths (owner: codex) (status: done)

## Slice 5 — Go semantic freshness

- Status: done
- Owner: codex

### Steps

- [x] Invalidate go/types cache on `*.go`, `go.mod`, `go.sum` changes (owner: codex) (status: done)
- [x] Rebuild semantic index in the background (owner: codex) (status: done)

## Slice 6 — Context quality + hard limits

- Status: done
- Owner: codex

### Steps

- [x] Add total byte limits + clamping metadata to `repo.context` (owner: codex) (status: done)
- [x] Improve chunk scoring (focus hits > semantic edges > imports > same-dir) (owner: codex) (status: done)

## Slice 7 — Ops/admin tools

- Status: done
- Owner: codex

### Steps

- [x] Add `repo.clear_index` / `memory.clear` tools (owner: codex) (status: done)
- [x] Add an index/debug tool (counts, filters applied, last error) (owner: codex) (status: done)

## Slice 8 — VS Code extension UX polish

- Status: done
- Owner: @caiowilson
- Difficulty: medium
- Scope: vscode-extension (commands, settings, UX surfaces)
- Agent: memento-mcp-vscode

### Steps

- [x] Add first-activation onboarding prompt that offers Install / Open Snippet / Copy Snippet (status: done)
- [x] Add `mementoMcp.serverPath` setting override and honor it in path resolution (status: done)
- [x] Add status bar item showing resolved server path + install state (status: done)
- [x] Improve install error UX with actionable guidance when releases/assets are missing (status: done)
- [x] Add command to open or create MCP config file using the snippet builder (status: done)
- [x] Add command palette/menu contributions for better discoverability (status: done)

## Slice 9 — Monorepo releases (server + extension)

- Status: done
- Owner: @caiowilson
- Difficulty: hard
- Scope: release tags + workflows + installer contract + docs
- Agent: memento-mcp-release

### Steps

- [x] Adopt tag namespaces: `server/vX.Y.Z`, `server/latest`, `extension/vA.B.C` (status: done)
- [x] Publish raw server binaries named `memento-mcp_${os}_${arch}[.exe]` (darwin/linux/windows × x64/arm64) (status: done)
- [x] Add `.github/workflows/release-server.yml` to build/upload server assets on `server/v*` tags (status: done)
- [x] Add `.github/workflows/move-server-latest.yml` to move `server/latest` and sync its release assets (status: done)
- [x] Add `.github/workflows/release-extension.yml` to package/publish VSIX on `extension/v*` tags (status: done)
- [x] Ensure installer supports namespaced tags (URL-encode in `vscode-extension/src/installer.ts`) (status: done)
- [x] Confirm defaults in `vscode-extension/package.json` target `caiowilson/memento-mcp` + `server/latest` (status: done)
- [x] Update docs: `README.md`, `docs/vscode.md`, `vscode-extension/README.md` (status: done)
- [x] Smoke test: install server via extension using `server/latest` (status: done) — verified `server/latest` assets in `caiowilson/MCP-memento` via `gh` (repo is private, public repo needed for unauthenticated install)

## Slice 10 — Signed macOS packaging + notarization

- Status: todo
- Owner: @caiowilson
- Difficulty: hard
- Scope: release workflows, Apple signing assets, notarization pipeline
- Agent: memento-mcp-release

### Steps

- [ ] Add Developer ID signing for macOS `.pkg` in release workflows (status: todo)
- [ ] Add notarization submit + staple step for generated `.pkg` assets (status: todo)
- [ ] Add secure GitHub secrets documentation for cert + keychain + notarization credentials (status: todo)
- [ ] Add CI verification step (`pkgutil --check-signature` and `spctl --assess`) before upload (status: todo)
- [ ] Document local and CI troubleshooting for signing/notarization failures (status: todo)

## Slice 11 — Deduplicate `repo_context` output (P0)

- Status: done
- Owner: @caiowilson
- Difficulty: small
- Scope: internal/mcp/context_tool.go
- Priority: P0

### Problem

When related files overlap (e.g. many siblings in `internal/mcp/`), `repo_context` returns the same file's chunks duplicated across the response. This wastes 30–50% of the context budget.

### Steps

- [x] Add `excludePaths` parameter to skip files already in caller's context from prior calls (status: done)
- [x] Track emitted `(path, startLine)` pairs globally across the candidate loop (status: done)
- [x] Skip already-emitted chunks when building `perFile` map (status: done)
- [x] Add test: `excludePaths` filtering, no duplicate chunks, exclude target file (status: done)

## Slice 12 — Outline / summary output mode for `repo_context` (P0)

- Status: done
- Owner: @caiowilson
- Difficulty: medium
- Scope: internal/mcp/context_tool.go, internal/mcp/outline.go
- Priority: P0

### Problem

`repo_context` always returns full source chunks. For navigation/planning, an outline (signatures + doc comments only) would reduce context by 80%+.

### Steps

- [x] Add `mode` parameter to `repo_context`: `full` (default, current), `outline`, `summary` (status: done)
- [x] Implement Go outline extractor using `go/ast` — emit func/type/method signatures + doc comments (status: done)
- [x] Implement JS/TS outline extractor using regex — emit export/function/class declarations (status: done)
- [x] Fallback: for unsupported languages, return first N lines + function-like line matches (status: done)
- [x] Add tests for each mode (status: done)

## Slice 13 — Syntax-aware chunk boundaries (P1)

- Status: todo
- Owner: @caiowilson
- Difficulty: medium
- Scope: internal/indexing/chunk.go
- Priority: P1

### Problem

Chunks split at arbitrary line/byte boundaries, often cutting functions in half. This wastes context on partial, less-useful code.

### Steps

- [ ] For Go files, use `go/ast` to find top-level declaration boundaries and split chunks there (status: todo)
- [ ] For JS/TS, detect function/class/export boundaries via regex heuristics (status: todo)
- [ ] Fallback to current line-based chunking for unknown languages (status: todo)
- [ ] Add tests: verify Go chunks align with function boundaries (status: todo)

## Slice 14 — `repo_diff_context` tool (P1)

- Status: todo
- Owner: @caiowilson
- Difficulty: medium
- Scope: internal/mcp/ (new tool + git integration)
- Priority: P1

### Problem

For edit/review workflows, the LLM only needs context around changed code, not the entire file graph. No tool exposes change-focused context.

### Steps

- [ ] Add `repo_diff_context` tool that detects changed files via `git status` or accepts explicit paths (status: todo)
- [ ] Return only chunks overlapping changed line ranges + their immediate dependency context (status: todo)
- [ ] Include a unified diff summary alongside the chunks (status: todo)
- [ ] Add test with a simulated dirty worktree (status: todo)

## Slice 15 — `repo_symbols` tool (P2)

- Status: todo
- Owner: @caiowilson
- Difficulty: medium
- Scope: internal/mcp/ (new tool)
- Priority: P2

### Problem

No tool exposes a structured symbol list. LLMs must read full chunks to discover what functions/types exist in a file.

### Steps

- [ ] Add `repo_symbols` tool returning `{name, kind, line, signature}` per symbol (status: todo)
- [ ] Implement Go symbol extraction via `go/ast` (status: todo)
- [ ] Implement JS/TS symbol extraction via regex (func, class, export, const) (status: todo)
- [ ] Fallback: generic regex for `func`, `def`, `class`, `interface` keywords (status: todo)
- [ ] Add tests (status: todo)

## Slice 16 — Trigram search index (P2)

- Status: todo
- Owner: @caiowilson
- Difficulty: medium
- Scope: internal/indexing/
- Priority: P2

### Problem

`repo_search` and `Indexer.Search` do linear scans of all indexed content. Slow for large repos.

### Steps

- [ ] Build a trigram index during `indexAll` / `indexOne` (status: todo)
- [ ] Use trigram index to pre-filter candidate files before substring matching (status: todo)
- [ ] Add optional regex mode to `repo_search` (status: todo)
- [ ] Benchmark: measure search latency before/after on a 1000-file repo (status: todo)

## Slice 17 — Auto-surface memories in `repo_context` (P3)

- Status: todo
- Owner: @caiowilson
- Difficulty: small
- Scope: internal/mcp/context_tool.go, internal/mcp/memory_tools.go
- Priority: P3

### Problem

`NoteStore` memories are disconnected from `repo_context`. The LLM must explicitly call `memory_search` to retrieve past insights.

### Steps

- [ ] When assembling `repo_context`, query `NoteStore` for notes matching the target file path (status: todo)
- [ ] Include matching notes in the response under a `memories` key (status: todo)
- [ ] Add test (status: todo)

## Slice 18 — Python import graph (P3)

- Status: todo
- Owner: @caiowilson
- Difficulty: small
- Scope: internal/mcp/ (new file: py_semantic.go)
- Priority: P3

### Problem

No semantic support for Python — one of the most common languages used with AI coding tools.

### Steps

- [ ] Build Python import graph via regex (`import X`, `from X import Y`, relative imports) (status: todo)
- [ ] Wire into `computeRelatedFiles` for `.py` files (status: todo)
- [ ] Add tests with sample Python import structures (status: todo)

## Slice 19 — Tree-sitter integration for language-agnostic parsing (P4)

- Status: todo
- Owner: @caiowilson
- Difficulty: large
- Scope: internal/indexing/, internal/mcp/
- Priority: P4

### Problem

Each language needs custom parsing for symbols, outlines, and chunk boundaries. Tree-sitter would provide a single dependency covering all languages.

### Steps

- [ ] Evaluate Go tree-sitter bindings (e.g. `smacker/go-tree-sitter`) (status: todo)
- [ ] Implement generic symbol extraction using tree-sitter queries (status: todo)
- [ ] Replace language-specific outline/chunk logic with tree-sitter where available (status: todo)
- [ ] Add tests across Go, JS/TS, Python, Rust (status: todo)

## Slice 20 — Chunk boundary regression fixtures (P1)

- Status: todo
- Owner: @caiowilson
- Difficulty: small
- Scope: internal/indexing/chunk.go, internal/indexing/\*\_test.go
- Priority: P1

### Problem

Chunking behavior is not pinned down tightly enough before the syntax-aware chunking work. That makes the next chunking change riskier than it needs to be.

### Steps

- [ ] Add Go fixture coverage for adjacent declarations and doc comments (status: todo)
- [ ] Add assertions for chunk start and end lines (status: todo)
- [ ] Add one non-Go fallback test proving line-based chunking still works (status: todo)

## Slice 21 — Package-level coverage reporting (P1)

- Status: todo
- Owner: @caiowilson
- Difficulty: small
- Scope: CI workflow, internal/indexing, internal/mcp
- Priority: P1

### Problem

The repo has green tests, but no package-level coverage signal for the highest-risk code paths.

### Steps

- [ ] Add coverage reporting for `internal/indexing` and `internal/mcp` in CI output (status: todo)
- [ ] Set an initial floor that only blocks coverage regressions for those packages (status: todo)
- [ ] Document the local coverage command in contributor-facing docs if needed (status: todo)

## Slice 22 — `repo_context` golden output tests (P1)

- Status: todo
- Owner: @caiowilson
- Difficulty: small
- Scope: internal/mcp/context_tool_test.go
- Priority: P1

### Problem

`repo_context` supports multiple intents and modes, but the output contract is only partially protected by tests.

### Steps

- [ ] Add stable test coverage for `intent: navigate` output shape (status: todo)
- [ ] Add stable test coverage for `intent: implement` and `intent: review` output shapes (status: todo)
- [ ] Add explicit-mode contract assertions for `full`, `outline`, and `summary` as needed (status: todo)

## Slice 23 — Docs landing page accuracy pass (P1)

- Status: done
- Owner: codex
- Difficulty: small
- Scope: docs/README.md
- Priority: P1

### Problem

The docs landing page described the repository as a scaffold/work-in-progress in ways that no longer match the current server and test state.

### Steps

- [x] Rewrite the opening description to match the current project state (status: done)
- [x] Remove stale WIP wording from the docs index where it is no longer accurate (status: done)

## Slice 24 — Deprecate `README-old.md` safely (P1)

- Status: todo
- Owner: @caiowilson
- Difficulty: small
- Scope: README-old.md, docs index
- Priority: P1

### Problem

An old top-level README remains in the repo and can compete with the current documentation set.

### Steps

- [ ] Decide whether to archive, delete, or hard-deprecate `README-old.md` (status: todo)
- [ ] If retained, add a top-of-file notice pointing to `README.md` (status: todo)
- [ ] Remove any remaining links that direct readers to the old file (status: todo)
