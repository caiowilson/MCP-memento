import * as vscode from "vscode";
import { ensureServerInstalled, getServerStatus, resolvePreferredServerPath } from "./installer";
import { buildConfigEntryJson, buildMcpServersConfigJson, buildSnippetMarkdown } from "./mcpConfig";

export function activate(context: vscode.ExtensionContext) {
  const statusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
  statusBar.command = "mementoMcp.openOrCreateMcpConfig";
  context.subscriptions.push(statusBar);

  context.subscriptions.push(
    vscode.commands.registerCommand("mementoMcp.installServer", async () => {
      try {
        const bin = await ensureServerInstalled(context);
        void vscode.window.showInformationMessage(`memento-mcp installed: ${bin}`);
        await refreshStatusBar(context, statusBar);
      } catch (err) {
        void vscode.window.showErrorMessage(asErrorMessage(err));
      }
    }),
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("mementoMcp.openMcpConfigSnippet", async () => {
      try {
        const serverPath = await resolvePreferredServerPath(context);
        const md = buildSnippetMarkdown(serverPath);
        const doc = await vscode.workspace.openTextDocument({ language: "markdown", content: md });
        await vscode.window.showTextDocument(doc, { preview: false });
      } catch (err) {
        void vscode.window.showErrorMessage(asErrorMessage(err));
      }
    }),
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("mementoMcp.copyMcpConfigSnippet", async () => {
      try {
        const serverPath = await resolvePreferredServerPath(context);
        const json = buildConfigEntryJson(serverPath);
        await vscode.env.clipboard.writeText(json);
        void vscode.window.showInformationMessage("Copied MCP config entry JSON to clipboard.");
      } catch (err) {
        void vscode.window.showErrorMessage(asErrorMessage(err));
      }
    }),
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("mementoMcp.openOrCreateMcpConfig", async () => {
      try {
        const serverPath = await resolvePreferredServerPath(context);
        const doc = await openOrCreateMcpConfig(serverPath);
        await vscode.window.showTextDocument(doc, { preview: false });
      } catch (err) {
        void vscode.window.showErrorMessage(asErrorMessage(err));
      }
    }),
  );

  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration(async (event) => {
      if (event.affectsConfiguration("mementoMcp")) {
        await refreshStatusBar(context, statusBar);
      }
    }),
  );

  context.subscriptions.push(
    vscode.workspace.onDidChangeWorkspaceFolders(async () => {
      await refreshStatusBar(context, statusBar);
    }),
  );

  void refreshStatusBar(context, statusBar);
  void maybeShowOnboarding(context);
}

export function deactivate() {}

function asErrorMessage(err: unknown): string {
  if (err instanceof Error) {
    return err.message;
  }
  return String(err);
}

async function refreshStatusBar(
  context: vscode.ExtensionContext,
  statusBar: vscode.StatusBarItem,
): Promise<void> {
  const status = await getServerStatus(context);
  const label =
    status.source === "installed"
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

async function maybeShowOnboarding(context: vscode.ExtensionContext): Promise<void> {
  const hasShown = context.globalState.get<boolean>("mementoMcp.onboardingShown", false);
  if (hasShown) {
    return;
  }
  await context.globalState.update("mementoMcp.onboardingShown", true);

  const choice = await vscode.window.showInformationMessage(
    "Welcome to memento-mcp. Set up the server?",
    "Install Server",
    "Open MCP Config",
    "Copy Snippet",
  );
  if (choice === "Install Server") {
    await vscode.commands.executeCommand("mementoMcp.installServer");
  } else if (choice === "Open MCP Config") {
    await vscode.commands.executeCommand("mementoMcp.openOrCreateMcpConfig");
  } else if (choice === "Copy Snippet") {
    await vscode.commands.executeCommand("mementoMcp.copyMcpConfigSnippet");
  }
}

async function openOrCreateMcpConfig(serverPath: string): Promise<vscode.TextDocument> {
  const folders = vscode.workspace.workspaceFolders;
  if (!folders || folders.length === 0) {
    throw new Error("Open a workspace folder to create mcp.json.");
  }

  const configUri = vscode.Uri.joinPath(folders[0].uri, "mcp.json");
  try {
    await vscode.workspace.fs.stat(configUri);
    return await vscode.workspace.openTextDocument(configUri);
  } catch {
    const content = buildMcpServersConfigJson(serverPath);
    await vscode.workspace.fs.writeFile(configUri, Buffer.from(content, "utf8"));
    return await vscode.workspace.openTextDocument(configUri);
  }
}
