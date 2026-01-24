# Memento MCP — VS Code Extension (WIP)

This is a companion extension for `memento-mcp` (the Go MCP server).

What it does:

- Downloads a platform-specific `memento-mcp` binary into VS Code extension storage.
- Generates an MCP server config snippet you can paste into your VS Code `mcp.json`.
- Opens or creates `mcp.json` in your workspace.
- Shows a status bar item with the resolved server path and install state.

## Development

From `vscode-extension/`:

```bash
npm install
npm run build
```

Then in VS Code:

- Open the `vscode-extension/` folder in VS Code.
- Press `F5` (uses the included `.vscode/launch.json`), or run `npm run watch` and reload the Extension Development Host.
- Use the commands:
  - `Memento MCP: Install Server Binary`
  - `Memento MCP: Open or Create mcp.json`
  - `Memento MCP: Open MCP Config Snippet`
  - `Memento MCP: Copy MCP Config Snippet`

On first activation, the extension offers quick actions to install the server or open/copy config snippets.

## Settings

- `mementoMcp.githubRepo` (default: `caiowilson/MCP-memento`)
- `mementoMcp.releaseTag` (default: `server/latest`)
- `mementoMcp.serverPath` (optional override)
- `mementoMcp.preferWorkspaceBinary` (default: `true`)

## Releases expectation

This extension expects GitHub Releases to include raw (uncompressed) binary assets named like:

- `memento-mcp_darwin_arm64`
- `memento-mcp_darwin_x64`
- `memento-mcp_linux_arm64`
- `memento-mcp_linux_x64`
- `memento-mcp_windows_x64.exe`

If no matching release asset exists, the install command will tell you what it looked for.

## Release tags

- Server releases: `server/vX.Y.Z` (with `server/latest` kept in sync)
- Extension releases: `extension/vA.B.C`
