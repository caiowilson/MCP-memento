package mcp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"memento-mcp/internal/indexing"
)

type serverMode int

const (
	serverModeBroker serverMode = iota
	serverModeLeaf
)

type workspaceClient interface {
	CallTool(context.Context, string, json.RawMessage) (toolCallResult, error)
	ToolDefinitions(context.Context) ([]Tool, error)
	Close() error
}

type childFactory func(string) (workspaceClient, error)

type managedChild struct {
	client     workspaceClient
	lastUsedAt time.Time
}

type Server struct {
	root   string
	tools  []Tool
	mem    *NoteStore
	idx    *indexing.Indexer
	mode   serverMode
	devLog bool

	devLogFilePath    string
	devLogFileErrOnce bool

	backgroundParentCtx context.Context
	backgroundCancel    context.CancelFunc

	executable        string
	childIdleTimeout  time.Duration
	childReapInterval time.Duration
	spawnChild        childFactory
	children          map[string]*managedChild
	childrenMu        sync.Mutex
	shutdownOnce      sync.Once
}

type Config struct {
	Root              string
	Child             bool
	Executable        string
	ChildIdleTimeout  time.Duration
	ChildReapInterval time.Duration

	childFactory childFactory
}

func NewServer(cfg Config) (*Server, error) {
	root := cfg.Root
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		root = wd
	}
	absRoot, err := normalizeWorkspaceRoot(root)
	if err != nil {
		return nil, err
	}

	s := &Server{
		devLog: os.Getenv("MEMENTO_MCP_DEV_LOG") == "1",
		mode:   serverModeBroker,
		root:   absRoot,
	}

	if cfg.Child {
		s.mode = serverModeLeaf
		if err := s.rebindWorkspace(absRoot); err != nil {
			return nil, err
		}
		s.tools = s.leafToolsetFor(absRoot, s.idx, s.mem)
		return s, nil
	}

	s.executable = strings.TrimSpace(cfg.Executable)
	if s.executable == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		s.executable = exe
	}
	s.childIdleTimeout = cfg.ChildIdleTimeout
	if s.childIdleTimeout <= 0 {
		s.childIdleTimeout = 10 * time.Minute
	}
	s.childReapInterval = cfg.ChildReapInterval
	if s.childReapInterval <= 0 {
		s.childReapInterval = time.Minute
	}
	if cfg.childFactory != nil {
		s.spawnChild = cfg.childFactory
	} else {
		s.spawnChild = s.spawnProcessChild
	}
	s.children = map[string]*managedChild{}

	defs, err := s.ensureChildToolDefinitions(context.Background(), absRoot)
	if err != nil {
		return nil, err
	}
	s.tools = s.brokerToolsetFrom(defs)
	return s, nil
}

func (s *Server) StartBackgroundIndexing(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.backgroundParentCtx = ctx
	if s.mode == serverModeBroker {
		go s.reapIdleChildren(ctx)
		go func() {
			<-ctx.Done()
			s.shutdown()
		}()
		return
	}
	s.restartBackgroundIndexing()
}

func normalizeWorkspaceRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("workspace root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absRoot = filepath.Clean(absRoot)
	info, err := os.Stat(absRoot)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace root is not a directory: %s", absRoot)
	}
	return absRoot, nil
}

func (s *Server) indexerConfig(rootAbs string) indexing.Config {
	pollSeconds := envInt("MEMENTO_INDEX_POLL_SECONDS", 10)
	if indexing.IsGitRepo(rootAbs) {
		pollSeconds = 0
	}
	return indexing.Config{
		RootAbs:       rootAbs,
		PollInterval:  time.Duration(pollSeconds) * time.Second,
		MaxTotalBytes: int64(envInt("MEMENTO_INDEX_MAX_TOTAL_BYTES", 20*1024*1024)),
		MaxFileBytes:  int64(envInt("MEMENTO_INDEX_MAX_FILE_BYTES", 1*1024*1024)),
	}
}

