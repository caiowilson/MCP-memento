package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeStatusFixture(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"alpha.go": "package alpha\n\nfunc Alpha() string { return \"alpha\" }\n",
		"beta.go":  "package beta\n\nfunc Beta() string { return \"beta\" }\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A re-index pass that finds every file already current must still report the
// size of the index, not the number of files rewritten during that pass.
func TestStatusReportsIndexSizeAfterNoOpReindex(t *testing.T) {
	root := t.TempDir()
	store := t.TempDir()
	writeStatusFixture(t, root)

	idx, err := New(Config{RootAbs: root, StoreDir: store})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idx.Start(ctx)

	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	first := idx.Status()
	if first.FilesIndexed != 2 {
		t.Fatalf("first pass FilesIndexed = %d, want 2", first.FilesIndexed)
	}

	// Nothing changed on disk, so this pass rewrites no files.
	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	second := idx.Status()
	if second.FilesIndexed != 2 {
		t.Fatalf("no-op reindex FilesIndexed = %d, want 2", second.FilesIndexed)
	}
	if second.BytesIndexed != first.BytesIndexed {
		t.Fatalf("no-op reindex BytesIndexed = %d, want %d", second.BytesIndexed, first.BytesIndexed)
	}
	if !second.Ready {
		t.Fatal("no-op reindex Ready = false, want true")
	}
}

// A process that reopens an existing store must report the persisted index
// before any pass runs; the manifest already answers outline and search.
func TestStatusReflectsLoadedManifestBeforeIndexPass(t *testing.T) {
	root := t.TempDir()
	store := t.TempDir()
	writeStatusFixture(t, root)

	warm, err := New(Config{RootAbs: root, StoreDir: store})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	warm.Start(ctx)
	if err := warm.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	want := warm.Status()
	cancel()

	reopened, err := New(Config{RootAbs: root, StoreDir: store})
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Status()
	if got.FilesIndexed != want.FilesIndexed {
		t.Fatalf("reopened FilesIndexed = %d, want %d", got.FilesIndexed, want.FilesIndexed)
	}
	if got.BytesIndexed != want.BytesIndexed {
		t.Fatalf("reopened BytesIndexed = %d, want %d", got.BytesIndexed, want.BytesIndexed)
	}
	if !got.Ready {
		t.Fatal("reopened Ready = false, want true")
	}
	if got.LastIndexedAt == "" {
		t.Fatal("reopened LastIndexedAt is empty, want persisted timestamp")
	}
	// DebugInfo derives its count from the manifest and is the cross-check
	// that the index really does hold these files.
	if debug := reopened.DebugInfo(); debug.FilesIndexed != want.FilesIndexed {
		t.Fatalf("reopened DebugInfo.FilesIndexed = %d, want %d", debug.FilesIndexed, want.FilesIndexed)
	}
}
