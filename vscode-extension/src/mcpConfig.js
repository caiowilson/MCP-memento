"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.buildConfigEntry = buildConfigEntry;
exports.buildConfigEntryJson = buildConfigEntryJson;
exports.buildSnippetMarkdown = buildSnippetMarkdown;
const DEFAULT_ENV = {
    MEMENTO_INDEX_POLL_SECONDS: "10",
    MEMENTO_GIT_POLL_SECONDS: "2",
    MEMENTO_GIT_DEBOUNCE_MS: "500",
    MEMENTO_FS_DEBOUNCE_MS: "500",
};
function buildConfigEntry(serverPath) {
    return {
        name: "memento-mcp",
        transport: "stdio",
        command: serverPath,
        args: [],
        cwd: "${workspaceFolder}",
        env: DEFAULT_ENV,
    };
}
function buildConfigEntryJson(serverPath) {
    return JSON.stringify(buildConfigEntry(serverPath), null, 2);
}
function buildSnippetMarkdown(serverPath) {
    const entry = buildConfigEntry(serverPath);
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