func (s *Server) leafToolsetFor(root string, idx *indexing.Indexer, mem *NoteStore) []Tool {
	return []Tool{
		newRepoListFilesTool(root),
		newRepoReadFileTool(root),
		newRepoSearchTool(root),
		newRepoRelatedFilesTool(root),
		newRepoContextTool(root, idx),
		newRepoIndexStatusTool(idx),
		newRepoReindexTool(idx),
		newRepoClearIndexTool(idx),
		newRepoIndexDebugTool(idx),
		newMemoryUpsertTool(mem),
		newMemorySearchTool(mem),
		newMemoryClearTool(mem),
	}
}

func (s *Server) rebindWorkspace(rootAbs string) error {
	mem, err := NewNoteStore(rootAbs)
	if err != nil {
		return err
	}
	idx, err := indexing.New(s.indexerConfig(rootAbs))
	if err != nil {
		return err
	}

	s.root = rootAbs
	s.mem = mem
	s.idx = idx
	s.tools = s.leafToolsetFor(rootAbs, idx, mem)

	s.devLogFilePath = ""
	s.devLogFileErrOnce = false
	if s.devLog {
		if p, err := defaultDevToolLogPath(rootAbs); err == nil {
			s.devLogFilePath = p
		}
	}
	return nil
}

func (s *Server) brokerToolsetFrom(defs []Tool) []Tool {
	tools := make([]Tool, 0, len(defs)+1)
	insertedSwitch := false
	for _, def := range defs {
		if def.Name == "repo_switch_workspace" {
			continue
		}
		tool := cloneToolDefinition(def)
		tool.InputSchema = augmentInputSchemaWithRoot(tool.InputSchema)
		tool.Handler = s.proxyToolHandler(tool.Name)
		tools = append(tools, tool)
		if def.Name == "repo_context" && !insertedSwitch {
			tools = append(tools, newRepoSwitchWorkspaceTool(s))
			insertedSwitch = true
		}
	}
	if !insertedSwitch {
		tools = append(tools, newRepoSwitchWorkspaceTool(s))
	}
	return tools
}

func (s *Server) restartBackgroundIndexing() {
	if s.backgroundCancel != nil {
		s.backgroundCancel()
		s.backgroundCancel = nil
	}
	if s.backgroundParentCtx == nil || s.idx == nil {
		return
	}

	runCtx, cancel := context.WithCancel(s.backgroundParentCtx)
	s.backgroundCancel = cancel

	root := s.root
	idx := s.idx

	idx.Start(runCtx)
	go func() {
		_ = idx.IndexAll(runCtx)
	}()

	notifySemantic := func(add, del []string) {
		if touchesGoSemantic(add) || touchesGoSemantic(del) {
			InvalidateGoSemanticCache(root)
			go WarmGoSemanticCache(runCtx, root)
		}
		if touchesJSRelations(add) || touchesJSRelations(del) {
			InvalidateJSImportGraphCache(root)
		}
		if touchesPHPRelations(add) || touchesPHPRelations(del) {
			InvalidatePHPIncludeGraphCache(root)
		}
	}

	startFS := func() bool {
		monitor := indexing.NewFSChangeMonitor(
			s.root,
			s.idx,
			time.Duration(envInt("MEMENTO_FS_DEBOUNCE_MS", 500))*time.Millisecond,
			notifySemantic,
		)
		if err := monitor.Start(runCtx); err != nil {
			if s.devLog {
				s.logf("fs watcher start failed, will fallback if possible: %v", err)
			}
			return false
		}
		return true
	}

	startGit := func() bool {
		if !indexing.IsGitRepo(s.root) {
			return false
		}
		monitor := indexing.NewGitChangeMonitor(
			root,
			idx,
			time.Duration(envInt("MEMENTO_GIT_POLL_SECONDS", 2))*time.Second,
			time.Duration(envInt("MEMENTO_GIT_DEBOUNCE_MS", 500))*time.Millisecond,
			notifySemantic,
		)
		monitor.Start(runCtx)
		return true
	}

	detector := strings.ToLower(strings.TrimSpace(os.Getenv("MEMENTO_CHANGE_DETECTOR")))
	switch detector {
	case "git":
		if startGit() {
			return
		}
		if s.devLog {
			s.logf("git polling not available, falling back to fs watcher")
		}
		startFS()
	case "fs":
		if startFS() {
			return
		}
		if s.devLog {
			s.logf("fs watcher failed, falling back to git polling")
		}
		startGit()
	default:
		// "auto" or unknown: fs-first, fallback to git polling
		if startFS() {
			return
		}
		if s.devLog {
			s.logf("fs watcher failed, falling back to git polling")
		}
		startGit()
	}
}

