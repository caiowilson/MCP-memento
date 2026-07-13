package indexing

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMeaningfulSearchTermsSplitIdentifiersAndDropStopWords(t *testing.T) {
	got := meaningfulSearchTerms("Where is JSONValue normalized by Report_Service?")
	want := []string{"json", "value", "normalized", "report", "service"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("meaningfulSearchTerms() = %#v; want %#v", got, want)
	}
	// Identifier splitting applies to indexed code/path text, not ordinary
	// query words that merely begin with a capital letter.
	if tokens := identifierSearchTokens("JsonValue ReportService"); !reflect.DeepEqual(tokens, []string{"json", "value", "report", "service"}) {
		t.Fatalf("identifierSearchTokens() = %#v", tokens)
	}
}

func TestSearchTermMatchQualityHandlesConservativeInflections(t *testing.T) {
	for _, pair := range [][2]string{
		{"normalized", "normalize"},
		{"handled", "handler"},
		{"strategies", "strategy"},
		{"typed", "type"},
		{"trimmed", "trim"},
		{"bound", "bind"},
		{"iterator", "iterable"},
		{"located", "location"},
	} {
		if quality := searchTermMatchQuality(pair[0], pair[1]); quality == 0 {
			t.Errorf("expected %q and %q to match", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{{"route", "repository"}, {"thing", "think"}, {"class", "classic"}} {
		if quality := searchTermMatchQuality(pair[0], pair[1]); quality != 0 {
			t.Errorf("unexpected match for %q and %q: %d", pair[0], pair[1], quality)
		}
	}
}

func TestSearchTermsFindsNaturalLanguageWithoutChangingExactSearch(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"src/Controller/ReportController.php": "<?php final class ReportController { public function summary(): array { return ['#theme' => 'report_summary']; } }\n",
		"src/Service/ReportRepository.php":    "<?php final class ReportRepository { public function recent(): array { return []; } }\n",
		"README.md":                           "A report repository stores report records.\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.indexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	query := "where is the report summary handled"
	exact, err := idx.SearchContext(context.Background(), query, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) != 0 {
		t.Fatalf("exact search contract changed: %#v", exact)
	}
	terms, err := idx.SearchTermsContext(context.Background(), query, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(terms) == 0 || terms[0].Path != "src/Controller/ReportController.php" {
		t.Fatalf("unexpected term-aware ranking: %#v", terms)
	}
}

func TestSearchTermsRejectsStopWordsOnly(t *testing.T) {
	idx, err := New(Config{RootAbs: t.TempDir(), StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := idx.SearchTermsContext(context.Background(), "where is the", 5, nil); err == nil {
		t.Fatal("expected stop-word-only query to fail closed")
	}
}

func TestSearchTermsPrefilterKeepsCanonicalIrregularMatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Bindings.php"), []byte("<?php // The repository is bound here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.indexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	chunks, err := idx.SearchTermsContext(context.Background(), "where is the repository binding", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || chunks[0].Path != "Bindings.php" {
		t.Fatalf("canonical match was lost by the trigram prefilter: %#v", chunks)
	}
}

func TestSearchTermsByPathIsStableAndDistinct(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"b/ReportHandler.php", "a/ReportHandler.php"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		content := "<?php\nfinal class ReportHandlerOne { public function handle(): void {} }\nfinal class ReportHandlerTwo { public function handle(): void {} }\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir(), MaxChunkLines: 1, MaxChunkBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.indexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	allChunks, err := idx.SearchTermsContext(context.Background(), "where is a report handled", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(allChunks) < 4 || allChunks[0].Path != allChunks[1].Path {
		t.Fatalf("fixture did not produce multiple ranked chunks per path: %#v", allChunks)
	}

	var want []string
	for run := 0; run < 2; run++ {
		chunks, err := idx.SearchTermsByPathContext(context.Background(), "where is a report handled", 5, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, 0, len(chunks))
		seen := map[string]bool{}
		for _, chunk := range chunks {
			if seen[chunk.Path] {
				t.Fatalf("duplicate path in distinct results: %#v", chunks)
			}
			seen[chunk.Path] = true
			got = append(got, chunk.Path)
		}
		if run == 0 {
			want = got
		} else if !reflect.DeepEqual(got, want) {
			t.Fatalf("unstable result order: first=%v second=%v", want, got)
		}
	}
	if !reflect.DeepEqual(want, []string{"a/ReportHandler.php", "b/ReportHandler.php"}) {
		t.Fatalf("unexpected tie order: %v", want)
	}
}
