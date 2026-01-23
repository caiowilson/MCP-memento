import * as vscode from "vscode";
import * as fsSync from "node:fs";
import * as fs from "node:fs/promises";
import * as path from "node:path";
import * as https from "node:https";

type GitHubRelease = {
  tag_name: string;
  assets: Array<{
    name: string;
    browser_download_url: string;
  }>;
};

export async function ensureServerInstalled(context: vscode.ExtensionContext): Promise<string> {
  const binPath = getInstalledBinaryPath(context);
  if (await fileExists(binPath)) {
    return binPath;
  }

  await vscode.window.withProgress(
    {
      location: vscode.ProgressLocation.Notification,
      title: "Installing memento-mcp server…",
      cancellable: false,
    },
    async () => {
      await fs.mkdir(path.dirname(binPath), { recursive: true });

      const cfg = vscode.workspace.getConfiguration("mementoMcp");
      const repo = String(cfg.get("githubRepo", "caiowilson/MCP-memento"));
      const tag = String(cfg.get("releaseTag", "latest"));

      const release = await fetchRelease(repo, tag);
      const wanted = desiredAssetNames();
      const asset = release.assets.find((a) => wanted.includes(a.name));
      if (!asset) {
        throw new Error(
          `No matching binary asset found in ${repo}@${release.tag_name}. Looked for: ${wanted.join(", ")}`,
        );
      }

      const tmpPath = path.join(path.dirname(binPath), `${path.basename(binPath)}.tmp`);
      await downloadToFile(asset.browser_download_url, tmpPath);
      if (process.platform !== "win32") {
        await fs.chmod(tmpPath, 0o755);
      }
      await fs.rename(tmpPath, binPath);
    },
  );

  return binPath;
}

export async function resolvePreferredServerPath(context: vscode.ExtensionContext): Promise<string> {
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

function getInstalledBinaryPath(context: vscode.ExtensionContext): string {
  return path.join(context.globalStorageUri.fsPath, "bin", binaryName());
}

function binaryName(): string {
  return process.platform === "win32" ? "memento-mcp.exe" : "memento-mcp";
}

async function findWorkspaceBinary(): Promise<string | null> {
  const folders = vscode.workspace.workspaceFolders;
  if (!folders || folders.length === 0) {
    return null;
  }
  const bin = path.join(folders[0].uri.fsPath, "bin", binaryName());
  return (await fileExists(bin)) ? bin : null;
}

function desiredAssetNames(): string[] {
  const arch = process.arch === "arm64" ? "arm64" : "x64";
  const osName =
    process.platform === "darwin" ? "darwin" : process.platform === "win32" ? "windows" : "linux";

  const base = `memento-mcp_${osName}_${arch}`;
  if (osName === "windows") {
    return [`${base}.exe`];
  }
  return [base];
}

async function fetchRelease(repo: string, tag: string): Promise<GitHubRelease> {
  const base = "https://api.github.com/repos/" + repo;
  const url = tag === "latest" ? `${base}/releases/latest` : `${base}/releases/tags/${tag}`;
  return await fetchJson<GitHubRelease>(url);
}

async function fetchJson<T>(url: string): Promise<T> {
  const raw = await httpGet(url, {
    Accept: "application/vnd.github+json",
    "User-Agent": "memento-mcp-vscode",
  });
  return JSON.parse(raw) as T;
}

async function downloadToFile(url: string, destPath: string): Promise<void> {
  const tmpDir = path.dirname(destPath);
  await fs.mkdir(tmpDir, { recursive: true });

  await new Promise<void>((resolve, reject) => {
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
    req.on("error", (err: unknown) => reject(err));
  });
}

async function httpGet(url: string, headers: Record<string, string>): Promise<string> {
  return await new Promise<string>((resolve, reject) => {
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
      const chunks: Buffer[] = [];
      res.on("data", (d) => chunks.push(Buffer.from(d)));
      res.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    });
    req.on("error", (err: unknown) => reject(err));
  });
}

async function fileExists(p: string): Promise<boolean> {
  try {
    await fs.stat(p);
    return true;
  } catch {
    return false;
  }
}
