![Memento MCP by Caio Wilson](../../assets/memento-mcp-banner.jpg)

# Memento MCP for Claude Code

This plugin starts Memento automatically for every Claude Code project where the plugin is enabled. It downloads the version-pinned prebuilt server for macOS, Linux, or Windows on first start, verifies its SHA-256 checksum, and caches it in Claude Code's persistent plugin data directory.

The supported targets are x64 and arm64 on macOS, Linux, and Windows. The first start requires access to the GitHub release; later starts use the verified cached binary and work offline.

New marketplace installs check for a Memento plugin update in the background once per 24 hours, after the verified MCP server has started. The check refreshes the marketplace and stages the package update; it never replaces the binary of the active task. Reload plugins or start a new task to activate a staged plugin update. Legacy marketplace installs remain opted out until you create `$HOME/.memento-mcp/marketplace-update.json` with `{ "autoUpdate": true }`. Network, timeout, or update-command failures leave the working server untouched.

From Claude Code:

```text
/plugin marketplace add caiowilson/MCP-memento
/plugin install memento@memento-mcp
/reload-plugins
/mcp
```

The bundled server is registered as `plugin:memento:memento`. Plugin-scoped MCP tools use names such as `mcp__plugin_memento_memento__repo_context`, and the prime prompt is `/mcp__plugin_memento_memento__prime`.

To update immediately, update the marketplace and plugin, then reload plugins or start a new task:

```text
/plugin marketplace update memento-mcp
/plugin update memento@memento-mcp
/reload-plugins
```

If first start fails, inspect Memento in `/plugin` and confirm the pinned GitHub release is reachable. A missing or corrupted cache is downloaded again automatically. An existing task-local Memento snapshot that predates staged updates must be removed and replaced once.

For local plugin development, validate and launch against a locally built server:

```bash
claude plugin validate --strict ./plugins/memento
make build
MEMENTO_PLUGIN_BINARY="$PWD/bin/memento-mcp" \
  claude --plugin-dir ./plugins/memento
```
