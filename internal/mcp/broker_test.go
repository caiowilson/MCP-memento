package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type localChildClient struct {
	srv    *Server
	cancel context.CancelFunc

	mu     sync.Mutex
	closed bool
}

func newLocalChildFactory(t *testing.T) childFactory {
	t.Helper()

	return func(root string) (workspaceClient, error) {
		srv, err := NewServer(Config{Root: root, Child: true})
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithCancel(context.Background())
		srv.StartBackgroundIndexing(ctx)
		return &localChildClient{srv: srv, cancel: cancel}, nil
	}
}

func (c *localChildClient) CallTool(ctx context.Context, name string, args json.RawMessage) (toolCallResult, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return toolCallResult{}, childTransportError{err: io.EOF}
	}
	return c.srv.callTool(ctx, toolCallParams{Name: name, Arguments: args})
}

func (c *localChildClient) ToolDefinitions(ctx context.Context) ([]Tool, error) {
	_ = ctx
	out := make([]Tool, 0, len(c.srv.tools))
	for _, def := range c.srv.tools {
		out = append(out, cloneToolDefinition(def))
	}
	return out, nil
}

func (c *localChildClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func newBrokerServerForTest(t *testing.T, root string) *Server {
	t.Helper()
	return newBrokerServerForTestWithTiming(t, root, 10*time.Minute, time.Minute)
}

func newBrokerServerForTestWithTiming(t *testing.T, root string, idleTimeout, reapInterval time.Duration) *Server {
	t.Helper()

	s, err := NewServer(Config{
		Root:              root,
		ChildIdleTimeout:  idleTimeout,
		ChildReapInterval: reapInterval,
		childFactory:      newLocalChildFactory(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	s.StartBackgroundIndexing(context.Background())
	t.Cleanup(func() {
		s.shutdown()
	})
	return s
}

func managedChildCount(s *Server) int {
	s.childrenMu.Lock()
	defer s.childrenMu.Unlock()
	return len(s.children)
}

func managedChildForRoot(s *Server, root string) workspaceClient {
	s.childrenMu.Lock()
	defer s.childrenMu.Unlock()
	if child := s.children[root]; child != nil {
		return child.client
	}
	return nil
}

func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestBrokerStartupSpawnsInitialChild(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "alpha.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newBrokerServerForTest(t, root)
	if got := managedChildCount(s); got != 1 {
		t.Fatalf("expected one initial child, got %d", got)
	}

	res, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_read_file",
		Arguments: json.RawMessage([]byte(`{"path":"alpha.txt"}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) == 0 || res.Content[0].Text == "" {
		t.Fatalf("expected proxied file content, got %#v", res.Content)
	}
}

func TestBrokerRootOverrideDoesNotChangeActiveRoot(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootA, "alpha.txt"), []byte("from-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootB, "beta.txt"), []byte("from-b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newBrokerServerForTest(t, rootA)

	override, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_read_file",
		Arguments: json.RawMessage([]byte(`{"path":"beta.txt","root":` + quoteJSONString(rootB) + `}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(override.Content) == 0 || override.Content[0].Text == "" {
		t.Fatalf("expected overridden child response, got %#v", override.Content)
	}
	if s.currentRoot() != rootA {
		t.Fatalf("expected active root to remain %q, got %q", rootA, s.currentRoot())
	}

	defaultRoot, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_read_file",
		Arguments: json.RawMessage([]byte(`{"path":"alpha.txt"}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultRoot.Content) == 0 || defaultRoot.Content[0].Text == "" {
		t.Fatalf("expected active-root file content, got %#v", defaultRoot.Content)
	}
}

func TestBrokerRespawnsClosedChild(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "alpha.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newBrokerServerForTest(t, root)
	original := managedChildForRoot(s, root)
	if original == nil {
		t.Fatal("expected initial managed child")
	}
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}

	res, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_read_file",
		Arguments: json.RawMessage([]byte(`{"path":"alpha.txt"}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) == 0 || res.Content[0].Text == "" {
		t.Fatalf("expected respawned child response, got %#v", res.Content)
	}

	replacement := managedChildForRoot(s, root)
	if replacement == nil || replacement == original {
		t.Fatal("expected broker to replace closed child")
	}
}

func TestBrokerIdleReapsAndRespawns(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "alpha.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newBrokerServerForTestWithTiming(t, root, 50*time.Millisecond, 20*time.Millisecond)
	waitForCondition(t, time.Second, func() bool {
		return managedChildCount(s) == 0
	})

	res, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_read_file",
		Arguments: json.RawMessage([]byte(`{"path":"alpha.txt"}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) == 0 || res.Content[0].Text == "" {
		t.Fatalf("expected respawn after reap, got %#v", res.Content)
	}
	if got := managedChildCount(s); got != 1 {
		t.Fatalf("expected one respawned child, got %d", got)
	}
}

func TestBrokerSwitchWorkspaceSpawnsThenReusesChild(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()

	s := newBrokerServerForTest(t, rootA)

	first, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_switch_workspace",
		Arguments: json.RawMessage([]byte(`{"path":` + quoteJSONString(rootB) + `}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	firstMap, ok := first.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured switch result, got %T", first.StructuredContent)
	}
	if got, _ := firstMap["spawned"].(bool); !got {
		t.Fatalf("expected first switch to spawn child, got %#v", firstMap["spawned"])
	}
	if got, _ := firstMap["previousRoot"].(string); got != rootA {
		t.Fatalf("expected previousRoot=%q, got %q", rootA, got)
	}
	if got, _ := firstMap["root"].(string); got != rootB {
		t.Fatalf("expected root=%q, got %q", rootB, got)
	}
	if got := managedChildCount(s); got != 2 {
		t.Fatalf("expected broker to manage two children after first switch, got %d", got)
	}

	back, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_switch_workspace",
		Arguments: json.RawMessage([]byte(`{"path":` + quoteJSONString(rootA) + `}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	backMap, ok := back.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured switch result, got %T", back.StructuredContent)
	}
	if got, _ := backMap["spawned"].(bool); got {
		t.Fatalf("expected switch back to reuse existing child, got %#v", backMap["spawned"])
	}

	reuse, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_switch_workspace",
		Arguments: json.RawMessage([]byte(`{"path":` + quoteJSONString(rootB) + `}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	reuseMap, ok := reuse.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured switch result, got %T", reuse.StructuredContent)
	}
	if got, _ := reuseMap["spawned"].(bool); got {
		t.Fatalf("expected second switch to rootB to reuse existing child, got %#v", reuseMap["spawned"])
	}
}

func TestBrokerMemoryIsolationAcrossRoots(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	rootA := t.TempDir()
	rootB := t.TempDir()
	s := newBrokerServerForTest(t, rootA)

	if _, err := s.callTool(context.Background(), toolCallParams{
		Name:      "memory_upsert",
		Arguments: json.RawMessage([]byte(`{"key":"shared-note","text":"note-from-a"}`)),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.callTool(context.Background(), toolCallParams{
		Name:      "repo_switch_workspace",
		Arguments: json.RawMessage([]byte(`{"path":` + quoteJSONString(rootB) + `}`)),
	}); err != nil {
		t.Fatal(err)
	}

	searchB, err := s.callTool(context.Background(), toolCallParams{
		Name:      "memory_search",
		Arguments: json.RawMessage([]byte(`{"query":"note-from-a"}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	searchBMap, ok := searchB.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured memory_search result, got %T", searchB.StructuredContent)
	}
	notesB, ok := searchBMap["notes"].([]any)
	if !ok {
		t.Fatalf("expected notes slice, got %T", searchBMap["notes"])
	}
	if len(notesB) != 0 {
		t.Fatalf("expected rootB memory search to be isolated from rootA, got %#v", notesB)
	}

	if _, err := s.callTool(context.Background(), toolCallParams{
		Name:      "memory_upsert",
		Arguments: json.RawMessage([]byte(`{"key":"shared-note","text":"note-from-b"}`)),
	}); err != nil {
		t.Fatal(err)
	}

	searchAOverride, err := s.callTool(context.Background(), toolCallParams{
		Name:      "memory_search",
		Arguments: json.RawMessage([]byte(`{"query":"note-from-a","root":` + quoteJSONString(rootA) + `}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	searchAOverrideMap, ok := searchAOverride.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured memory_search result, got %T", searchAOverride.StructuredContent)
	}
	notesAOverride, ok := searchAOverrideMap["notes"].([]any)
	if !ok || len(notesAOverride) != 1 {
		t.Fatalf("expected one note from rootA override, got %#v", searchAOverrideMap["notes"])
	}

	searchBOverride, err := s.callTool(context.Background(), toolCallParams{
		Name:      "memory_search",
		Arguments: json.RawMessage([]byte(`{"query":"note-from-b","root":` + quoteJSONString(rootB) + `}`)),
	})
	if err != nil {
		t.Fatal(err)
	}
	searchBOverrideMap, ok := searchBOverride.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured memory_search result, got %T", searchBOverride.StructuredContent)
	}
	notesBOverride, ok := searchBOverrideMap["notes"].([]any)
	if !ok || len(notesBOverride) != 1 {
		t.Fatalf("expected one note from rootB override, got %#v", searchBOverrideMap["notes"])
	}
}

func TestBrokerToolsListAddsRootOverrideSchema(t *testing.T) {
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
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}

	assertHasRoot := func(name string) {
		t.Helper()
		for _, tool := range decoded.Tools {
			if tool.Name != name {
				continue
			}
			props, _ := tool.InputSchema["properties"].(map[string]any)
			if props == nil {
				t.Fatalf("expected properties for %s input schema", name)
			}
			rootProp, ok := props["root"].(map[string]any)
			if !ok {
				t.Fatalf("expected %s to expose root override property, got %#v", name, props["root"])
			}
			if got, _ := rootProp["type"].(string); got != "string" {
				t.Fatalf("expected %s root override type=string, got %#v", name, rootProp["type"])
			}
			return
		}
		t.Fatalf("tool %s not found in tools/list", name)
	}

	assertHasRoot("repo_read_file")
	assertHasRoot("memory_search")
}

func TestLeafServerDoesNotExposeSwitchWorkspace(t *testing.T) {
	root := t.TempDir()
	s, err := NewServer(Config{Root: root, Child: true})
	if err != nil {
		t.Fatal(err)
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
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, tool := range decoded.Tools {
		if tool.Name == "repo_switch_workspace" {
			t.Fatal("leaf child server should not expose repo_switch_workspace")
		}
	}
}