func (s *Server) ensureChildToolDefinitions(ctx context.Context, root string) ([]Tool, error) {
	client, _, err := s.ensureChild(ctx, root)
	if err != nil {
		return nil, err
	}
	return client.ToolDefinitions(ctx)
}

func (s *Server) ensureChild(ctx context.Context, root string) (workspaceClient, bool, error) {
	now := time.Now()

	s.childrenMu.Lock()
	if existing := s.children[root]; existing != nil {
		existing.lastUsedAt = now
		client := existing.client
		s.childrenMu.Unlock()
		return client, false, nil
	}
	spawn := s.spawnChild
	s.childrenMu.Unlock()

	client, err := spawn(root)
	if err != nil {
		return nil, false, err
	}

	s.childrenMu.Lock()
	defer s.childrenMu.Unlock()
	if existing := s.children[root]; existing != nil {
		existing.lastUsedAt = now
		_ = client.Close()
		return existing.client, false, nil
	}
	s.children[root] = &managedChild{client: client, lastUsedAt: now}
	return client, true, nil
}

func (s *Server) currentRoot() string {
	if s.mode != serverModeBroker {
		return s.root
	}
	s.childrenMu.Lock()
	defer s.childrenMu.Unlock()
	return s.root
}

func (s *Server) setCurrentRoot(root string) {
	if s.mode != serverModeBroker {
		s.root = root
		return
	}
	s.childrenMu.Lock()
	s.root = root
	s.childrenMu.Unlock()
}

func (s *Server) closeChild(root string) {
	s.childrenMu.Lock()
	child := s.children[root]
	if child != nil {
		delete(s.children, root)
	}
	s.childrenMu.Unlock()
	if child != nil {
		_ = child.client.Close()
	}
}

func (s *Server) shutdown() {
	s.shutdownOnce.Do(func() {
		if s.backgroundCancel != nil {
			s.backgroundCancel()
		}
		if s.mode != serverModeBroker {
			return
		}
		s.childrenMu.Lock()
		children := make([]workspaceClient, 0, len(s.children))
		for root, child := range s.children {
			children = append(children, child.client)
			delete(s.children, root)
		}
		s.childrenMu.Unlock()
		for _, child := range children {
			_ = child.Close()
		}
	})
}

func (s *Server) reapIdleChildren(ctx context.Context) {
	ticker := time.NewTicker(s.childReapInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-s.childIdleTimeout)
			var roots []string

			s.childrenMu.Lock()
			for root, child := range s.children {
				if child.lastUsedAt.Before(cutoff) {
					roots = append(roots, root)
				}
			}
			s.childrenMu.Unlock()

			for _, root := range roots {
				s.closeChild(root)
			}
		}
	}
}

func cloneToolDefinition(src Tool) Tool {
	return Tool{
		Name:         src.Name,
		Title:        src.Title,
		Description:  src.Description,
		InputSchema:  deepCopyMap(src.InputSchema),
		OutputSchema: deepCopyMap(src.OutputSchema),
		Annotations:  deepCopyMap(src.Annotations),
	}
}

func deepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	b, err := json.Marshal(src)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func augmentInputSchemaWithRoot(schema map[string]any) map[string]any {
	out := deepCopyMap(schema)
	if out == nil {
		out = map[string]any{}
	}
	if typ, ok := out["type"].(string); !ok || typ == "" {
		out["type"] = "object"
	}
	props, _ := out["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
		out["properties"] = props
	}
	props["root"] = map[string]any{
		"type":        "string",
		"description": "Optional workspace root override. When provided, routes this tool call to that workspace without changing the active session root.",
	}
	return out
}

