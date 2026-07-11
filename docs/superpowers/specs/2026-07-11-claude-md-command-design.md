# `claude-md` CLI command — design

- Date: 2026-07-11
- Status: Approved

## Problem

The README's "Recommended workflow" section tells users to add a note in a project's `CLAUDE.md` (or a repo rule) so the guidance loads every session. Today that's a manual copy-paste step. We want a CLI command that does it for them.

## Decision

Add a new subcommand, `memento-mcp claude-md [--print-only]`, that upserts a Memento guidance block into `./CLAUDE.local.md` (relative to the caller's cwd).

- **Target file**: `CLAUDE.local.md`, not `CLAUDE.md`, while this command is still being refined. `CLAUDE.local.md` is typically personal/gitignored, so early iterations of the embedded guidance don't land in a committed, shared file.
- **No `--path`/workspace-root override in v1.** Always resolves against cwd. Cheap to add later; not requested now.
- **Content**: a new Go string constant containing the README's "Recommended workflow" heading, intro, and two bullets (prefer Memento memory; prime the repo index before reading whole files) — but not the trailing "To make this automatic…" sentence, since that line is instructing a human to add this note, not guidance for an agent. The constant is hand-kept in sync with the README section; no automated drift check (same tradeoff `print-guidance`'s `clientGuidanceText()` already accepts for its own content).
- **Idempotent upsert via sentinel comments**, mirroring how `upsertConfig` (in `setup.go`) already handles re-running `setup` against JSON client configs, adapted to markdown:
  - Block is wrapped in `<!-- memento-mcp:recommended-workflow:start -->` / `<!-- memento-mcp:recommended-workflow:end -->`.
  - If the markers aren't found in the file (including when the file doesn't exist yet), append the block at the end — one blank line before it if the file already has content, no leading blank line if the file is new/empty.
  - If the markers are found, replace everything from start-marker to end-marker (inclusive) with the fresh block. Content outside the markers is untouched. This is what makes rerunning safe after the embedded text changes during refinement — no duplicate blocks.
  - Ensure exactly one trailing newline on write.
- **`--print-only`**: print the block to stdout; do not touch disk. Mirrors `setup --print-only`.
- **Confirmation output**: on a real write, print a single `✓ CLAUDE.local.md   wrote <path>` line, matching `configureClients`'s existing style in `setup.go`.

## Why not alternatives

- **Naive "contains text?" dedup instead of markers**: fragile — the moment the embedded constant changes (expected, since this is "in development, refinement"), the substring check stops matching the old block and a second, slightly different copy gets appended. Sentinel markers avoid this by keying off a stable wrapper, not the content inside it.
- **Overwrite the whole file**: rejected — `CLAUDE.local.md` may hold unrelated personal notes; clobbering it violates the same "don't touch anything else" expectation as the earlier README-only change.
- **Fold into `setup` as a flag**: rejected per user preference — `setup` configures global, user-level MCP client configs (VS Code, Cursor, Claude Desktop, Windsurf under the user's home directory); this command writes a project-local file relative to cwd. Different enough to warrant its own subcommand, own help text.

## Components

- `internal/app/claudemd.go` (new): the guidance-block constant, marker constants, the upsert algorithm (`buildClaudeLocalMD(existing []byte, block string) []byte` or similar pure function), and `runClaudeMD(args []string, stdout, stderr io.Writer) error` as the command entry point (parses `--print-only`, resolves `./CLAUDE.local.md` via cwd, reads/writes, prints confirmation).
- `internal/app/cli.go`: add a `"claude-md"` case to `handleCLICommand`'s switch, calling `runClaudeMD`; add a line to `cliHelpText()`.

## Data flow

1. `handleCLICommand` sees `args[0] == "claude-md"`, calls `runClaudeMD(args[1:], stdout, stderr)`.
2. `runClaudeMD` resolves `path = filepath.Join(cwd, "CLAUDE.local.md")` via `os.Getwd()`.
3. Reads existing bytes at `path` (missing file treated as empty content, not an error).
4. Computes the new file content via the pure upsert function (append-or-replace-between-markers, as above).
5. If `--print-only`: write just the block to stdout, return.
6. Else: `os.WriteFile` the computed content (0o644), print the confirmation line.

## Error handling

- `os.Getwd()` or read/write errors (permissions, etc.) other than "file does not exist" propagate up and are printed as `claude-md: <err>` on stderr with exit code 1, matching the existing `setup` error path in `handleCLICommand`.

## Testing

New `internal/app/claudemd_test.go`, using `t.TempDir()` + `os.Chdir` (or by testing the pure upsert function directly against byte slices where possible, avoiding cwd mutation in most cases):

- Creates `CLAUDE.local.md` with just the marked block when the file doesn't exist.
- Appends the block (with a blank-line separator) when the file exists with unrelated content and no markers.
- Rerunning after the file already has the markers replaces the block in place — no duplication — covering the "content changes during refinement" case.
- `--print-only` writes nothing to disk and prints the block to stdout.
- `internal/app/cli_test.go`: dispatch test confirming `handleCLICommand([]string{"claude-md"}, ...)` is handled and `cliHelpText()` mentions it.

## Out of scope

- No `--path` override, no `CLAUDE_PROJECT_DIR` fallback — cwd only, for now.
- No change to `CLAUDE.md` (the shared/committed file) — this command only ever targets `CLAUDE.local.md`.
- No Makefile target — this is a feature of the installed `memento-mcp` binary, run from within whatever project the user wants guidance in; a Makefile target in this repo wouldn't reach those other projects.
- No automated sync between the README section and the new Go constant.
