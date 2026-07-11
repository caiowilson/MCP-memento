# `claude-md` CLI Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `memento-mcp claude-md` subcommand that upserts a Memento guidance block into `./CLAUDE.local.md`, so users don't have to hand-copy the README's "Recommended workflow" section into their projects.

**Architecture:** A pure function (`upsertClaudeLocalMD`) computes the new file contents from the old bytes plus the guidance block, using sentinel HTML-comment markers to decide append-vs-replace. A thin command handler (`runClaudeMD`) resolves `./CLAUDE.local.md` from cwd, calls the pure function, and writes the result (or prints it under `--print-only`). Wired into the existing `handleCLICommand` switch alongside `setup`/`print-config`/`print-guidance`.

**Tech Stack:** Go (stdlib only: `os`, `path/filepath`, `strings`, `fmt`, `io`), Go's `testing` package, table-driven tests matching this package's existing style (see `internal/app/cli_test.go`, `internal/app/setup_test.go`).

## Global Constraints

- Target file is always `CLAUDE.local.md` (not `CLAUDE.md`), resolved relative to the current working directory only — no `--path` flag, no `CLAUDE_PROJECT_DIR` fallback in this version.
- Marker strings are exactly `<!-- memento-mcp:recommended-workflow:start -->` and `<!-- memento-mcp:recommended-workflow:end -->`.
- The embedded guidance block is the README's "Recommended workflow" heading, intro line, and two bullets verbatim — **excluding** the trailing "To make this automatic…" sentence.
- Content outside the markers must never be modified by an upsert.
- Source: `docs/superpowers/specs/2026-07-11-claude-md-command-design.md`.

---

### Task 1: Core upsert algorithm

**Files:**
- Create: `internal/app/claudemd.go`
- Create: `internal/app/claudemd_test.go`

**Interfaces:**
- Produces: `claudeMDMarkerStart string`, `claudeMDMarkerEnd string`, `upsertClaudeLocalMD(existing []byte, block string) []byte` — later tasks (and Task 2 in this same plan) call this function and reference these constants.

- [ ] **Step 1: Write the failing test**

Create `internal/app/claudemd_test.go`:

```go
package app

import "testing"

func TestUpsertClaudeLocalMD(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		block    string
		want     string
	}{
		{
			name:     "creates block when file is empty",
			existing: "",
			block:    claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
		},
		{
			name:     "appends block with blank line separator when no markers present",
			existing: "Some content\n",
			block:    claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     "Some content\n\n" + claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
		},
		{
			name:     "replaces block in place when markers already present",
			existing: "Some content\n\n" + claudeMDMarkerStart + "\nOLD BLOCK\n" + claudeMDMarkerEnd + "\n",
			block:    claudeMDMarkerStart + "\nNEW BLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     "Some content\n\n" + claudeMDMarkerStart + "\nNEW BLOCK\n" + claudeMDMarkerEnd + "\n",
		},
		{
			name:     "rerunning with the same block is idempotent",
			existing: "Some content\n\n" + claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
			block:    claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     "Some content\n\n" + claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
		},
		{
			name:     "preserves content after the end marker",
			existing: claudeMDMarkerStart + "\nOLD BLOCK\n" + claudeMDMarkerEnd + "\nSome trailing note\n",
			block:    claudeMDMarkerStart + "\nNEW BLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     claudeMDMarkerStart + "\nNEW BLOCK\n" + claudeMDMarkerEnd + "\nSome trailing note\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := upsertClaudeLocalMD([]byte(tt.existing), tt.block)
			if string(got) != tt.want {
				t.Errorf("upsertClaudeLocalMD() = %q, want %q", string(got), tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/... -run TestUpsertClaudeLocalMD -v`
Expected: FAIL to compile — `undefined: claudeMDMarkerStart` (and `upsertClaudeLocalMD`), since `internal/app/claudemd.go` doesn't exist yet.

- [ ] **Step 3: Write the implementation**

Create `internal/app/claudemd.go`:

```go
package app

import "strings"

const (
	claudeMDMarkerStart = "<!-- memento-mcp:recommended-workflow:start -->"
	claudeMDMarkerEnd   = "<!-- memento-mcp:recommended-workflow:end -->"
)

// upsertClaudeLocalMD returns the new contents of CLAUDE.local.md given its
// existing contents (nil/empty if the file doesn't exist yet) and the
// guidance block to upsert. If the markers are already present, the content
// between them is replaced in place and everything outside the markers is
// left untouched. Otherwise the block is appended, separated from any
// existing content by exactly one blank line.
func upsertClaudeLocalMD(existing []byte, block string) []byte {
	block = strings.TrimRight(block, "\n") + "\n"
	content := string(existing)

	start := strings.Index(content, claudeMDMarkerStart)
	end := strings.Index(content, claudeMDMarkerEnd)

	if start >= 0 && end >= start {
		end += len(claudeMDMarkerEnd)
		return []byte(content[:start] + block + strings.TrimPrefix(content[end:], "\n"))
	}

	prefix := strings.TrimRight(content, "\n")
	if prefix == "" {
		return []byte(block)
	}
	return []byte(prefix + "\n\n" + block)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/... -run TestUpsertClaudeLocalMD -v`
