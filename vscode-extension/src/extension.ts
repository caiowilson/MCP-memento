import * as vscode from "vscode";
import { ensureServerInstalled, resolvePreferredServerPath } from "./installer";
import { buildConfigEntryJson, buildSnippetMarkdown } from "./mcpConfig";

export function activate(context: vscode.ExtensionContext) {
  context.subscriptions.push(
    vscode.commands.registerCommand("mementoMcp.installServer", async () => {
      try {
        const bin = await ensureServerInstalled(context);
        void vscode.window.showInformationMessage(`memento-mcp installed: ${bin}`);
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
}

export function deactivate() {}

function asErrorMessage(err: unknown): string {
  if (err instanceof Error) {
    return err.message;
  }
  return String(err);
}

