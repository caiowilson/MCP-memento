![Memento MCP by Caio Wilson](../../assets/memento-mcp-banner.jpg)

# Memento MCP plugin

This directory is the shared source for the native Codex and Claude Code plugins. Both hosts use the same version-pinned launcher, checksum verification, platform selection, persistent cache, and `MEMENTO_PLUGIN_BINARY` development override. Their manifests and lifecycle commands remain provider-specific.

The supported targets are x64 and arm64 on macOS, Linux, and Windows. First start requires Node.js on `PATH` and access to the pinned GitHub release. Later starts rehash the cached binary and work offline. Provider plugin data stores only the executable, checksum, and provider-specific update state. The server always keeps durable notes in the shared canonical `~/.memento-mcp/repos/<repo-id>/` store, using the same repository identity as standalone Memento clients; Codex does not create a separate memory silo.

## Codex

Install the repository marketplace and plugin from a terminal:

```bash
codex plugin marketplace add caiowilson/MCP-memento
codex plugin add memento@memento-mcp
```

Start a new Codex task after installation, then use `/mcp` or `codex mcp list` to verify the `memento` STDIO server. The Codex manifest supplies `${PLUGIN_ROOT}` and `${PLUGIN_DATA}` to the shared launcher. Codex does not run the Claude-only background marketplace updater; the launcher instead reports the Codex plugin as the managing host, so update notices point at `codex plugin marketplace upgrade memento-mcp` and `memento-mcp update`/`memento-mcp setup` refuse to mutate the plugin cache. The launcher also passes the Codex workspace directory to the server as `MEMENTO_PROJECT_DIR`, so `memory_list` and `memory_search` work without an explicit `root`.

Refresh the marketplace and reinstall the current plugin package to update it, then start a new task:

```bash
codex plugin marketplace upgrade memento-mcp
codex plugin add memento@memento-mcp
```

For local development, add this checkout as the marketplace source, build the server, and start Codex with the binary override:

```bash
codex plugin marketplace add "$PWD"
codex plugin add memento@memento-mcp
make build
MEMENTO_PLUGIN_BINARY="$PWD/bin/memento-mcp" codex
```

If first start fails, confirm `node` is on `PATH`, the pinned GitHub release is reachable, and the host is one of the supported OS/CPU pairs. A missing or corrupted cache is downloaded again automatically. Use a new task after every install or update so Codex loads the new plugin snapshot.

The layout and commands follow the official [Codex plugin packaging](https://developers.openai.com/plugins/build/plugins) and [local MCP](https://developers.openai.com/codex/mcp) documentation.

## Claude Code

From Claude Code:

```text
/plugin marketplace add caiowilson/MCP-memento
/plugin install memento@memento-mcp
/reload-plugins
/mcp
```

The bundled server is registered as `plugin:memento:memento`. Plugin-scoped MCP tools use names such as `mcp__plugin_memento_memento__repo_context`, and the prime prompt is `/mcp__plugin_memento_memento__prime`.

New marketplace installs check for a Memento plugin update in the background once per 24 hours, after the verified MCP server has started. The check refreshes the marketplace and stages the package update; it never replaces the binary of the active task. Reload plugins or start a new task to activate a staged plugin update. Legacy marketplace installs remain opted out until you create `$HOME/.memento-mcp/marketplace-update.json` with `{ "autoUpdate": true }`. Network, timeout, or update-command failures leave the working server untouched.

To update immediately, update the marketplace and plugin, then reload plugins or start a new task:

```text
/plugin marketplace update memento-mcp
/plugin update memento@memento-mcp
/reload-plugins
```

If first start fails, inspect Memento in `/plugin` and confirm the pinned GitHub release is reachable. A missing or corrupted cache is downloaded again automatically. An existing task-local Memento snapshot that predates staged updates must be removed and replaced once.

For local Claude Code development, validate and launch against a locally built server:

```bash
claude plugin validate --strict ./plugins/memento
make build
MEMENTO_PLUGIN_BINARY="$PWD/bin/memento-mcp" \
  claude --plugin-dir ./plugins/memento
```
