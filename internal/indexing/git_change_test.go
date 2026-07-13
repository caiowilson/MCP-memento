package indexing

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParsePorcelainZ(t *testing.T) {
	input := []byte(" M a.go\x00D  b.go\x00R  old.go\x00new.go\x00?? c.go\x00")
	add, del, err := parsePorcelainZ(input)
	if err != nil {
		t.Fatal(err)
	}

	expectAdd := map[string]struct{}{
		"a.go":   {},
		"new.go": {},
		"c.go":   {},
	}
	expectDel := map[string]struct{}{
		"b.go":   {},
		"old.go": {},
	}

	for _, p := range add {
		delete(expectAdd, p)
	}
	for _, p := range del {
		delete(expectDel, p)
	}
	if len(expectAdd) != 0 {
		t.Fatalf("missing add paths: %#v", expectAdd)
	}
	if len(expectDel) != 0 {
		t.Fatalf("missing delete paths: %#v", expectDel)
	}
}

func TestGitChangeMonitorReindexesWhenIgnoreFileChanges(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.go"), []byte("package secret\n\nconst SecretNeedle = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	idx.Start(ctx)
	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.FileChunks("secret.go"); err != nil {
		t.Fatalf("expected initial chunks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("secret.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	monitor := NewGitChangeMonitor(root, idx, 0, 0, nil)
	monitor.pendingAdd[".gitignore"] = struct{}{}
	monitor.flush()
	if _, err := idx.FileChunks("secret.go"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Git ignore change left stale chunks: %v", err)
	}
}
