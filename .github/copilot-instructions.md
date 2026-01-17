# MCP Tooling Policy (VS Code)

Use MCP tools to ground answers in the repo:

- For any code question, call `repo_context` for the active file first.
- If more context is needed, call `repo_related_files`, then `repo_read_file` or `repo_search`.
- If unsure, say so and call a tool rather than guessing.
- If results look stale or empty, call `repo_index_status` or `repo_index_debug`, then `repo_reindex`.

Tool names use underscore style (e.g., `repo_context`, `repo_read_file`, `memory_search`).

## Tool notes

### `repo_index_status`

- Use to confirm the automatic indexer is ready (and whether indexing is partial or errored).
- If `ready` is false or results are empty/stale: call `repo_index_debug`, then `repo_reindex`.
