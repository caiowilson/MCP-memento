# Technical Debt Remediation Backlog

Thin-slice remediation tracker for code, tests, and documentation.

## Tracking

- Status: `todo` | `in-progress` | `done` | `blocked`
- Ease of Remediation: `1` trivial to `5` complex
- Impact: `1` minimal to `5` critical
- Risk: `🟢 Low` | `🟡 Medium` | `🔴 High`
- Convention: keep slices independently shippable; prefer a follow-up slice over a broad mixed change

## Current Baseline

- Go validation: `go test ./...` passes
- VS Code extension validation: `npm test` passes in `vscode-extension/`
- Main debt themes: chunk quality, change-focused context, test depth, doc drift

## Summary Table

| ID         | Area  | Overview                                                                        | Ease | Impact | Risk      |
| ---------- | ----- | ------------------------------------------------------------------------------- | ---: | -----: | --------- |
| TD-CODE-01 | Code  | Add chunk-boundary regression fixtures before changing chunking logic           |    2 |      4 | 🔴 High   |
| TD-CODE-02 | Code  | Implement Go top-level declaration chunk boundaries                             |    3 |      5 | 🔴 High   |
| TD-CODE-03 | Code  | Add `repo_diff_context` MVP for explicit file paths only                        |    3 |      4 | 🟡 Medium |
| TD-CODE-04 | Code  | Add dirty-worktree auto-detection to `repo_diff_context`                        |    3 |      4 | 🟡 Medium |
| TD-TEST-01 | Tests | Add package-level coverage reporting for `internal/indexing` and `internal/mcp` |    2 |      4 | 🟡 Medium |
| TD-TEST-02 | Tests | Add golden tests for `repo_context` intent and mode output shapes               |    2 |      4 | 🟡 Medium |
| TD-TEST-03 | Tests | Expand VS Code extension tests for config merge and path resolution             |    2 |      3 | 🟡 Medium |
| TD-DOC-01  | Docs  | Remove stale scaffold/WIP language from docs landing pages                      |    1 |      3 | 🟡 Medium |
| TD-DOC-02  | Docs  | Deprecate or archive `README-old.md`                                            |    1 |      3 | 🟡 Medium |
| TD-DOC-03  | Docs  | Consolidate config and LLM guidance into a single canonical source              |    2 |      3 | 🟡 Medium |

## TD-CODE-01 — Chunk Boundary Regression Fixtures

- Status: `todo`
- Ease of Remediation: `2`
- Impact: `4`
- Risk: `🔴 High`

### Overview

Current chunking in `internal/indexing/chunk.go` is line- and byte-based, so behavior can change accidentally during remediation.

### Explanation

Before changing chunking logic, lock down the current and desired behavior with fixture-driven tests. This reduces the chance of a context-quality fix creating silent regressions.

### Requirements

- Representative Go fixture files with multiple top-level declarations
- One non-Go fixture proving fallback behavior remains supported

### Implementation Steps

1. Add test fixtures covering adjacent functions, doc comments, and long declarations.
2. Add assertions for chunk start and end lines.
3. Add one fallback test for unknown-language line-based chunking.

### Testing

- Run `go test ./internal/indexing -run Chunk`

## TD-CODE-02 — Go Syntax-Aware Chunking MVP

- Status: `todo`
- Ease of Remediation: `3`
- Impact: `5`
- Risk: `🔴 High`

### Overview

Go files should chunk on top-level declaration boundaries instead of arbitrary line limits where possible.

### Explanation

This is the smallest production change that materially improves `repo_context` quality without changing the external tool contract.

### Requirements

- TD-CODE-01 completed first
- `go/ast`-based declaration boundary detection limited to top-level declarations

### Implementation Steps

1. Parse Go files in `ChunkFile` or a Go-specific helper.
2. Build chunks from declaration spans while still enforcing max byte limits.
3. Fall back to current logic when parsing fails or a declaration exceeds limits.

### Testing

- Run `go test ./internal/indexing`
- Re-run any `repo_context` tests affected by chunk ordering or boundaries

## TD-CODE-03 — `repo_diff_context` MVP With Explicit Paths

- Status: `todo`
- Ease of Remediation: `3`
- Impact: `4`
- Risk: `🟡 Medium`

### Overview

Add a narrow first version of `repo_diff_context` that accepts explicit paths and returns bounded nearby context.

### Explanation

This avoids mixing Git status detection, diff parsing, and response design in one change. The first slice should prove the tool contract and response shape only.

### Requirements

- Tool schema defined in `internal/mcp/`
- Reuse existing chunk loading and related-file selection where practical

### Implementation Steps

1. Add a new MCP tool accepting a list of repo-relative paths.
2. Return chunks from those files only, with a compact summary block.
3. Keep automatic Git detection out of this slice.

### Testing

- Run `go test ./internal/mcp -run DiffContext`

## TD-CODE-04 — Dirty-Worktree Auto-Detection For `repo_diff_context`

- Status: `todo`
- Ease of Remediation: `3`
- Impact: `4`
- Risk: `🟡 Medium`

