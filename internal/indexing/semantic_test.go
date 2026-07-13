package indexing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"memento-mcp/internal/embedding"
)

type testEmbedder struct {
	fingerprint string
	failTask    embedding.Task

	mu               sync.Mutex
	documentCalls    int
	queryCalls       int
	documentRequests int
}

func (e *testEmbedder) Embed(_ context.Context, task embedding.Task, inputs []string) ([][]float32, error) {
	e.mu.Lock()
	if task == embedding.TaskDocument {
		e.documentCalls += len(inputs)
		e.documentRequests++
	} else {
		e.queryCalls += len(inputs)
	}
	e.mu.Unlock()
	if task == e.failTask {
		return nil, errors.New("embedding unavailable")
	}
	out := make([][]float32, 0, len(inputs))
	for _, input := range inputs {
		lower := strings.ToLower(input)
		if task == embedding.TaskQuery && strings.Contains(lower, "findme") {
			out = append(out, []float32{1, 0})
			continue
		}
		switch {
		case strings.Contains(lower, "auth.go"), strings.Contains(lower, "authentication"), strings.Contains(lower, "login security"), strings.Contains(lower, "semantic concept"):
			out = append(out, []float32{1, 0})
		case strings.Contains(lower, "database"), strings.Contains(lower, "schema"), strings.Contains(lower, "lexical.go"), strings.Contains(lower, "findme"):
			out = append(out, []float32{0, 1})
		default:
			out = append(out, []float32{0.70710677, 0.70710677})
		}
	}
	return out, nil
}

func (e *testEmbedder) Fingerprint() string { return e.fingerprint }
func (e *testEmbedder) Name() string        { return "test/" + e.fingerprint }

func (e *testEmbedder) calls() (documents, queries int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.documentCalls, e.queryCalls
}

func (e *testEmbedder) requests() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.documentRequests
}

func TestHybridSearchFindsConceptWithoutLexicalOverlap(t *testing.T) {
	root := t.TempDir()
	writeSemanticFixture(t, root, "auth.go", "package fixture\n\n// Authentication middleware validates sessions.\nfunc Guard() {}\n")
	writeSemanticFixture(t, root, "database.go", "package fixture\n\n// Schema migration updates storage.\nfunc Migrate() {}\n")
	embedder := &testEmbedder{fingerprint: "model-a"}
	idx := startSemanticIndexer(t, root, t.TempDir(), embedder, 0.65)

	results, err := idx.SearchContext(context.Background(), "login security", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].Path != "auth.go" {
		t.Fatalf("expected conceptual auth.go match, got %#v", results)
	}
	if strings.Contains(strings.ToLower(results[0].Content), "login security") {
		t.Fatal("fixture unexpectedly had lexical overlap")
	}
	debug := idx.DebugInfo()
	if !debug.SemanticEnabled || debug.VectorsIndexed == 0 || debug.EmbeddingModel != "test/model-a" {
		t.Fatalf("unexpected semantic debug info: %#v", debug)
	}
}

