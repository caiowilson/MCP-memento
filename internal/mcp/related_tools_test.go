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

func TestRepoRelatedFilesPHPComposerImportsAndSemanticReferences(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"composer.json":                           `{"autoload":{"psr-4":{"App\\":"app/"}}}`,
		"app/Models/User.php":                     "<?php\nnamespace App\\Models;\nclass User {}\n",
		"app/Support/Clock.php":                   "<?php // resolved from its PSR-4 path even before a declaration is indexed\n",
		"app/Services/UserService.php":            "<?php\nnamespace App\\Services;\nuse App\\Models\\User as DomainUser;\nclass UserService { public function make(): DomainUser { return new DomainUser(); } }\n",
		"app/Http/Controllers/UserController.php": "<?php\nnamespace App\\Http\\Controllers;\nuse App\\Services\\UserService;\nuse App\\Support\\Clock;\nclass UserController { public function __invoke(UserService $service) { Clock::now(); } }\n",
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

	tool := newRepoRelatedFilesTool(root)
	call := func(path string, extra map[string]any) []relatedCandidate {
		t.Helper()
		args := map[string]any{"path": path, "includeSameDir": false}
		for key, value := range extra {
			args[key] = value
		}
		raw, _ := json.Marshal(args)
		got, err := tool.Handler(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		return got.(map[string]any)["related"].([]relatedCandidate)
	}

	service := call("app/Services/UserService.php", nil)
	if !hasRelatedReason(service, "app/Models/User.php", "imports") || !hasRelatedReason(service, "app/Models/User.php", "semantic_reference") {
		t.Fatalf("expected Composer import and semantic reference to User: %#v", service)
	}
	user := call("app/Models/User.php", nil)
	if !hasRelatedReason(user, "app/Services/UserService.php", "imported_by") || !hasRelatedReason(user, "app/Services/UserService.php", "referenced_by") {
		t.Fatalf("expected reverse PHP importer and reference: %#v", user)
	}
	controller := call("app/Http/Controllers/UserController.php", map[string]any{"includeReferences": false})
	if !hasRelatedReason(controller, "app/Services/UserService.php", "imports") {
		t.Fatalf("expected PHP namespace import when references are disabled: %#v", controller)
	}
	if !hasRelatedReason(controller, "app/Support/Clock.php", "imports") {
		t.Fatalf("expected Composer PSR-4 path fallback: %#v", controller)
	}
}

func TestRepoRelatedFilesPHPLaravelViewAndConfigConventions(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"app/Http/Controllers/DashboardController.php": "<?php\nclass DashboardController { public function show() { config('services.stripe'); return view('dashboard.index'); } }\n",
		"resources/views/dashboard/index.blade.php":    "@extends('layouts.app')\n",
		"resources/views/layouts/app.blade.php":        "<html>{{ $slot }}</html>\n",
		"config/services.php":                          "<?php return [];\n",
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
	tool := newRepoRelatedFilesTool(root)
	raw, _ := json.Marshal(map[string]any{"path": "app/Http/Controllers/DashboardController.php", "includeSameDir": false, "includeImports": false, "includeImporters": false})
	got, err := tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	related := got.(map[string]any)["related"].([]relatedCandidate)
	if !hasRelatedReason(related, "resources/views/dashboard/index.blade.php", "semantic_reference") || !hasRelatedReason(related, "config/services.php", "semantic_reference") {
		t.Fatalf("expected Laravel view and config references: %#v", related)
	}

	raw, _ = json.Marshal(map[string]any{"path": "resources/views/dashboard/index.blade.php", "includeSameDir": false, "includeImports": false, "includeImporters": false})
	got, err = tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	related = got.(map[string]any)["related"].([]relatedCandidate)
	if !hasRelatedReason(related, "resources/views/layouts/app.blade.php", "semantic_reference") {
		t.Fatalf("expected Blade layout reference: %#v", related)
	}
}

func TestRepoRelatedFilesTSInvalidatesStaleGraphOnRename(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.ts"), []byte("import { b } from './b'\nconsole.log(b)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dir, "b.ts")
	if err := os.WriteFile(oldPath, []byte("export const b = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := newRepoRelatedFilesTool(root)
	raw, _ := json.Marshal(map[string]any{"path": "src/a.ts", "includeSameDir": false})
	if _, err := tool.Handler(context.Background(), raw); err != nil {
		t.Fatal(err)
	}

	newPath := filepath.Join(dir, "c.ts")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	InvalidateJSImportGraphCache(root)

	got, err := tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	related := got.(map[string]any)["related"].([]relatedCandidate)
	if containsRelated(related, "src/b.ts") {
		t.Fatalf("did not expect stale src/b.ts after invalidation: %#v", related)
	}
}

func TestRepoRelatedFilesPHPInvalidatesStaleGraphOnDelete(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.php"), []byte("<?php\nrequire './util.php';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	utilPath := filepath.Join(dir, "util.php")
	if err := os.WriteFile(utilPath, []byte("<?php\nfunction util() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := newRepoRelatedFilesTool(root)
	raw, _ := json.Marshal(map[string]any{"path": "app/main.php", "includeSameDir": false})
	if _, err := tool.Handler(context.Background(), raw); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(utilPath); err != nil {
		t.Fatal(err)
	}
	InvalidatePHPIncludeGraphCache(root)

	got, err := tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	related := got.(map[string]any)["related"].([]relatedCandidate)
	if containsRelated(related, "app/util.php") {
		t.Fatalf("did not expect stale app/util.php after invalidation: %#v", related)
	}
}

func TestRepoRelatedFilesFiltersStaleCachedCandidates(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.ts"), []byte("import { b } from './b'\nconsole.log(b)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(dir, "b.ts")
	if err := os.WriteFile(stalePath, []byte("export const b = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := newRepoRelatedFilesTool(root)
	raw, _ := json.Marshal(map[string]any{"path": "src/a.ts", "includeSameDir": false})
	if _, err := tool.Handler(context.Background(), raw); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(stalePath); err != nil {
		t.Fatal(err)
	}

	got, err := tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	related := got.(map[string]any)["related"].([]relatedCandidate)
	if containsRelated(related, "src/b.ts") {
		t.Fatalf("did not expect stale cached src/b.ts: %#v", related)
	}
}

func TestRepoRelatedFilesPythonImportsAndImporters(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	files := map[string]string{
		"pkg/main.py":       "import pkg.util\nfrom pkg.sub import helper\nfrom . import local\n",
		"pkg/util.py":       "def util():\n    return 1\n",
		"pkg/local.py":      "LOCAL = True\n",
		"pkg/sub/helper.py": "HELPER = True\n",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tool := newRepoRelatedFilesTool(root)

	rawMain, _ := json.Marshal(map[string]any{"path": "pkg/main.py", "includeSameDir": false})
	gotMain, err := tool.Handler(context.Background(), rawMain)
	if err != nil {
		t.Fatal(err)
	}
	resMain := gotMain.(map[string]any)
	relatedMain := resMain["related"].([]relatedCandidate)
	if !containsRelated(relatedMain, "pkg/util.py") {
		t.Fatalf("expected pkg/util.py related to pkg/main.py: %#v", relatedMain)
	}
	if !containsRelated(relatedMain, "pkg/sub/helper.py") {
		t.Fatalf("expected pkg/sub/helper.py related to pkg/main.py: %#v", relatedMain)
	}
	if !containsRelated(relatedMain, "pkg/local.py") {
		t.Fatalf("expected pkg/local.py related to pkg/main.py: %#v", relatedMain)
	}

	rawUtil, _ := json.Marshal(map[string]any{"path": "pkg/util.py", "includeSameDir": false})
	gotUtil, err := tool.Handler(context.Background(), rawUtil)
	if err != nil {
		t.Fatal(err)
	}
	resUtil := gotUtil.(map[string]any)
	relatedUtil := resUtil["related"].([]relatedCandidate)
	if !containsRelated(relatedUtil, "pkg/main.py") {
		t.Fatalf("expected pkg/main.py to import pkg/util.py: %#v", relatedUtil)
	}
}

func TestRepoRelatedFilesPythonRelativeParentImport(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	files := map[string]string{
		"pkg/util.py":       "VALUE = 1\n",
		"pkg/sub/worker.py": "from .. import util\n",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tool := newRepoRelatedFilesTool(root)

	rawWorker, _ := json.Marshal(map[string]any{"path": "pkg/sub/worker.py", "includeSameDir": false})
	gotWorker, err := tool.Handler(context.Background(), rawWorker)
	if err != nil {
		t.Fatal(err)
	}
	resWorker := gotWorker.(map[string]any)
	relatedWorker := resWorker["related"].([]relatedCandidate)
	if !containsRelated(relatedWorker, "pkg/util.py") {
		t.Fatalf("expected pkg/util.py related to pkg/sub/worker.py: %#v", relatedWorker)
	}

	rawUtil, _ := json.Marshal(map[string]any{"path": "pkg/util.py", "includeSameDir": false})
	gotUtil, err := tool.Handler(context.Background(), rawUtil)
	if err != nil {
		t.Fatal(err)
	}
	resUtil := gotUtil.(map[string]any)
	relatedUtil := resUtil["related"].([]relatedCandidate)
	if !containsRelated(relatedUtil, "pkg/sub/worker.py") {
		t.Fatalf("expected pkg/sub/worker.py to import pkg/util.py: %#v", relatedUtil)
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

func hasRelatedReason(list []relatedCandidate, path, reason string) bool {
	for _, candidate := range list {
		if candidate.Path != path {
			continue
		}
		for _, got := range candidate.Reasons {
			if got == reason {
				return true
			}
		}
	}
	return false
}
