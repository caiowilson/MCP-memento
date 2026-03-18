package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolsListIncludesMetadata(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	s := &Server{
		root: root,
		idx:  idx,
		tools: []Tool{
			newRepoContextTool(root, idx),
		},
	}

	resp := s.handleRPC(context.Background(), rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	})

	b, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatal(err)
	}

	var decoded struct {
		Tools []struct {
			Name         string         `json:"name"`
			Title        string         `json:"title"`
			Annotations  map[string]any `json:"annotations"`
			OutputSchema map[string]any `json:"outputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Tools) != 1 {
		t.Fatalf("expected one tool, got %d", len(decoded.Tools))
	}
	tool := decoded.Tools[0]

	if tool.Name != "repo_context" {
		t.Fatalf("expected tool name repo_context, got %q", tool.Name)
	}
	if tool.Title == "" {
		t.Fatal("expected title in tool metadata")
	}
	if got, ok := tool.Annotations["readOnlyHint"].(bool); !ok || !got {
		t.Fatalf("expected readOnlyHint=true, got %#v", tool.Annotations["readOnlyHint"])
	}
	if len(tool.OutputSchema) == 0 {
		t.Fatal("expected outputSchema in tool metadata")
	}
}

func TestCallToolReturnsStructuredContent(t *testing.T) {
	root, idx := setupContextTestRepo(t)
	s := &Server{
		root: root,
		idx:  idx,
		tools: []Tool{
			newRepoIndexStatusTool(idx),
		},
	}

	result, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_index_status",
		Arguments: json.RawMessage([]byte(`{}`)),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Content) != 1 || result.Content[0].Type != "text" || result.Content[0].Text == "" {
		t.Fatalf("expected text JSON fallback content, got %#v", result.Content)
	}
	if result.StructuredContent == nil {
		t.Fatal("expected structuredContent")
	}

	decoded, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structuredContent object, got %T", result.StructuredContent)
	}
	if _, ok := decoded["ready"]; !ok {
		t.Fatalf("expected index status fields in structuredContent, got %#v", decoded)
	}
}

func TestSwitchWorkspaceToolRebindsRootAndIsolation(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootA, "alpha.txt"), []byte("from-root-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootA, "alpha.go"), []byte("package alpha\n\nfunc AlphaSwitchToken() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootB, "beta.txt"), []byte("from-root-b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootB, "beta.go"), []byte("package beta\n\nfunc BetaSwitchToken() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewServer(Config{Root: rootA})
	if err != nil {
		t.Fatal(err)
	}

	readA, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_read_file",
		Arguments: json.RawMessage([]byte(`{"path":"alpha.txt"}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(readA.Content) == 0 || !strings.Contains(readA.Content[0].Text, "from-root-a") {
		t.Fatalf("expected alpha.txt from rootA, got %#v", readA.Content)
	}

	debugA, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_index_debug",
		Arguments: json.RawMessage([]byte(`{}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	debugMapA, ok := debugA.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured index debug result, got %T", debugA.StructuredContent)
	}
	storeA, _ := debugMapA["storeDir"].(string)
	if storeA == "" {
		t.Fatalf("expected non-empty storeDir for rootA, got %#v", debugMapA["storeDir"])
	}

	switched, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_switch_workspace",
		Arguments: json.RawMessage([]byte(`{"path":` + quoteJSONString(rootB) + `,"reindexNow":true}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	sw, ok := switched.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured switch result, got %T", switched.StructuredContent)
	}
	if v, _ := sw["switched"].(bool); !v {
		t.Fatalf("expected switched=true, got %#v", sw["switched"])
	}
	if got, _ := sw["previousRoot"].(string); got != rootA {
		t.Fatalf("expected previousRoot=%q, got %q", rootA, got)
	}
	if got, _ := sw["root"].(string); got != rootB {
		t.Fatalf("expected root=%q, got %q", rootB, got)
	}
	debugB, ok := sw["indexDebug"].(map[string]any)
	if !ok {
		t.Fatalf("expected indexDebug object in switch response, got %T", sw["indexDebug"])
	}
	storeB, _ := debugB["storeDir"].(string)
	if storeB == "" {
		t.Fatalf("expected non-empty storeDir for rootB, got %#v", debugB["storeDir"])
	}
	if storeA == storeB {
		t.Fatalf("expected isolated store dir per workspace, got same storeDir=%q", storeA)
	}

	readB, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_read_file",
		Arguments: json.RawMessage([]byte(`{"path":"beta.txt"}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(readB.Content) == 0 || !strings.Contains(readB.Content[0].Text, "from-root-b") {
		t.Fatalf("expected beta.txt from rootB, got %#v", readB.Content)
	}

	if _, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_read_file",
		Arguments: json.RawMessage([]byte(`{"path":"alpha.txt"}`)),
	}); err == nil {
		t.Fatal("expected alpha.txt lookup to fail after switch to rootB")
	}

	searchB, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_search",
		Arguments: json.RawMessage([]byte(`{"query":"BetaSwitchToken","maxResults":10}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	searchMap, ok := searchB.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured repo_search result, got %T", searchB.StructuredContent)
	}
	matches, ok := searchMap["matches"].([]any)
	if !ok || len(matches) == 0 {
		t.Fatalf("expected repo_search matches in rootB, got %#v", searchMap["matches"])
	}
	foundBetaPath := false
	for _, item := range matches {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if p, _ := m["path"].(string); p == "beta.go" {
			foundBetaPath = true
		}
		if p, _ := m["path"].(string); p == "alpha.go" {
			t.Fatalf("unexpected cross-workspace match path: %s", p)
		}
	}
	if !foundBetaPath {
		t.Fatalf("expected at least one beta.go match, got %#v", matches)
	}

	// repo_context internally calls EnsureIndexed, which requires an active index worker.
	// In production this is started by StartBackgroundIndexing; tests start it explicitly.
	idxCtx, idxCancel := context.WithCancel(context.Background())
	defer idxCancel()
	s.idx.Start(idxCtx)

	contextB, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_context",
		Arguments: json.RawMessage([]byte(`{"path":"beta.go","intent":"navigate"}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	contextMap, ok := contextB.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured repo_context result, got %T", contextB.StructuredContent)
	}
	files, ok := contextMap["files"].([]any)
	if !ok || len(files) == 0 {
		t.Fatalf("expected context files, got %#v", contextMap["files"])
	}
	foundContextBeta := false
	for _, item := range files {
		fm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if p, _ := fm["path"].(string); p == "beta.go" {
			foundContextBeta = true
		}
		if p, _ := fm["path"].(string); p == "alpha.go" {
			t.Fatalf("unexpected cross-workspace context file path: %s", p)
		}
	}
	if !foundContextBeta {
		t.Fatalf("expected beta.go in context files, got %#v", files)
	}

	if _, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_context",
		Arguments: json.RawMessage([]byte(`{"path":"alpha.go","intent":"navigate"}`)),
	}); err == nil {
		t.Fatal("expected alpha.go context lookup to fail after switch to rootB")
	}
}

func TestSwitchWorkspaceNoopWhenSameRoot(t *testing.T) {
	root := t.TempDir()
	s, err := NewServer(Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_switch_workspace",
		Arguments: json.RawMessage([]byte(`{"path":` + quoteJSONString(root) + `}`)),
	})
	if err != nil {
		t.Fatal(err)
	}

	decoded, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured switch result, got %T", res.StructuredContent)
	}
	if got, _ := decoded["switched"].(bool); got {
		t.Fatalf("expected switched=false for same-root switch, got %#v", decoded["switched"])
	}
}

func quoteJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
