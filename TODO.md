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

- Status: todo
- Owner: unassigned

### Steps

- [ ] Implement explicit allowlist (Go + high-signal files) and denylist (secrets/binaries) (owner: unassigned) (status: todo)
- [ ] Document default include/exclude rules (owner: unassigned) (status: todo)

## Slice 3 — Git-first incremental reindex

- Status: todo
- Owner: unassigned

### Steps

- [ ] Detect git worktree and use `git status --porcelain -z --untracked-files=all` (owner: unassigned) (status: todo)
- [ ] Debounce and re-index only changed paths (owner: unassigned) (status: todo)

## Slice 4 — Filesystem watcher fallback

- Status: todo
- Owner: unassigned

### Steps

- [ ] Add fs watcher for non-git repos (create/modify/delete/rename) (owner: unassigned) (status: todo)
- [ ] Debounce and re-index touched paths (owner: unassigned) (status: todo)

## Slice 5 — Go semantic freshness

- Status: todo
- Owner: unassigned

### Steps

- [ ] Invalidate go/types cache on `*.go`, `go.mod`, `go.sum` changes (owner: unassigned) (status: todo)
- [ ] Rebuild semantic index in the background (owner: unassigned) (status: todo)

## Slice 6 — Context quality + hard limits

- Status: todo
- Owner: unassigned

### Steps

- [ ] Add total byte limits + clamping metadata to `repo.context` (owner: unassigned) (status: todo)
- [ ] Improve chunk scoring (focus hits > semantic edges > imports > same-dir) (owner: unassigned) (status: todo)

## Slice 7 — Ops/admin tools

- Status: todo
- Owner: unassigned

### Steps

- [ ] Add `repo.clear_index` / `memory.clear` tools (owner: unassigned) (status: todo)
- [ ] Add an index/debug tool (counts, filters applied, last error) (owner: unassigned) (status: todo)
