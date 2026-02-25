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
exports.ensureServerInstalled = ensureServerInstalled;
exports.resolvePreferredServerPath = resolvePreferredServerPath;
exports.getServerStatus = getServerStatus;
const vscode = __importStar(require("vscode"));
const fsSync = __importStar(require("node:fs"));
const fs = __importStar(require("node:fs/promises"));
const path = __importStar(require("node:path"));
const https = __importStar(require("node:https"));
async function ensureServerInstalled(context) {
    const binPath = getInstalledBinaryPath(context);
    if (await fileExists(binPath)) {
        return binPath;
    }
    await vscode.window.withProgress({
        location: vscode.ProgressLocation.Notification,
        title: "Installing memento-mcp server…",
        cancellable: false,
    }, async () => {
        await fs.mkdir(path.dirname(binPath), { recursive: true });
        const cfg = vscode.workspace.getConfiguration("mementoMcp");
        const repo = String(cfg.get("githubRepo", "caiowilson/MCP-memento"));
        const tag = String(cfg.get("releaseTag", "server/latest"));
        const release = await fetchRelease(repo, tag);
        const wanted = desiredAssetNames();
        const asset = release.assets.find((a) => wanted.includes(a.name));
        if (!asset) {
            throw new Error([
                `No matching binary asset found in ${repo}@${release.tag_name}.`,
                `Looked for: ${wanted.join(", ")}.`,
                "Check that the release includes raw binaries and that the settings",
                "`mementoMcp.githubRepo` and `mementoMcp.releaseTag` point to the right repo/tag.",
            ].join(" "));
        }
        const tmpPath = path.join(path.dirname(binPath), `${path.basename(binPath)}.tmp`);
        await downloadToFile(asset.browser_download_url, tmpPath);
        if (process.platform !== "win32") {
            await fs.chmod(tmpPath, 0o755);
        }
        await fs.rename(tmpPath, binPath);
    });
    return binPath;
}
async function resolvePreferredServerPath(context) {
    const override = getConfiguredServerPath();
    if (override) {
        return override;
    }
    const cfg = vscode.workspace.getConfiguration("mementoMcp");
    const preferWorkspace = Boolean(cfg.get("preferWorkspaceBinary", true));
    const workspaceBin = await findWorkspaceBinary();
    if (preferWorkspace && workspaceBin) {
        return workspaceBin;
    }
    const installed = getInstalledBinaryPath(context);
    if (await fileExists(installed)) {
        return installed;
    }
    if (workspaceBin) {
        return workspaceBin;
    }
    return "${workspaceFolder}/bin/" + binaryName();
}
async function getServerStatus(context) {
    const override = getConfiguredServerPath();
    if (override) {
        return { path: override, source: "override" };
    }
    const cfg = vscode.workspace.getConfiguration("mementoMcp");
    const preferWorkspace = Boolean(cfg.get("preferWorkspaceBinary", true));
    const workspaceBin = await findWorkspaceBinary();
    const installed = getInstalledBinaryPath(context);
    if (preferWorkspace && workspaceBin) {
        return { path: workspaceBin, source: "workspace" };
    }
    if (await fileExists(installed)) {
        return { path: installed, source: "installed" };
    }
    if (workspaceBin) {
        return { path: workspaceBin, source: "workspace" };
    }
    return { path: "${workspaceFolder}/bin/" + binaryName(), source: "missing" };
}
function getInstalledBinaryPath(context) {
    return path.join(context.globalStorageUri.fsPath, "bin", binaryName());
}
function binaryName() {
    return process.platform === "win32" ? "memento-mcp.exe" : "memento-mcp";
}
async function findWorkspaceBinary() {
    const folders = vscode.workspace.workspaceFolders;
    if (!folders || folders.length === 0) {
        return null;
    }
    const bin = path.join(folders[0].uri.fsPath, "bin", binaryName());
    return (await fileExists(bin)) ? bin : null;
}
function desiredAssetNames() {
    const arch = process.arch === "arm64" ? "arm64" : "x64";
    const osName = process.platform === "darwin" ? "darwin" : process.platform === "win32" ? "windows" : "linux";
    const base = `memento-mcp_${osName}_${arch}`;
    if (osName === "windows") {
        return [`${base}.exe`];
    }
    return [base];
}
async function fetchRelease(repo, tag) {
    const base = "https://api.github.com/repos/" + repo;
    const url = tag === "latest"
        ? `${base}/releases/latest`
        : `${base}/releases/tags/${encodeURIComponent(tag)}`;
    try {
        return await fetchJson(url);
    }
    catch (err) {
        throw new Error([
            `Failed to fetch release for ${repo}@${tag}.`,
            "If you use namespaced tags like `server/latest`, they must exist on GitHub Releases.",
            "Check `mementoMcp.githubRepo` and `mementoMcp.releaseTag` settings.",
            asErrorMessage(err),
        ].join(" "));
    }
}
async function fetchJson(url) {
    const raw = await httpGet(url, {
        Accept: "application/vnd.github+json",
        "User-Agent": "memento-mcp-vscode",
    });
    return JSON.parse(raw);
}
async function downloadToFile(url, destPath) {
    const tmpDir = path.dirname(destPath);
    await fs.mkdir(tmpDir, { recursive: true });
    await new Promise((resolve, reject) => {
        const file = fsSync.createWriteStream(destPath);
        const req = https.get(url, { headers: { "User-Agent": "memento-mcp-vscode" } }, (res) => {
            const status = res.statusCode ?? 0;
            const redirect = res.headers.location;
            if (status >= 300 && status < 400 && redirect) {
                file.close();
                fsSync.unlinkSync(destPath);
                downloadToFile(redirect, destPath).then(resolve, reject);
                return;
            }
            if (status < 200 || status >= 300) {
                file.close();
                reject(new Error(`download failed: HTTP ${status}`));
                return;
            }
            res.pipe(file);
            file.on("finish", () => file.close(() => resolve()));
        });
        req.on("error", (err) => reject(err));
    });
}
async function httpGet(url, headers) {
    return await new Promise((resolve, reject) => {
        const req = https.get(url, { headers }, (res) => {
            const status = res.statusCode ?? 0;
            const redirect = res.headers.location;
            if (status >= 300 && status < 400 && redirect) {
                httpGet(redirect, headers).then(resolve, reject);
                return;
            }
            if (status < 200 || status >= 300) {
                reject(new Error(`HTTP ${status} for ${url}`));
                return;
            }
            const chunks = [];
            res.on("data", (d) => chunks.push(Buffer.from(d)));
            res.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
        });
        req.on("error", (err) => reject(err));
    });
}
async function fileExists(p) {
    try {
        await fs.stat(p);
        return true;
    }
    catch {
        return false;
    }
}
function getConfiguredServerPath() {
    const cfg = vscode.workspace.getConfiguration("mementoMcp");
    const raw = String(cfg.get("serverPath", "")).trim();
    return raw.length > 0 ? raw : null;
}
function asErrorMessage(err) {
    if (err instanceof Error) {
        return err.message;
    }
    return String(err);
}
