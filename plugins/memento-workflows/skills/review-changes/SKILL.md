---
name: review-changes
description: Review current staged, unstaged, and untracked Git changes for correctness and regression risk, grounded in changed paths, diffs, focused file reads, and nearby test discovery. Use when the user asks to review local changes or a working-tree diff; do not use for broad repository audits.
allowed-tools: Read, Glob, Grep, Bash(git status *), Bash(git diff --no-ext-diff *), Bash(git diff --cached --no-ext-diff *), Bash(git ls-files *)
---

# Review local changes

Perform a read-only review. Never edit files or Git state.

1. Establish scope with `git status --short`, then inspect staged and unstaged name-status and diff statistics separately with `--no-ext-diff`.
2. Read the actual staged and unstaged diffs with `--no-ext-diff`, batching by changed path if needed. Never enable external diff drivers. Read relevant untracked files directly; do not infer their content from filenames.
3. Prioritize behavior, security boundaries, public contracts, data loss, concurrency, and compatibility. Read only the surrounding production code needed to validate a concern.
4. Discover nearby tests through changed-file naming, root manifests, test scripts, and CI configuration. Identify the smallest relevant validation commands, but do not claim they ran unless they did.
5. Check whether tests cover the changed behavior and important failure paths. Avoid style-only findings unless they create a concrete maintenance or correctness risk.

Present findings first, ordered by severity. Each finding must name a path and location, explain the failure mode, and connect it to diff evidence. Then list open questions and validation gaps. If there are no findings, say so directly and state residual risks.
