import * as vscode from "vscode";
import * as fsSync from "node:fs";
import * as fs from "node:fs/promises";
import * as path from "node:path";
import * as https from "node:https";
import { spawn } from "node:child_process";

type GitHubRelease = {
  tag_name: string;
  assets: Array<{
    name: string;
    browser_download_url: string;
  }>;
};

export type InstallResult = {
  binPath: string;
  hasRepoSwitchWorkspace: boolean;
  installedFromTag?: string;
  releaseTried: boolean;
};

export async function ensureServerInstalled(
  context: vscode.ExtensionContext,
  opts: { force?: boolean; tagOverride?: string } = {},
): Promise<string> {
  const binPath = getInstalledBinaryPath(context);
  if (!opts.force && (await fileExists(binPath))) {
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
      const tag = (opts.tagOverride ?? String(cfg.get("releaseTag", "server/latest"))).trim();

      const release = await fetchRelease(repo, tag);
      const wanted = desiredAssetNames();
      const asset = release.assets.find((a) => wanted.includes(a.name));
      if (!asset) {
        throw new Error(
          [
            `No matching binary asset found in ${repo}@${release.tag_name}.`,
            `Looked for: ${wanted.join(", ")}.`,
            "Check that the release includes raw binaries and that the settings",
            "`mementoMcp.githubRepo` and `mementoMcp.releaseTag` point to the right repo/tag.",
          ].join(" "),
        );
      }

      const tmpPath = path.join(path.dirname(binPath), `${path.basename(binPath)}.tmp`);
      await downloadToFile(asset.browser_download_url, tmpPath);
      if (process.platform !== "win32") {
        await fs.chmod(tmpPath, 0o755);
      }
      if (await fileExists(binPath)) {
        await fs.rm(binPath, { force: true });
      }
      await fs.rename(tmpPath, binPath);
    },
  );

  return binPath;
}

export async function ensureServerSupportsRepoSwitchWorkspace(
  context: vscode.ExtensionContext,
): Promise<InstallResult> {
  const cfg = vscode.workspace.getConfiguration("mementoMcp");
  const configuredTag = String(cfg.get("releaseTag", "server/latest")).trim();
  const binPath = getInstalledBinaryPath(context);

  const hadToolBefore = await binaryExposesTool(binPath, "repo_switch_workspace");

  const fallbackTags = uniqueNonEmpty(["server/latest", "latest", configuredTag]);
  for (const tag of fallbackTags) {
    try {
      await ensureServerInstalled(context, { force: true, tagOverride: tag });
      if (await binaryExposesTool(binPath, "repo_switch_workspace")) {
        return {
          binPath,
          hasRepoSwitchWorkspace: true,
          installedFromTag: tag,
          releaseTried: true,
        };
      }
    } catch {
      // Continue trying the next fallback tag.
    }
  }

  if (hadToolBefore) {
    return {
      binPath,
      hasRepoSwitchWorkspace: true,
      releaseTried: true,
    };
  }

  return {
    binPath,
    hasRepoSwitchWorkspace: false,
    releaseTried: true,
    installedFromTag: fallbackTags.join(","),
  };
}

export function sourceBuildReadmeUrl(repo: string): string {
  const trimmed = repo.trim();
  if (!trimmed) {
    return "https://github.com/caiowilson/MCP-memento#local-development";
  }
  return `https://github.com/${trimmed}#local-development`;
}

export async function resolvePreferredServerPath(context: vscode.ExtensionContext): Promise<string> {
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

export async function getServerStatus(
  context: vscode.ExtensionContext,
): Promise<{ path: string; source: "override" | "workspace" | "installed" | "missing" }> {
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
  const url =
    tag === "latest"
      ? `${base}/releases/latest`
      : `${base}/releases/tags/${encodeURIComponent(tag)}`;
  try {
    return await fetchJson<GitHubRelease>(url);
  } catch (err) {
    throw new Error(
      [
        `Failed to fetch release for ${repo}@${tag}.`,
        "If you use namespaced tags like `server/latest`, they must exist on GitHub Releases.",
        "Check `mementoMcp.githubRepo` and `mementoMcp.releaseTag` settings.",
        asErrorMessage(err),
      ].join(" "),
    );
  }
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

function getConfiguredServerPath(): string | null {
  const cfg = vscode.workspace.getConfiguration("mementoMcp");
  const raw = String(cfg.get("serverPath", "")).trim();
  return raw.length > 0 ? raw : null;
}

function asErrorMessage(err: unknown): string {
  if (err instanceof Error) {
    return err.message;
  }
  return String(err);
}

function uniqueNonEmpty(values: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const v of values) {
    const key = v.trim();
    if (!key || seen.has(key)) {
      continue;
    }
    seen.add(key);
    out.push(key);
  }
  return out;
}

async function binaryExposesTool(binPath: string, toolName: string): Promise<boolean> {
  if (!(await fileExists(binPath))) {
    return false;
  }

  return await new Promise<boolean>((resolve) => {
    const child = spawn(binPath, [], { stdio: ["pipe", "pipe", "ignore"] });
    let settled = false;
    let buffer = "";

    const settle = (ok: boolean): void => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timeout);
      try {
        child.stdin.end();
      } catch {
        // ignore
      }
      try {
        child.kill();
      } catch {
        // ignore
      }
      resolve(ok);
    };

    const timeout = setTimeout(() => settle(false), 3000);

    child.on("error", () => settle(false));
    child.on("exit", () => settle(false));
    child.stdout.on("data", (chunk: Buffer) => {
      buffer += chunk.toString("utf8");
      for (;;) {
        const idx = buffer.indexOf("\n");
        if (idx < 0) {
          break;
        }
        const line = buffer.slice(0, idx).trim();
        buffer = buffer.slice(idx + 1);
        if (!line) {
          continue;
        }
        let parsed: unknown;
        try {
          parsed = JSON.parse(line);
        } catch {
          continue;
        }
        const tools = (parsed as { result?: { tools?: Array<{ name?: unknown }> } })?.result?.tools;
        if (!Array.isArray(tools)) {
          continue;
        }
        const hasTool = tools.some((t) => String(t?.name ?? "") === toolName);
        settle(hasTool);
        return;
      }
    });

    try {
      child.stdin.write(
        JSON.stringify({
          jsonrpc: "2.0",
          id: 1,
          method: "initialize",
          params: { protocolVersion: "2024-11-05" },
        }) + "\n",
      );
      child.stdin.write(
        JSON.stringify({
          jsonrpc: "2.0",
          id: 2,
          method: "tools/list",
        }) + "\n",
      );
    } catch {
      settle(false);
    }
  });
}
