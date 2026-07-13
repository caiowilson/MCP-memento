package indexing

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"memento-mcp/internal/gitstate"
)

func TestGitStatusChangesUsesStructuredRenameDirectionAndPreservesWhitespace(t *testing.T) {
	root := t.TempDir()
	gitChangeTest(t, root, "init", "-q")
	gitChangeTest(t, root, "config", "user.email", "test@example.com")
	gitChangeTest(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, " old.go "), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deleted.go"), []byte("deleted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitChangeTest(t, root, "add", ".")
	gitChangeTest(t, root, "commit", "-qm", "initial")
	gitChangeTest(t, root, "mv", " old.go ", "new.go")
	if err := os.Remove(filepath.Join(root, "deleted.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, " untracked.go "), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	add, del, err := gitStatusChanges(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{" untracked.go ", "new.go"}; !reflect.DeepEqual(add, want) {
		t.Fatalf("add paths = %#v, want %#v", add, want)
	}
	if want := []string{" old.go ", "deleted.go"}; !reflect.DeepEqual(del, want) {
		t.Fatalf("delete paths = %#v, want %#v", del, want)
	}
}

func TestGitChangeMonitorReindexesWhenIgnoreFileChanges(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.go"), []byte("package secret\n\nconst SecretNeedle = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	idx.Start(ctx)
	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.FileChunks("secret.go"); err != nil {
		t.Fatalf("expected initial chunks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("secret.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	monitor := NewGitChangeMonitor(root, idx, 0, 0, nil)
	monitor.pendingAdd[".gitignore"] = struct{}{}
	monitor.flush()
	if _, err := idx.FileChunks("secret.go"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Git ignore change left stale chunks: %v", err)
	}
}

func TestClassifyGitStatusChangesKeepsLiveUnmergedFileIndexable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "conflict.go"), []byte("package conflict\n<<<<<<< ours\n=======\n>>>>>>> theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	add, del := classifyGitStatusChanges(root, []gitstate.WorktreeChange{{
		Path: "conflict.go", Kind: gitstate.WorktreeChangeUnmerged, Deleted: true,
	}})
	if want := []string{"conflict.go"}; !reflect.DeepEqual(add, want) {
		t.Fatalf("add paths = %#v, want %#v", add, want)
	}
	if len(del) != 0 {
		t.Fatalf("live unmerged file was classified as deleted: %#v", del)
	}
}

func gitChangeTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+filepath.Join(root, "global.gitconfig"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
