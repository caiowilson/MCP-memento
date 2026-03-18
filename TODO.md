# Backlog

Single source of truth for pending work. This file supersedes the previous duplicate debt tracker.

## Tracking

- Status: `todo` | `in-progress` | `done` | `blocked`
- Owner: GitHub handle (for example, `@caiowilson`) or name
- Convention: when a step is complete, mark it `[x]` and set `(status: done)`
- Execution policy: complete priorities in order `P0 -> P1 -> P2 -> P3 -> P4`

## P0 — Critical Path

### Slice 25 — Per-project MCP server instances + isolated vector stores

- Status: todo
- Owner: @caiowilson
- Difficulty: hard
- Scope: `internal/mcp/server.go`, `internal/indexing/indexer.go`, `internal/app/`, `vscode-extension/`
- Priority: P0

#### Problem

A single MCP server instance is effectively shared across projects, which risks stale cross-project context leakage.

#### Steps

- [ ] Spawn a dedicated MCP server process per project/workspace (one server per `cwd`) (status: todo)
- [ ] Create an isolated vector/chunk store per project under `~/.memento-mcp/<project-hash>/` (status: todo)
- [ ] Set the server root to the current working directory for that project instance (status: todo)
- [ ] Ensure lifecycle management: start server on project open, stop on project close (status: todo)
- [ ] Prevent cross-project index contamination (no shared state between server instances) (status: todo)
- [ ] Update VS Code extension to manage multiple server processes (one per workspace folder) (status: todo)
- [ ] Add tests verifying two concurrent servers maintain independent stores (status: todo)

### Slice 10 — Signed macOS packaging + notarization

- Status: todo
- Owner: @caiowilson
- Difficulty: hard
- Scope: release workflows, Apple signing assets, notarization pipeline
- Priority: P0

#### Steps

- [ ] Add Developer ID signing for macOS `.pkg` in release workflows (status: todo)
- [ ] Add notarization submit + staple steps for generated `.pkg` assets (status: todo)
- [ ] Add secure GitHub secrets documentation for cert + keychain + notarization credentials (status: todo)
- [ ] Add CI verification (`pkgutil --check-signature` and `spctl --assess`) before upload (status: todo)
- [ ] Document local and CI troubleshooting for signing/notarization failures (status: todo)

## P1 — Quality and Safety

### Slice 20 — Chunk boundary regression fixtures

- Status: todo
- Owner: @caiowilson
- Difficulty: small
- Scope: `internal/indexing/chunk.go`, `internal/indexing/*_test.go`
- Priority: P1

#### Problem

Chunking behavior is not pinned down tightly enough before syntax-aware chunking changes.

#### Steps

- [ ] Add Go fixture coverage for adjacent declarations and doc comments (status: todo)
- [ ] Add assertions for chunk start and end lines (status: todo)
- [ ] Add one non-Go fallback test proving line-based chunking still works (status: todo)

### Slice 13 — Syntax-aware chunk boundaries

- Status: todo
- Owner: @caiowilson
- Difficulty: medium
- Scope: `internal/indexing/chunk.go`
- Priority: P1
- Depends on: Slice 20

#### Problem

Chunks currently split at arbitrary line/byte boundaries and can cut functions in half.

#### Steps

- [ ] For Go files, use `go/ast` to split on top-level declaration boundaries (status: todo)
- [ ] For JS/TS, detect function/class/export boundaries with regex heuristics (status: todo)
- [ ] Fallback to line-based chunking for unknown languages (status: todo)
- [ ] Add tests verifying Go chunks align to declaration boundaries (status: todo)

### Slice 14A — `repo_diff_context` MVP (explicit paths)

- Status: todo
- Owner: @caiowilson
- Difficulty: medium
- Scope: `internal/mcp/` (new tool)
- Priority: P1

#### Problem

Edit/review workflows need context centered on changed files, not full graph context.

#### Steps

- [ ] Add `repo_diff_context` tool that accepts explicit repo-relative paths (status: todo)
- [ ] Return only chunks overlapping those paths with compact nearby context (status: todo)
- [ ] Include a concise summary block in the response (status: todo)
- [ ] Add tests for explicit-path behavior (status: todo)

### Slice 14B — `repo_diff_context` dirty-worktree auto-detection

- Status: todo
- Owner: @caiowilson
- Difficulty: medium
- Scope: `internal/mcp/` (git integration)
- Priority: P1
- Depends on: Slice 14A

#### Steps

- [ ] Detect changed files via `git status` when paths are omitted (status: todo)
- [ ] Exclude deleted files from chunk loading (status: todo)
- [ ] Include a unified diff summary alongside returned chunks (status: todo)
- [ ] Add tests with a simulated dirty worktree (status: todo)

### Slice 21 — Package-level coverage reporting

- Status: todo
- Owner: @caiowilson
- Difficulty: small
- Scope: CI workflow, `internal/indexing`, `internal/mcp`
- Priority: P1

#### Steps

- [ ] Add CI coverage reporting for `internal/indexing` and `internal/mcp` (status: todo)
- [ ] Set an initial floor that only blocks regressions for those packages (status: todo)
- [ ] Document local coverage command in contributor-facing docs (status: todo)

### Slice 22 — `repo_context` golden output tests

