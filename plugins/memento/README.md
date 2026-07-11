# Memento MCP for Claude Code

This plugin starts Memento automatically for every Claude Code project where the plugin is enabled. It downloads the version-pinned prebuilt server for macOS, Linux, or Windows on first start, verifies its SHA-256 checksum, and caches it in Claude Code's persistent plugin data directory.

The supported targets are x64 and arm64 on macOS, Linux, and Windows. The first start requires access to the GitHub release; later starts use the verified cached binary and work offline.

From Claude Code:

```text
/plugin marketplace add caiowilson/MCP-memento
/plugin install memento@memento-mcp
/reload-plugins
/mcp
```

The bundled server is registered as `plugin:memento:memento`. Plugin-scoped MCP tools use names such as `mcp__plugin_memento_memento__repo_context`, and the prime prompt is `/mcp__plugin_memento_memento__prime`.

Update the marketplace and plugin, then reconnect it in the current session:

```text
/plugin marketplace update memento-mcp
/plugin update memento@memento-mcp
/reload-plugins
```

If first start fails, inspect Memento in `/plugin` and confirm the pinned GitHub release is reachable. A missing or corrupted cache is downloaded again automatically.

For local plugin development, validate and launch against a locally built server:

```bash
claude plugin validate --strict ./plugins/memento
make build
MEMENTO_PLUGIN_BINARY="$PWD/bin/memento-mcp" \
  claude --plugin-dir ./plugins/memento
```
