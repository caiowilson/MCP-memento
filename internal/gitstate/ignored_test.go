package gitstate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLoadIgnoredPathsUsesAllStandardGitExcludesAndKeepsTrackedFiles(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+filepath.Join(root, "global.gitconfig"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "root.log\nnested/ignored/\ntracked.log\n")
	write("nested/.gitignore", "local.tmp\n")
	write(".git/info/exclude", "info.secret\n")
	write("global.gitconfig", "[core]\n\texcludesFile = "+filepath.Join(root, "global-ignore")+"\n")
	write("global-ignore", "*.global\n")
	write("root.log", "ignored")
	write("nested/local.tmp", "ignored")
	write("nested/ignored/file.go", "ignored")
	write("info.secret", "ignored")
	write("machine.global", "ignored")
	write("tracked.log", "tracked")
	git("add", "-f", "tracked.log")

	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "global.gitconfig"))
	ignored := LoadIgnoredPaths(root)
	for _, rel := range []string{"root.log", "nested/local.tmp", "nested/ignored", "nested/ignored/file.go", "info.secret", "machine.global"} {
		if !ignored.Matches(rel) {
			t.Errorf("expected %s to be ignored", rel)
		}
	}
	if ignored.Matches("tracked.log") {
		t.Error("tracked files must remain visible even when an ignore pattern matches")
	}
}

func TestLoadIgnoredPathsOutsideGitReturnsEmpty(t *testing.T) {
	if LoadIgnoredPaths(t.TempDir()).Matches("anything.log") {
		t.Fatal("non-Git workspace should return an empty Git-ignore snapshot")
	}
}

func TestLoadIgnoredPathsContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := LoadIgnoredPathsContext(ctx, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestIgnoredPathsSafetyLimitStateMatchesValidPathsFailClosed(t *testing.T) {
	ignored := &IgnoredPaths{failClosed: true}
	if !ignored.Matches("src/private.go") {
		t.Fatal("safety-limit snapshot did not match a valid path fail-closed")
	}
	if ignored.Matches("../outside.go") {
		t.Fatal("fail-closed snapshot accepted an invalid relative path")
	}
}
