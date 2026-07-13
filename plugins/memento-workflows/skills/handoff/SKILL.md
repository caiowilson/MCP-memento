---
name: handoff
description: Prepare a concise repository-work handoff and write it only after explicit confirmation to the visible repository-root Markdown file MEMENTO_HANDOFF.md. Use when the user asks to capture, save, or pass current work to a later session or collaborator; never perform hidden or automatic writes.
allowed-tools: Read, Glob, AskUserQuestion, Bash(git rev-parse *), Bash(git status *), Bash(git diff --no-ext-diff *), Bash(git diff --cached --no-ext-diff *), Bash(test -L *), Bash(test -e *), Bash(test -f *)
---

# Create a visible handoff

Use this storage contract:

- Default target: `<repository-root>/MEMENTO_HANDOFF.md`.
- Format: plain Markdown with the sections below.
- Operation: create the file, or replace its existing handoff after reading it.
- Visibility: never use hidden files, `.claude/`, Git metadata, caches, MCP memory, or any second storage location.
- Filesystem boundary: abort if the target is a symbolic link or, when it exists, is not a regular file. Never follow a link for reading or writing.

Gather evidence with read-only tools: repository instructions, `git status --short`, staged and unstaged diff statistics with `--no-ext-diff`, and focused reads of changed files. Never enable external diff drivers. Draft:

```markdown
# Repository Handoff

## Goal
## Done
## Relevant files
## Decisions
## Constraints
## Tried and learned
## Next steps
## Validate
## Risks
```

Before reading an existing target, check it without following links: reject it when `test -L` succeeds; when `test -e` succeeds, require `test -f` to succeed. Show the complete draft, exact target path, and whether the operation will create or replace the file. Ask the user to confirm that exact write. Do not invoke Write, Edit, Bash redirection, or any other mutating tool before an affirmative response.

After confirmation, repeat the link and regular-file checks immediately before writing. Abort if the target changed or cannot be verified safely. Otherwise write only the approved Markdown to the approved repo-local path, read it back, and report success. Never modify another file, hide or ignore the handoff, commit it, or perform an implicit write. If confirmation is declined, return the draft in chat only.
