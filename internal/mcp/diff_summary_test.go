package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"memento-mcp/internal/gitstate"
	"memento-mcp/internal/redact"
)

func TestDiffContextDiffSummaryIncludesStagedUnstagedAndUntracked(t *testing.T) {
	root := setupDiffSummaryGitRepo(t, map[string]string{
		"staged.go":   "package example\n\nconst Value = \"base\"\n",
		"unstaged.go": "package example\n\nconst Value = \"base\"\n",
	})
	writeDiffSummaryFile(t, root, "staged.go", "package example\n\nconst Value = \"INDEX_ONLY_NEEDLE\"\n")
	runDiffSummaryGit(t, root, "add", "--", "staged.go")
	writeDiffSummaryFile(t, root, "unstaged.go", "package example\n\nconst Value = \"WORKTREE_ONLY_NEEDLE\"\n")
	writeDiffSummaryFile(t, root, "new file.go", "package example\n\nconst Value = \"NEW_FILE_ONLY_NEEDLE\"\n")

	changes := []gitstate.WorktreeChange{
		{Path: "staged.go", Staged: true},
		{Path: "unstaged.go", Unstaged: true},
		{Path: "new file.go", Untracked: true},
	}
	got, err := buildDiffContextDiffSummary(context.Background(), root, changes, 16_000, 2, redact.Default())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Format != diffContextFormat || got.ContextLines != 2 {
		t.Fatalf("unexpected summary metadata: %#v", got)
	}
	if len(got.Sections) != 3 {
		t.Fatalf("sections = %#v, want staged, unstaged, and untracked", got.Sections)
	}
	wantScopes := []string{diffContextScopeStaged, diffContextScopeUnstaged, diffContextScopeUntracked}
	wantNeedles := []string{"INDEX_ONLY_NEEDLE", "WORKTREE_ONLY_NEEDLE", "NEW_FILE_ONLY_NEEDLE"}
	used := 0
	for index, section := range got.Sections {
		if section.Scope != wantScopes[index] || !strings.Contains(section.Text, wantNeedles[index]) {
			t.Fatalf("section %d = %#v", index, section)
		}
		for otherIndex, otherNeedle := range wantNeedles {
			if otherIndex != index && strings.Contains(section.Text, otherNeedle) {
				t.Fatalf("section %q leaked another scope's %q: %s", section.Scope, otherNeedle, section.Text)
			}
		}
		if section.UsedBytes != len(section.Text) {
			t.Fatalf("section bytes = %d, want %d", section.UsedBytes, len(section.Text))
		}
		used += section.UsedBytes
	}
	if got.UsedBytes != used || got.UsedBytes > got.MaxBytes || got.Truncated {
		t.Fatalf("unexpected aggregate limits: %#v", got)
	}
}

