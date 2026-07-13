# Memento Workflows for Claude Code

This companion plugin provides three credential-free Claude Code skills using only built-in tools. It has no MCP configuration, hooks, native executable, install script, or runtime download.

Install it from the Memento marketplace:

```text
/plugin marketplace add caiowilson/MCP-memento
/plugin install memento-workflows@memento-mcp
/reload-plugins
```

Ask Claude naturally or invoke the namespaced skills directly:

- `/memento-workflows:prime` performs bounded repository orientation without writing files.
- `/memento-workflows:review-changes` reviews local Git changes and discovers relevant tests without writing files.
- `/memento-workflows:handoff` drafts a handoff and asks before writing it to the visible repository-root file `MEMENTO_HANDOFF.md`.

## Capability matrix

| Capability | `memento-workflows` | Full `memento` MCP plugin |
| --- | --- | --- |
| Repository orientation | Bounded built-in file and Git reads | Indexed repository context |
| Change review | Local changed paths, diffs, and test discovery | MCP repository tools and prompts |
| Handoff storage | User-confirmed `MEMENTO_HANDOFF.md` | Structured durable memory tools |
| Semantic vectors and indexed retrieval | No | Yes |
| MCP resources and prompts | No | Yes |
| Automatic background indexing | No | Yes |
| Cross-client MCP support | No; Claude Code skills only | Yes |
| Native executable or runtime download | None | Verified release binary on first start |

The workflow plugin is not a transparent replacement for the MCP server. It does not provide semantic search, persistent indexed memory, or background services.

## Distribution decision

These skills remain exclusive to `memento-workflows` for now. The full `memento` plugin already exposes an indexed `prime` prompt and structured MCP workflows; duplicating the skills there would blur the capability boundary for users who install both plugins. Revisit this only if real usage shows a distinct full-plugin workflow that does not duplicate the MCP surface.

For local development, validate the plugin with:

```bash
claude plugin validate --strict ./plugins/memento-workflows
```
