import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { chmod, mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const installer = join(root, "install.sh");

const candidate = `#!/bin/sh
case "\${1:-}" in
  version)
    if [ "\${MEMENTO_FAIL_VERSION:-}" = "1" ]; then exit 1; fi
    printf '%s\\n' "\${MEMENTO_CANDIDATE_VERSION:-1.2.3}"
    ;;
  setup|doctor)
    printf '%s\\n' "$*" >> "$MEMENTO_TEST_LOG"
    ;;
  *) exit 2 ;;
esac
`;

async function makeSandbox(t, { os = "Darwin", arch = "arm64", asset = "memento-mcp_darwin_arm64", body = candidate } = {}) {
  const sandbox = await mkdtemp(join(tmpdir(), "memento-installer-test-"));
  const fakeBin = join(sandbox, "bin");
  const downloads = join(sandbox, "downloads");
  const installDir = join(sandbox, "install");
  const log = join(sandbox, "calls.log");
  t.after(() => rm(sandbox, { recursive: true, force: true }));
  await mkdir(fakeBin);
  await mkdir(downloads);

  const uname = join(fakeBin, "uname");
  await writeFile(uname, `#!/bin/sh
case "\${1:-}" in
  -s) printf '%s\\n' "$FAKE_UNAME_S" ;;
  -m) printf '%s\\n' "$FAKE_UNAME_M" ;;
  *) exit 2 ;;
esac
`);
  await chmod(uname, 0o755);

  const curl = join(fakeBin, "curl");
  await writeFile(curl, `#!/bin/sh
out=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out=$2; shift 2 ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
[ -n "$out" ] && [ -n "$url" ] || exit 2
printf '%s\\n' "$url" >> "$FAKE_CURL_LOG"
cp "$FAKE_DOWNLOADS/\${url##*/}" "$out"
`);
  await chmod(curl, 0o755);

  const binary = join(downloads, asset);
  await writeFile(binary, body);
  const digest = createHash("sha256").update(body).digest("hex");
  await writeFile(`${binary}.sha256`, `${digest}  ${asset}\n`);

  const env = {
    ...process.env,
    PATH: `${fakeBin}:${process.env.PATH}`,
    HOME: sandbox,
    FAKE_UNAME_S: os,
    FAKE_UNAME_M: arch,
    FAKE_DOWNLOADS: downloads,
    FAKE_CURL_LOG: join(sandbox, "curl.log"),
    MEMENTO_TEST_LOG: log,
    MEMENTO_RELEASE_BASE_URL: "https://release.invalid/server%2Flatest",
  };
  return { sandbox, downloads, installDir, log, env, asset, body };
}

function runInstall(box, args = [], env = {}) {
  return spawnSync("sh", [installer, "--install-dir", box.installDir, ...args], {
    encoding: "utf8",
    env: { ...box.env, ...env },
  });
}

test("verified install replaces atomically, retains the previous binary, and configures selected clients", async (t) => {
  const box = await makeSandbox(t);
  await mkdir(box.installDir);
  const target = join(box.installDir, "memento-mcp");
  await writeFile(target, "old binary\n");

  const result = runInstall(box, ["--clients", "codex,claude"]);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(await readFile(target, "utf8"), box.body);
  assert.equal(await readFile(`${target}.previous`, "utf8"), "old binary\n");
  assert.match(result.stdout, /Installed memento-mcp 1\.2\.3/);
  assert.equal(
    await readFile(box.log, "utf8"),
    "setup --clients=codex,claude --force\ndoctor --clients=codex,claude\n",
  );
  assert.ok(((await stat(target)).mode & 0o111) !== 0);
});

test("checksum mismatch leaves an existing installation byte-identical", async (t) => {
  const box = await makeSandbox(t);
  await mkdir(box.installDir);
  const target = join(box.installDir, "memento-mcp");
  await writeFile(target, "old binary without newline");
  await writeFile(join(box.downloads, `${box.asset}.sha256`), `${"0".repeat(64)}  ${box.asset}\n`);

  const result = runInstall(box, ["--no-setup"]);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /checksum mismatch/);
  assert.equal(await readFile(target, "utf8"), "old binary without newline");
});

test("candidate preflight failure leaves an existing installation byte-identical", async (t) => {
  const box = await makeSandbox(t);
  await mkdir(box.installDir);
  const target = join(box.installDir, "memento-mcp");
  await writeFile(target, "old binary\n");

  const result = runInstall(box, ["--no-setup"], { MEMENTO_FAIL_VERSION: "1" });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /version preflight/);
  assert.equal(await readFile(target, "utf8"), "old binary\n");
});

test("platform mapping covers x64 and arm64 macOS, Linux, and Windows assets", async (t) => {
  const cases = [
    ["Darwin", "x86_64", "memento-mcp_darwin_x64", "memento-mcp"],
    ["Darwin", "arm64", "memento-mcp_darwin_arm64", "memento-mcp"],
    ["Linux", "x86_64", "memento-mcp_linux_x64", "memento-mcp"],
    ["Linux", "aarch64", "memento-mcp_linux_arm64", "memento-mcp"],
    ["MINGW64_NT", "amd64", "memento-mcp_windows_x64.exe", "memento-mcp.exe"],
    ["MSYS_NT", "arm64", "memento-mcp_windows_arm64.exe", "memento-mcp.exe"],
  ];
  for (const [os, arch, asset, targetName] of cases) {
    await t.test(asset, async (t) => {
      const box = await makeSandbox(t, { os, arch, asset });
      const result = runInstall(box, ["--no-setup"]);
      assert.equal(result.status, 0, result.stderr);
      assert.equal(await readFile(join(box.installDir, targetName), "utf8"), box.body);
      const urls = await readFile(box.env.FAKE_CURL_LOG, "utf8");
      assert.match(urls, new RegExp(`${asset.replaceAll(".", "\\.")}\\n`));
      assert.match(urls, new RegExp(`${asset.replaceAll(".", "\\.")}\\.sha256\\n`));
    });
  }
});

test("unknown clients and options fail before downloading", async (t) => {
  const box = await makeSandbox(t);
  const clientResult = runInstall(box, ["--clients=other"]);
  assert.equal(clientResult.status, 2);
  assert.match(clientResult.stderr, /unsupported client/);
  const optionResult = runInstall(box, ["--wat"]);
  assert.equal(optionResult.status, 2);
  assert.match(optionResult.stderr, /unknown option/);
});

test("auto setup does not duplicate an installed Claude Code plugin", async (t) => {
  const box = await makeSandbox(t);
  const claude = join(box.sandbox, "bin", "claude");
  await writeFile(claude, `#!/bin/sh
printf '%s\\n' '[{"id":"memento@memento-mcp","enabled":true}]'
`);
  await chmod(claude, 0o755);
  const result = runInstall(box, [], { PATH: `${join(box.sandbox, "bin")}:/usr/bin:/bin` });
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /already uses the Memento plugin/);
  await assert.rejects(readFile(box.log, "utf8"), /ENOENT/);
});
