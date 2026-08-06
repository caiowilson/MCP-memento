package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNoteStoreSharesMemoryAcrossLinkedWorktrees(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mainRoot := t.TempDir()
	runNoteStoreGit(t, mainRoot, "init", "-q")
	runNoteStoreGit(t, mainRoot, "config", "user.email", "memento@example.test")
	runNoteStoreGit(t, mainRoot, "config", "user.name", "Memento Test")
	if err := os.WriteFile(filepath.Join(mainRoot, "tracked.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runNoteStoreGit(t, mainRoot, "add", "tracked.txt")
	runNoteStoreGit(t, mainRoot, "commit", "-qm", "initial")

	linkedRoot := filepath.Join(t.TempDir(), "linked")
	runNoteStoreGit(t, mainRoot, "worktree", "add", "--detach", "--quiet", linkedRoot, "HEAD")
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", mainRoot, "worktree", "remove", "--force", linkedRoot).Run()
	})

	mainStore, err := NewNoteStore(mainRoot)
	if err != nil {
		t.Fatal(err)
	}
	linkedStore, err := NewNoteStore(linkedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if mainStore.path != linkedStore.path {
		t.Fatalf("linked worktree note path = %q, want shared main path %q", linkedStore.path, mainStore.path)
	}
	if linkedStore.repo == mainStore.repo {
		t.Fatalf("active checkout roots should remain distinct, both were %q", linkedStore.repo)
	}

	if _, err := mainStore.Upsert(Note{Key: "shared-handoff", Text: "retrievable from linked worktree"}); err != nil {
		t.Fatal(err)
	}
	notes, err := linkedStore.Search("retrievable", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Key != "shared-handoff" {
		t.Fatalf("linked worktree search returned %#v, want shared-handoff", notes)
	}
}

func TestMemoryScopeRootPreservesWorkspaceSubdirectory(t *testing.T) {
	mainRoot := t.TempDir()
	runNoteStoreGit(t, mainRoot, "init", "-q")
	runNoteStoreGit(t, mainRoot, "config", "user.email", "memento@example.test")
	runNoteStoreGit(t, mainRoot, "config", "user.name", "Memento Test")
	if err := os.MkdirAll(filepath.Join(mainRoot, "services", "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mainRoot, "services", "api", "main.go"), []byte("package api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runNoteStoreGit(t, mainRoot, "add", ".")
	runNoteStoreGit(t, mainRoot, "commit", "-qm", "initial")

	linkedRoot := filepath.Join(t.TempDir(), "linked")
	runNoteStoreGit(t, mainRoot, "worktree", "add", "--detach", "--quiet", linkedRoot, "HEAD")
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", mainRoot, "worktree", "remove", "--force", linkedRoot).Run()
	})

	got, err := memoryScopeRoot(filepath.Join(linkedRoot, "services", "api"))
	if err != nil {
		t.Fatal(err)
	}
	want := canonicalPath(filepath.Join(mainRoot, "services", "api"))
	if got != want {
		t.Fatalf("memoryScopeRoot() = %q, want %q", got, want)
	}
}

func TestNoteStoreSerializesConcurrentLinkedWorktreeWrites(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mainRoot := t.TempDir()
	runNoteStoreGit(t, mainRoot, "init", "-q")
	runNoteStoreGit(t, mainRoot, "config", "user.email", "memento@example.test")
	runNoteStoreGit(t, mainRoot, "config", "user.name", "Memento Test")
	if err := os.WriteFile(filepath.Join(mainRoot, "tracked.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runNoteStoreGit(t, mainRoot, "add", "tracked.txt")
	runNoteStoreGit(t, mainRoot, "commit", "-qm", "initial")

	linkedRoots := []string{
		filepath.Join(t.TempDir(), "linked-a"),
		filepath.Join(t.TempDir(), "linked-b"),
	}
	stores := make([]*NoteStore, 0, len(linkedRoots))
	for _, linkedRoot := range linkedRoots {
		runNoteStoreGit(t, mainRoot, "worktree", "add", "--detach", "--quiet", linkedRoot, "HEAD")
		linkedRoot := linkedRoot
		t.Cleanup(func() {
			_ = exec.Command("git", "-C", mainRoot, "worktree", "remove", "--force", linkedRoot).Run()
		})
		store, err := NewNoteStore(linkedRoot)
		if err != nil {
			t.Fatal(err)
		}
		stores = append(stores, store)
	}

	const writesPerStore = 20
	start := make(chan struct{})
	errs := make(chan error, len(stores))
	var wg sync.WaitGroup
	for storeIndex, store := range stores {
		storeIndex, store := storeIndex, store
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for noteIndex := 0; noteIndex < writesPerStore; noteIndex++ {
				key := fmt.Sprintf("worktree-%d-note-%d", storeIndex, noteIndex)
				if _, err := store.Upsert(Note{Key: key, Text: key}); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	mainStore, err := NewNoteStore(mainRoot)
	if err != nil {
		t.Fatal(err)
	}
	notes, err := mainStore.List()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(notes), len(stores)*writesPerStore; got != want {
		t.Fatalf("concurrent note count = %d, want %d", got, want)
	}
}

func TestNoteStoreMigratesLegacyLinkedWorktreeNotes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mainRoot := t.TempDir()
	runNoteStoreGit(t, mainRoot, "init", "-q")
	runNoteStoreGit(t, mainRoot, "config", "user.email", "memento@example.test")
	runNoteStoreGit(t, mainRoot, "config", "user.name", "Memento Test")
	if err := os.WriteFile(filepath.Join(mainRoot, "tracked.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runNoteStoreGit(t, mainRoot, "add", "tracked.txt")
	runNoteStoreGit(t, mainRoot, "commit", "-qm", "initial")

	linkedRoot := filepath.Join(t.TempDir(), "linked")
	runNoteStoreGit(t, mainRoot, "worktree", "add", "--detach", "--quiet", linkedRoot, "HEAD")
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", mainRoot, "worktree", "remove", "--force", linkedRoot).Run()
	})

	legacyPath := noteStorePath(home, linkedRoot)
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := noteFile{Repo: linkedRoot, Notes: []Note{{
		Key: "legacy-handoff", Text: "preserved during migration", Status: NoteStatusFresh,
		UpdatedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	}}}
	encoded, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewNoteStore(linkedRoot)
	if err != nil {
		t.Fatal(err)
	}
	notes, err := store.Search("preserved", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Key != "legacy-handoff" {
		t.Fatalf("migrated notes = %#v, want legacy-handoff", notes)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy note file should be archived, stat error = %v", err)
	}
	archives, err := filepath.Glob(legacyPath + ".migrated-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 || !strings.HasPrefix(archives[0], legacyPath+".migrated-") {
		t.Fatalf("legacy archives = %#v, want one migrated archive", archives)
	}
}

func runNoteStoreGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmdArgs := append([]string{"-C", root}, args...)
	if out, err := exec.Command("git", cmdArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
