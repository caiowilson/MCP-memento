# Generic MCP Clients

This page covers non-VS Code usage for `memento-mcp`.

## Quick start

Build the binary from the repo root:

```bash
go build -o ./bin/memento-mcp ./cmd/server
```

Print a generic MCP config snippet:

```bash
./bin/memento-mcp print-config
```

Print copyable LLM guidance:

```bash
./bin/memento-mcp print-guidance
```

Show built-in help:

```bash
./bin/memento-mcp help
```

## Claude Code

### Plugin installation (recommended)

The official plugin removes the per-machine binary path and MCP configuration steps. In Claude Code, run:

```text
/plugin marketplace add caiowilson/MCP-memento
/plugin install memento@memento-mcp
/reload-plugins
/mcp
```

The marketplace and plugin are maintained in this repository. When enabled, the plugin MCP server starts automatically at the beginning of a session. Installing, enabling, disabling, or updating it during a running session requires `/reload-plugins` before the MCP server is connected or disconnected. `/mcp` lists it as a plugin-provided server.

The plugin pins its launcher and server to the same version. On first start, the launcher detects x64 or arm64 macOS, Linux, or Windows, downloads that version's prebuilt server from the GitHub release, verifies its SHA-256 sidecar, and stores both under `${CLAUDE_PLUGIN_DATA}`. Every later start rehashes the cached executable. A valid cache works offline; a missing or invalid cache requires GitHub access so the launcher can replace it. Go is not required for plugin users.

Update an installed plugin with:

```text
/plugin marketplace update memento-mcp
/plugin update memento@memento-mcp
/reload-plugins
```

Plugin MCP names include the plugin and server namespace. Examples are `mcp__plugin_memento_memento__repo_context` and `/mcp__plugin_memento_memento__prime`. Manually configured servers retain shorter names such as `mcp__memento__repo_context` and `/mcp__memento__prime`.

If first start fails, open `/plugin`, inspect the Memento error, and confirm the machine can reach the pinned release on GitHub. Unsupported operating-system or CPU combinations fail explicitly. For local plugin development, set `MEMENTO_PLUGIN_BINARY` to an existing server executable; normal marketplace installs should leave it unset.

### Manual one-command setup

Build memento, then run the following command from the project you want Claude Code to index. Replace the executable path with the absolute path to your build:

```bash
claude mcp add memento -- /absolute/path/to/MCP-memento/bin/memento-mcp
```

The command uses Claude Code's default `local` scope, so it applies only to the current project and stays out of version control. Claude Code passes that project's root to the server through `CLAUDE_PROJECT_DIR`; memento indexes it automatically.

### Shared manual project setup

To share the server configuration with a team, commit this `.mcp.json` at the project root:

```json
{
  "mcpServers": {
    "memento": {
      "type": "stdio",
      "command": "${CLAUDE_PROJECT_DIR:-.}/bin/memento-mcp",
      "args": [],
      "env": {}
    }
  }
}
```

The `${CLAUDE_PROJECT_DIR:-.}` default keeps the command valid while Claude Code prepares the server environment. This portable config expects the memento executable at `bin/memento-mcp` in every checkout, so provide it through the project's build or bootstrap process. If memento is installed elsewhere, replace `command` with its absolute path and keep that machine-specific config local instead of committing it.

Claude Code prompts each user to approve servers loaded from a project `.mcp.json` before first use. To verify either setup:

```bash
claude mcp list
```

Use `/mcp` inside Claude Code to inspect the connection or approve a pending project server.

## Claude Desktop

Add this stdio entry to the existing `mcpServers` object in `claude_desktop_config.json`, replacing the executable path, then restart Claude Desktop:

```json
{
  "mcpServers": {
    "memento": {
      "command": "/absolute/path/to/MCP-memento/bin/memento-mcp",
      "args": [],
      "env": {}
    }
  }
}
```

An empty `env` uses the same built-in indexing defaults shown by `./bin/memento-mcp print-config`. Claude Desktop treats a `command` entry as a local stdio server.

On macOS or WSL, import configured Claude Desktop servers into Claude Code with:

```bash
claude mcp add-from-claude-desktop
```

Select `memento` in the interactive importer, then verify it with `claude mcp list`. Desktop import is not available on native Windows or Linux.

## Recommended client config

`print-config` emits a generic `mcpServers` map that uses:

- stdio transport
- the current binary path as `command`
- `${workspaceFolder}` as `cwd`
- the default indexing environment variables

If your MCP client expects a different JSON shape, reuse the same values for `command`, `args`, `cwd`, `env`, and stdio transport.

## Workspace root detection

The server resolves its workspace root in this order:

1. Explicit `--root DIR` or client config
2. `CLAUDE_PROJECT_DIR` from Claude Code
3. MCP client `roots/list`, when the client advertises Roots support
4. Current working directory

Claude Code sessions therefore index the launched project without a manual `repo_switch_workspace` call. Use `repo_switch_workspace` only when you intentionally retarget the same MCP session to another repository.

## Optional semantic retrieval

Semantic retrieval is disabled by default. Install Ollama, run `ollama pull nomic-embed-text:v1.5`, and add the following environment variable to the client server entry to enable local hybrid retrieval:

