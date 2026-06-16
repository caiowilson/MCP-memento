package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
