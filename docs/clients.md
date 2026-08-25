# Client Configuration and LLM Guidance

This is the canonical page for configuring `memento-mcp` in Claude Code, Claude Desktop, ChatGPT/Codex, VS Code, and other MCP clients, and for teaching an LLM how to use the server. Client-specific pages should link here instead of copying configuration or agent instructions.

## Quick start

Install the verified standalone release and automatically configure detected Codex and Claude Code CLIs:

```bash
curl -fsSL https://github.com/caiowilson/MCP-memento/releases/download/server%2Flatest/install.sh | sh
```

For explicit client selection, pass arguments through the shell:

```bash
curl -fsSL https://github.com/caiowilson/MCP-memento/releases/download/server%2Flatest/install.sh | sh -s -- --clients codex,claude
```

Build the binary from the repo root:

```bash
make build
```

Let Memento detect supported clients and write their configuration, or preview the proposed changes:

```bash
./bin/memento-mcp setup
./bin/memento-mcp setup --print-only
```

For a deterministic non-interactive target, repeat `--client` or pass a comma-separated `--clients` list. Supported values are `codex`, `claude`/`claude-code`, `vscode`, `cursor`, `claude-desktop`, and `windsurf`. For example:

```bash
./bin/memento-mcp setup --client=vscode --client=cursor
./bin/memento-mcp setup --clients=codex,claude
```

Setup validates all selected targets before mutation, preserves existing server entries, refuses malformed JSON, writes JSON atomically, and requires `--force` before replacing a CLI registration that points to another executable. Validate the installed binary and exact registrations without changing them:

```bash
./bin/memento-mcp doctor --clients=codex,claude
```

Print a generic MCP config snippet:

```bash
./bin/memento-mcp print-config
```

Print copyable LLM guidance:

```bash
./bin/memento-mcp print-guidance
```

Write the recommended-workflow guidance into the current project's `CLAUDE.local.md` (rerun to update the block in place; `--print-only` previews without writing):

```bash
./bin/memento-mcp claude-md
```

Show built-in help:

```bash
./bin/memento-mcp help
```

For a standalone binary, check for or install the latest published server release with:

```bash
memento-mcp update --check
memento-mcp update
```

The update command downloads the matching macOS, Linux, or Windows asset, verifies its published SHA-256 sidecar, and replaces the current executable only after verification succeeds. On Windows, a short-lived PowerShell helper finishes the replacement after the running update command exits. A failed download or checksum leaves the current executable unchanged. Plugin-managed installs are intentionally rejected because the plugin pins its launcher and server versions together. Re-running the installer is also safe: it preflights the staged binary and retains the prior standalone executable as `.previous`.

Published builds check for a newer server release in the background at most once every 24 hours. The request contains no workspace path or repository content. Only an available-update notice is written to stderr, keeping MCP stdout protocol-safe; network and cache failures stay silent. Set `MEMENTO_UPDATE_CHECK=false` to opt out. Development builds do not perform the automatic check.

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

New marketplace installs check once per 24 hours for an updated marketplace package after the verified MCP server has spawned. The check stages the package using Claude's normal plugin commands, never changes the binary running in the active task, and never blocks MCP startup. Reload plugins or start a new task to activate a staged plugin update. Legacy marketplace installs remain opted out until `$HOME/.memento-mcp/marketplace-update.json` contains `{ "autoUpdate": true }`; unavailable networks, command failures, and timeouts preserve the current server.

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

`print-config` is the source of truth for the generic JSON shape. It emits an `mcpServers` map that uses:

- stdio transport
- the current binary path as `command`
- `${workspaceFolder}` as `cwd`
- the default indexing environment variables

If your MCP client expects a different JSON shape, reuse the same values for `command`, `args`, `cwd`, `env`, and stdio transport.

## ChatGPT and Codex

Register a standalone build with Codex using its absolute path:

```bash
codex mcp add memento -- /absolute/path/to/MCP-memento/bin/memento-mcp
```

The registration is shared by the ChatGPT desktop app, Codex CLI, and the Codex IDE extension. Alternatively, add a local STDIO server named `memento` in the desktop app's **Settings → MCP servers**. Verify it with `codex mcp list` or `/mcp`.

For a manual or project-scoped Codex entry, use:

