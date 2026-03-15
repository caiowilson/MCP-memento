"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
exports.activate = activate;
exports.deactivate = deactivate;
const vscode = __importStar(require("vscode"));
const node_crypto_1 = require("node:crypto");
const path = __importStar(require("node:path"));
const installer_1 = require("./installer");
const mcpConfig_1 = require("./mcpConfig");
function activate(context) {
    const statusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
    statusBar.command = "mementoMcp.configureMcp";
    context.subscriptions.push(statusBar);
    context.subscriptions.push(vscode.commands.registerCommand("mementoMcp.installServer", async () => {
        try {
            const bin = await (0, installer_1.ensureServerInstalled)(context);
            void vscode.window.showInformationMessage(`memento-mcp installed: ${bin}`);
            await refreshStatusBar(context, statusBar);
            const followUp = await vscode.window.showInformationMessage("Configure MCP to use the installed server?", "Configure (Workspace)", "Configure (Global)");
            if (followUp === "Configure (Workspace)") {
                await vscode.commands.executeCommand("mementoMcp.configureMcp", { scope: "workspace" });
            }
            else if (followUp === "Configure (Global)") {
                await vscode.commands.executeCommand("mementoMcp.configureMcp", { scope: "global" });
            }
        }
        catch (err) {
            void vscode.window.showErrorMessage(asErrorMessage(err));
        }
    }));
    context.subscriptions.push(vscode.commands.registerCommand("mementoMcp.openMcpConfigSnippet", async () => {
        try {
            const serverPath = await (0, installer_1.resolvePreferredServerPath)(context);
            const md = (0, mcpConfig_1.buildSnippetMarkdown)(serverPath, getExtraEnvFromSettings());
            const doc = await vscode.workspace.openTextDocument({ language: "markdown", content: md });
            await vscode.window.showTextDocument(doc, { preview: false });
        }
        catch (err) {
            void vscode.window.showErrorMessage(asErrorMessage(err));
        }
    }));
    context.subscriptions.push(vscode.commands.registerCommand("mementoMcp.copyMcpConfigSnippet", async () => {
        try {
            const serverPath = await (0, installer_1.resolvePreferredServerPath)(context);
            const json = (0, mcpConfig_1.buildConfigEntryJson)(serverPath, getExtraEnvFromSettings());
            await vscode.env.clipboard.writeText(json);
            void vscode.window.showInformationMessage("Copied MCP config entry JSON to clipboard.");
        }
        catch (err) {
            void vscode.window.showErrorMessage(asErrorMessage(err));
        }
    }));
    context.subscriptions.push(vscode.commands.registerCommand("mementoMcp.openOrCreateMcpConfig", async () => {
        void vscode.window.showWarningMessage("This command has moved. Use “Memento MCP: Configure MCP (Workspace/Global)”.");
        await vscode.commands.executeCommand("mementoMcp.configureMcp", { scope: "workspace" });
    }));
    context.subscriptions.push(vscode.commands.registerCommand("mementoMcp.configureMcp", async (args) => {
        try {
            const serverPath = await (0, installer_1.resolvePreferredServerPath)(context);
            const { scope, promptForScope } = parseScope(args);
            const doc = await openOrCreateMcpConfig(serverPath, scope, promptForScope);
            await vscode.window.showTextDocument(doc, { preview: false });
        }
        catch (err) {
            void vscode.window.showErrorMessage(asErrorMessage(err));
        }
    }));
    context.subscriptions.push(vscode.commands.registerCommand("mementoMcp.saveDevToolLogTail", async () => {
        try {
            const cfg = vscode.workspace.getConfiguration("mementoMcp");
            const enabled = Boolean(cfg.get("devLogToolCalls", false));
            if (!enabled) {
                void vscode.window.showInformationMessage("Enable `mementoMcp.devLogToolCalls` to use this dev-only command.");
                return;
            }
            const tail = await readDevToolLogTail();
            if (!tail) {
                void vscode.window.showInformationMessage("No dev tool-call log found. Enable `mementoMcp.devLogToolCalls`, restart the MCP server, then try again.");
                return;
            }
            const action = await vscode.window.showQuickPick(["Copy to Clipboard", "Save to File"], { placeHolder: "Dev tool-call tail" });
            if (!action) {
                return;
            }
            if (action === "Copy to Clipboard") {
                await vscode.env.clipboard.writeText(tail);
                void vscode.window.showInformationMessage("Copied dev tool-call tail to clipboard.");
                return;
            }
            const folders = vscode.workspace.workspaceFolders;
            const defaultUri = folders?.[0]?.uri
                ? vscode.Uri.joinPath(folders[0].uri, "memento-mcp-tool-calls.tail.txt")
                : undefined;
            const uri = await vscode.window.showSaveDialog({
                defaultUri,
                saveLabel: "Save",
                filters: { Text: ["txt", "log"] },
            });
            if (!uri) {
                return;
            }
            await vscode.workspace.fs.writeFile(uri, Buffer.from(tail, "utf8"));
            void vscode.window.showInformationMessage(`Saved dev tool-call tail: ${uri.fsPath}`);
        }
        catch (err) {
            void vscode.window.showErrorMessage(asErrorMessage(err));
        }
    }));
    context.subscriptions.push(vscode.workspace.onDidChangeConfiguration(async (event) => {
        if (event.affectsConfiguration("mementoMcp")) {
            await refreshStatusBar(context, statusBar);
        }
    }));
    context.subscriptions.push(vscode.workspace.onDidChangeWorkspaceFolders(async () => {
        await refreshStatusBar(context, statusBar);
    }));
    void refreshStatusBar(context, statusBar);
    void maybeShowOnboarding(context);
}
function deactivate() { }
function asErrorMessage(err) {
    if (err instanceof Error) {
        return err.message;
    }
    return String(err);
}
async function refreshStatusBar(context, statusBar) {
    const status = await (0, installer_1.getServerStatus)(context);
    const label = status.source === "installed"
        ? "Installed"
        : status.source === "workspace"
            ? "Workspace"
            : status.source === "override"
                ? "Override"
                : "Not installed";
    statusBar.text = `Memento MCP: ${label}`;
    statusBar.tooltip = `Server path: ${status.path}`;
    statusBar.show();
}
async function maybeShowOnboarding(context) {
    const hasShown = context.globalState.get("mementoMcp.onboardingShown", false);
    if (hasShown) {
        return;
    }
    await context.globalState.update("mementoMcp.onboardingShown", true);
    const choice = await vscode.window.showInformationMessage("Welcome to memento-mcp. Set up the server? For LLM workflows, prefer `repo_context` with `intent` and omit `mode` unless you need to force a specific output.", "Install Server", "Configure MCP", "Copy Snippet");
    if (choice === "Install Server") {
        await vscode.commands.executeCommand("mementoMcp.installServer");
    }
    else if (choice === "Configure MCP") {
        await vscode.commands.executeCommand("mementoMcp.configureMcp");
    }
    else if (choice === "Copy Snippet") {
        await vscode.commands.executeCommand("mementoMcp.copyMcpConfigSnippet");
    }
}
function parseScope(args) {
    if (typeof args === "object" && args !== null && "scope" in args) {
        const s = args.scope;
        if (s === "workspace" || s === "global") {
            return { scope: s, promptForScope: false };
        }
    }
    return { scope: "workspace", promptForScope: true };
}
async function openOrCreateMcpConfig(serverPath, initialScope, promptForScope) {
    const folders = vscode.workspace.workspaceFolders;
    const scope = promptForScope
        ? await chooseScope(initialScope, Boolean(folders && folders.length > 0))
        : initialScope;
    const configUri = await resolveConfigUri(scope, folders?.[0]?.uri);
    await upsertMcpEntry(configUri, serverPath);
    return await vscode.workspace.openTextDocument(configUri);
}
async function chooseScope(initial, hasWorkspaceFolder) {
    if (!hasWorkspaceFolder && initial === "workspace") {
        return "global";
    }
    if (hasWorkspaceFolder) {
        const picked = await vscode.window.showQuickPick([
            { label: "Workspace", description: "Write to ${workspaceFolder}/mcp.json", value: "workspace" },
            { label: "Global", description: "Write to your user MCP config file", value: "global" },
        ], { placeHolder: "Configure MCP for…" });
        if (picked) {
            return picked.value;
        }
    }
    return initial;
}
async function resolveConfigUri(scope, workspaceUri) {
    if (scope === "workspace") {
        if (!workspaceUri) {
            throw new Error("Open a workspace folder to configure workspace mcp.json.");
        }
        return vscode.Uri.joinPath(workspaceUri, "mcp.json");
    }
    const home = process.env.HOME;
    const appData = process.env.APPDATA;
    const candidates = [];
    if (home) {
        candidates.push({ label: "~/.vscode/mcp.json", uri: vscode.Uri.file(`${home}/.vscode/mcp.json`) });
    }
    if (process.platform === "darwin" && home) {
        candidates.push({
            label: "~/Library/Application Support/Code/User/mcp.json",
            uri: vscode.Uri.file(`${home}/Library/Application Support/Code/User/mcp.json`),
        });
    }
    if (process.platform === "linux" && home) {
        candidates.push({
            label: "~/.config/Code/User/mcp.json",
            uri: vscode.Uri.file(`${home}/.config/Code/User/mcp.json`),
        });
    }
    if (process.platform === "win32" && appData) {
        candidates.push({
            label: "%APPDATA%\\Code\\User\\mcp.json",
            uri: vscode.Uri.file(`${appData}\\Code\\User\\mcp.json`),
        });
    }
    const picked = await vscode.window.showQuickPick([
        ...candidates.map((c) => ({ label: c.label, value: c.uri })),
        { label: "Browse…", value: "browse" },
    ], { placeHolder: "Select your global MCP config file" });
    if (!picked) {
        throw new Error("Canceled.");
    }
    if (picked.value === "browse") {
        const res = await vscode.window.showSaveDialog({
            defaultUri: candidates[0]?.uri,
            saveLabel: "Use this file",
            filters: { JSON: ["json"] },
        });
        if (!res) {
            throw new Error("Canceled.");
        }
        return res;
    }
    return picked.value;
}
async function upsertMcpEntry(configUri, serverPath) {
    const extraEnv = getExtraEnvFromSettings();
    const entry = (0, mcpConfig_1.buildConfigEntry)(serverPath, extraEnv);
    const text = await readTextOrEmpty(configUri);
    if (text.trim().length === 0) {
        await vscode.workspace.fs.writeFile(configUri, Buffer.from((0, mcpConfig_1.buildMcpServersConfigJson)(serverPath, extraEnv), "utf8"));
        return;
    }
    let parsed;
    try {
        parsed = JSON.parse(text);
    }
    catch {
        const choice = await vscode.window.showWarningMessage("Existing MCP config is not valid JSON. Overwrite it?", "Overwrite", "Cancel");
        if (choice !== "Overwrite") {
            throw new Error("Canceled.");
        }
        await vscode.workspace.fs.writeFile(configUri, Buffer.from((0, mcpConfig_1.buildMcpServersConfigJson)(serverPath, extraEnv), "utf8"));
        return;
    }
    const updated = upsertIntoKnownSchema(parsed, entry);
    if (!updated) {
        const choice = await vscode.window.showWarningMessage("Could not detect MCP config schema (expected mcpServers map or servers array). Overwrite with a default schema?", "Overwrite", "Cancel");
        if (choice !== "Overwrite") {
            throw new Error("Canceled.");
        }
        await vscode.workspace.fs.writeFile(configUri, Buffer.from((0, mcpConfig_1.buildMcpServersConfigJson)(serverPath, extraEnv), "utf8"));
        return;
    }
    await vscode.workspace.fs.writeFile(configUri, Buffer.from(JSON.stringify(updated, null, 2), "utf8"));
}
function upsertIntoKnownSchema(config, entry) {
    if (Array.isArray(config)) {
        return upsertIntoServersArray(config, entry);
    }
    if (typeof config !== "object" || config === null) {
        return null;
    }
    const obj = config;
    const mcpServers = obj["mcpServers"];
    if (typeof mcpServers === "object" && mcpServers !== null && !Array.isArray(mcpServers)) {
        const next = { ...obj };
        next["mcpServers"] = { ...mcpServers, "memento-mcp": entry };
        return next;
    }
    const servers = obj["servers"];
    if (Array.isArray(servers)) {
        const next = { ...obj };
        next["servers"] = upsertIntoServersArray(servers, entry);
        return next;
    }
    return null;
}
function upsertIntoServersArray(arr, entry) {
    const name = String(entry["name"] ?? "memento-mcp");
    const next = [...arr];
    for (let i = 0; i < next.length; i++) {
        const item = next[i];
        if (typeof item === "object" && item !== null && "name" in item) {
            const itemName = String(item["name"] ?? "");
            if (itemName === name) {
                next[i] = entry;
                return next;
            }
        }
    }
    next.push(entry);
    return next;
}
async function readTextOrEmpty(uri) {
    try {
        const data = await vscode.workspace.fs.readFile(uri);
        return Buffer.from(data).toString("utf8");
    }
    catch {
        return "";
    }
}
function getExtraEnvFromSettings() {
    const cfg = vscode.workspace.getConfiguration("mementoMcp");
    const devLogToolCalls = Boolean(cfg.get("devLogToolCalls", false));
    if (!devLogToolCalls) {
        return {};
    }
    return { MEMENTO_MCP_DEV_LOG: "1" };
}
async function readDevToolLogTail() {
    const folders = vscode.workspace.workspaceFolders;
    if (!folders || folders.length === 0) {
        return null;
    }
    const root = folders[0].uri.fsPath;
    const repoID = (0, node_crypto_1.createHash)("sha256").update(root).digest("hex").slice(0, 32);
    const home = process.env.HOME;
    if (!home) {
        return null;
    }
    const logPath = path.join(home, ".memento-mcp", "repos", repoID, "tool-calls.log");
    const logUri = vscode.Uri.file(logPath);
    let raw;
    try {
        raw = await vscode.workspace.fs.readFile(logUri);
    }
    catch {
        return null;
    }
    const cfg = vscode.workspace.getConfiguration("mementoMcp");
    const tailLines = Math.max(1, Math.min(2000, Number(cfg.get("devLogTailLines", 200))));
    const text = Buffer.from(raw).toString("utf8");
    const lines = text.split(/\r?\n/).filter((l) => l.length > 0);
    const tail = lines.slice(Math.max(0, lines.length - tailLines)).join("\n");
    return tail.length > 0 ? tail + "\n" : null;
}
