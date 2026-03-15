package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"memento-mcp/internal/indexing"
)

func setupContextTestRepo(t *testing.T) (string, *indexing.Indexer) {
	t.Helper()
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package pkg\n\nfunc A() { B() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package pkg\n\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.go"), []byte("package pkg\n\nfunc C() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := indexing.New(indexing.Config{
		RootAbs:       root,
		MaxTotalBytes: 20 * 1024 * 1024,
		MaxFileBytes:  1 * 1024 * 1024,
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
	return root, idx
}

// contextResultFiles extracts file paths from a repo_context result via JSON round-trip.
func contextResultFiles(t *testing.T, result any) []string {
	t.Helper()
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Files []struct {
			Path   string `json:"path"`
			Chunks []struct {
				StartLine int `json:"startLine"`
			} `json:"chunks"`
		} `json:"files"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(decoded.Files))
	for _, f := range decoded.Files {
		paths = append(paths, f.Path)
	}
	return paths
}

// contextResultChunkKeys extracts (path, startLine) pairs from a repo_context result.
func contextResultChunkKeys(t *testing.T, result any) [][2]any {
	t.Helper()
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Files []struct {
			Path   string `json:"path"`
			Chunks []struct {
				StartLine int `json:"startLine"`
			} `json:"chunks"`
		} `json:"files"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	var keys [][2]any
	for _, f := range decoded.Files {
		for _, ch := range f.Chunks {
			keys = append(keys, [2]any{f.Path, ch.StartLine})
		}
	}
	return keys
}

func TestRepoContextExcludePaths(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	tool := newRepoContextTool(root, idx)

	// Call without excludePaths — should include pkg/b.go as same-dir related
	args := map[string]any{
		"path":              "pkg/a.go",
		"includeImports":    false,
		"includeImporters":  false,
		"includeReferences": false,
	}
	raw, _ := json.Marshal(args)
	got, err := tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	paths := contextResultFiles(t, got)

	hasBGo := false
	for _, p := range paths {
		if p == "pkg/b.go" {
			hasBGo = true
		}
	}
	if !hasBGo {
		t.Fatalf("expected pkg/b.go in results without excludePaths, got: %v", paths)
	}

	// Call WITH excludePaths — should NOT include pkg/b.go
	args2 := map[string]any{
		"path":              "pkg/a.go",
		"excludePaths":      []any{"pkg/b.go"},
		"includeImports":    false,
		"includeImporters":  false,
		"includeReferences": false,
	}
	raw2, _ := json.Marshal(args2)
	got2, err := tool.Handler(context.Background(), raw2)
	if err != nil {
		t.Fatal(err)
	}
	paths2 := contextResultFiles(t, got2)

	for _, p := range paths2 {
		if p == "pkg/b.go" {
			t.Fatal("pkg/b.go should have been excluded by excludePaths")
		}
	}
}

func TestRepoContextNoDuplicateChunks(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	tool := newRepoContextTool(root, idx)

	args := map[string]any{
		"path":              "pkg/a.go",
		"maxChunksPerFile":  5,
		"includeImports":    false,
		"includeImporters":  false,
		"includeReferences": false,
	}
	raw, _ := json.Marshal(args)
	got, err := tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	keys := contextResultChunkKeys(t, got)

	type chunkKey struct {
		path      string
		startLine int
	}
	seen := map[chunkKey]struct{}{}
	for _, k := range keys {
		ck := chunkKey{path: k[0].(string), startLine: k[1].(int)}
		if _, dup := seen[ck]; dup {
			t.Fatalf("duplicate chunk: path=%s startLine=%d", ck.path, ck.startLine)
		}
		seen[ck] = struct{}{}
	}
}

func TestRepoContextExcludeTargetFile(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	tool := newRepoContextTool(root, idx)

	// Excluding the target file itself — verify no crash and results are returned.
	args := map[string]any{
		"path":              "pkg/a.go",
		"excludePaths":      []any{"pkg/a.go"},
		"includeImports":    false,
		"includeImporters":  false,
		"includeReferences": false,
	}
	raw, _ := json.Marshal(args)
	got, err := tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	paths := contextResultFiles(t, got)

	for _, p := range paths {
		if p == "pkg/a.go" {
			t.Fatal("target file pkg/a.go should have been excluded by excludePaths")
		}
	}
}

func TestRepoContextOutlineMode(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	tool := newRepoContextTool(root, idx)

	args := map[string]any{
		"path":              "pkg/a.go",
		"mode":              "outline",
		"includeImports":    false,
		"includeImporters":  false,
		"includeReferences": false,
	}
	raw, _ := json.Marshal(args)
	result, err := tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := json.Marshal(result)
	var resp struct {
		Mode  string `json:"mode"`
		Files []struct {
			Path    string `json:"path"`
			Outline string `json:"outline"`
		} `json:"files"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Mode != "outline" {
		t.Errorf("expected mode=outline, got %q", resp.Mode)
	}
	if len(resp.Files) == 0 {
		t.Fatal("expected at least one file in outline response")
	}

	found := false
	for _, f := range resp.Files {
		if f.Path == "pkg/a.go" {
			found = true
			if !strings.Contains(f.Outline, "package pkg") {
				t.Errorf("outline should contain package declaration, got: %s", f.Outline)
			}
			if !strings.Contains(f.Outline, "func A()") {
				t.Errorf("outline should contain func A signature, got: %s", f.Outline)
			}
			if strings.Contains(f.Outline, "B()") {
				t.Errorf("outline should NOT contain function body (B() call), got: %s", f.Outline)
			}
		}
		// Outline entries should NOT have empty outlines
		if f.Outline == "" {
			t.Errorf("file %s has empty outline", f.Path)
		}
	}
	if !found {
		t.Error("pkg/a.go should be in outline results")
	}
}

func TestRepoContextSummaryMode(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	tool := newRepoContextTool(root, idx)

	args := map[string]any{
		"path":              "pkg/a.go",
		"mode":              "summary",
		"includeImports":    false,
		"includeImporters":  false,
		"includeReferences": false,
	}
	raw, _ := json.Marshal(args)
	result, err := tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := json.Marshal(result)
	var resp struct {
		Mode  string `json:"mode"`
		Files []struct {
			Path    string `json:"path"`
			Outline string `json:"outline"`
		} `json:"files"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Mode != "summary" {
		t.Errorf("expected mode=summary, got %q", resp.Mode)
	}
	if len(resp.Files) == 0 {
		t.Fatal("expected at least one file in summary response")
	}

	for _, f := range resp.Files {
		if f.Path == "pkg/a.go" {
			if !strings.Contains(f.Outline, "L") {
				t.Errorf("summary should contain line numbers, got: %s", f.Outline)
			}
			if !strings.Contains(f.Outline, "func A") {
				t.Errorf("summary should contain func A, got: %s", f.Outline)
			}
		}
	}
}

func TestRepoContextAutoMode(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	tool := newRepoContextTool(root, idx)

	args := map[string]any{
		"path":              "pkg/a.go",
		"mode":              "auto",
		"includeImports":    false,
		"includeImporters":  false,
		"includeReferences": false,
	}
	raw, _ := json.Marshal(args)
	result, err := tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := json.Marshal(result)
	var resp struct {
		Mode  string `json:"mode"`
		Files []struct {
			Path   string `json:"path"`
			Mode   string `json:"mode"`
			Chunks []struct {
				Content string `json:"content"`
			} `json:"chunks"`
			Outline string `json:"outline"`
		} `json:"files"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Mode != "auto" {
		t.Errorf("expected mode=auto, got %q", resp.Mode)
	}

	var targetEntry, relatedEntry bool
	for _, f := range resp.Files {
		if f.Path == "pkg/a.go" {
			targetEntry = true
			if f.Mode != "full" {
				t.Errorf("target file should be mode=full, got %q", f.Mode)
			}
			if len(f.Chunks) == 0 {
				t.Error("target file should have full source chunks")
			}
			if f.Outline != "" {
				t.Errorf("target file should not have outline when in full mode, got: %s", f.Outline)
			}
		} else {
			relatedEntry = true
			if f.Mode != "outline" {
				t.Errorf("related file %s should be mode=outline, got %q", f.Path, f.Mode)
			}
			if f.Outline == "" {
				t.Errorf("related file %s should have outline content", f.Path)
			}
			if len(f.Chunks) > 0 {
				t.Errorf("related file %s should not have chunks in auto mode", f.Path)
			}
		}
	}

	if !targetEntry {
		t.Error("expected target file pkg/a.go in results")
	}
	if !relatedEntry {
		t.Error("expected at least one related file with outline")
	}
}

func TestRepoContextDefaultResolvesToAuto(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	tool := newRepoContextTool(root, idx)

	raw, _ := json.Marshal(map[string]any{
		"path":              "pkg/a.go",
		"includeImports":    false,
		"includeImporters":  false,
		"includeReferences": false,
	})
	result, err := tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := json.Marshal(result)
	var resp struct {
		Mode     string `json:"mode"`
		Resolved struct {
			RequestedIntent *string `json:"requestedIntent"`
			RequestedMode   *string `json:"requestedMode"`
			ResolvedMode    string  `json:"resolvedMode"`
			Strategy        string  `json:"strategy"`
		} `json:"resolved"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Mode != "auto" {
		t.Fatalf("expected default mode=auto, got %q", resp.Mode)
	}
	if resp.Resolved.ResolvedMode != "auto" {
		t.Fatalf("expected resolved mode=auto, got %q", resp.Resolved.ResolvedMode)
	}
	if resp.Resolved.Strategy != "default_auto" {
		t.Fatalf("expected default strategy, got %q", resp.Resolved.Strategy)
	}
	if resp.Resolved.RequestedIntent != nil {
		t.Fatal("expected requestedIntent to be null")
	}
	if resp.Resolved.RequestedMode != nil {
		t.Fatal("expected requestedMode to be null")
	}
}

func TestRepoContextIntentRouting(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	tool := newRepoContextTool(root, idx)

	tests := []struct {
		name         string
		intent       string
		wantMode     string
		wantStrategy string
	}{
		{name: "navigate", intent: "navigate", wantMode: "outline", wantStrategy: "intent:navigate"},
		{name: "implement", intent: "implement", wantMode: "auto", wantStrategy: "intent:implement"},
		{name: "review", intent: "review", wantMode: "auto", wantStrategy: "intent:review"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{
				"path":              "pkg/a.go",
				"intent":            tc.intent,
				"includeImports":    false,
				"includeImporters":  false,
				"includeReferences": false,
			})
			result, err := tool.Handler(context.Background(), raw)
			if err != nil {
				t.Fatal(err)
			}

			data, _ := json.Marshal(result)
			var resp struct {
				Mode     string `json:"mode"`
				Resolved struct {
					RequestedIntent string `json:"requestedIntent"`
					ResolvedMode    string `json:"resolvedMode"`
					Strategy        string `json:"strategy"`
				} `json:"resolved"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				t.Fatal(err)
			}

			if resp.Mode != tc.wantMode {
				t.Fatalf("expected mode=%q, got %q", tc.wantMode, resp.Mode)
			}
			if resp.Resolved.RequestedIntent != tc.intent {
				t.Fatalf("expected requested intent=%q, got %q", tc.intent, resp.Resolved.RequestedIntent)
			}
			if resp.Resolved.ResolvedMode != tc.wantMode {
				t.Fatalf("expected resolved mode=%q, got %q", tc.wantMode, resp.Resolved.ResolvedMode)
			}
			if resp.Resolved.Strategy != tc.wantStrategy {
				t.Fatalf("expected strategy=%q, got %q", tc.wantStrategy, resp.Resolved.Strategy)
			}
		})
	}
}

func TestRepoContextExplicitModeWinsOverIntent(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	tool := newRepoContextTool(root, idx)

	raw, _ := json.Marshal(map[string]any{
		"path":              "pkg/a.go",
		"intent":            "navigate",
		"mode":              "summary",
		"includeImports":    false,
		"includeImporters":  false,
		"includeReferences": false,
	})
	result, err := tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := json.Marshal(result)
	var resp struct {
		Mode     string `json:"mode"`
		Resolved struct {
			RequestedIntent string `json:"requestedIntent"`
			RequestedMode   string `json:"requestedMode"`
			ResolvedMode    string `json:"resolvedMode"`
			Strategy        string `json:"strategy"`
		} `json:"resolved"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Mode != "summary" {
		t.Fatalf("expected mode=summary, got %q", resp.Mode)
	}
	if resp.Resolved.RequestedIntent != "navigate" {
		t.Fatalf("expected requested intent=navigate, got %q", resp.Resolved.RequestedIntent)
	}
	if resp.Resolved.RequestedMode != "summary" {
		t.Fatalf("expected requested mode=summary, got %q", resp.Resolved.RequestedMode)
	}
	if resp.Resolved.ResolvedMode != "summary" {
		t.Fatalf("expected resolved mode=summary, got %q", resp.Resolved.ResolvedMode)
	}
	if resp.Resolved.Strategy != "explicit_mode" {
		t.Fatalf("expected strategy=explicit_mode, got %q", resp.Resolved.Strategy)
	}
}

func TestRepoContextSuggestedNextCall(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	tool := newRepoContextTool(root, idx)

	raw, _ := json.Marshal(map[string]any{
		"path":              "pkg/a.go",
		"intent":            "review",
		"includeImports":    false,
		"includeImporters":  false,
		"includeReferences": false,
	})
	result, err := tool.Handler(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := json.Marshal(result)
	var resp struct {
		SuggestedNextCall *struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
			Reason    string         `json:"reason"`
		} `json:"suggestedNextCall"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}

	if resp.SuggestedNextCall == nil {
		t.Fatal("expected suggestedNextCall for auto response with related files")
	}
	if resp.SuggestedNextCall.Name != "repo_context" {
		t.Fatalf("expected suggested call name=repo_context, got %q", resp.SuggestedNextCall.Name)
	}
	if got := resp.SuggestedNextCall.Arguments["mode"]; got != "full" {
		t.Fatalf("expected suggested mode=full, got %#v", got)
	}
	excludePaths, ok := resp.SuggestedNextCall.Arguments["excludePaths"].([]any)
	if !ok || len(excludePaths) == 0 {
		t.Fatalf("expected excludePaths in suggested call, got %#v", resp.SuggestedNextCall.Arguments["excludePaths"])
	}
	if strings.TrimSpace(resp.SuggestedNextCall.Reason) == "" {
		t.Fatal("expected suggestedNextCall reason")
	}
}
