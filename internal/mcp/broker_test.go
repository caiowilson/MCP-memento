package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

type recordingChildCalls struct {
	mu     sync.Mutex
	counts map[string]map[string]int
}

func newRecordingChildFactory(t *testing.T, calls *recordingChildCalls) childFactory {
	t.Helper()
	base := newLocalChildFactory(t)
	return func(root string) (workspaceClient, error) {
		client, err := base(root)
		if err != nil {
			return nil, err
		}
		return &recordingChildClient{workspaceClient: client, root: root, calls: calls}, nil
	}
}

type recordingChildClient struct {
	workspaceClient
	root  string
	calls *recordingChildCalls
}

func (c *recordingChildClient) CallTool(ctx context.Context, name string, args json.RawMessage) (toolCallResult, error) {
	c.calls.mu.Lock()
	if c.calls.counts == nil {
		c.calls.counts = map[string]map[string]int{}
	}
	if c.calls.counts[c.root] == nil {
		c.calls.counts[c.root] = map[string]int{}
	}
	c.calls.counts[c.root][name]++
	c.calls.mu.Unlock()
	return c.workspaceClient.CallTool(ctx, name, args)
}

func (c *recordingChildCalls) count(root, name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[root][name]
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

func TestBrokerSwitchWorkspaceAutomaticallyReindexesChangedRoot(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	calls := &recordingChildCalls{}
	s, err := NewServer(Config{Root: rootA, childFactory: newRecordingChildFactory(t, calls)})
	if err != nil {
		t.Fatal(err)
	}
	s.StartBackgroundIndexing(context.Background())
	t.Cleanup(s.shutdown)

	switchRoot := func(root string) map[string]any {
		t.Helper()
		res, err := s.callTool(context.Background(), toolCallParams{
			Name:      "repo_switch_workspace",
			Arguments: json.RawMessage([]byte(`{"path":` + quoteJSONString(root) + `}`)),
		})
		if err != nil {
			t.Fatal(err)
		}
		result, ok := res.StructuredContent.(map[string]any)
		if !ok {
			t.Fatalf("expected structured switch result, got %T", res.StructuredContent)
		}
		return result
	}

	first := switchRoot(rootB)
	if triggered, _ := first["reindexTriggered"].(bool); !triggered {
		t.Fatalf("expected changed root to trigger reindex, got %#v", first)
	}
	if waited, _ := first["reindexWaited"].(bool); waited {
		t.Fatalf("expected default automatic reindex not to wait, got %#v", first)
	}
	waitForCondition(t, time.Second, func() bool {
		return calls.count(rootB, "repo_index_status") >= 1
	})
	if got := calls.count(rootB, "repo_reindex"); got != 0 {
		t.Fatalf("expected fresh rootB child startup indexing to avoid duplicate reindex, got %d", got)
	}

	same := switchRoot(rootB)
	if triggered, _ := same["reindexTriggered"].(bool); triggered {
		t.Fatalf("expected same-root switch not to reindex, got %#v", same)
	}
	if got := calls.count(rootB, "repo_reindex"); got != 0 {
		t.Fatalf("expected no rootB reindex after same-root switch, got %d", got)
	}

	switchRoot(rootA)
	waitForCondition(t, time.Second, func() bool {
		return calls.count(rootA, "repo_reindex") == 1
	})
	switchRoot(rootB)
	waitForCondition(t, time.Second, func() bool {
		return calls.count(rootB, "repo_reindex") == 1
	})
}

func TestClientRootsChangeAutomaticallyReindexesChangedRoot(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	calls := &recordingChildCalls{}
	s, err := NewServer(Config{Root: rootA, childFactory: newRecordingChildFactory(t, calls)})
	if err != nil {
		t.Fatal(err)
	}
	s.StartBackgroundIndexing(context.Background())
	t.Cleanup(s.shutdown)
	s.rootSource = workspaceRootSourceCWD
	s.allowClientRootsFallback = true
	if _, spawned, err := s.ensureChild(context.Background(), rootB); err != nil {
		t.Fatal(err)
	} else if !spawned {
		t.Fatal("expected rootB child to be prewarmed for reuse test")
	}

	result, err := json.Marshal(map[string]any{
		"roots": []map[string]any{{"uri": (&url.URL{Scheme: "file", Path: rootB}).String()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := &stdioSession{pendingRootsID: "number:1"}
	s.handleClientRPCResponse(context.Background(), rpcMessage{ID: float64(1), Result: result}, session)

	if got := s.currentRoot(); got != rootB {
		t.Fatalf("expected client roots to select %q, got %q", rootB, got)
	}
	waitForCondition(t, time.Second, func() bool {
		return calls.count(rootB, "repo_reindex") == 1
	})
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

func TestBrokerLinkedWorktreeKeepsRepoContextLocalAndMemoryShared(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mainRoot := t.TempDir()
	runNoteStoreGit(t, mainRoot, "init", "-q")
	runNoteStoreGit(t, mainRoot, "config", "user.email", "memento@example.test")
	runNoteStoreGit(t, mainRoot, "config", "user.name", "Memento Test")
	if err := os.WriteFile(filepath.Join(mainRoot, "branch.txt"), []byte("linked-checkout\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runNoteStoreGit(t, mainRoot, "add", "branch.txt")
	runNoteStoreGit(t, mainRoot, "commit", "-qm", "initial")

	linkedRoot := filepath.Join(t.TempDir(), "linked")
	runNoteStoreGit(t, mainRoot, "worktree", "add", "--detach", "--quiet", linkedRoot, "HEAD")
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", mainRoot, "worktree", "remove", "--force", linkedRoot).Run()
	})
	if err := os.WriteFile(filepath.Join(mainRoot, "branch.txt"), []byte("main-checkout\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runNoteStoreGit(t, mainRoot, "add", "branch.txt")
	runNoteStoreGit(t, mainRoot, "commit", "-qm", "advance main")

	mainStore, err := NewNoteStore(mainRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mainStore.Upsert(Note{Key: "shared-main-note", Text: "visible from linked checkout"}); err != nil {
		t.Fatal(err)
	}

	s := newBrokerServerForTest(t, linkedRoot)
	read, err := s.callTool(context.Background(), toolCallParams{
		Name: "repo_read_file", Arguments: json.RawMessage(`{"path":"branch.txt"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	readMap, ok := read.StructuredContent.(map[string]any)
	if !ok || !strings.Contains(readMap["content"].(string), "linked-checkout") {
		t.Fatalf("repo_read_file used wrong checkout: %#v", read.StructuredContent)
	}
	readWorkspace, ok := readMap["workspace"].(map[string]any)
	if !ok || readWorkspace["checkoutRoot"] != linkedRoot {
		t.Fatalf("repo workspace context = %#v, want checkoutRoot %q", readMap["workspace"], linkedRoot)
	}

	search, err := s.callTool(context.Background(), toolCallParams{
		Name: "memory_search", Arguments: json.RawMessage(`{"query":"visible from linked"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	searchMap, ok := search.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("memory_search result = %T", search.StructuredContent)
	}
	notes, ok := searchMap["notes"].([]any)
	if !ok || len(notes) != 1 {
		t.Fatalf("shared memory notes = %#v, want one", searchMap["notes"])
	}
	searchWorkspace, ok := searchMap["workspace"].(map[string]any)
	if !ok || searchWorkspace["checkoutRoot"] != linkedRoot || searchWorkspace["memoryScopeRoot"] != canonicalPath(mainRoot) || searchWorkspace["linkedWorktree"] != true {
		t.Fatalf("memory workspace context = %#v", searchMap["workspace"])
	}

	if _, err := s.callTool(context.Background(), toolCallParams{
		Name: "memory_upsert", Arguments: json.RawMessage(`{"key":"linked-note","text":"written through linked checkout"}`),
	}); err != nil {
		t.Fatal(err)
	}
	shared, err := mainStore.Search("written through linked", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(shared) != 1 || shared[0].Key != "linked-note" {
		t.Fatalf("main memory search = %#v, want linked-note", shared)
	}
	if got := s.currentRoot(); got != linkedRoot {
		t.Fatalf("active checkout changed to %q, want %q", got, linkedRoot)
	}
}

func TestBrokerMemoryReadsRejectUnverifiedCWD(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	s := newBrokerServerForTest(t, root)
	s.rootSource = workspaceRootSourceCWD
	s.allowClientRootsFallback = true

	for _, tc := range []struct {
		name string
		args json.RawMessage
	}{
		{name: "memory_search", args: json.RawMessage(`{"query":"handoff"}`)},
		{name: "memory_list", args: json.RawMessage(`{}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.callTool(context.Background(), toolCallParams{Name: tc.name, Arguments: tc.args})
			if err == nil || !strings.Contains(err.Error(), "active workspace is unverified") {
				t.Fatalf("expected unverified active workspace error, got %v", err)
			}
		})
	}

	res, err := s.callTool(context.Background(), toolCallParams{
		Name:      "memory_list",
		Arguments: json.RawMessage(`{"root":` + quoteJSONString(root) + `}`),
	})
	if err != nil {
		t.Fatalf("expected explicit root to permit memory read: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected successful explicit-root memory read, got %#v", res)
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
			if description, _ := rootProp["description"].(string); !strings.Contains(description, "active checkout/worktree") {
				t.Fatalf("expected %s root override to define checkout semantics, got %q", name, description)
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
