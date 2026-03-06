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
const installer_1 = require("./installer");
const mcpConfig_1 = require("./mcpConfig");
function activate(context) {
    context.subscriptions.push(vscode.commands.registerCommand("mementoMcp.installServer", async () => {
        try {
            const bin = await (0, installer_1.ensureServerInstalled)(context);
            void vscode.window.showInformationMessage(`memento-mcp installed: ${bin}`);
        }
        catch (err) {
            void vscode.window.showErrorMessage(asErrorMessage(err));
        }
    }));
    context.subscriptions.push(vscode.commands.registerCommand("mementoMcp.openMcpConfigSnippet", async () => {
        try {
            const serverPath = await (0, installer_1.resolvePreferredServerPath)(context);
            const md = (0, mcpConfig_1.buildSnippetMarkdown)(serverPath);
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
            const json = (0, mcpConfig_1.buildConfigEntryJson)(serverPath);
            await vscode.env.clipboard.writeText(json);
            void vscode.window.showInformationMessage("Copied MCP config entry JSON to clipboard.");
        }
        catch (err) {
            void vscode.window.showErrorMessage(asErrorMessage(err));
        }
    }));
}
function deactivate() { }
function asErrorMessage(err) {
    if (err instanceof Error) {
        return err.message;
    }
    return String(err);
}