### Overview

Teach `repo_diff_context` to discover changed paths automatically in a dirty Git worktree.

### Explanation

This completes the edit-review workflow value of the new tool after the explicit-path contract is already proven.

### Requirements

- TD-CODE-03 completed first
- Reusable Git status parsing from existing indexing logic where possible

### Implementation Steps

1. Detect changed files with Git status when no explicit paths are provided.
2. Filter deleted files out of chunk loading.
3. Include a brief diff summary in the response.

### Testing

- Run `go test ./internal/mcp -run DiffContext`
- Add a dirty-worktree fixture or temporary Git repo integration test

## TD-TEST-01 — Package Coverage Reporting

- Status: `todo`
- Ease of Remediation: `2`
- Impact: `4`
- Risk: `🟡 Medium`

### Overview

The repo needs visible coverage signals for the highest-risk packages, not just green test runs.

### Explanation

`internal/indexing` and `internal/mcp` carry most of the correctness risk. Coverage reporting here makes test debt visible without requiring an immediate repo-wide threshold.

### Requirements

- CI workflow location identified
- Initial threshold policy agreed for package-level reporting only

### Implementation Steps

1. Add coverage collection for `internal/indexing` and `internal/mcp`.
2. Publish the percentage in CI output.
3. Fail only if coverage drops below the initial floor for those packages.

### Testing

- Run `go test ./... -cover`
- Validate the CI output on a branch or local workflow dry run

## TD-TEST-02 — `repo_context` Golden Output Tests

- Status: `todo`
- Ease of Remediation: `2`
- Impact: `4`
- Risk: `🟡 Medium`

### Overview

`repo_context` has multiple modes and intents, but the response shape is only partially pinned down by tests.

### Explanation

Golden-style tests for `navigate`, `implement`, `review`, and explicit `mode` outputs reduce regression risk when chunking and ranking logic evolve.

### Requirements

- Small stable fixture repo
- Output normalization for non-deterministic fields if needed

### Implementation Steps

1. Add stable fixture inputs under test temp setup.
2. Capture expected output shape for each intent or mode.
3. Assert only the stable contract fields needed by callers.

### Testing

- Run `go test ./internal/mcp -run Context`

## TD-TEST-03 — VS Code Extension Config Tests

- Status: `todo`
- Ease of Remediation: `2`
- Impact: `3`
- Risk: `🟡 Medium`

### Overview

The extension currently has narrow test coverage relative to its configuration behavior.

### Explanation

The smallest high-value additions are path resolution and MCP config merge scenarios because those are user-facing and easy to regress.

### Requirements

- Reusable extension test helpers
- Clear separation between pure config logic and VS Code host behavior

### Implementation Steps

1. Add tests for workspace-binary preference and explicit server-path override.
2. Add tests for merge behavior when an MCP config already exists.
3. Keep installer network behavior out of this slice.

### Testing

- Run `npm test` in `vscode-extension/`

## TD-DOC-01 — Remove Stale WIP Messaging

- Status: `todo`
- Ease of Remediation: `1`
- Impact: `3`
- Risk: `🟡 Medium`

### Overview

Some docs still describe the repository as a scaffold or placeholder despite the server and tests being functional.

### Explanation

This creates avoidable trust and onboarding friction. The first doc slice should only align wording with current reality.

### Requirements

- Review `README.md`, `docs/README.md`, and `docs/vscode.md`

### Implementation Steps

1. Remove or rewrite outdated scaffold and WIP language.
2. Keep scope limited to accuracy fixes.
3. Do not restructure the doc set in this slice.

### Testing

- Manual read-through of the updated pages
- Verify linked commands still exist

## TD-DOC-02 — Deprecate Or Archive `README-old.md`

- Status: `todo`
- Ease of Remediation: `1`
- Impact: `3`
- Risk: `🟡 Medium`

### Overview

An older top-level README is still present and can compete with the current documentation set.

### Explanation

The smallest fix is to either remove it, move it under an archive location, or label it clearly as historical.

### Requirements

- Decide whether the file has any remaining historical value

### Implementation Steps

1. Choose archive, deprecate, or delete.
2. If retained, add a top-of-file notice pointing to `README.md`.
3. Remove any links that still direct users there.

### Testing

- Search for references to `README-old.md`
- Verify the docs index points only to canonical entry points

## TD-DOC-03 — Canonicalize Config And LLM Guidance

- Status: `todo`
- Ease of Remediation: `2`
- Impact: `3`
- Risk: `🟡 Medium`

### Overview

Config snippets and LLM guidance appear in multiple docs and can drift.

### Explanation

Pick one canonical source and reduce the others to short references or synchronized excerpts.

### Requirements

- Inventory current guidance in `README.md`, `docs/clients.md`, `docs/vscode.md`, and `vscode-extension/README.md`

### Implementation Steps

1. Choose the canonical guidance page.
2. Shorten duplicate sections in the other docs.
3. Add cross-links instead of repeating full guidance blocks.

### Testing

- Manual comparison of all guidance pages after update
- Verify examples still match current tool names and arguments
