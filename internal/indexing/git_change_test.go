package indexing

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

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
	monitor := NewGitChangeMonitor(GitChangeMonitorConfig{RootAbs: root, Indexer: idx})
	monitor.pendingAdd[".gitignore"] = struct{}{}
	monitor.flush()
	if _, err := idx.FileChunks("secret.go"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Git ignore change left stale chunks: %v", err)
	}
}

func TestGitChangeMonitorReindexesActiveEditsAndCleanTransition(t *testing.T) {
	root := t.TempDir()
	gitChangeTest(t, root, "init", "-q")
	gitChangeTest(t, root, "config", "user.email", "test@example.com")
	gitChangeTest(t, root, "config", "user.name", "Test")
	path := filepath.Join(root, "active.go")
	if err := os.WriteFile(path, []byte("package active\n\nconst OriginalNeedle = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitChangeTest(t, root, "add", "active.go")
	gitChangeTest(t, root, "commit", "-qm", "initial")

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
	monitor := NewGitChangeMonitor(GitChangeMonitorConfig{
		RootAbs: root, Indexer: idx, HotPollInterval: time.Second, Debounce: time.Hour,
	})

	if err := os.WriteFile(path, []byte("package active\n\nconst FirstEditNeedle = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	firstEditTime := time.Now().Add(-time.Minute).Truncate(time.Second)
	if err := os.Chtimes(path, firstEditTime, firstEditTime); err != nil {
		t.Fatal(err)
	}
	if got := monitor.pollOnce(ctx); got != gitPollChanged {
		t.Fatalf("first edit outcome = %v, want changed", got)
	}
	monitor.flush()
	assertGitChangeSearch(t, idx, "FirstEditNeedle", true)
	if got := monitor.pollOnce(ctx); got != gitPollUnchanged {
		t.Fatalf("unchanged dirty file outcome = %v, want unchanged", got)
	}

	if err := os.WriteFile(path, []byte("package active\n\nconst SecondEditNeedle = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	secondEditTime := firstEditTime.Add(time.Second)
	if err := os.Chtimes(path, secondEditTime, secondEditTime); err != nil {
		t.Fatal(err)
	}
	if got := monitor.pollOnce(ctx); got != gitPollChanged {
		t.Fatalf("second edit outcome = %v, want changed", got)
	}
	monitor.flush()
	assertGitChangeSearch(t, idx, "SecondEditNeedle", true)
	assertGitChangeSearch(t, idx, "FirstEditNeedle", false)

	gitChangeTest(t, root, "restore", "active.go")
	if got := monitor.pollOnce(ctx); got != gitPollChanged {
		t.Fatalf("clean transition outcome = %v, want changed", got)
	}
	monitor.flush()
	assertGitChangeSearch(t, idx, "OriginalNeedle", true)
	assertGitChangeSearch(t, idx, "SecondEditNeedle", false)
}

func TestAdaptiveGitPollScheduleBacksOffResetsAndCaps(t *testing.T) {
	schedule := newAdaptiveGitPollSchedule(2*time.Second, 30*time.Second, 60*time.Second)
	for index, want := range []time.Duration{4, 8, 16, 30, 30} {
		want *= time.Second
		if got := schedule.Observe(gitPollUnchanged); got != want {
			t.Fatalf("unchanged step %d = %s, want %s", index, got, want)
		}
	}
	if got := schedule.Observe(gitPollChanged); got != 2*time.Second {
		t.Fatalf("changed interval = %s, want 2s", got)
	}
	for index, want := range []time.Duration{4, 8, 16, 32, 60, 60} {
		want *= time.Second
		if got := schedule.Observe(gitPollFailed); got != want {
			t.Fatalf("failure step %d = %s, want %s", index, got, want)
		}
	}
	if got := schedule.Observe(gitPollUnchanged); got != 30*time.Second {
		t.Fatalf("successful idle interval after failures = %s, want 30s", got)
	}
	if got := schedule.Wake(); got != 2*time.Second {
		t.Fatalf("activity wake interval = %s, want 2s", got)
	}
}

func TestGitPollJitterStaysWithinTwentyPercent(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	const nominal = 100 * time.Second
	seen := map[time.Duration]struct{}{}
	for range 100 {
		got := jitterGitPollInterval(rng, nominal)
		if got < 80*time.Second || got > 120*time.Second {
			t.Fatalf("jittered interval %s outside 80s..120s", got)
		}
		seen[got] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatal("jitter did not vary intervals")
	}
}

func TestGitChangeMonitorWakeCoalescesAndPollErrorsBackOff(t *testing.T) {
	monitor := NewGitChangeMonitor(GitChangeMonitorConfig{RootAbs: t.TempDir()})
	monitor.Wake()
	monitor.Wake()
	if got := len(monitor.wake); got != 1 {
		t.Fatalf("queued wake signals = %d, want 1", got)
	}
	if got := monitor.pollOnce(context.Background()); got != gitPollFailed {
		t.Fatalf("non-repository poll outcome = %v, want failed", got)
	}
}

func TestGitChangeDetectionRecognizesLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	gitChangeTest(t, root, "init", "-q")
	gitChangeTest(t, root, "config", "user.email", "test@example.com")
	gitChangeTest(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "tracked.go"), []byte("package tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitChangeTest(t, root, "add", "tracked.go")
	gitChangeTest(t, root, "commit", "-qm", "initial")

	linked := filepath.Join(t.TempDir(), "isolated")
	gitChangeTest(t, root, "worktree", "add", "--detach", "--quiet", linked)
	t.Cleanup(func() {
		gitChangeTest(t, root, "worktree", "remove", "--force", linked)
	})
	gitMarker, err := os.Stat(filepath.Join(linked, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if !gitMarker.Mode().IsRegular() {
		t.Fatalf("linked worktree .git marker mode = %s, want regular file", gitMarker.Mode())
	}
	if !hasGitWorktreeMarker(linked) {
		t.Fatal("linked worktree marker was not recognized without spawning Git")
	}
	if !IsGitRepo(linked) {
		t.Fatal("linked worktree was not recognized as a Git repository")
	}
	if err := os.WriteFile(filepath.Join(linked, "tracked.go"), []byte("package tracked\n\nconst LinkedNeedle = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	add, del, err := gitStatusChanges(context.Background(), linked)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tracked.go"}; !reflect.DeepEqual(add, want) {
		t.Fatalf("linked worktree add paths = %#v, want %#v", add, want)
	}
	if len(del) != 0 {
		t.Fatalf("linked worktree delete paths = %#v, want none", del)
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

func assertGitChangeSearch(t *testing.T, idx *Indexer, query string, want bool) {
	t.Helper()
	results, err := idx.Search(query, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(results) > 0; got != want {
		t.Fatalf("search %q found=%t, want %t", query, got, want)
	}
}
