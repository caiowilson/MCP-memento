const DEFAULT_ENV: Record<string, string> = {
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
    "Notes:",
    "- `cwd` should be the repository root (usually `${workspaceFolder}`).",
    "- Tool names in this server use underscore style (e.g. `repo_index_status`)."
  ].join("\n");
}