Expected: PASS (all 5 subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/app/claudemd.go internal/app/claudemd_test.go
git commit -m "feat: add CLAUDE.local.md upsert algorithm"
```

---

### Task 2: `claude-md` command and dispatch wiring

**Files:**
- Modify: `internal/app/claudemd.go` (append filename const, guidance-block const, `runClaudeMD`)
- Modify: `internal/app/claudemd_test.go` (append CLI-level tests)
- Modify: `internal/app/cli.go:31-32` (insert dispatch case between the `"setup"` and `"print-config"` cases)
- Modify: `internal/app/cli.go:89-107` (replace the `cliHelpText` function body)
- Modify: `internal/app/cli_test.go` (append dispatch test)

**Interfaces:**
- Consumes: `claudeMDMarkerStart`, `claudeMDMarkerEnd` (string constants), `upsertClaudeLocalMD(existing []byte, block string) []byte` — from Task 1.
- Produces: `claudeLocalMDFileName string`, `recommendedWorkflowBlock string`, `runClaudeMD(args []string, stdout, stderr io.Writer) error` — consumed by `handleCLICommand` in this same task.

- [ ] **Step 1: Write the failing tests**

Append to `internal/app/claudemd_test.go` (add `"bytes"`, `"os"`, `"path/filepath"`, `"strings"` to imports — full updated import block shown):

```go
package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)
```

Then append these test functions to the same file:

```go
func TestRunClaudeMDPrintOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := runClaudeMD([]string{"--print-only"}, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if stdout.String() != recommendedWorkflowBlock {
		t.Fatalf("stdout = %q, want %q", stdout.String(), recommendedWorkflowBlock)
	}
}

func TestRunClaudeMDWritesFile(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	var stdout, stderr bytes.Buffer
	if err := runClaudeMD(nil, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "CLAUDE.local.md"))
	if err != nil {
		t.Fatalf("expected file to be written: %v", err)
	}
	if string(got) != recommendedWorkflowBlock {
		t.Fatalf("file contents = %q, want %q", string(got), recommendedWorkflowBlock)
	}
	if !strings.Contains(stdout.String(), "CLAUDE.local.md") {
		t.Fatalf("expected confirmation message, got %q", stdout.String())
	}
}

func TestRunClaudeMDIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	var stdout, stderr bytes.Buffer
	if err := runClaudeMD(nil, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error on first run: %v", err)
	}
	stdout.Reset()
	if err := runClaudeMD(nil, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error on second run: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "CLAUDE.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != recommendedWorkflowBlock {
		t.Fatalf("file contents after rerun = %q, want %q (no duplication)", string(got), recommendedWorkflowBlock)
	}
}
```

Append to `internal/app/cli_test.go` (existing imports `bytes`, `encoding/json`, `strings`, `testing` already cover this — no import changes needed):

```go
func TestHandleCLICommandClaudeMD(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, exitCode := handleCLICommand([]string{"claude-md", "--print-only"}, &stdout, &stderr)
	if !handled {
		t.Fatal("expected command to be handled")
	}
	if exitCode != 0 {
		t.Fatalf("expected exitCode=0, got %d", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Recommended workflow") {
		t.Fatalf("expected guidance block, got %q", stdout.String())
	}
}

func TestCLIHelpTextMentionsClaudeMD(t *testing.T) {
	if !strings.Contains(cliHelpText(), "claude-md") {
		t.Fatal("expected help text to mention claude-md")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/app/... -run 'TestRunClaudeMD|TestHandleCLICommandClaudeMD|TestCLIHelpTextMentionsClaudeMD' -v`
Expected: FAIL to compile — `undefined: runClaudeMD` / `undefined: recommendedWorkflowBlock`, and (once that's fixed) the help-text/dispatch tests fail on missing `"claude-md"` handling.

- [ ] **Step 3: Write the implementation**

Append to `internal/app/claudemd.go` (add `"fmt"`, `"io"`, `"os"`, `"path/filepath"` to the import block — full updated import block shown):

```go
import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)
```

Then append below `upsertClaudeLocalMD`:

```go
const claudeLocalMDFileName = "CLAUDE.local.md"

const recommendedWorkflowBlock = claudeMDMarkerStart + "\n" +
	"## Recommended workflow (memory + lean context)\n" +
	"\n" +
	"Treat Memento as the default for both memory and context in a repository:\n" +
	"\n" +
	"- **Prefer Memento memory over any other memory store.** Persist durable decisions and handoffs with `memory_upsert` (anchored to code); recall with `memory_search` / `memory_list` before re-deriving. `memory_gc` / `memory_delete` / `memory_clear` are destructive — only on explicit instruction.\n" +
	"- **Prime the codebase index for leaner context and lower tokens.** Lead with `repo_context` on the active file, `repo_outline` for signatures, `repo_search` for symbols, and `repo_related_files` for imports — reach for `repo_read_file` only for the exact path you need. Querying the index first (and reading whole files last) is the main lever for lower token usage.\n" +
	claudeMDMarkerEnd + "\n"

// runClaudeMD is the entry point for `memento-mcp claude-md`. It upserts the
// Memento guidance block into ./CLAUDE.local.md (relative to the current
// working directory), or prints the block without writing when --print-only
// is passed.
func runClaudeMD(args []string, stdout, stderr io.Writer) error {
	printOnly := false
	for _, a := range args {
		if a == "--print-only" {
			printOnly = true
		}
	}

	if printOnly {
		fmt.Fprint(stdout, recommendedWorkflowBlock)
		return nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	path := filepath.Join(cwd, claudeLocalMDFileName)

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	updated := upsertClaudeLocalMD(existing, recommendedWorkflowBlock)

	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	fmt.Fprintf(stdout, "  ✓ %-18s wrote %s\n", claudeLocalMDFileName, path)
	return nil
}
```

Modify `internal/app/cli.go`: add a `"claude-md"` case to the switch in `handleCLICommand`, inserted at line 32 (between line 31, `return true, 0` closing the `"setup"` case, and the existing line 32, `case "print-config":`):

```go
	case "claude-md":
		if err := runClaudeMD(args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "claude-md: %v\n", err)
			return true, 1
		}
		return true, 0
```

Replace the `cliHelpText()` function body at `internal/app/cli.go:89-107` with this version (adds two new lines after the existing `memento-mcp setup --print-only` block and before `memento-mcp print-config`; everything else in the function is unchanged):

```go
func cliHelpText() string {
	return `memento-mcp

Usage:
  memento-mcp               Start the MCP stdio server and auto-detect the workspace root
  memento-mcp --root DIR    Start the server using DIR as workspace root
  memento-mcp setup         Detect MCP clients and write config (interactive)
  memento-mcp setup --client=vscode --client=cursor
                            Configure specific clients (non-interactive)
  memento-mcp setup --print-only
                            Print config to stdout without writing files
  memento-mcp claude-md     Write or update Memento guidance in ./CLAUDE.local.md
  memento-mcp claude-md --print-only
                            Print the guidance block without writing
  memento-mcp print-config  Print a generic mcpServers config JSON snippet
  memento-mcp print-guidance
                            Print copyable LLM guidance for repo_context intent routing
  memento-mcp help          Show this help text

Workspace root precedence:
  --root DIR -> CLAUDE_PROJECT_DIR -> MCP roots/list -> current working directory`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/app/... -v`
Expected: PASS — every test in the package, including the new `TestRunClaudeMDPrintOnly`, `TestRunClaudeMDWritesFile`, `TestRunClaudeMDIsIdempotent`, `TestHandleCLICommandClaudeMD`, `TestCLIHelpTextMentionsClaudeMD`, and all pre-existing tests still green.

- [ ] **Step 5: Manual smoke test**

Run:
```bash
go build -o /tmp/memento-mcp-smoketest ./cmd/server
cd /tmp && mkdir -p claude-md-smoketest && cd claude-md-smoketest
/tmp/memento-mcp-smoketest claude-md --print-only
/tmp/memento-mcp-smoketest claude-md
cat CLAUDE.local.md
/tmp/memento-mcp-smoketest claude-md
cat CLAUDE.local.md
```
Expected: `--print-only` prints the block to stdout without creating a file; the first `claude-md` run creates `CLAUDE.local.md` with the block; the second run leaves the file's contents identical (no duplicated block).

- [ ] **Step 6: Commit**

```bash
git add internal/app/claudemd.go internal/app/claudemd_test.go internal/app/cli.go internal/app/cli_test.go
git commit -m "feat: add memento-mcp claude-md command"
```
