package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAnchoredNoteDetectsChurnAndRemainsSearchable(t *testing.T) {
	store, root := newLifecycleTestStore(t, true)
	writeLifecycleFile(t, root, "service.go", "package service\n\nfunc Start() string {\n\treturn \"v1\"\n}\n")
	gitLifecycle(t, root, "add", "service.go")
	gitLifecycle(t, root, "commit", "-m", "add service")

	note, err := store.Upsert(Note{Key: "service", Text: "Start returns v1", Anchors: []NoteAnchor{{Path: "service.go", Symbol: "Start"}}})
	if err != nil {
		t.Fatal(err)
	}
	if note.Status != NoteStatusFresh || len(note.Anchors) != 1 || note.Anchors[0].ContentHash == "" || note.Anchors[0].CommitSHA == "" || note.Anchors[0].StartLine == 0 {
		t.Fatalf("expected hydrated fresh anchor, got %#v", note)
	}

	for version := 2; version <= 4; version++ {
		writeLifecycleFile(t, root, "service.go", "package service\n\nfunc Start() string {\n\treturn \"v"+string(rune('0'+version))+"\"\n}\n")
		if err := store.ReconcileChanged([]string{"service.go"}); err != nil {
			t.Fatal(err)
		}
	}
	notes, err := store.Search("returns", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Status != NoteStatusStale || notes[0].FailedAdjudications != 0 || notes[0].RetrievalCount != 1 {
		t.Fatalf("expected visible stale note without deletion pressure, got %#v", notes)
	}
}

func TestMemorySearchRanksFreshBeforeStale(t *testing.T) {
	store, _ := newLifecycleTestStore(t, false)
	if _, err := store.Upsert(Note{Key: "stale", Text: "shared decision"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(Note{Key: "fresh", Text: "shared decision"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkStale("stale", "superseded", false, false); err != nil {
		t.Fatal(err)
	}
	notes, err := store.Search("shared", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || notes[0].Key != "fresh" || notes[1].Key != "stale" {
		t.Fatalf("expected fresh note before stale note, got %#v", notes)
	}
}

func TestAnchoredRenameFlagsButDoesNotOrphan(t *testing.T) {
	store, root := newLifecycleTestStore(t, true)
	writeLifecycleFile(t, root, "old.go", "package fixture\n\nfunc Run() {}\n")
	gitLifecycle(t, root, "add", "old.go")
	gitLifecycle(t, root, "commit", "-m", "add old")
	if _, err := store.Upsert(Note{Key: "rename", Text: "Run exists", Anchors: []NoteAnchor{{Path: "old.go", Symbol: "Run"}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "old.go"), filepath.Join(root, "new.go")); err != nil {
		t.Fatal(err)
	}
	gitLifecycle(t, root, "add", "-A")
	gitLifecycle(t, root, "commit", "-m", "rename file")
	if err := store.ReconcileChanged([]string{"old.go", "new.go"}); err != nil {
		t.Fatal(err)
	}
	notes, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Status != NoteStatusStale || notes[0].Orphaned || notes[0].Anchors[0].Path != "new.go" {
		t.Fatalf("expected recoverable rename, got %#v", notes)
	}
}

func TestAnchoredSymbolMoveSurvivesInconclusiveGitRename(t *testing.T) {
	store, root := newLifecycleTestStore(t, true)
	writeLifecycleFile(t, root, "old.go", "package fixture\n\nfunc Run() string { return \"old\" }\n")
	gitLifecycle(t, root, "add", "old.go")
	gitLifecycle(t, root, "commit", "-m", "add old")
	if _, err := store.Upsert(Note{Key: "move", Text: "Run contract", Anchors: []NoteAnchor{{Path: "old.go", Symbol: "Run"}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "old.go")); err != nil {
		t.Fatal(err)
	}
	writeLifecycleFile(t, root, "nested/new.go", "package nested\n\n// Run now has substantially different implementation.\nfunc Run() string {\n\tvalue := \"new\"\n\treturn value\n}\n")
	if err := store.ReconcileChanged([]string{"old.go", "nested/new.go"}); err != nil {
		t.Fatal(err)
	}
	notes, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Status != NoteStatusStale || notes[0].Orphaned || notes[0].Anchors[0].Path != "nested/new.go" {
		t.Fatalf("expected symbol move to remain recoverable, got %#v", notes)
	}
}

func TestBranchWhereAnchorIsAbsentDoesNotOrphan(t *testing.T) {
	store, root := newLifecycleTestStore(t, true)
	writeLifecycleFile(t, root, "branch.go", "package fixture\n\nfunc BranchOnly() {}\n")
	gitLifecycle(t, root, "add", "branch.go")
	gitLifecycle(t, root, "commit", "-m", "add branch file")
	if _, err := store.Upsert(Note{Key: "branch", Text: "branch contract", Anchors: []NoteAnchor{{Path: "branch.go", Symbol: "BranchOnly"}}}); err != nil {
		t.Fatal(err)
	}
	gitLifecycle(t, root, "switch", "-c", "without-anchor")
	gitLifecycle(t, root, "rm", "branch.go")
	gitLifecycle(t, root, "commit", "-m", "remove on branch")
	if err := store.ReconcileChanged([]string{"branch.go"}); err != nil {
		t.Fatal(err)
	}
	notes, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Orphaned || notes[0].Status == NoteStatusTombstoned {
		t.Fatalf("branch absence must not orphan the note: %#v", notes)
	}
}

func TestDetachedCheckoutWhereAnchorIsAbsentDoesNotOrphan(t *testing.T) {
	store, root := newLifecycleTestStore(t, true)
	writeLifecycleFile(t, root, "README.md", "base\n")
	gitLifecycle(t, root, "add", "README.md")
	gitLifecycle(t, root, "commit", "-m", "base")
	writeLifecycleFile(t, root, "detached.go", "package fixture\n\nfunc Detached() {}\n")
	gitLifecycle(t, root, "add", "detached.go")
	gitLifecycle(t, root, "commit", "-m", "add detached")
	if _, err := store.Upsert(Note{Key: "detached", Text: "detached contract", Anchors: []NoteAnchor{{Path: "detached.go", Symbol: "Detached"}}}); err != nil {
		t.Fatal(err)
	}
	gitLifecycle(t, root, "checkout", "--detach", "HEAD^")
	if err := store.ReconcileChanged([]string{"detached.go"}); err != nil {
		t.Fatal(err)
	}
	notes, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Orphaned || notes[0].Status == NoteStatusTombstoned {
		t.Fatalf("detached checkout absence must not orphan the note: %#v", notes)
	}
}

func TestJavaScriptAndPHPMethodAnchorsIncludeBodies(t *testing.T) {
	tests := []struct {
		path   string
		symbol string
		before string
		after  string
	}{
		{path: "service.ts", symbol: "Service.run", before: "export class Service {\n  run(): string {\n    return 'v1';\n  }\n}\n", after: "export class Service {\n  run(): string {\n    return 'v2';\n  }\n}\n"},
		{path: "service.php", symbol: "Service.run", before: "<?php\nclass Service {\n  public function run(): string {\n    return 'v1';\n  }\n}\n", after: "<?php\nclass Service {\n  public function run(): string {\n    return 'v2';\n  }\n}\n"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			store, root := newLifecycleTestStore(t, false)
			writeLifecycleFile(t, root, test.path, test.before)
			if _, err := store.Upsert(Note{Key: test.path, Text: "body contract", Anchors: []NoteAnchor{{Path: test.path, Symbol: test.symbol}}}); err != nil {
				t.Fatal(err)
			}
			writeLifecycleFile(t, root, test.path, test.after)
			if err := store.ReconcileChanged([]string{test.path}); err != nil {
				t.Fatal(err)
			}
			notes, err := store.List()
			if err != nil {
				t.Fatal(err)
			}
			if len(notes) != 1 || notes[0].Status != NoteStatusStale {
				t.Fatalf("expected body change to stale anchored note, got %#v", notes)
			}
		})
	}
}

func TestSameLineageDeletionTombstonesWithoutHardDelete(t *testing.T) {
	store, root := newLifecycleTestStore(t, true)
	writeLifecycleFile(t, root, "removed.go", "package fixture\n\nfunc Removed() {}\n")
	gitLifecycle(t, root, "add", "removed.go")
	gitLifecycle(t, root, "commit", "-m", "add removed")
	if _, err := store.Upsert(Note{Key: "removed", Text: "old approach", Anchors: []NoteAnchor{{Path: "removed.go", Symbol: "Removed"}}}); err != nil {
		t.Fatal(err)
	}
	gitLifecycle(t, root, "rm", "removed.go")
	if err := store.ReconcileChanged([]string{"removed.go"}); err != nil {
		t.Fatal(err)
	}
	listed, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || !listed[0].Orphaned || listed[0].Status != NoteStatusTombstoned {
		t.Fatalf("expected recoverable orphan tombstone, got %#v", listed)
	}
	searched, err := store.Search("approach", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(searched) != 0 {
		t.Fatalf("expected tombstone outside active search, got %#v", searched)
	}
}

func TestMultiAnchorLossStaysRecoverableAndSearchable(t *testing.T) {
	store, root := newLifecycleTestStore(t, true)
	writeLifecycleFile(t, root, "one.go", "package fixture\n\nfunc One() {}\n")
	writeLifecycleFile(t, root, "two.go", "package fixture\n\nfunc Two() {}\n")
	gitLifecycle(t, root, "add", "one.go", "two.go")
	gitLifecycle(t, root, "commit", "-m", "add anchors")
	if _, err := store.Upsert(Note{Key: "pair", Text: "paired contract", Anchors: []NoteAnchor{{Path: "one.go", Symbol: "One"}, {Path: "two.go", Symbol: "Two"}}}); err != nil {
		t.Fatal(err)
	}
	gitLifecycle(t, root, "rm", "one.go")
	if err := store.ReconcileChanged([]string{"one.go"}); err != nil {
		t.Fatal(err)
	}
	notes, err := store.Search("paired", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Status != NoteStatusStale || notes[0].Orphaned {
		t.Fatalf("one lost anchor must not tombstone multi-anchor note: %#v", notes)
	}
}

func TestAllMultiAnchorReferentsOrphanToTombstone(t *testing.T) {
	store, root := newLifecycleTestStore(t, true)
	writeLifecycleFile(t, root, "one.go", "package fixture\n\nfunc One() {}\n")
	writeLifecycleFile(t, root, "two.go", "package fixture\n\nfunc Two() {}\n")
	gitLifecycle(t, root, "add", "one.go", "two.go")
	gitLifecycle(t, root, "commit", "-m", "add anchors")
	if _, err := store.Upsert(Note{Key: "pair", Text: "paired contract", Anchors: []NoteAnchor{{Path: "one.go", Symbol: "One"}, {Path: "two.go", Symbol: "Two"}}}); err != nil {
		t.Fatal(err)
	}
	gitLifecycle(t, root, "rm", "one.go", "two.go")
	if err := store.ReconcileChanged([]string{"one.go", "two.go"}); err != nil {
		t.Fatal(err)
	}
	notes, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Status != NoteStatusTombstoned || !notes[0].Orphaned {
		t.Fatalf("all confirmed orphan anchors should tombstone, got %#v", notes)
	}
}

func TestCommitOnlyAnchorFlagsSameBranchAdvance(t *testing.T) {
	store, root := newLifecycleTestStore(t, true)
	writeLifecycleFile(t, root, "README.md", "one\n")
	gitLifecycle(t, root, "add", "README.md")
	gitLifecycle(t, root, "commit", "-m", "one")
	note, err := store.Upsert(Note{Key: "revision", Text: "repo-wide decision", Anchors: []NoteAnchor{{CommitSHA: "HEAD"}}})
	if err != nil {
		t.Fatal(err)
	}
	if note.Anchors[0].Path != "" || note.Anchors[0].CommitSHA == "" || note.Anchors[0].Branch != "main" {
		t.Fatalf("expected hydrated commit-only anchor, got %#v", note.Anchors[0])
	}
	writeLifecycleFile(t, root, "README.md", "two\n")
	gitLifecycle(t, root, "add", "README.md")
	gitLifecycle(t, root, "commit", "-m", "two")
	if err := store.ReconcileChanged(nil); err != nil {
		t.Fatal(err)
	}
	notes, err := store.Search("repo-wide", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Status != NoteStatusStale || notes[0].Orphaned {
		t.Fatalf("expected same-branch commit drift without orphaning, got %#v", notes)
	}
}

func TestMemoryVerifyRefreshesAnchorsAndLifecycle(t *testing.T) {
	store, root := newLifecycleTestStore(t, false)
	writeLifecycleFile(t, root, "config.go", "package fixture\n\nconst Port = 1\n")
	original, err := store.Upsert(Note{Key: "config", Text: "port decision", Anchors: []NoteAnchor{{Path: "config.go", Symbol: "Port"}}})
	if err != nil {
		t.Fatal(err)
	}
	writeLifecycleFile(t, root, "config.go", "package fixture\n\nconst Port = 2\n")
	if _, err := store.MarkStale("config", "port changed", true, false); err != nil {
		t.Fatal(err)
	}
	verified, err := store.Verify("config")
	if err != nil {
		t.Fatal(err)
	}
	if verified.Status != NoteStatusFresh || verified.StaleReason != "" || verified.FailedAdjudications != 0 || verified.Anchors[0].ContentHash == original.Anchors[0].ContentHash {
		t.Fatalf("expected refreshed verified note, got %#v", verified)
	}
}

func TestMemoryGCRequiresEveryFactorAndProtectsUsage(t *testing.T) {
	store, _ := newLifecycleTestStore(t, false)
	for _, key := range []string{"eligible", "used", "recently-used", "not-orphaned", "not-adjudicated"} {
		if _, err := store.Upsert(Note{Key: key, Text: "gc candidate " + key}); err != nil {
			t.Fatal(err)
		}
	}
	for count := 0; count < defaultMemoryGCMaxRetrievals; count++ {
		if _, err := store.Search("gc candidate used", nil, 20); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Search("gc candidate recently-used", nil, 20); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"eligible", "used", "recently-used"} {
		if _, err := store.MarkStale(key, "orphan", true, true); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Tombstone(key, "obsolete"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.MarkStale("not-orphaned", "stale", true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Tombstone("not-orphaned", "obsolete"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkStale("not-adjudicated", "orphan", false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Tombstone("not-adjudicated", "obsolete"); err != nil {
		t.Fatal(err)
	}
	ageLifecycleTombstones(t, store, 120*24*time.Hour)
	result, err := store.GarbageCollect(memoryGCRules{MaximumRetrievalCount: 100})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 1 || len(result.DeletedKeys) != 1 || result.DeletedKeys[0] != "eligible" {
		t.Fatalf("expected only fully eligible note deleted, got %#v", result)
	}
	remaining, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 4 {
		t.Fatalf("expected protected notes to survive, got %#v", remaining)
	}
}

func TestLegacyNoteFileLoadsAsFresh(t *testing.T) {
	store, _ := newLifecycleTestStore(t, false)
	legacy := `{"repo":"legacy","notes":[{"key":"old","text":"legacy note","updatedAt":"2020-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(store.path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	notes, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Status != NoteStatusFresh {
		t.Fatalf("expected backward-compatible fresh note, got %#v", notes)
	}
}

func TestMemoryLifecycleTools(t *testing.T) {
	store, _ := newLifecycleTestStore(t, false)
	for _, tool := range []Tool{newMemorySearchTool(store), newMemoryListTool(store), memorySearchToolDefinition(), memoryListToolDefinition()} {
		if readOnly, _ := tool.Annotations["readOnlyHint"].(bool); readOnly {
			t.Fatalf("%s persists reconciliation or usage metadata and must not claim read-only", tool.Name)
		}
	}
	if _, err := store.Upsert(Note{Key: "tool-note", Text: "tool lifecycle"}); err != nil {
		t.Fatal(err)
	}
	mark := newMemoryMarkStaleTool(store)
	if _, err := mark.Handler(context.Background(), rawJSON(t, map[string]any{"key": "tool-note", "reason": "contradiction", "failedAdjudication": true})); err != nil {
		t.Fatal(err)
	}
	verify := newMemoryVerifyTool(store)
	verifiedAny, err := verify.Handler(context.Background(), json.RawMessage(`{"key":"tool-note"}`))
	if err != nil {
		t.Fatal(err)
	}
	verified := verifiedAny.(map[string]any)["note"].(Note)
	if verified.Status != NoteStatusFresh {
		t.Fatalf("expected verified fresh note, got %#v", verified)
	}
	if _, err := newMemoryTombstoneTool(store).Handler(context.Background(), rawJSON(t, map[string]any{"key": "tool-note", "reason": "obsolete"})); err != nil {
		t.Fatal(err)
	}
	if _, err := newMemoryGCTool(store).Handler(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryUpsertToolParsesAnchors(t *testing.T) {
	store, root := newLifecycleTestStore(t, false)
	writeLifecycleFile(t, root, "service.go", "package fixture\n\nfunc Start() {}\n")
	resultAny, err := newMemoryUpsertTool(store).Handler(context.Background(), rawJSON(t, map[string]any{
		"key":  "anchored-tool",
		"text": "tool anchor",
		"anchors": []any{
			map[string]any{"path": "service.go", "symbol": "Start"},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	note := resultAny.(map[string]any)["stored"].(Note)
	if len(note.Anchors) != 1 || note.Anchors[0].ContentHash == "" {
		t.Fatalf("expected parsed hydrated anchor, got %#v", note)
	}
}

func newLifecycleTestStore(t *testing.T, git bool) (*NoteStore, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if git {
		gitLifecycle(t, root, "init", "-b", "main")
		gitLifecycle(t, root, "config", "user.email", "test@example.com")
		gitLifecycle(t, root, "config", "user.name", "Test User")
	}
	store, err := NewNoteStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return store, root
}

func writeLifecycleFile(t *testing.T, root, path, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitLifecycle(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func ageLifecycleTombstones(t *testing.T, store *NoteStore, age time.Duration) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	f, err := store.loadLocked()
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-age).Format(time.RFC3339)
	for index := range f.Notes {
		if noteStatus(f.Notes[index]) == NoteStatusTombstoned {
			f.Notes[index].TombstonedAt = old
		}
	}
	if err := store.saveLocked(f); err != nil {
		t.Fatal(err)
	}
}