func TestHybridWeightControlsRanking(t *testing.T) {
	root := t.TempDir()
	writeSemanticFixture(t, root, "lexical.go", "package fixture\n\n// findme exact lexical match.\nfunc Exact() {}\n")
	writeSemanticFixture(t, root, "auth.go", "package fixture\n\n// Authentication concept.\nfunc Concept() {}\n")
	store := t.TempDir()

	highSemantic := startSemanticIndexer(t, root, store, &testEmbedder{fingerprint: "shared-model"}, 0.8)
	results, err := highSemantic.Search("findme", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 || results[0].Path != "auth.go" {
		t.Fatalf("expected semantic result first at weight 0.8, got %#v", results)
	}

	lowSemantic, err := New(Config{
		RootAbs:        root,
		StoreDir:       store,
		Embedder:       &testEmbedder{fingerprint: "shared-model"},
		SemanticWeight: 0.2,
	})
	if err != nil {
		t.Fatal(err)
	}
	results, err = lowSemantic.Search("findme", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 || results[0].Path != "lexical.go" {
		t.Fatalf("expected lexical result first at weight 0.2, got %#v", results)
	}
}

func TestSemanticVectorsPersistAndReuseAcrossRestart(t *testing.T) {
	root := t.TempDir()
	store := t.TempDir()
	writeSemanticFixture(t, root, "auth.go", "package fixture\n// Authentication middleware.\n")
	firstEmbedder := &testEmbedder{fingerprint: "stable-model"}
	first := startSemanticIndexer(t, root, store, firstEmbedder, 0.65)
	documentCalls, _ := firstEmbedder.calls()
	if documentCalls == 0 {
		t.Fatal("expected initial document embedding")
	}
	entry := first.manifest.Files["auth.go"]
	if entry.Vectors != entry.Chunks || entry.Vectors == 0 {
		t.Fatalf("expected vectors for every chunk: %#v", entry)
	}
	if _, err := os.Stat(first.vectorFilePath(entry.ID)); err != nil {
		t.Fatal(err)
	}

	secondEmbedder := &testEmbedder{fingerprint: "stable-model"}
	second, err := New(Config{RootAbs: root, StoreDir: store, Embedder: secondEmbedder})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	second.Start(ctx)
	if err := second.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	secondDocumentCalls, _ := secondEmbedder.calls()
	if secondDocumentCalls != 0 {
		t.Fatalf("expected persisted vectors to be reused, got %d document calls", secondDocumentCalls)
	}
}

func TestEmbeddingFingerprintChangeReindexesUnchangedFiles(t *testing.T) {
	root := t.TempDir()
	store := t.TempDir()
	content := "package fixture\n// Authentication middleware.\n"
	writeSemanticFixture(t, root, "auth.go", content)
	firstEmbedder := &testEmbedder{fingerprint: "model-a"}
	first, err := New(Config{
		RootAbs:        root,
		StoreDir:       store,
		MaxTotalBytes:  int64(len(content)),
		Embedder:       firstEmbedder,
		SemanticWeight: 0.65,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstContext, firstCancel := context.WithCancel(context.Background())
	t.Cleanup(firstCancel)
	first.Start(firstContext)
	if err := first.IndexAll(firstContext); err != nil {
		t.Fatal(err)
	}
	entry := first.manifest.Files["auth.go"]
	vectorPath := first.vectorFilePath(entry.ID)
	if _, err := os.Stat(vectorPath); err != nil {
		t.Fatal(err)
	}

	secondEmbedder := &testEmbedder{fingerprint: "model-b"}
	second, err := New(Config{
		RootAbs:       root,
		StoreDir:      store,
		MaxTotalBytes: int64(len(content)),
		Embedder:      secondEmbedder,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(vectorPath); !os.IsNotExist(err) {
		t.Fatalf("expected old model vectors to be removed, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	second.Start(ctx)
	if err := second.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	documentCalls, _ := secondEmbedder.calls()
	if documentCalls == 0 {
		t.Fatal("expected unchanged source to be re-embedded after model change")
	}
}

func TestChunkingFingerprintChangeReembedsUnchangedFiles(t *testing.T) {
	root := t.TempDir()
	store := t.TempDir()
	content := "package fixture\n\nfunc One() {\n\tprintln(1)\n}\nfunc Two() {}\n"
	writeSemanticFixture(t, root, "fixture.go", content)
	firstEmbedder := &testEmbedder{fingerprint: "stable-model"}
	first, err := New(Config{
		RootAbs:        root,
		StoreDir:       store,
		MaxChunkLines:  2,
		MaxChunkBytes:  1 << 20,
		MaxTotalBytes:  int64(len(content)),
		Embedder:       firstEmbedder,
		SemanticWeight: 0.65,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstContext, firstCancel := context.WithCancel(context.Background())
	first.Start(firstContext)
	if err := first.IndexAll(firstContext); err != nil {
		firstCancel()
		t.Fatal(err)
	}
	firstCancel()
	entry := first.manifest.Files["fixture.go"]
	vectorPath := first.vectorFilePath(entry.ID)
	if _, err := os.Stat(vectorPath); err != nil {
		t.Fatal(err)
	}

	secondEmbedder := &testEmbedder{fingerprint: "stable-model"}
	second, err := New(Config{
		RootAbs:        root,
		StoreDir:       store,
		MaxChunkLines:  20,
		MaxChunkBytes:  1 << 20,
		MaxTotalBytes:  int64(len(content)),
		Embedder:       secondEmbedder,
		SemanticWeight: 0.65,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(vectorPath); !os.IsNotExist(err) {
		t.Fatalf("expected old vectors to be removed after chunking change, got %v", err)
	}
	secondContext, secondCancel := context.WithCancel(context.Background())
	defer secondCancel()
	second.Start(secondContext)
	if err := second.IndexAll(secondContext); err != nil {
		t.Fatal(err)
	}
	documentCalls, _ := secondEmbedder.calls()
	if documentCalls == 0 {
		t.Fatal("expected unchanged source to be re-embedded after chunking change")
	}
	rebuilt := second.manifest.Files["fixture.go"]
	if rebuilt.Vectors == 0 || rebuilt.Vectors != rebuilt.Chunks {
		t.Fatalf("expected vectors for every rebuilt chunk, got %#v", rebuilt)
	}
}

func TestEmbeddingFailureFallsBackToLexicalSearch(t *testing.T) {
	root := t.TempDir()
	writeSemanticFixture(t, root, "auth.go", "package fixture\n// ExactNeedle remains searchable.\n")
	writeSemanticFixture(t, root, "database.go", "package fixture\n// Another file should hit backoff.\n")
	embedder := &testEmbedder{fingerprint: "failing-model", failTask: embedding.TaskDocument}
	idx := startSemanticIndexer(t, root, t.TempDir(), embedder, 0.65)
	if !strings.Contains(idx.Status().Error, "embedding unavailable") {
		t.Fatalf("expected embedding failure in status, got %#v", idx.Status())
	}
	if requests := embedder.requests(); requests != 1 {
		t.Fatalf("expected one failed embedding request before backoff, got %d", requests)
	}
	if err := idx.IndexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(idx.Status().Error, "embedding unavailable") {
		t.Fatalf("expected retry backoff to preserve embedding error, got %#v", idx.Status())
	}
	if requests := embedder.requests(); requests != 1 {
		t.Fatalf("expected backoff reindex not to call embedding runtime, got %d requests", requests)
	}
	results, err := idx.Search("ExactNeedle", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Path != "auth.go" {
		t.Fatalf("expected lexical fallback result, got %#v", results)
	}
}

func TestCorruptSemanticVectorsFallBackAndRebuild(t *testing.T) {
	root := t.TempDir()
	writeSemanticFixture(t, root, "auth.go", "package fixture\n// ExactNeedle protects authentication.\n")
	embedder := &testEmbedder{fingerprint: "repair-model"}
	idx := startSemanticIndexer(t, root, t.TempDir(), embedder, 0.65)
	entry := idx.manifest.Files["auth.go"]
	if err := os.WriteFile(idx.vectorFilePath(entry.ID), []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := idx.Search("ExactNeedle", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Path != "auth.go" {
		t.Fatalf("expected lexical fallback for corrupt vectors, got %#v", results)
	}
	if !strings.Contains(idx.Status().Error, "semantic vectors for auth.go are stale") {
		t.Fatalf("expected corrupt vector diagnostic, got %#v", idx.Status())
	}
	if idx.manifest.Files["auth.go"].Vectors != 0 {
		t.Fatalf("expected corrupt vectors to be marked stale, got %#v", idx.manifest.Files["auth.go"])
	}

	requestsBefore := embedder.requests()
	if err := idx.IndexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if embedder.requests() <= requestsBefore {
		t.Fatal("expected stale vectors to be re-embedded")
	}
	rebuilt := idx.manifest.Files["auth.go"]
	if rebuilt.Vectors == 0 || rebuilt.Vectors != rebuilt.Chunks {
		t.Fatalf("expected rebuilt vectors for every chunk, got %#v", rebuilt)
	}
}

func TestSemanticSearchHonorsCanceledContext(t *testing.T) {
	root := t.TempDir()
	writeSemanticFixture(t, root, "auth.go", "package fixture\n// Authentication middleware.\n")
	embedder := &testEmbedder{fingerprint: "canceled-search-model"}
	idx := startSemanticIndexer(t, root, t.TempDir(), embedder, 0.65)
	_, queryCallsBefore := embedder.calls()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := idx.SearchContext(ctx, "login security", 5, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled search, got %v", err)
	}
	_, queryCallsAfter := embedder.calls()
	if queryCallsAfter != queryCallsBefore {
		t.Fatalf("expected canceled search not to call embedder, got %d calls", queryCallsAfter-queryCallsBefore)
	}
}

func startSemanticIndexer(t *testing.T, root, store string, embedder embedding.Embedder, weight float64) *Indexer {
	t.Helper()
	idx, err := New(Config{
		RootAbs:            root,
		StoreDir:           store,
		Embedder:           embedder,
		SemanticWeight:     weight,
		EmbeddingBatchSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	idx.Start(ctx)
	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	return idx
}

func writeSemanticFixture(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
