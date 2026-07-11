package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
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
			Meta         map[string]any `json:"_meta"`
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
	if got, _ := tool.Meta["anthropic/maxResultSizeChars"].(float64); int(got) != defaultAnthropicMaxResultSizeChars {
		t.Fatalf("expected anthropic max result size metadata, got %#v", tool.Meta)
	}
}

func TestNewServerRejectsInvalidRedactionConfiguration(t *testing.T) {
	t.Setenv("MEMENTO_REDACTION_ADDITIONAL_PATTERNS", `["["]`)
	if _, err := NewServer(Config{Root: t.TempDir(), Child: true}); err == nil {
		t.Fatal("expected invalid redaction pattern to fail server startup")
	}
}

func TestNewServerRejectsNonLocalSemanticEndpoint(t *testing.T) {
	t.Setenv("MEMENTO_SEMANTIC_ENABLED", "true")
	t.Setenv("MEMENTO_OLLAMA_URL", "https://example.com")
	if _, err := NewServer(Config{Root: t.TempDir(), Child: true}); err == nil {
		t.Fatal("expected non-loopback semantic endpoint to fail server startup")
	}
}

func TestNewServerWiresSemanticIndexerConfiguration(t *testing.T) {
	t.Setenv("MEMENTO_SEMANTIC_ENABLED", "true")
	t.Setenv("MEMENTO_OLLAMA_URL", "http://127.0.0.1:11434")
	t.Setenv("MEMENTO_EMBEDDING_MODEL", "nomic-embed-text:v1.5")
	t.Setenv("MEMENTO_HYBRID_SEMANTIC_WEIGHT", "0.7")
	server, err := NewServer(Config{Root: t.TempDir(), Child: true})
	if err != nil {
		t.Fatal(err)
	}
	debug := server.idx.DebugInfo()
	if !debug.SemanticEnabled || debug.EmbeddingModel != "ollama/nomic-embed-text:v1.5" || debug.SemanticWeight != 0.7 {
		t.Fatalf("unexpected semantic indexer config: %#v", debug)
	}
}

func TestLargeResultToolsAdvertiseAnthropicMaxResultSize(t *testing.T) {
	root := t.TempDir()
	s := newBrokerServerForTest(t, root)

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
			Name string         `json:"name"`
			Meta map[string]any `json:"_meta"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"repo_context":   false,
		"repo_read_file": false,
		"repo_search":    false,
	}
	for _, tool := range decoded.Tools {
		if _, ok := want[tool.Name]; !ok {
			continue
		}
		got, _ := tool.Meta["anthropic/maxResultSizeChars"].(float64)
		if int(got) != defaultAnthropicMaxResultSizeChars {
			t.Fatalf("expected %s to advertise max result size %d, got %#v", tool.Name, defaultAnthropicMaxResultSizeChars, tool.Meta)
		}
		want[tool.Name] = true
	}
	for name, found := range want {
		if !found {
			t.Fatalf("expected tool %s in tools/list", name)
		}
	}
}

func TestToolDescriptionsFitToolSearchLimit(t *testing.T) {
	root := t.TempDir()
	s := newBrokerServerForTest(t, root)

	if len(s.tools) == 0 {
		t.Fatal("expected registered tools")
	}
	for _, tool := range s.tools {
		t.Run(tool.Name, func(t *testing.T) {
			description := tool.Description
			if strings.TrimSpace(description) == "" {
				t.Fatal("tool description must not be empty")
			}
			if len(description) > toolSearchDescriptionLimitBytes {
				t.Fatalf("tool description exceeds %d-byte Tool Search limit: %d bytes", toolSearchDescriptionLimitBytes, len(description))
			}
		})
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

	s := newBrokerServerForTest(t, rootA)

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
	s := newBrokerServerForTest(t, root)

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

func TestInitializeReturnsToolSearchInstructions(t *testing.T) {
	root := t.TempDir()
	s := newBrokerServerForTest(t, root)
	result := s.initializeResult(json.RawMessage(`{"protocolVersion":"2024-11-05"}`))
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("initializeResult serverInfo has type %T", result["serverInfo"])
	}
	if got := serverInfo["version"]; got != serverVersion {
		t.Fatalf("initializeResult version = %v, want %q", got, serverVersion)
	}
	instructions, ok := result["instructions"]
	if !ok {
		t.Fatal("initializeResult missing 'instructions' field")
	}
	str, ok := instructions.(string)
	if !ok || strings.TrimSpace(str) == "" {
		t.Fatal("instructions must be a non-empty string")
	}
	if !strings.HasPrefix(str, "Use when ") {
		t.Fatalf("instructions must lead with use-when guidance, got %q", str)
	}
	if len([]byte(str)) >= toolSearchDescriptionLimitBytes {
		t.Fatalf("instructions must stay under %d bytes: %d bytes", toolSearchDescriptionLimitBytes, len([]byte(str)))
	}
	for _, category := range []string{"Repository context:", "Durable repo-scoped memory:"} {
		if !strings.Contains(str, category) {
			t.Errorf("instructions missing task category %q", category)
		}
	}
	for _, tool := range s.tools {
		if !strings.Contains(str, tool.Name) {
			t.Errorf("instructions missing registered tool %q", tool.Name)
		}
	}
}

func TestServeStdioInitializeIncludesToolSearchInstructions(t *testing.T) {
	root := t.TempDir()
	s := newBrokerServerForTest(t, root)
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}` + "\n")

	var out bytes.Buffer
	if err := s.ServeStdio(context.Background(), input, &out); err != nil {
		t.Fatal(err)
	}

	var response struct {
		Result struct {
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &response); err != nil {
		t.Fatal(err)
	}
	if response.Result.Instructions != serverInstructions {
		t.Fatalf("wire initialize instructions mismatch:\n%s", response.Result.Instructions)
	}
}

func TestNewServerUsesClaudeProjectDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", dir)
	s, err := NewServer(Config{
		childFactory: newLocalChildFactory(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.shutdown() })
	s.StartBackgroundIndexing(context.Background())
	if got := s.root; got != dir {
		t.Fatalf("expected root %s from CLAUDE_PROJECT_DIR, got %s", dir, got)
	}
}

func TestNewServerExplicitRootBeatsClaudeProjectDir(t *testing.T) {
	explicit := t.TempDir()
	env := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", env)
	s, err := NewServer(Config{
		Root:         explicit,
		childFactory: newLocalChildFactory(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.shutdown() })
	s.StartBackgroundIndexing(context.Background())
	if got := s.root; got != explicit {
		t.Fatalf("explicit root should win over CLAUDE_PROJECT_DIR; got %s", got)
	}
}

func TestNewServerFallsBackToCwd(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	wantRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	t.Setenv("CLAUDE_PROJECT_DIR", "")

	s, err := NewServer(Config{
		childFactory: newLocalChildFactory(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.shutdown() })
	s.StartBackgroundIndexing(context.Background())

	if got := s.root; got != wantRoot {
		t.Fatalf("expected root %s from cwd fallback, got %s", wantRoot, got)
	}
	if s.rootSource != workspaceRootSourceCWD {
		t.Fatalf("expected rootSource=%s, got %s", workspaceRootSourceCWD, s.rootSource)
	}
	if !s.allowClientRootsFallback {
		t.Fatal("cwd fallback should allow roots/list fallback")
	}
}

func TestNewServerClaudeProjectDirNonExistent(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "/nonexistent/path/that/cannot/exist")
	_, err := NewServer(Config{childFactory: newLocalChildFactory(t)})
	if err == nil {
		t.Fatal("expected error when CLAUDE_PROJECT_DIR points to non-existent path")
	}
}

func TestServeStdioUsesRootsListFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwdRoot := t.TempDir()
	rootsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwdRoot, "alpha.txt"), []byte("from-cwd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootsRoot, "beta.txt"), []byte("from-roots\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwdRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	t.Setenv("CLAUDE_PROJECT_DIR", "")

	s, err := NewServer(Config{
		childFactory: newLocalChildFactory(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	s.StartBackgroundIndexing(context.Background())

	var input bytes.Buffer
	enc := json.NewEncoder(&input)
	if err := enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"roots": map[string]any{"listChanged": true},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]any{
			"roots": []map[string]any{
				{
					"uri":  (&url.URL{Scheme: "file", Path: rootsRoot}).String(),
					"name": "rootsRoot",
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "repo_read_file",
			"arguments": map[string]any{
				"path": "beta.txt",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := s.ServeStdio(context.Background(), &input, &out); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected initialize response, roots/list request, and tool response; got %d lines:\n%s", len(lines), out.String())
	}
	var rootsReq rpcMessage
	if err := json.Unmarshal([]byte(lines[1]), &rootsReq); err != nil {
		t.Fatal(err)
	}
	if rootsReq.Method != "roots/list" {
		t.Fatalf("expected roots/list request, got %q in %s", rootsReq.Method, lines[1])
	}
	if !strings.Contains(lines[2], "from-roots") {
		t.Fatalf("expected tool response from roots/list root, got %s", lines[2])
	}
	if got := s.currentRoot(); got != rootsRoot {
		t.Fatalf("expected active root from roots/list %s, got %s", rootsRoot, got)
	}
	if s.rootSource != workspaceRootSourceClientRoots {
		t.Fatalf("expected rootSource=%s, got %s", workspaceRootSourceClientRoots, s.rootSource)
	}
}

func TestServeStdioRejectsToolCallWhileRootsListPending(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwdRoot := t.TempDir()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwdRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	t.Setenv("CLAUDE_PROJECT_DIR", "")

	s, err := NewServer(Config{
		childFactory: newLocalChildFactory(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	s.StartBackgroundIndexing(context.Background())

	var input bytes.Buffer
	enc := json.NewEncoder(&input)
	if err := enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"roots": map[string]any{"listChanged": true},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "repo_list_files",
			"arguments": map[string]any{
				"max": 1,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := s.ServeStdio(context.Background(), &input, &out); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected initialize response, roots/list request, and pending-error response; got %d lines:\n%s", len(lines), out.String())
	}
	var toolResp rpcResponse
	if err := json.Unmarshal([]byte(lines[2]), &toolResp); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(toolResp.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result toolCallResult
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if !result.IsError || len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "workspace root discovery is pending") {
		t.Fatalf("expected pending discovery tool error, got %#v", result)
	}
	if got := managedChildCount(s); got != 0 {
		t.Fatalf("expected no cwd child to spawn while roots/list is pending, got %d", got)
	}
}

func TestServeStdioRejectsToolCallWhileRootsListRefreshPending(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwdRoot := t.TempDir()
	initialRoot := t.TempDir()

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwdRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	t.Setenv("CLAUDE_PROJECT_DIR", "")

	s, err := NewServer(Config{childFactory: newLocalChildFactory(t)})
	if err != nil {
		t.Fatal(err)
	}
	s.StartBackgroundIndexing(context.Background())

	var input bytes.Buffer
	enc := json.NewEncoder(&input)
	requests := []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"roots": map[string]any{"listChanged": true},
				},
			},
		},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"roots": []map[string]any{{"uri": (&url.URL{Scheme: "file", Path: initialRoot}).String()}},
			},
		},
		{"jsonrpc": "2.0", "method": "notifications/roots/list_changed"},
		{
			"jsonrpc": "2.0",
			"id":      3,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      "memory_list",
				"arguments": map[string]any{},
			},
		},
	}
	for _, request := range requests {
		if err := enc.Encode(request); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	if err := s.ServeStdio(context.Background(), &input, &out); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected initialize response, two roots/list requests, and pending-error response; got %d lines:\n%s", len(lines), out.String())
	}
	var refresh rpcMessage
	if err := json.Unmarshal([]byte(lines[2]), &refresh); err != nil {
		t.Fatal(err)
	}
	if refresh.Method != "roots/list" {
		t.Fatalf("expected roots/list refresh request, got %q", refresh.Method)
	}

	var toolResp rpcResponse
	if err := json.Unmarshal([]byte(lines[3]), &toolResp); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(toolResp.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result toolCallResult
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if !result.IsError || len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "workspace root discovery is pending") {
		t.Fatalf("expected pending roots refresh error, got %#v", result)
	}
	if got := s.currentRoot(); got != initialRoot {
		t.Fatalf("expected stale root to remain selected but unused until refresh completes, got %q", got)
	}
}

func quoteJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
