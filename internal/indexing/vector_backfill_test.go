package indexing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"memento-mcp/internal/embedding"
)

// flakyEmbedder fails until release is closed, then succeeds.
type flakyEmbedder struct {
	release chan struct{}
	calls   int
}

func (e *flakyEmbedder) Embed(_ context.Context, _ embedding.Task, inputs []string) ([][]float32, error) {
	e.calls++
	select {
	case <-e.release:
	default:
		return nil, errors.New("embedding unavailable")
	}
	out := make([][]float32, len(inputs))
	for index := range out {
		out[index] = []float32{1, 0}
	}
	return out, nil
}

func (*flakyEmbedder) Fingerprint() string { return "flaky-v1" }
func (*flakyEmbedder) Name() string        { return "test/flaky" }

func backfillFixture(t *testing.T) (root, store string) {
	t.Helper()
	root = t.TempDir()
	store = t.TempDir()
	body := "package alpha\n\nfunc Alpha() string { return \"alpha\" }\n"
	if err := os.WriteFile(filepath.Join(root, "alpha.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, store
}

func TestUnavailableEmbedderDoesNotReReadSource(t *testing.T) {
	root, store := backfillFixture(t)
	embedder := &flakyEmbedder{release: make(chan struct{})}
	idx, err := New(Config{RootAbs: root, StoreDir: store, Embedder: embedder})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idx.Start(ctx)
	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}

	idx.mu.Lock()
	first := idx.manifest.Files["alpha.go"]
	idx.mu.Unlock()
	if first.Vectors != 0 {
		t.Fatalf("expected no vectors while embedder fails, got %d", first.Vectors)
	}

	// Same length, different content, original mtime restored. Only a re-read
	// can observe this change.
	path := filepath.Join(root, "alpha.go")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := "package alpha\n\nfunc Alpha() string { return \"BRAVO\" }\n"
	if len(tampered) != int(info.Size()) {
		t.Fatalf("fixture error: tampered length %d != original %d", len(tampered), info.Size())
	}
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	idx.mu.Lock()
	second := idx.manifest.Files["alpha.go"]
	idx.mu.Unlock()
	if second.Hash != first.Hash {
		t.Fatal("file was re-read and re-chunked while the embedder was unavailable")
	}
}

func TestBackfillFillsVectorsFromPersistedChunks(t *testing.T) {
	root, store := backfillFixture(t)
	embedder := &flakyEmbedder{release: make(chan struct{})}
	idx, err := New(Config{RootAbs: root, StoreDir: store, Embedder: embedder})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idx.Start(ctx)
	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	idx.mu.Lock()
	before := idx.manifest.Files["alpha.go"]
	idx.mu.Unlock()
	if before.Vectors != 0 {
		t.Fatalf("expected 0 vectors, got %d", before.Vectors)
	}
	if got := idx.pendingVectorCount(); got != 1 {
		t.Fatalf("pendingVectorCount = %d, want 1", got)
	}

	close(embedder.release)
	idx.clearEmbeddingBackoffForTest()

	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	idx.mu.Lock()
	after := idx.manifest.Files["alpha.go"]
	idx.mu.Unlock()
	if after.Vectors != after.Chunks {
		t.Fatalf("backfill left %d vectors for %d chunks", after.Vectors, after.Chunks)
	}
	if after.Hash != before.Hash {
		t.Fatal("backfill re-read the source instead of using persisted chunks")
	}
	if got := idx.pendingVectorCount(); got != 0 {
		t.Fatalf("pendingVectorCount = %d, want 0", got)
	}
	if _, err := os.Stat(idx.vectorFilePath(after.ID)); err != nil {
		t.Fatalf("expected vector sidecar: %v", err)
	}
}