- Status: todo
- Owner: @caiowilson
- Difficulty: small
- Scope: `internal/mcp/context_tool_test.go`
- Priority: P1

#### Steps

- [ ] Add stable coverage for `intent: navigate` output shape (status: todo)
- [ ] Add stable coverage for `intent: implement` and `intent: review` output shapes (status: todo)
- [ ] Add explicit-mode contract assertions for `full`, `outline`, and `summary` (status: todo)

### Slice 24 — Deprecate `README-old.md` safely

- Status: todo
- Owner: @caiowilson
- Difficulty: small
- Scope: `README-old.md`, docs index
- Priority: P1

#### Steps

- [ ] Decide whether to archive, delete, or hard-deprecate `README-old.md` (status: todo)
- [ ] If retained, keep a top-of-file notice pointing to `README.md` (status: todo)
- [ ] Remove any remaining links that direct readers to the old file (status: todo)

### Slice 26 — VS Code extension config tests

- Status: todo
- Owner: @caiowilson
- Difficulty: small
- Scope: `vscode-extension/` tests
- Priority: P1

#### Steps

- [ ] Add tests for workspace-binary preference and explicit server-path override (status: todo)
- [ ] Add tests for MCP config merge behavior when a config already exists (status: todo)
- [ ] Keep installer network behavior out of this slice (status: todo)

## P2 — Capability Expansion

### Slice 15 — `repo_symbols` tool

- Status: todo
- Owner: @caiowilson
- Difficulty: medium
- Scope: `internal/mcp/` (new tool)
- Priority: P2

#### Steps

- [ ] Add `repo_symbols` returning `{name, kind, line, signature}` per symbol (status: todo)
- [ ] Implement Go symbol extraction via `go/ast` (status: todo)
- [ ] Implement JS/TS symbol extraction via regex (func, class, export, const) (status: todo)
- [ ] Add generic fallback regex for `func`, `def`, `class`, `interface` keywords (status: todo)
- [ ] Add tests (status: todo)

### Slice 16 — Trigram search index

- Status: todo
- Owner: @caiowilson
- Difficulty: medium
- Scope: `internal/indexing/`
- Priority: P2

#### Problem

`repo_search` and `Indexer.Search` currently do linear scans of indexed content.

#### Steps

- [ ] Build a trigram index during `indexAll` and `indexOne` (status: todo)
- [ ] Use trigram index to pre-filter candidate files before substring matching (status: todo)
- [ ] Add optional regex mode to `repo_search` (status: todo)
- [ ] Benchmark search latency before/after on a 1000-file repo (status: todo)

## P3 — Context and Docs Cohesion

### Slice 17 — Auto-surface memories in `repo_context`

- Status: todo
- Owner: @caiowilson
- Difficulty: small
- Scope: `internal/mcp/context_tool.go`, `internal/mcp/memory_tools.go`
- Priority: P3

#### Steps

- [ ] Query `NoteStore` for notes matching the target file path during `repo_context` assembly (status: todo)
- [ ] Include matching notes under a `memories` key in the response (status: todo)
- [ ] Add tests (status: todo)

### Slice 18 — Python import graph

- Status: todo
- Owner: @caiowilson
- Difficulty: small
- Scope: `internal/mcp/` (`py_semantic.go`)
- Priority: P3

#### Steps

- [ ] Build Python import graph via regex (`import X`, `from X import Y`, relative imports) (status: todo)
- [ ] Wire it into `computeRelatedFiles` for `.py` files (status: todo)
- [ ] Add tests with sample Python import structures (status: todo)

### Slice 27 — Canonicalize config and LLM guidance

- Status: todo
- Owner: @caiowilson
- Difficulty: small
- Scope: `README.md`, `docs/clients.md`, `docs/vscode.md`, `vscode-extension/README.md`
- Priority: P3

#### Steps

- [ ] Choose one canonical guidance page for config + LLM usage (status: todo)
- [ ] Shorten duplicated sections in other docs and replace with links (status: todo)
- [ ] Verify examples match current tool names and arguments (status: todo)

## P4 — Long-term Architecture

### Slice 19 — Tree-sitter integration for language-agnostic parsing

- Status: todo
- Owner: @caiowilson
- Difficulty: large
- Scope: `internal/indexing/`, `internal/mcp/`
- Priority: P4

#### Steps

- [ ] Evaluate Go tree-sitter bindings (for example, `smacker/go-tree-sitter`) (status: todo)
- [ ] Implement generic symbol extraction using tree-sitter queries (status: todo)
- [ ] Replace language-specific outline/chunk logic with tree-sitter where available (status: todo)
- [ ] Add tests across Go, JS/TS, Python, and Rust (status: todo)

## Recently Completed (historical)

- Slice 1: VS Code happy path (done)
- Slice 2: Indexer safety + file selection (done)
- Slice 3: Git-first incremental reindex (done)
- Slice 4: Filesystem watcher fallback (done)
- Slice 5: Go semantic freshness (done)
- Slice 6: Context quality + hard limits (done)
- Slice 7: Ops/admin tools (done)
- Slice 8: VS Code extension UX polish (done)
- Slice 9: Monorepo releases (server + extension) (done)
- Slice 11: Deduplicate `repo_context` output (done)
- Slice 12: Outline/summary output mode for `repo_context` (done)
- Slice 23: Docs landing page accuracy pass (done)
