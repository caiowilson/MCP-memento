import test from "node:test";
import assert from "node:assert/strict";

import { selectServerPath } from "../.test-dist/serverPath.mjs";

const defaults = {
  overridePath: null,
  workspacePath: "/workspace/bin/memento-mcp",
  installedPath: "/global/bin/memento-mcp",
  preferWorkspace: true,
  fallbackPath: "${workspaceFolder}/bin/memento-mcp",
};

test("selectServerPath prefers the workspace binary when configured", () => {
  assert.deepEqual(selectServerPath(defaults), {
    path: "/workspace/bin/memento-mcp",
    source: "workspace",
  });
});

test("selectServerPath gives an explicit server-path override highest priority", () => {
  assert.deepEqual(
    selectServerPath({ ...defaults, overridePath: "  /custom/memento-mcp  " }),
    { path: "/custom/memento-mcp", source: "override" },
  );
});

test("selectServerPath uses the installed binary when workspace preference is disabled", () => {
  assert.deepEqual(selectServerPath({ ...defaults, preferWorkspace: false }), {
    path: "/global/bin/memento-mcp",
    source: "installed",
  });
});
