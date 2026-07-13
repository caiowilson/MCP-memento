package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryToolsExcludeGitIgnoredPaths(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "-C", root, "init", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	files := map[string]string{
		".gitignore":            "ignored/\nsecret.go\n",
		"visible.go":            "package visible\nconst Visible = true\n",
		"secret.go":             "package secret\nconst Needle = \"GIT_IGNORED_NEEDLE\"\n",
		"ignored/generated.php": "<?php // GIT_IGNORED_NEEDLE\n",
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

	listed, err := newRepoListFilesTool(root).Handler(context.Background(), rawJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	paths := listed.(map[string]any)["files"].([]string)
	for _, ignored := range []string{"secret.go", "ignored/generated.php"} {
		for _, path := range paths {
			if path == ignored {
				t.Fatalf("repo_list_files returned Git-ignored path %q: %#v", ignored, paths)
			}
		}
	}

	searched, err := newRepoSearchTool(root).Handler(context.Background(), rawJSON(t, map[string]any{"query": "GIT_IGNORED_NEEDLE"}))
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := searched.(map[string]any)["matches"].([]map[string]any)
	if len(matches) != 0 {
		t.Fatalf("repo_search returned Git-ignored content: %#v", searched)
	}

	for name, tool := range map[string]Tool{
		"read":    newRepoReadFileTool(root),
		"outline": newRepoOutlineTool(root),
		"context": newRepoContextTool(root, nil),
		"related": newRepoRelatedFilesTool(root),
	} {
		_, err := tool.Handler(context.Background(), rawJSON(t, map[string]any{"path": "secret.go"}))
		if err == nil || !strings.Contains(err.Error(), "ignored by Git") {
			t.Errorf("%s should reject Git-ignored target, got %v", name, err)
		}
	}
}
