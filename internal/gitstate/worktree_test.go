package gitstate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParsePorcelainV1ZReturnsStructuredDeterministicChanges(t *testing.T) {
	raw := []byte(
		"?? sub/untracked.go\x00" +
			"R  sub/new.go\x00sub/ old.go \x00" +
			"MM sub/both.go\x00" +
			" D sub/deleted.go\x00" +
			"C  sub/copied.go\x00sub/source.go\x00",
	)

	changes, err := parsePorcelainV1Z(raw, "sub/")
	if err != nil {
		t.Fatal(err)
	}
	want := []WorktreeChange{
		{
			Path: "both.go", IndexStatus: "M", WorktreeStatus: "M", Kind: WorktreeChangeModified,
			Staged: true, Unstaged: true,
		},
		{
			Path: "copied.go", PreviousPath: "source.go", IndexStatus: "C", WorktreeStatus: " ", Kind: WorktreeChangeCopied,
			Staged: true, Copied: true,
		},
		{
			Path: "deleted.go", IndexStatus: " ", WorktreeStatus: "D", Kind: WorktreeChangeDeleted,
			Unstaged: true, Deleted: true,
		},
		{
			Path: "new.go", PreviousPath: " old.go ", IndexStatus: "R", WorktreeStatus: " ", Kind: WorktreeChangeRenamed,
			Staged: true, Renamed: true,
		},
		{
			Path: "untracked.go", IndexStatus: "?", WorktreeStatus: "?", Kind: WorktreeChangeUntracked,
			Untracked: true,
		},
	}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("changes mismatch\n got: %#v\nwant: %#v", changes, want)
	}
}

func TestParsePorcelainV1ZRejectsMalformedRenameAndTermination(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  []byte
	}{
		{name: "missing previous path", raw: []byte("R  new.go\x00")},
		{name: "missing terminator", raw: []byte(" M file.go")},
		{name: "short entry", raw: []byte(" M\x00")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parsePorcelainV1Z(test.raw, ""); err == nil {
				t.Fatal("expected malformed porcelain error")
			}
		})
	}
}

func TestLoadWorktreeChangesScopesNestedWorkspaceAndPreservesWhitespace(t *testing.T) {
	repo := t.TempDir()
	gitWorktreeTest(t, repo, "init", "-q")
	gitWorktreeTest(t, repo, "config", "user.email", "test@example.com")
	gitWorktreeTest(t, repo, "config", "user.name", "Test")

	writeWorktreeTestFile(t, repo, "workspace/ old.go ", "old\n")
	writeWorktreeTestFile(t, repo, "workspace/deleted.go", "deleted\n")
	writeWorktreeTestFile(t, repo, "sibling/ignored.go", "sibling\n")
	gitWorktreeTest(t, repo, "add", ".")
	gitWorktreeTest(t, repo, "commit", "-qm", "initial")
	gitWorktreeTest(t, repo, "config", "status.renames", "false")
	gitWorktreeTest(t, repo, "mv", "workspace/ old.go ", "workspace/new.go")
	if err := os.Remove(filepath.Join(repo, "workspace", "deleted.go")); err != nil {
		t.Fatal(err)
	}
	writeWorktreeTestFile(t, repo, "workspace/ untracked.go ", "untracked\n")
	writeWorktreeTestFile(t, repo, "sibling/outside.go", "outside\n")

	changes, err := LoadWorktreeChanges(context.Background(), filepath.Join(repo, "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	want := []WorktreeChange{
		{
			Path: " untracked.go ", IndexStatus: "?", WorktreeStatus: "?", Kind: WorktreeChangeUntracked,
			Untracked: true,
		},
		{
			Path: "deleted.go", IndexStatus: " ", WorktreeStatus: "D", Kind: WorktreeChangeDeleted,
			Unstaged: true, Deleted: true,
		},
		{
			Path: "new.go", PreviousPath: " old.go ", IndexStatus: "R", WorktreeStatus: " ", Kind: WorktreeChangeRenamed,
			Staged: true, Renamed: true,
		},
	}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("changes mismatch\n got: %#v\nwant: %#v", changes, want)
	}
}

func TestLoadWorktreeChangesReturnsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := LoadWorktreeChanges(ctx, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestBoundedGitBufferCapsWhileDrainingWrites(t *testing.T) {
	buffer := newBoundedGitBuffer(4)
	if written, err := buffer.Write([]byte("abcdef")); err != nil || written != 6 {
		t.Fatalf("Write() = (%d, %v), want (6, nil)", written, err)
	}
	if got := buffer.buf.String(); got != "abcd" || !buffer.truncated {
		t.Fatalf("bounded buffer = %q, truncated=%v", got, buffer.truncated)
	}
}

func gitWorktreeTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+filepath.Join(root, "global.gitconfig"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func writeWorktreeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
