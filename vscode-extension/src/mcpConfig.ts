const DEFAULT_ENV: Record<string, string> = {
  MEMENTO_CHANGE_DETECTOR: "auto",
  MEMENTO_INDEX_POLL_SECONDS: "10",
  MEMENTO_GIT_POLL_SECONDS: "2",
  MEMENTO_GIT_DEBOUNCE_MS: "500",
  MEMENTO_FS_DEBOUNCE_MS: "500",
};

export function buildConfigEntry(
  serverPath: string,
  extraEnv: Record<string, string> = {},
): Record<string, unknown> {
  return {
    name: "memento-mcp",
    transport: "stdio",
    command: serverPath,
    args: [],
    cwd: "${workspaceFolder}",
    env: { ...DEFAULT_ENV, ...extraEnv },
  };
}

export function buildConfigEntryJson(serverPath: string, extraEnv: Record<string, string> = {}): string {
  return JSON.stringify(buildConfigEntry(serverPath, extraEnv), null, 2);
}

export function buildMcpServersConfig(
  serverPath: string,
  extraEnv: Record<string, string> = {},
): Record<string, unknown> {
  return {
    mcpServers: {
      "memento-mcp": buildConfigEntry(serverPath, extraEnv),
    },
  };
}

export function buildMcpServersConfigJson(serverPath: string, extraEnv: Record<string, string> = {}): string {
  return JSON.stringify(buildMcpServersConfig(serverPath, extraEnv), null, 2);
}

export function buildSnippetMarkdown(serverPath: string, extraEnv: Record<string, string> = {}): string {
  const entry = buildConfigEntry(serverPath, extraEnv);

  const entryOnly = JSON.stringify(entry, null, 2);
  const asServersArray = JSON.stringify({ servers: [entry] }, null, 2);
  const asMcpServersMap = JSON.stringify({ mcpServers: { "memento-mcp": entry } }, null, 2);
  const llmGuidance = [
    "When using `memento-mcp`, start with `repo_context` and set `intent` to `navigate`, `implement`, or `review`.",
    "Omit `mode` unless you need to force a low-level output shape such as `full`, `outline`, or `summary`.",
    "If the tool returns `suggestedNextCall`, prefer following it for a deeper read without repeating context."
  ].join(" ");

  return [
    "# memento-mcp — MCP config snippet",
    "",
    "This extension does not know your exact `mcp.json` schema, so it provides three common shapes.",
    "",
    "## Option A: entry only",
    "Paste into whatever list/map your client uses.",
    "",
    "```json",
    entryOnly,
    "```",
    "",
    "## Option B: `{ \"servers\": [ ... ] }`",
    "",
    "```json",
    asServersArray,
    "```",
    "",
    "## Option C: `{ \"mcpServers\": { ... } }`",
    "",
    "```json",
    asMcpServersMap,
    "```",
    "",
    "## Recommended LLM guidance",
    "Paste this into your MCP-capable client instructions if it does not surface server metadata clearly.",
    "",
    "```text",
    llmGuidance,
    "```",
    "",
    "Notes:",
    "- `cwd` should be the repository root (usually `${workspaceFolder}`).",
    "- Tool names in this server use underscore style (e.g. `repo_index_status`).",
    "- New callers should prefer `repo_context` with `intent`; existing explicit `mode` calls still work unchanged."
  ].join("\n");
}
