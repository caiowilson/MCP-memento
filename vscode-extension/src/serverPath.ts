export type ServerPathSource = "override" | "workspace" | "installed" | "missing";

export type ServerPathCandidates = {
  overridePath?: string | null;
  workspacePath?: string | null;
  installedPath?: string | null;
  preferWorkspace: boolean;
  fallbackPath: string;
};

export function selectServerPath(
  candidates: ServerPathCandidates,
): { path: string; source: ServerPathSource } {
  const overridePath = candidates.overridePath?.trim();
  if (overridePath) {
    return { path: overridePath, source: "override" };
  }

  if (candidates.preferWorkspace && candidates.workspacePath) {
    return { path: candidates.workspacePath, source: "workspace" };
  }
  if (candidates.installedPath) {
    return { path: candidates.installedPath, source: "installed" };
  }
  if (candidates.workspacePath) {
    return { path: candidates.workspacePath, source: "workspace" };
  }
  return { path: candidates.fallbackPath, source: "missing" };
}