```toml
[mcp_servers.memento]
command = "/absolute/path/to/MCP-memento/bin/memento-mcp"
args = []
```

ChatGPT on the web cannot launch this local STDIO server; use the desktop app or a Codex client.

## Workspace root detection

The server resolves its workspace root in this order:

1. Explicit `--root DIR` or client config
2. `CLAUDE_PROJECT_DIR` from Claude Code
3. MCP client `roots/list`, when the client advertises Roots support
4. Current working directory

Claude Code sessions therefore index the launched project without a manual `repo_switch_workspace` call. Use `repo_switch_workspace` only when you intentionally retarget the same MCP session to another repository.

The current-working-directory fallback is not treated as verified active-workspace scope for `memory_search` or `memory_list`. Those reads fail closed until the caller passes `root` or the workspace is established by one of the first three sources above or by `repo_switch_workspace`. For clients with MCP Roots support, tool calls are also held while a `roots/list_changed` refresh is pending, preventing results from the previously active workspace.

Durable notes are shared across linked Git worktrees. Memento maps a linked checkout to the corresponding path in the main worktree for locked, atomic note storage, while repository context, diffs, and memory-anchor reconciliation continue to use the active checkout. The public `root` argument always means the active checkout for both `memory_*` and `repo_*`; never pass the main worktree merely to reach shared memory. Structured tool results report the resolved checkout, and memory results also report the durable-memory scope. Older worktree-specific note files are merged into the shared store once and archived in place.

## Optional semantic retrieval

`repo_context` focus queries use deterministic term-aware retrieval without configuration; `repo_search` remains literal unless regex mode is requested. Semantic retrieval defaults to auto-detection: after Ollama is installed and `ollama pull nomic-embed-text:v1.5` completes, reachable local embeddings are added to focused context ranking. An unavailable runtime falls back to lexical retrieval; set `MEMENTO_SEMANTIC_ENABLED=false` to opt out entirely, or `true` to require semantic availability while keeping lexical fallback usable.

```json
{
  "env": {
    "MEMENTO_SEMANTIC_ENABLED": "auto"
  }
}
```

