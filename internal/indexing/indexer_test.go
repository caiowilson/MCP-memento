package indexing

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"memento-mcp/internal/embedding"
	"memento-mcp/internal/redact"
)

type lifecycleBlockingEmbedder struct {
	started chan struct{}
	once    sync.Once
}

func (e *lifecycleBlockingEmbedder) Embed(ctx context.Context, _ embedding.Task, _ []string) ([][]float32, error) {
	e.once.Do(func() { close(e.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func (*lifecycleBlockingEmbedder) Fingerprint() string { return "lifecycle-blocking-v1" }
func (*lifecycleBlockingEmbedder) Name() string        { return "test/lifecycle-blocking" }

type releaseIndexEmbedder struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *releaseIndexEmbedder) Embed(ctx context.Context, _ embedding.Task, inputs []string) ([][]float32, error) {
	e.once.Do(func() { close(e.started) })
	select {
	case <-e.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	vectors := make([][]float32, len(inputs))
	for index := range vectors {
		vectors[index] = []float32{1}
	}
	return vectors, nil
}

func (*releaseIndexEmbedder) Fingerprint() string { return "release-index-v1" }
func (*releaseIndexEmbedder) Name() string        { return "test/release-index" }

func TestIndexerRequestsFailAfterWorkerStops(t *testing.T) {
	idx, err := New(Config{RootAbs: t.TempDir(), StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	workerContext, stop := context.WithCancel(context.Background())
	idx.Start(workerContext)
	stop()
	select {
	case <-idx.workerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("indexer worker did not stop")
	}
	if err := idx.EnsureIndexed(context.Background(), []string{"fixture.go"}); !errors.Is(err, errIndexerStopped) {
		t.Fatalf("EnsureIndexed error = %v, want %v", err, errIndexerStopped)
	}
	if err := idx.IndexAll(context.Background()); !errors.Is(err, errIndexerStopped) {
		t.Fatalf("IndexAll error = %v, want %v", err, errIndexerStopped)
	}
}

func TestIndexerLifecycleCancellationStopsActiveRequest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fixture.go"), []byte("package fixture\n\nfunc Active() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	embedder := &lifecycleBlockingEmbedder{started: make(chan struct{})}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir(), Embedder: embedder})
	if err != nil {
		t.Fatal(err)
	}
	workerContext, stop := context.WithCancel(context.Background())
	idx.Start(workerContext)
	requestDone := make(chan error, 1)
	go func() {
		requestDone <- idx.EnsureIndexed(context.Background(), []string{"fixture.go"})
	}()
	select {
	case <-embedder.started:
	case <-time.After(2 * time.Second):
		t.Fatal("embedding request did not start")
	}
	stop()
	select {
	case <-idx.workerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("active request kept indexer worker alive after lifecycle cancellation")
	}
	select {
	case <-requestDone:
	case <-time.After(2 * time.Second):
		t.Fatal("active EnsureIndexed request did not return after lifecycle cancellation")
	}
}

func TestIndexerRequestsRequireStartedWorker(t *testing.T) {
	idx, err := New(Config{RootAbs: t.TempDir(), StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.EnsureIndexed(context.Background(), []string{"fixture.go"}); !errors.Is(err, errIndexerNotStarted) {
		t.Fatalf("EnsureIndexed error = %v, want %v", err, errIndexerNotStarted)
	}
}

func TestRefreshIgnoreRulesHonorsCancellation(t *testing.T) {
	idx, err := New(Config{RootAbs: t.TempDir(), StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := idx.RefreshIgnoreRules(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestEnsureIndexedEvictsStaleChunksWhenFileBecomesUnindexable(t *testing.T) {
	root := t.TempDir()
	store := t.TempDir()
	path := filepath.Join(root, "fixture.go")
	if err := os.WriteFile(path, []byte("package fixture\n\nfunc Small() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := New(Config{RootAbs: root, StoreDir: store, MaxFileBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	idx.Start(ctx)
	if err := idx.EnsureIndexed(ctx, []string{"fixture.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.FileChunks("fixture.go"); err != nil {
		t.Fatalf("expected initial chunks: %v", err)
	}
	if err := os.WriteFile(path, []byte("package fixture\n"+strings.Repeat("x", 128)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := idx.EnsureIndexed(ctx, []string{"fixture.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.FileChunks("fixture.go"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale chunks remained after file exceeded MaxFileBytes: %v", err)
	}
}

func TestIndexerSkipsSymlinksForTargetedAndFullIndexing(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("package outside\n\nconst OutsideSymlinkNeedle = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	idx.Start(ctx)
	if err := idx.EnsureIndexed(ctx, []string{"linked.go"}); err != nil {
		t.Fatal(err)
	}
	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.FileChunks("linked.go"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink target was indexed: %v", err)
	}
}

func TestIndexerDoesNotCommitAfterIgnoreRulesChangeDuringIndexing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "secret.go"), []byte("package secret\n\nconst ConcurrentIgnoreNeedle = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	embedder := &releaseIndexEmbedder{started: make(chan struct{}), release: make(chan struct{})}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir(), Embedder: embedder})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	idx.Start(ctx)
	done := make(chan error, 1)
	go func() {
		done <- idx.EnsureIndexed(ctx, []string{"secret.go"})
	}()
	select {
	case <-embedder.started:
	case <-time.After(2 * time.Second):
		t.Fatal("index request did not reach the controlled embedding barrier")
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("secret.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := idx.ReloadIgnoreRules(); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.PurgeDisallowedPaths(); err != nil {
		t.Fatal(err)
	}
	close(embedder.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("index request did not finish after release")
	}
	if _, err := idx.FileChunks("secret.go"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("request committed a newly ignored path: %v", err)
	}
	results, err := idx.SearchContext(ctx, "ConcurrentIgnoreNeedle", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("newly ignored content remained searchable: %#v", results)
	}
}

func TestPathSafeForSummaryKeepsBuiltInSensitiveDeniesWithCustomGlobs(t *testing.T) {
	idx, err := New(Config{RootAbs: t.TempDir(), StoreDir: t.TempDir(), DenyGlobs: []string{"*.tmp"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".env", "private.pem", "scratch.tmp"} {
		if idx.PathSafeForSummary(rel) {
			t.Errorf("PathSafeForSummary(%q) = true, want false", rel)
		}
	}
	if !idx.PathSafeForSummary("artifact.bin") {
		t.Fatal("custom deny config should not suppress a benign binary diff")
	}
}

func TestIndexerIndexesAndSearches(t *testing.T) {
	root := t.TempDir()
	store := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package main\n\nfunc Hello() { println(\"hi\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.ts"), []byte("export const answer = 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := New(Config{
		RootAbs:       root,
		StoreDir:      store,
		PollInterval:  0,
		MaxTotalBytes: 1 << 20,
		MaxFileBytes:  1 << 20,
		MaxChunkBytes: 1024,
		MaxChunkLines: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	idx.Start(context.Background())

	if err := idx.IndexAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	res, err := idx.Search("Hello", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatalf("expected search results")
	}
	found := false
	for _, ch := range res {
		if ch.Path == "a.go" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a.go in results: %#v", res)
	}
}

func TestIndexerRedactsPersistedChunksAndIgnoresEnvFiles(t *testing.T) {
	root := t.TempDir()
	store := t.TempDir()
	secret := "A1b2C3d4E5f6G7h8I9j0K1l2M3n4"
	if err := os.WriteFile(filepath.Join(root, "config.go"), []byte("package config\nconst API_KEY = \""+secret+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env.local"), []byte("TOKEN="+secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

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

	chunks, err := idx.FileChunks("config.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 || !strings.Contains(chunks[0].Content, redact.Marker) || strings.Contains(chunks[0].Content, secret) {
		t.Fatalf("expected persisted chunks to contain only a marker, got %#v", chunks)
	}
	if _, err := idx.FileChunks(".env.local"); err == nil {
		t.Fatal("expected .env* files to be ignored by default")
	}
	persisted, err := os.ReadFile(idx.chunkFilePath(fileID("config.go")))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(persisted), secret) {
		t.Fatal("secret was written to the on-disk chunk file")
	}
}

func TestChunkingFingerprintChangeReindexesUnchangedFiles(t *testing.T) {
	root := t.TempDir()
	store := t.TempDir()
	content := "package fixture\n\nfunc One() {\n\tprintln(1)\n}\nfunc Two() {}\n"
	path := filepath.Join(root, "fixture.go")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := New(Config{RootAbs: root, StoreDir: store, MaxChunkLines: 2, MaxChunkBytes: 1 << 20})
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
	chunkPath := first.chunkFilePath(entry.ID)
	if entry.Chunks < 2 {
		t.Fatalf("expected small chunk limit to create multiple chunks, got %#v", entry)
	}

	second, err := New(Config{RootAbs: root, StoreDir: store, MaxChunkLines: 20, MaxChunkBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(chunkPath); !os.IsNotExist(err) {
		t.Fatalf("expected changed chunking configuration to invalidate persisted chunks, got %v", err)
	}
	secondContext, secondCancel := context.WithCancel(context.Background())
	defer secondCancel()
	second.Start(secondContext)
	if err := second.IndexAll(secondContext); err != nil {
		t.Fatal(err)
	}
	chunks, err := second.FileChunks("fixture.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || chunks[0].StartLine != 1 || chunks[0].EndLine != 6 {
		t.Fatalf("expected unchanged file to be rebuilt with new chunking limits, got %#v", chunks)
	}
	if second.manifest.ChunkingFingerprint != second.chunkingFingerprint() {
		t.Fatalf("manifest chunking fingerprint = %q, want %q", second.manifest.ChunkingFingerprint, second.chunkingFingerprint())
	}
	if second.DebugInfo().ChunkingIdentity != second.chunkingFingerprint() {
		t.Fatalf("debug info omitted effective chunking identity: %#v", second.DebugInfo())
	}
}

func TestIndexerPurgesChunksWhenRedactionConfigurationChanges(t *testing.T) {
	root := t.TempDir()
	store := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.go"), []byte("package config\nconst password = \"legacy-secret\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	disabled, err := redact.New(redact.Config{Disabled: true})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := New(Config{RootAbs: root, StoreDir: store, Redactor: disabled})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	legacy.Start(ctx)
	if err := legacy.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	chunkPath := legacy.chunkFilePath(fileID("config.go"))
	if _, err := os.Stat(chunkPath); err != nil {
		t.Fatal(err)
	}

	current, err := New(Config{RootAbs: root, StoreDir: store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(chunkPath); !os.IsNotExist(err) {
		t.Fatalf("expected stale chunk file to be purged, got %v", err)
	}
	if _, err := current.FileChunks("config.go"); !os.IsNotExist(err) {
		t.Fatalf("expected stale manifest entries to be reset, got %v", err)
	}
}

func TestIndexerPollsForChanges(t *testing.T) {
	root := t.TempDir()
	store := t.TempDir()
	p := filepath.Join(root, "a.go")
	if err := os.WriteFile(p, []byte("package main\n\nfunc X() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	idx, err := New(Config{
		RootAbs:       root,
		StoreDir:      store,
		PollInterval:  50 * time.Millisecond,
		MaxTotalBytes: 1 << 20,
		MaxFileBytes:  1 << 20,
		MaxChunkBytes: 1024,
		MaxChunkLines: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	idx.Start(ctx)

	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(p, []byte("package main\n\nfunc Y() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		res, _ := idx.Search("func Y", 10, nil)
		if len(res) > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("expected poller to pick up changes")
}

func TestIndexerRespectsGitignore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.log"), []byte("log content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := New(Config{RootAbs: root})
	if err != nil {
		t.Fatal(err)
	}
	idx.Start(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := idx.FileChunks("main.go"); err != nil {
		t.Fatal("expected main.go to be indexed")
	}
	if _, err := idx.FileChunks("app.log"); err == nil {
		t.Fatal("expected app.log to be excluded by .gitignore")
	}
}

func TestIndexerRespectsGitignoreDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("generated/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	genDir := filepath.Join(root, "generated")
	if err := os.MkdirAll(genDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "output.go"), []byte("package gen\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := New(Config{RootAbs: root})
	if err != nil {
		t.Fatal(err)
	}
	idx.Start(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := idx.FileChunks("main.go"); err != nil {
		t.Fatal("expected main.go to be indexed")
	}
	if _, err := idx.FileChunks("generated/output.go"); err == nil {
		t.Fatal("expected generated/output.go to be excluded by .gitignore dir pattern")
	}
}

func TestIndexerMementoignoreNegation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mementoignore"), []byte("!important.log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.log"), []byte("regular log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "important.log"), []byte("important log\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := New(Config{RootAbs: root})
	if err != nil {
		t.Fatal(err)
	}
	// .log files are not preferred or allowed by default, so add them
	idx.cfg.AllowGlobs = append(idx.cfg.AllowGlobs, "*.log")
	idx.Start(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := idx.FileChunks("app.log"); err == nil {
		t.Fatal("expected app.log to be excluded by .gitignore")
	}
	if _, err := idx.FileChunks("important.log"); err != nil {
		t.Fatal("expected important.log to be re-included by .mementoignore negation")
	}
}

func TestIndexerMementoignoreCannotReincludeGitIgnoredFile(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "-C", root, "init", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mementoignore"), []byte("!ignored.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.go"), []byte("package ignored\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := New(Config{RootAbs: root})
	if err != nil {
		t.Fatal(err)
	}
	idx.Start(context.Background())
	if err := idx.IndexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.FileChunks("ignored.go"); err == nil {
		t.Fatal("expected Git ignore to remain authoritative over .mementoignore negation")
	}
}