func (s *Server) proxyToolHandler(name string) ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		targetRoot, forwarded, err := s.resolveProxyRequest(raw)
		if err != nil {
			return nil, err
		}
		res, err := s.callChildTool(ctx, targetRoot, name, forwarded)
		if err != nil {
			return nil, err
		}
		return res, nil
	}
}

func (s *Server) resolveProxyRequest(raw json.RawMessage) (string, json.RawMessage, error) {
	args, err := requireArgs(raw)
	if err != nil {
		return "", nil, err
	}

	targetRoot := s.currentRoot()
	if value, ok := args["root"]; ok {
		root, ok := value.(string)
		if !ok {
			return "", nil, fmt.Errorf("invalid argument: root must be a string")
		}
		targetRoot, err = normalizeWorkspaceRoot(root)
		if err != nil {
			return "", nil, err
		}
		delete(args, "root")
	}
	if targetRoot == "" {
		return "", nil, fmt.Errorf("workspace root is required")
	}

	if len(args) == 0 {
		return targetRoot, json.RawMessage([]byte(`{}`)), nil
	}
	b, err := json.Marshal(args)
	if err != nil {
		return "", nil, err
	}
	return targetRoot, json.RawMessage(b), nil
}

func (s *Server) callChildTool(ctx context.Context, root, name string, args json.RawMessage) (toolCallResult, error) {
	client, _, err := s.ensureChild(ctx, root)
	if err != nil {
		return toolCallResult{}, err
	}
	res, err := client.CallTool(ctx, name, args)
	if err == nil {
		return res, nil
	}
	if !isChildTransportError(err) {
		return toolCallResult{}, err
	}

	s.closeChild(root)

	client, _, err = s.ensureChild(ctx, root)
	if err != nil {
		return toolCallResult{}, err
	}
	return client.CallTool(ctx, name, args)
}

func touchesGoSemantic(paths []string) bool {
	for _, p := range paths {
		if isGoSemanticPath(p) {
			return true
		}
	}
	return false
}

func isGoSemanticPath(rel string) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	base := filepath.Base(rel)
	if base == "go.mod" || base == "go.sum" {
		return true
	}
	return strings.HasSuffix(rel, ".go")
}

func touchesJSRelations(paths []string) bool {
	for _, p := range paths {
		if isJSRelationPath(p) {
			return true
		}
	}
	return false
}

func isJSRelationPath(rel string) bool {
	switch strings.ToLower(filepath.Ext(filepath.ToSlash(filepath.Clean(rel)))) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return true
	default:
		return false
	}
}

func touchesPHPRelations(paths []string) bool {
	for _, p := range paths {
		if isPHPRelationPath(p) {
			return true
		}
	}
	return false
}

func isPHPRelationPath(rel string) bool {
	return strings.EqualFold(filepath.Ext(filepath.ToSlash(filepath.Clean(rel))), ".php")
}

func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	defer s.shutdown()

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024), 10*1024*1024)
	enc := json.NewEncoder(out)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		if req.ID == nil {
			continue
		}

		resp := s.handleRPC(ctx, req)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func (s *Server) handleRPC(ctx context.Context, req rpcRequest) rpcResponse {
	switch req.Method {
	case "initialize":
		return rpcOK(req.ID, s.initializeResult(req.Params))
	case "tools/list":
		return rpcOK(req.ID, map[string]any{"tools": s.tools})
	case "tools/call":
		var params toolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return rpcErr(req.ID, -32602, "Invalid params", err.Error())
		}
		res, err := s.callTool(ctx, params)
		if err != nil {
			return rpcOK(req.ID, toolCallResult{
				Content: []toolContent{{Type: "text", Text: err.Error()}},
				IsError: true,
			})
		}
		return rpcOK(req.ID, res)
	default:
		return rpcErr(req.ID, -32601, "Method not found", req.Method)
	}
}

