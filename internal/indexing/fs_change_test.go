package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFSChangeMonitorDetectsWrites(t *testing.T) {
	root := t.TempDir()
	store := t.TempDir()

	p := filepath.Join(root, "a.go")
	if err := os.WriteFile(p, []byte("package main\n\nfunc A() {}\n"), 0o644); err != nil {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mon := NewFSChangeMonitor(root, idx, 50*time.Millisecond)
	if err := mon.Start(ctx); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(p, []byte("package main\n\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		res, _ := idx.Search("func B", 10, nil)
		if len(res) > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("expected fs watcher to pick up changes")
}
