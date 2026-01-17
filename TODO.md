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

- Status: in-progress
- Owner: codex

### Steps

- [ ] Add `repo.clear_index` / `memory.clear` tools (owner: codex) (status: in-progress)
- [ ] Add an index/debug tool (counts, filters applied, last error) (owner: codex) (status: in-progress)
