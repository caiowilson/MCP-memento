---
name: prime
description: Orient to an unfamiliar or resumed repository through bounded reads of repo instructions, root manifests, Git state, and a few high-signal files. Use when the user asks to prime, orient to, understand, or resume work in a repository; do not use for semantic search or background indexing.
allowed-tools: Read, Glob, Grep, Bash(git rev-parse *), Bash(git status *), Bash(git log *), Bash(git diff --no-ext-diff *), Bash(git diff --cached --no-ext-diff *)
---

# Prime a repository

Build a compact, evidence-based orientation. Never write files.

1. Locate the repository root with `git rev-parse --show-toplevel` and stay within it.
2. Read up to four root instruction files when present: `CLAUDE.md`, `AGENTS.md`, `.claude/CLAUDE.md`, and the nearest equivalent named by the repository.
3. Read up to six root manifests or task entrypoints, prioritizing files such as `go.mod`, `package.json`, `pyproject.toml`, `Cargo.toml`, `Makefile`, and `Taskfile.yml`.
4. Inspect `git status --short`, the latest five one-line commits, and `git diff --no-ext-diff --stat` plus `git diff --cached --no-ext-diff --stat`. Never enable external diff drivers or mutate Git state.
5. Read up to four high-signal files selected from the root README, TODO or roadmap, documentation index, and files implicated by current Git changes. Avoid secrets, dependencies, generated output, and broad tree dumps.

Limit each initial file read to about 200 lines. Read one additional targeted segment only when it resolves an important gap.

Report the repository purpose, stack, instructions, common validation commands, current work, and concrete risks or unknowns. Cite paths for important claims and distinguish evidence from inference.
