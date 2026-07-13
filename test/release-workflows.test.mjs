import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { access, chmod, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const releaseWorkflowPath = join(root, ".github/workflows/release-server.yml");
const latestWorkflowPath = join(root, ".github/workflows/move-server-latest.yml");
const signingScriptPath = join(root, "scripts/macos-sign-and-notarize.sh");

test("release workflow signs and notarizes every macOS artifact", async () => {
  const workflow = await readFile(releaseWorkflowPath, "utf8");
  const genericBuild = workflow.match(/  build-binaries:[\s\S]*?\n  package-deb:/)?.[0];

  assert.ok(genericBuild, "generic binary job must remain identifiable");
  assert.doesNotMatch(genericBuild, /goos: \[[^\]]*darwin/);
  assert.match(workflow, /environment:\n      name: macos-release-signing/);
  assert.match(workflow, /APPLE_DEVELOPER_ID_APPLICATION_P12_BASE64/);
  assert.match(workflow, /APPLE_DEVELOPER_ID_INSTALLER_P12_BASE64/);
  assert.match(workflow, /APPLE_NOTARY_KEY_P8_BASE64/);
  assert.match(workflow, /notarytool store-credentials/);
  assert.match(workflow, /\.\/scripts\/macos-sign-and-notarize\.sh/);
  assert.match(workflow, /security delete-keychain/);
  assert.match(workflow, /dist\/memento-mcp_darwin_\*/);
});

test("server/latest mirrors verified versioned assets without rebuilding", async () => {
  const workflow = await readFile(latestWorkflowPath, "utf8");

  assert.doesNotMatch(workflow, /\bgo build\b/);
  assert.match(workflow, /gh release download "\$SOURCE_TAG"/);
  assert.match(workflow, /sha256sum -c "\$checksum"/);
  assert.match(workflow, /gh release delete server\/latest --yes/);
  assert.match(workflow, /gh release create server\/latest dist\/\*/);
  assert.match(workflow, /Exact verified asset mirror/);

  const expectedAssets = workflow.match(/^\s{14}(?:"?)memento-mcp_[^\n]+/gm) ?? [];
  assert.equal(expectedAssets.length, 16, "latest must require the exact 16-asset manifest");
});

test("macOS packaging script executes the hardened signing pipeline", async (t) => {
  execFileSync("bash", ["-n", signingScriptPath]);

  const sandbox = await mkdtemp(join(tmpdir(), "memento-macos-signing-test-"));
  const fakeBin = join(sandbox, "bin");
  const commandLog = join(sandbox, "commands.log");
  const binary = join(sandbox, "memento-mcp");
  const keychain = join(sandbox, "signing.keychain-db");
  const packagePath = join(sandbox, "memento-mcp.pkg");
  t.after(() => rm(sandbox, { force: true, recursive: true }));

  await mkdir(fakeBin);
  await writeFile(binary, "test binary\n");
  await writeFile(keychain, "test keychain\n");

  const writeStub = async (name, body) => {
    const path = join(fakeBin, name);
    await writeFile(path, `#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s' '${name}' >> "$COMMAND_LOG"\nprintf ' %q' "$@" >> "$COMMAND_LOG"\nprintf '\\n' >> "$COMMAND_LOG"\n${body}\n`);
    await chmod(path, 0o755);
  };

  await writeStub("codesign", "");
  await writeStub("pkgbuild", "touch \"${!#}\"");
  await writeStub("pkgutil", "");
  await writeStub("spctl", "");
  await writeStub("plutil", "if [[ ${2:-} == id ]]; then echo submission-1; else echo Accepted; fi");
  await writeStub(
    "xcrun",
    "if [[ ${1:-} == notarytool && ${2:-} == submit ]]; then printf '%s\\n' '{\"id\":\"submission-1\",\"status\":\"Accepted\"}'; fi",
  );

  const result = spawnSync(
    "bash",
    [
      signingScriptPath,
      "--binary", binary,
      "--package", packagePath,
      "--version", "1.2.3",
      "--application-identity", "Developer ID Application: Example",
      "--installer-identity", "Developer ID Installer: Example",
      "--keychain", keychain,
      "--notary-profile", "memento-test",
    ],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        COMMAND_LOG: commandLog,
        PATH: `${fakeBin}:${process.env.PATH}`,
      },
    },
  );

  assert.equal(result.status, 0, result.stderr);
  await access(packagePath);
  const log = await readFile(commandLog, "utf8");
  assert.match(log, /codesign .*--options runtime .*--timestamp/);
  assert.match(log, /pkgbuild .*--sign Developer\\ ID\\ Installer/);
  assert.match(log, /xcrun notarytool submit/);
  assert.match(log, /xcrun stapler staple/);
  assert.match(log, /xcrun stapler validate/);
  assert.match(log, /pkgutil --check-signature/);
  assert.match(log, /spctl --assess --type install/);
});
