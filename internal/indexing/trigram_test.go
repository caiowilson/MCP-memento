package indexing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrigramIndexCandidatesReplaceAndRemove(t *testing.T) {
	index := newTrigramIndex()
	index.replace("first.go", trigramKeysForChunks([]Chunk{{Content: "Alpha NEEDLE café"}}))
	index.replace("second.go", trigramKeysForChunks([]Chunk{{Content: "unrelated content"}}))

	candidates, enabled := index.candidates("needle")
	if !enabled {
		t.Fatal("expected a query with at least three bytes to enable filtering")
	}
	if _, ok := candidates["first.go"]; !ok {
		t.Fatalf("expected first.go candidate, got %#v", candidates)
	}
	if _, ok := candidates["second.go"]; ok {
		t.Fatalf("unexpected second.go candidate: %#v", candidates)
	}

	unicodeCandidates, _ := index.candidates("CAFÉ")
	if _, ok := unicodeCandidates["first.go"]; !ok {
		t.Fatalf("case-folded Unicode query missed first.go: %#v", unicodeCandidates)
	}

	index.replace("first.go", trigramKeysForChunks([]Chunk{{Content: "replacement content"}}))
	candidates, _ = index.candidates("needle")
	if _, ok := candidates["first.go"]; ok {
		t.Fatalf("replacement retained stale postings: %#v", candidates)
	}
	index.remove("first.go")
	if _, ready := index.byPath["first.go"]; ready {
		t.Fatal("removed path remains marked ready")
	}
}

func TestTrigramIndexShortQueryFallsBack(t *testing.T) {
	index := newTrigramIndex()
	index.replace("tiny.go", nil)
	if candidates, enabled := index.candidates("go"); enabled || candidates != nil {
		t.Fatalf("short query unexpectedly filtered candidates: enabled=%v candidates=%#v", enabled, candidates)
	}
}

func TestIndexerRebuildsTrigramsFromPersistedChunks(t *testing.T) {
	root := t.TempDir()
	store := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fixture.go"), []byte("package fixture\n\nconst RestartNeedle = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := New(Config{RootAbs: root, StoreDir: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.indexAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	second, err := New(Config{RootAbs: root, StoreDir: store})
	if err != nil {
		t.Fatal(err)
	}
	second.mu.Lock()
	_, ready := second.trigrams.byPath["fixture.go"]
	second.mu.Unlock()
	if !ready {
		t.Fatal("persisted chunks were not restored into the trigram index")
	}
	results, err := second.Search("restartneedle", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Path != "fixture.go" {
		t.Fatalf("unexpected restart search results: %#v", results)
	}
}

func TestIndexerRefreshesAndRemovesTrigramPostings(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fixture.go")
	if err := os.WriteFile(path, []byte("package fixture\n\nconst OriginalSearchMarker = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.indexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package fixture\n\nconst ReplacementSearchMarkerWithDifferentSize = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := idx.indexFiles(context.Background(), []string{"fixture.go"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if idx.SubstringCandidates("OriginalSearchMarker").MayContain("fixture.go", info.Size(), info.ModTime()) {
		t.Fatal("updated path retained postings for its previous content")
	}
	if !idx.SubstringCandidates("ReplacementSearchMarker").MayContain("fixture.go", info.Size(), info.ModTime()) {
		t.Fatal("updated path is absent from replacement-content candidates")
	}
	if err := idx.RemovePaths([]string{"fixture.go"}); err != nil {
		t.Fatal(err)
	}
	idx.mu.Lock()
	_, ready := idx.trigrams.byPath["fixture.go"]
	idx.mu.Unlock()
	if ready {
		t.Fatal("removed manifest path retained trigram state")
	}
}

func TestSubstringCandidateSnapshotFallsBackForStaleAndUnknownFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fixture.go")
	if err := os.WriteFile(path, []byte("package fixture\n\nconst Present = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.indexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := idx.SubstringCandidates("absentneedle")
	if snapshot.MayContain("fixture.go", info.Size(), info.ModTime()) {
		t.Fatal("fresh indexed noncandidate was not filtered")
	}
	if !snapshot.MayContain("unindexed.txt", info.Size(), info.ModTime()) {
		t.Fatal("unindexed path must remain eligible")
	}
	if !snapshot.MayContain("fixture.go", info.Size()+1, info.ModTime()) {
		t.Fatal("size-stale indexed path must remain eligible")
	}
	if !snapshot.MayContain("fixture.go", info.Size(), info.ModTime().Add(time.Second)) {
		t.Fatal("mtime-stale indexed path must remain eligible")
	}
}

var benchmarkSearchResults []Chunk
var benchmarkTrigramKeys []uint32

func BenchmarkTrigramIndexHighEntropy1MiB(b *testing.B) {
	content := make([]byte, 1<<20)
	state := uint32(1)
	for index := range content {
		state = state*1664525 + 1013904223
		content[index] = byte(32 + state%95)
	}
	text := string(content)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		benchmarkTrigramKeys = trigramKeys(text)
	}
}

func BenchmarkIndexerSearch1000Files(b *testing.B) {
	root := b.TempDir()
	store := b.TempDir()
	const query = "distinctive_search_target"
	for index := 0; index < 1000; index++ {
		body := fmt.Sprintf("package fixture\n\nconst Value%d = %q\n", index, "ordinary fixture content")
		if index == 777 {
			body += "const Target = \"" + query + "\"\n"
		}
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("fixture_%04d.go", index)), []byte(body), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	idx, err := New(Config{RootAbs: root, StoreDir: store})
	if err != nil {
		b.Fatal(err)
	}
	if err := idx.indexAll(context.Background()); err != nil {
		b.Fatal(err)
	}

	b.Run("linear_baseline", func(b *testing.B) {
		for iteration := 0; iteration < b.N; iteration++ {
			results, err := linearSearchForBenchmark(idx, query)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkSearchResults = results
		}
	})
	b.Run("trigram_prefilter", func(b *testing.B) {
		for iteration := 0; iteration < b.N; iteration++ {
			results, err := idx.Search(query, 20, nil)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkSearchResults = results
		}
	})
}

func linearSearchForBenchmark(idx *Indexer, query string) ([]Chunk, error) {
	idx.mu.Lock()
	entries := make([]fileEntry, 0, len(idx.manifest.Files))
	for _, entry := range idx.manifest.Files {
		entries = append(entries, entry)
	}
	idx.mu.Unlock()
	results := make([]Chunk, 0, 1)
	for _, entry := range entries {
		chunks, err := idx.readChunksFile(entry.ID)
		if err != nil {
			return nil, err
		}
		for _, chunk := range chunks {
			if score := lexicalChunkScore(chunk, query); score > 0 {
				chunk.Score = score
				results = append(results, chunk)
			}
		}
	}
	return results, nil
}