The MCP client must launch Memento in an environment that can reach the local Ollama process at `http://127.0.0.1:11434`. See [`docs/README.md`](./README.md#optional-semantic-retrieval) for model caching and fallback behavior, and [runtime configuration](#runtime-configuration) below for tuning variables.

## Runtime configuration

Keep defaults unless a workspace needs different limits or configured behavior. `print-config` includes the normal client entry defaults; the complete environment surface is:

- Indexing: `MEMENTO_CHANGE_DETECTOR` (`auto`), `MEMENTO_INDEX_POLL_SECONDS` (`10`), `MEMENTO_INDEX_MAX_TOTAL_BYTES` (`20971520`), `MEMENTO_INDEX_MAX_FILE_BYTES` (`1048576`), `MEMENTO_GIT_POLL_SECONDS` (`2` hot), `MEMENTO_GIT_MAX_POLL_SECONDS` (`30` quiet), `MEMENTO_GIT_ERROR_MAX_POLL_SECONDS` (`60` after errors), `MEMENTO_GIT_DEBOUNCE_MS` (`500`), and `MEMENTO_FS_DEBOUNCE_MS` (`500`). Git intervals use ±20% per-process jitter; changes and MCP tool activity reset polling to the hot interval.
- Context: `MEMENTO_CONTEXT_MAX_TOKENS` (`7000`), `MEMENTO_OUTLINE_MAX_FILE_BYTES` (`1048576`), `MEMENTO_RESOURCE_MAX_BYTES` (`32000`), and `MEMENTO_PRIME_MAX_BYTES` (`24000`).
- Semantic retrieval: `MEMENTO_SEMANTIC_ENABLED` (`auto`), `MEMENTO_EMBEDDING_MODEL` (`nomic-embed-text:v1.5`), `MEMENTO_OLLAMA_URL` (`http://127.0.0.1:11434`), `MEMENTO_HYBRID_SEMANTIC_WEIGHT` (`0.65`), `MEMENTO_EMBEDDING_BATCH_SIZE` (`32`), and `MEMENTO_EMBEDDING_TIMEOUT_SECONDS` (`30`).
- Redaction: `MEMENTO_REDACTION_ENABLED` (`true`), `MEMENTO_REDACTION_ENTROPY_ENABLED` (`true`), `MEMENTO_REDACTION_ENTROPY_THRESHOLD` (`4.3`), `MEMENTO_REDACTION_HEX_ENTROPY_THRESHOLD` (`3.5`), `MEMENTO_REDACTION_MIN_TOKEN_LENGTH` (`24`), plus JSON regular-expression arrays in `MEMENTO_REDACTION_ADDITIONAL_PATTERNS` and `MEMENTO_REDACTION_ALLOW_PATTERNS`.
- Operations: `MEMENTO_UPDATE_CHECK` (enabled for release builds unless set to `false`), `MEMENTO_FEEDBACK_ENABLED` (`false`), optional `MEMENTO_FEEDBACK_DIR`, and `MEMENTO_MCP_DEV_LOG` (`0`; use `1` for stderr tool-call logs).

Invalid values fail closed or fall back as documented by `memento-mcp help`; security-sensitive Ollama URLs must remain unauthenticated loopback HTTP.

## Recommended LLM guidance

`print-guidance` is the source of truth. Its current output is:

```text
At the start of coding work, before substantial implementation or review, call memory_search for prior handoffs and decisions; use memory_list when no useful query is known. Linked Git worktrees share the main repository's durable notes automatically.
When using memento-mcp, start repository context with repo_context and set intent to navigate, implement, or review.
Use repo_diff_context without paths to auto-detect staged, unstaged, and untracked Git changes, or pass a non-empty ordered path list to override detection; it returns exact-file chunks and a bounded, redacted unified diff summary without related-file expansion.
Use repo_outline when you need signatures and file structure without implementation bodies.
Anchor durable notes to code when possible. Verify stale notes before refreshing or tombstoning them.
Use the prime MCP prompt at session start and explicit note/file resources when the client supports native prompts and @-mentions.
Omit mode unless you need to force a low-level output such as full, outline, or summary.
If repo_context returns suggestedNextCall, prefer following it for a deeper read without repeating context.
When you change repositories in the same MCP session, call repo_switch_workspace with the new root path instead of restarting.
Existing explicit mode calls still work, but new callers should prefer intent.
```

## Claude Code output limits

`repo_context`, `repo_diff_context`, `repo_read_file`, and `repo_search` default to compact responses to avoid Claude Code's MCP result warning near 10k tokens:

- `repo_context`: `maxTokens` defaults to `7000` using an approximate `ceil(UTF-8 bytes / 4)` estimator; `maxTotalBytes` remains a `32000` hard ceiling.
- `repo_diff_context`: at most 20 resolved paths, three chunks per file, `maxTokens: 4000`, `maxTotalBytes: 16000`, `maxDiffBytes: 12000`, and `diffContextLines: 3` by default. Omit `paths` to auto-detect deterministic safe Git changes; supplied paths override detection and preserve order. Auto-detection reports overflow, summarizes and evicts deleted or rename-source paths without chunk-loading them, succeeds empty for a clean Git worktree, and errors outside Git. The unified diff summary is bounded and redacted.
- `repo_read_file`: `maxBytes` defaults to `32000`.
- `repo_search`: each snippet is capped by `maxSnippetBytes`, default `500`.

The same four tools advertise `_meta["anthropic/maxResultSizeChars"] = 500000` for intentional large reads.

### Client-side `MAX_MCP_OUTPUT_TOKENS`

`MAX_MCP_OUTPUT_TOKENS` is Claude Code's client-side output cap and can be lower than Memento's server-side limits. Set `MAX_MCP_OUTPUT_TOKENS` in Claude Code, not in Memento's server entry. When raising `MAX_MCP_OUTPUT_TOKENS` for intentionally larger context, tune the tool arguments and the client setting together; otherwise lower the tool arguments to keep responses compact.

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

When `memory_search` returns `status: "stale"`, verify the code and use `memory_verify` if the note remains correct, or `memory_tombstone` for recoverable soft eviction. `memory_list` includes tombstones; `memory_gc` requires the full conservative eligibility policy described in [`docs/README.md`](./README.md#durable-memory-lifecycle).

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

Every actual workspace change triggers a full index refresh. Set `reindexNow: true` to wait for completion; otherwise the refresh runs asynchronously. Selecting the already-active root does not schedule a redundant pass.