func (s *Server) initializeResult(raw json.RawMessage) map[string]any {
	protocolVersion := "2024-11-05"

	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(raw, &params); err == nil && params.ProtocolVersion != "" {
		protocolVersion = params.ProtocolVersion
	}

	return map[string]any{
		"protocolVersion": protocolVersion,
		"serverInfo": map[string]any{
			"name":    "memento-mcp",
			"version": "0.6.0",
		},
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
	}
}

func (s *Server) callTool(ctx context.Context, params toolCallParams) (toolCallResult, error) {
	var tool *Tool
	for i := range s.tools {
		if s.tools[i].Name == params.Name {
			tool = &s.tools[i]
			break
		}
	}
	if tool == nil && strings.Contains(params.Name, ".") {
		alt := strings.ReplaceAll(params.Name, ".", "_")
		for i := range s.tools {
			if s.tools[i].Name == alt {
				tool = &s.tools[i]
				break
			}
		}
	}
	if tool == nil || tool.Handler == nil {
		return toolCallResult{}, fmt.Errorf("unknown tool: %s", params.Name)
	}

	args := params.Arguments
	if len(args) == 0 {
		args = json.RawMessage([]byte(`{}`))
	}
	if s.devLog {
		line := fmt.Sprintf(
			"%s tool-call name=%s args=%s",
			time.Now().UTC().Format(time.RFC3339Nano),
			params.Name,
			formatArgsForLog(args),
		)
		s.logf("%s", line)
		s.appendDevLogLine(line)
	}

	content, err := tool.Handler(ctx, args)
	if err != nil {
		return toolCallResult{}, err
	}

	switch v := content.(type) {
	case string:
		return toolCallResult{Content: []toolContent{{Type: "text", Text: v}}}, nil
	case toolCallResult:
		return v, nil
	case *toolCallResult:
		if v == nil {
			return toolCallResult{}, nil
		}
		return *v, nil
	default:
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return toolCallResult{}, err
		}
		var structured any
		if err := json.Unmarshal(b, &structured); err != nil {
			return toolCallResult{}, err
		}
		return toolCallResult{
			Content:           []toolContent{{Type: "text", Text: string(b)}},
			StructuredContent: structured,
		}, nil
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func rpcOK(id any, result any) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func rpcErr(id any, code int, msg string, data any) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg, Data: data}}
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type toolCallResult struct {
	Content           []toolContent `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Tool struct {
	Name         string         `json:"name"`
	Title        string         `json:"title,omitempty"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	Annotations  map[string]any `json:"annotations,omitempty"`
	Handler      ToolHandler    `json:"-"`
}

type ToolHandler func(context.Context, json.RawMessage) (any, error)

func readOnlyAnnotations() map[string]any {
	return map[string]any{
		"readOnlyHint": true,
	}
}

func mutatingAnnotations() map[string]any {
	return map[string]any{
		"readOnlyHint": false,
	}
}

func destructiveAnnotations() map[string]any {
	return map[string]any{
		"readOnlyHint":    false,
		"destructiveHint": true,
	}
}

func requireArgs(raw json.RawMessage) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return map[string]any{}, nil
	}
	return m, nil
}

func asString(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func asFloat(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}

func asStringSlice(m map[string]any, key string) ([]string, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return nil, false
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

var errOutsideRoot = errors.New("path escapes workspace root")

func (s *Server) logf(format string, args ...any) {
	log.Printf("[mcp] "+format, args...)
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func formatArgsForLog(raw json.RawMessage) string {
	const maxBytes = 2000
	if len(raw) <= maxBytes {
		return string(raw)
	}
	return string(raw[:maxBytes]) + "…"
}

func defaultDevToolLogPath(repoRoot string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(repoRoot))
	repoID := hex.EncodeToString(sum[:16])
	dir := filepath.Join(home, ".memento-mcp", "repos", repoID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "tool-calls.log"), nil
}

func (s *Server) appendDevLogLine(line string) {
	if s.devLogFilePath == "" {
		return
	}
	f, err := os.OpenFile(s.devLogFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		if !s.devLogFileErrOnce {
			s.devLogFileErrOnce = true
			s.logf("dev log file open failed: %v", err)
		}
		return
	}
	_, _ = f.WriteString(line + "\n")
	_ = f.Close()
}
