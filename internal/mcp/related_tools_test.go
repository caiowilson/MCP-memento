package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRepoRelatedFilesToolSameDir(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package pkg\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package pkg\n\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := newRepoRelatedFilesTool(root)
	args := map[string]any{
		"path":           "pkg/a.go",
		"max":            10,
		"includeSameDir": true,
		"includeImports": false,
	}
	raw, _ := json.Marshal(args)
	got, err := tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}

	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", got)
	}
	related, ok := m["related"].([]relatedCandidate)
	if !ok {
		t.Fatalf("expected []relatedCandidate related, got %T", m["related"])
	}

	found := false
	for _, row := range related {
		if row.Path == "pkg/b.go" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected pkg/b.go to be related: %#v", related)
	}
}

func TestSafeJoinBlocksTraversal(t *testing.T) {
	root := t.TempDir()
	if _, err := safeJoin(root, "../escape.txt"); err == nil {
		t.Fatal("expected error for traversal path")
	}
}

func TestRepoRelatedFilesTSImportsAndImporters(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.ts"), []byte("import { b } from './b'\nconsole.log(b)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.ts"), []byte("export const b = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := newRepoRelatedFilesTool(root)

	rawA, _ := json.Marshal(map[string]any{"path": "src/a.ts", "includeSameDir": false})
	gotA, err := tool.Handler(context.Background(), rawA)
	if err != nil {
		t.Fatal(err)
	}
	resA := gotA.(map[string]any)
	relatedA := resA["related"].([]relatedCandidate)
	if !containsRelated(relatedA, "src/b.ts") {
		t.Fatalf("expected src/b.ts related to src/a.ts: %#v", relatedA)
	}

	rawB, _ := json.Marshal(map[string]any{"path": "src/b.ts", "includeSameDir": false})
	gotB, err := tool.Handler(context.Background(), rawB)
	if err != nil {
		t.Fatal(err)
	}
	resB := gotB.(map[string]any)
	relatedB := resB["related"].([]relatedCandidate)
	if !containsRelated(relatedB, "src/a.ts") {
		t.Fatalf("expected src/a.ts to import src/b.ts: %#v", relatedB)
	}
}

func TestRepoRelatedFilesPHPIncludesAndIncludedBy(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.php"), []byte("<?php\nrequire './util.php';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "util.php"), []byte("<?php\nfunction util() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := newRepoRelatedFilesTool(root)

	rawMain, _ := json.Marshal(map[string]any{"path": "app/main.php", "includeSameDir": false})
	gotMain, err := tool.Handler(context.Background(), rawMain)
	if err != nil {
		t.Fatal(err)
	}
	resMain := gotMain.(map[string]any)
	relatedMain := resMain["related"].([]relatedCandidate)
	if !containsRelated(relatedMain, "app/util.php") {
		t.Fatalf("expected app/util.php related to app/main.php: %#v", relatedMain)
	}

	rawUtil, _ := json.Marshal(map[string]any{"path": "app/util.php", "includeSameDir": false})
	gotUtil, err := tool.Handler(context.Background(), rawUtil)
	if err != nil {
		t.Fatal(err)
	}
	resUtil := gotUtil.(map[string]any)
	relatedUtil := resUtil["related"].([]relatedCandidate)
	if !containsRelated(relatedUtil, "app/main.php") {
		t.Fatalf("expected app/main.php to include app/util.php: %#v", relatedUtil)
	}
}

func containsRelated(list []relatedCandidate, path string) bool {
	for _, c := range list {
		if c.Path == path {
			return true
		}
	}
	return false
}
