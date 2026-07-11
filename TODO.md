# Backlog

Single source of truth for pending work. This file supersedes the previous duplicate debt tracker.

## Tracking

- Status: `todo` | `in-progress` | `done` | `blocked`
- Owner: GitHub handle (for example, `@caiowilson`) or name
- Convention: when a step is complete, mark it `[x]` and set `(status: done)`
- Execution policy: complete unblocked priorities in order `P0 -> P1 -> P2 -> P3 -> P4`
- Last audited: 2026-07-11 against `main` at `053720d`
- Slice numbers are legacy backlog identifiers and do not map to GitHub issue numbers.

## P0 — Critical Path

### Slice 25 — Per-project MCP server instances + isolated vector stores

- Status: done
- Owner: @caiowilson
- Difficulty: hard
- Scope: `internal/mcp/server.go`, `internal/indexing/indexer.go`, `internal/app/`, `vscode-extension/`
- Priority: P0

#### Problem

A single MCP server instance is effectively shared across projects, which risks stale cross-project context leakage.

#### Steps

- [x] Spawn a dedicated child MCP server process per project/workspace root (status: done)
- [x] Create isolated vector/chunk and note stores under `~/.memento-mcp/repos/<project-hash>/` (status: done)
- [x] Set each child server root to its routed workspace root (status: done)
- [x] Manage child startup, idle reaping, respawn, and shutdown in the broker (status: done)
- [x] Prevent cross-project index and memory contamination (status: done)
- [x] Supersede extension-owned processes with broker-owned per-workspace child processes (status: done)
- [x] Add tests verifying independent stores, routing, reuse, idle reaping, and respawn (status: done)

Delivered by `99438fc`, `28baf3f`, and the broker isolation tests in `internal/mcp/broker_test.go`.

### Slice 10 — Signed macOS packaging + notarization

- Status: blocked
- Owner: @caiowilson
- Difficulty: hard
- Scope: release workflows, Apple signing assets, notarization pipeline
- Priority: P0

Blocked on Apple Developer ID Installer credentials, notarization credentials, and GitHub Actions secrets. Unsigned macOS binaries and packages continue to ship alongside the credential-free Claude Code marketplace/plugin.

#### Steps

- [ ] Add Developer ID signing for macOS `.pkg` in release workflows (status: todo)
- [ ] Add notarization submit + staple steps for generated `.pkg` assets (status: todo)
- [ ] Add secure GitHub secrets documentation for cert + keychain + notarization credentials (status: todo)
- [ ] Add CI verification (`pkgutil --check-signature` and `spctl --assess`) before upload (status: todo)
- [ ] Document local and CI troubleshooting for signing/notarization failures (status: todo)

## P1 — Quality and Safety

### Slice 20 — Chunk boundary regression fixtures

- Status: done
- Owner: @caiowilson
- Difficulty: small
- Scope: `internal/indexing/chunk.go`, `internal/indexing/*_test.go`
- Priority: P1

#### Problem

Chunking behavior is not pinned down tightly enough before syntax-aware chunking changes.

#### Steps

- [x] Add Go fixture coverage for adjacent declarations and doc comments (status: done)
- [x] Add assertions for chunk start and end lines (status: done)
- [x] Add one non-Go fallback test proving line-based chunking still works (status: done)

Delivered by `91b6366` in `internal/indexing/chunk_test.go`.

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

- Status: in-progress
- Owner: @caiowilson
- Difficulty: small
- Scope: CI workflow, `internal/indexing`, `internal/mcp`
- Priority: P1

#### Steps

- [x] Add CI coverage reporting for `internal/indexing` and `internal/mcp` (status: done)
- [x] Set an initial floor that only blocks regressions for those packages (status: done)
- [ ] Document local coverage command in contributor-facing docs (status: todo)

CI reporting was delivered by `0c4f77e` and `.github/workflows/coverage-internal.yml`; the documentation follow-up remains open.

### Slice 22 — `repo_context` golden output tests

- Status: done
- Owner: @caiowilson
- Difficulty: small
- Scope: `internal/mcp/context_tool_test.go`
- Priority: P1