```json
{
  "env": {
    "MEMENTO_SEMANTIC_ENABLED": "true"
  }
}
```

The MCP client must launch Memento in an environment that can reach the local Ollama process at `http://127.0.0.1:11434`. See `README.md#optional-semantic-retrieval` for model caching, fallback behavior, and tuning variables.

## Recommended LLM guidance

Use the output of `print-guidance` directly, or paste the following into client instructions:

```text
When using memento-mcp, start with repo_context and set intent to navigate, implement, or review.
Use repo_outline when you need signatures and file structure without implementation bodies.
Anchor durable notes to code when possible. Treat stale notes as evidence to verify, then call memory_verify or memory_tombstone after adjudication.
Omit mode unless you need to force a low-level output such as full, outline, or summary.
If repo_context returns suggestedNextCall, prefer following it for a deeper read without repeating context.
When you change repositories in the same MCP session, call repo_switch_workspace with the new root path instead of restarting.
Existing explicit mode calls still work, but new callers should prefer intent.
```

## Claude Code output limits

`repo_context`, `repo_read_file`, and `repo_search` default to compact responses to avoid Claude Code's MCP result warning near 10k tokens:

- `repo_context`: `maxTokens` defaults to `7000` using an approximate `ceil(UTF-8 bytes / 4)` estimator; `maxTotalBytes` remains a `32000` hard ceiling.
- `repo_read_file`: `maxBytes` defaults to `32000`.
- `repo_search`: each snippet is capped by `maxSnippetBytes`, default `500`.

The same three tools advertise `_meta["anthropic/maxResultSizeChars"] = 500000` for intentional large reads. Claude Code's own `MAX_MCP_OUTPUT_TOKENS` setting can still be lower than the server-side caps, so tune the tool arguments and the client setting together when you need larger context.

Set `MEMENTO_CONTEXT_MAX_TOKENS` to change the default token budget, or pass `maxTokens` to one `repo_context` call. Responses report `usedTokens`, `usedBytes`, and the estimator name under `limits`.

## Claude Code resources and prime prompt

Claude Code discovers Memento resources in the `@` autocomplete menu and MCP prompts in the `/` command menu. For a manually configured server named `memento`, examples are:

```text
@memento:note://memory/repo-overview
@memento:repo://file/README.md
/mcp__memento__prime
/mcp__memento__prime internal/mcp/server.go "workspace routing"
```

Replace `memento` in the prefix when your MCP server entry uses a different name. Prefer the autocomplete menu for note keys containing spaces or punctuation; Memento emits properly escaped URIs. The `prime` prompt takes optional positional `path` and `focus` arguments, includes bounded durable notes and project manifests, and adds a body-free outline when `path` is provided.

For the plugin installation, Claude Code scopes the server as `plugin:memento:memento`; the equivalent prompt is `/mcp__plugin_memento_memento__prime`, and plugin resource autocomplete uses that scoped server identity.

Only active notes appear as resources. Stale notes are labeled and should be verified; tombstones remain available through `memory_list` but are intentionally absent from `@` discovery. File resources are bounded and redacted, and sensitive/binary paths are rejected rather than attached.

Protocol-level verification:

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"resources/list","params":{}}' \
  '{"jsonrpc":"2.0","id":3,"method":"prompts/list","params":{}}' | ./bin/memento-mcp --root "$PWD"
```

The initialize result must advertise `resources` and `prompts`; subsequent results should include `note://memory/...` / `repo://file/...` descriptors and the `prime` prompt. Claude Code formats discovered prompts as `/mcp__<server-name>__<prompt-name>`.

## Example tool calls

Save a note anchored to a symbol:

```json
{
  "name": "memory_upsert",
  "arguments": {
    "key": "context-packing-contract",
    "text": "Oversized chunks are skipped so smaller candidates can still fit.",
    "anchors": [
      {
        "path": "internal/mcp/token_budget.go",
        "symbol": "contextBudget.tryAdd"
      }
    ]
  }
}
```

When `memory_search` returns `status: "stale"`, verify the code and use `memory_verify` if the note remains correct, or `memory_tombstone` for recoverable soft eviction. `memory_list` includes tombstones; `memory_gc` requires the full conservative eligibility policy described in `docs/README.md#durable-memory-lifecycle`.

Inspect a file's structure without reading its bodies:

```json
{
  "name": "repo_outline",
  "arguments": {
    "path": "internal/mcp/context_tool.go"
  }
}
```

Navigate:

```json
{
  "name": "repo_context",
  "arguments": {
    "path": "internal/mcp/context_tool.go",
    "intent": "navigate"
  }
}
```

Implement:

```json
{
  "name": "repo_context",
  "arguments": {
    "path": "internal/mcp/context_tool.go",
    "intent": "implement",
    "focus": "repoContextOutputSchema"
  }
}
```

Review:

```json
{
  "name": "repo_context",
  "arguments": {
    "path": "internal/mcp/context_tool.go",
    "intent": "review"
  }
}
```

Switch workspace without restart:

```json
{
  "name": "repo_switch_workspace",
  "arguments": {
    "path": "/absolute/path/to/another/repo",
    "reindexNow": true
  }
}
```
