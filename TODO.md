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