func TestDiffContextDiffSummarySharesOneByteLimitAcrossSections(t *testing.T) {
	root := setupDiffSummaryGitRepo(t, map[string]string{
		"staged.go": "package example\nconst Value = \"base\"\n",
	})
	writeDiffSummaryFile(t, root, "staged.go", "package example\nconst Value = \"INDEX_DELTA\"\n")
	runDiffSummaryGit(t, root, "add", "--", "staged.go")
	writeDiffSummaryFile(t, root, "new.go", "package example\nconst Value = \"UNTRACKED_DELTA\"\n")

	got, err := buildDiffContextDiffSummary(
		context.Background(),
		root,
		[]gitstate.WorktreeChange{
			{Path: "staged.go", Staged: true},
			{Path: "new.go", Untracked: true},
		},
		32,
		1,
		redact.Default(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sections) != 2 {
		t.Fatalf("sections = %#v, want both requested scopes represented", got.Sections)
	}
	if got.UsedBytes > got.MaxBytes || got.UsedBytes != len(got.Sections[0].Text)+len(got.Sections[1].Text) {
		t.Fatalf("aggregate byte limit not enforced: %#v", got)
	}
	if got.Sections[1].Text != "" || !got.Sections[1].Truncated || !got.Truncated {
		t.Fatalf("budget-exhausted section = %#v, summary = %#v", got.Sections[1], got)
	}
}

func TestDiffContextDiffSummaryBoundsRedactsAndPreservesUTF8(t *testing.T) {
	root := setupDiffSummaryGitRepo(t, map[string]string{
		"secret.go": "package example\n",
	})
	secret := "sk-proj-ABCDEFGHIJKLMNOPQRSTUVWXYZ123456"
	content := "package example\nAPI_KEY=" + secret + "\n" + strings.Repeat("é界", 400) + "\n"
	writeDiffSummaryFile(t, root, "secret.go", content)

	got, err := buildDiffContextDiffSummary(
		context.Background(),
		root,
		[]gitstate.WorktreeChange{{Path: "secret.go", Unstaged: true}},
		180,
		1,
		redact.Default(),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("diff leaked secret: %s", encoded)
	}
	if got.UsedBytes > got.MaxBytes || !got.Truncated {
		t.Fatalf("expected bounded truncated result, got %#v", got)
	}
	for _, section := range got.Sections {
		if !utf8.ValidString(section.Text) {
			t.Fatalf("section is not valid UTF-8: %q", section.Text)
		}
	}
}

func TestDiffContextDiffSummaryUsesLiteralPathspecs(t *testing.T) {
	magicPath := ":(glob)*.go"
	root := setupDiffSummaryGitRepo(t, map[string]string{
		magicPath:  "package example\nconst Value = \"base\"\n",
		"other.go": "package example\nconst Value = \"base\"\n",
	})
	writeDiffSummaryFile(t, root, magicPath, "package example\nconst Value = \"MAGIC_NEEDLE\"\n")
	writeDiffSummaryFile(t, root, "other.go", "package example\nconst Value = \"OTHER_NEEDLE\"\n")

	got, err := buildDiffContextDiffSummary(
		context.Background(),
		root,
		[]gitstate.WorktreeChange{{Path: magicPath, Unstaged: true}},
		16_000,
		1,
		redact.Default(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sections) != 1 || !strings.Contains(got.Sections[0].Text, "MAGIC_NEEDLE") {
		t.Fatalf("literal path diff missing target: %#v", got.Sections)
	}
	if strings.Contains(got.Sections[0].Text, "OTHER_NEEDLE") {
		t.Fatalf("pathspec magic expanded to another file: %s", got.Sections[0].Text)
	}
}

func TestDiffContextDiffSummaryRepresentsBinaryWithoutRawBytes(t *testing.T) {
	root := setupDiffSummaryGitRepoBytes(t, map[string][]byte{
		"tracked.bin": {0, 1, 2, 3},
	})
	if err := os.WriteFile(filepath.Join(root, "tracked.bin"), []byte{0, 9, 8, 7}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.bin"), []byte{0, 0xff, 5, 6}, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := buildDiffContextDiffSummary(
		context.Background(),
		root,
		[]gitstate.WorktreeChange{
			{Path: "tracked.bin", Unstaged: true},
			{Path: "new.bin", Untracked: true},
		},
		16_000,
		1,
		redact.Default(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sections) != 2 {
		t.Fatalf("binary sections = %#v", got.Sections)
	}
	for _, section := range got.Sections {
		if !strings.Contains(section.Text, "Binary files") || strings.IndexByte(section.Text, 0) >= 0 || !utf8.ValidString(section.Text) {
			t.Fatalf("unsafe binary section: %#v", section)
		}
	}
}

func TestDiffContextDiffSummaryOmitsUntrackedSymlinkWithoutReadingTarget(t *testing.T) {
	root := setupDiffSummaryGitRepo(t, map[string]string{"tracked.go": "package tracked\n"})
	secret := "OUTSIDE-SYMLINK-SECRET-ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	got, err := buildDiffContextDiffSummary(
		context.Background(),
		root,
		[]gitstate.WorktreeChange{{Path: "linked.go", Untracked: true}},
		16_000,
		1,
		redact.Default(),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) || len(got.Sections) != 0 {
		t.Fatalf("untracked symlink target was exposed: %s", encoded)
	}
}

func TestDiffContextDiffSummarySuppressesRawCaptureWhenCustomRedactionCouldSpanBoundary(t *testing.T) {
	root := setupDiffSummaryGitRepo(t, map[string]string{"large.go": "package example\n"})
	prefix := "CUSTOM-BEGIN-sensitive-prefix"
	content := "package example\n" + prefix + strings.Repeat("x", diffRawCaptureLimit(128)+1024) + "CUSTOM-END\n"
	writeDiffSummaryFile(t, root, "large.go", content)
	custom, err := redact.New(redact.Config{
		EntropyDisabled:    true,
		AdditionalPatterns: []string{`(?s)CUSTOM-BEGIN.*CUSTOM-END`},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := buildDiffContextDiffSummary(
		context.Background(),
		root,
		[]gitstate.WorktreeChange{{Path: "large.go", Unstaged: true}},
		128,
		1,
		custom,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sections) != 1 || !got.Sections[0].Truncated || got.Sections[0].Text != diffCaptureOmittedMarker {
		t.Fatalf("capture-boundary result = %#v", got)
	}
	if strings.Contains(got.Sections[0].Text, prefix) {
		t.Fatalf("capture-boundary prefix leaked: %q", got.Sections[0].Text)
	}
}

func TestDiffContextDiffSummaryHonorsCancellation(t *testing.T) {
	root := setupDiffSummaryGitRepo(t, map[string]string{"changed.go": "package example\n"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := buildDiffContextDiffSummary(
		ctx,
		root,
		[]gitstate.WorktreeChange{{Path: "changed.go", Unstaged: true}},
		16_000,
		1,
		redact.Default(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestDiffContextDiffSummaryIsUnavailableOutsideGit(t *testing.T) {
	got, err := buildDiffContextDiffSummary(
		context.Background(),
		t.TempDir(),
		[]gitstate.WorktreeChange{{Path: "changed.go", Unstaged: true}},
		16_000,
		1,
		redact.Default(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Available || len(got.Sections) != 0 || got.UsedBytes != 0 {
		t.Fatalf("non-Git diff summary = %#v", got)
	}
}

func setupDiffSummaryGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	byteFiles := make(map[string][]byte, len(files))
	for path, content := range files {
		byteFiles[path] = []byte(content)
	}
	return setupDiffSummaryGitRepoBytes(t, byteFiles)
}

func setupDiffSummaryGitRepoBytes(t *testing.T, files map[string][]byte) string {
	t.Helper()
	root := t.TempDir()
	runDiffSummaryGit(t, root, "init", "-q")
	runDiffSummaryGit(t, root, "config", "user.email", "diff-summary@example.com")
	runDiffSummaryGit(t, root, "config", "user.name", "Diff Summary Test")
	for path, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runDiffSummaryGit(t, root, "add", "--", ".")
	runDiffSummaryGit(t, root, "commit", "-q", "-m", "base")
	return root
}

func writeDiffSummaryFile(t *testing.T, root, path, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runDiffSummaryGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