#### Steps

- [x] Add stable coverage for `intent: navigate` output shape (status: done)
- [x] Add stable coverage for `intent: implement` and `intent: review` output shapes (status: done)
- [x] Add explicit-mode contract assertions for `full`, `outline`, and `summary` (status: done)

Covered in `internal/mcp/context_tool_test.go`, including intent routing, explicit-mode precedence, auto mode, and suggested follow-up calls.

### Slice 24 — Deprecate `README-old.md` safely

- Status: done
- Owner: @caiowilson
- Difficulty: small
- Scope: `README-old.md`, docs index
- Priority: P1

#### Steps

- [x] Decide whether to archive, delete, or hard-deprecate `README-old.md` (status: done)
- [x] If retained, keep a top-of-file notice pointing to `README.md` (status: done)
- [x] Remove any remaining links that direct readers to the old file (status: done)

Delivered by `d3a26a5`; the only remaining non-backlog reference is a Makefile audit helper.

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

### Slice 28 — Credential-free Claude workflow skills

- Status: todo
- Owner: @caiowilson
- Difficulty: medium
- Scope: `.claude-plugin/marketplace.json`, `plugins/memento-workflows/`, plugin docs and validation CI
- Priority: P1

#### Problem

Some users cannot or do not want to run an unsigned native MCP binary. Claude skills can still provide repeatable repository orientation, change review, and handoff workflows using Claude Code's built-in tools, without downloading an executable or requiring Apple signing credentials.

#### Boundary

This is a companion distribution, not a transparent replacement for the MCP server. It must not claim indexed retrieval, semantic vectors, MCP resources/prompts, automatic background indexing, cross-client support, or the server's structured durable-memory lifecycle.

#### Steps

- [ ] Add a separate `memento-workflows` marketplace plugin with skills only and no `.mcp.json`, hooks, native executables, or install-time downloads (status: todo)
- [ ] Add a `prime` skill that inspects repository instructions, manifests, Git state, and high-signal files with bounded built-in reads (status: todo)
- [ ] Add a `review-changes` skill grounded in changed paths and `git diff`, with focused reads and test discovery (status: todo)
- [ ] Add a `handoff` skill with an explicit, user-visible repo-local Markdown storage contract and no hidden writes (status: todo)
- [ ] Use narrow skill descriptions, progressive disclosure, and minimal pre-approved tool patterns (status: todo)
- [ ] Document the capability matrix for `memento-workflows` versus the full `memento` MCP plugin (status: todo)
- [ ] Add strict plugin validation, isolated marketplace installation, and skill smoke tests (status: todo)
- [ ] Decide after validation whether the same workflow skills should also ship inside the full `memento` plugin (status: todo)

## P2 — Capability Expansion

### Slice 15 — `repo_symbols` capability (delivered as `repo_outline`)

- Status: done
- Owner: @caiowilson
- Difficulty: medium
- Scope: `internal/mcp/` (new tool)
- Priority: P2

#### Steps

- [x] Return names, kinds, signatures, documentation, containers, and line ranges through `repo_outline` (status: done)
- [x] Implement Go symbol extraction via `go/ast` (status: done)
- [x] Implement JS/TS structural extraction (status: done)
- [x] Add a generic declaration fallback for unsupported languages (status: done)
- [x] Add structured and fallback tests (status: done)

Superseded by the richer `repo_outline` tool delivered in `3942370` for GitHub issue #24; a duplicate `repo_symbols` tool would add API surface without a distinct capability.

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

- Status: done
- Owner: @caiowilson
- Difficulty: small
- Scope: `internal/mcp/` (`py_semantic.go`)
- Priority: P3

#### Steps

- [x] Build Python import graph via regex (`import X`, `from X import Y`, relative imports) (status: done)
- [x] Wire it into `computeRelatedFiles` for `.py` files (status: done)
- [x] Add tests with sample Python import structures (status: done)

Delivered by `8c7865b` in `internal/mcp/py_semantic.go` and `internal/mcp/related_tools_test.go`.

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
