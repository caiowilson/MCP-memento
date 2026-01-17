package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"memento-mcp/internal/indexing"
)

type Server struct {
	root  string
	tools []Tool
	mem   *NoteStore
	idx   *indexing.Indexer
}

type Config struct {
	Root string
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
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	mem, err := NewNoteStore(absRoot)
	if err != nil {
		return nil, err
	}

	idxCfg := indexing.Config{
		RootAbs:       absRoot,
		PollInterval:  time.Duration(envInt("MEMENTO_INDEX_POLL_SECONDS", 10)) * time.Second,
		MaxTotalBytes: int64(envInt("MEMENTO_INDEX_MAX_TOTAL_BYTES", 20*1024*1024)),
		MaxFileBytes:  int64(envInt("MEMENTO_INDEX_MAX_FILE_BYTES", 1*1024*1024)),
	}
	idx, err := indexing.New(idxCfg)
	if err != nil {
		return nil, err
	}

	s := &Server{
		root: absRoot,
		mem:  mem,
		idx:  idx,
	}
	s.tools = []Tool{
		newRepoListFilesTool(absRoot),
		newRepoReadFileTool(absRoot),
		newRepoSearchTool(absRoot),
		newRepoRelatedFilesTool(absRoot),
		newRepoContextTool(absRoot, idx),
		newRepoIndexStatusTool(idx),
		newRepoReindexTool(idx),
		newMemoryUpsertTool(mem),
		newMemorySearchTool(mem),
	}
	return s, nil
}

func (s *Server) StartBackgroundIndexing(ctx context.Context) {
	if s.idx == nil {
		return
	}
	s.idx.Start(ctx)
	go func() {
		_ = s.idx.IndexAll(ctx)
	}()
}

func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
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
			"version": "0.1.0",
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
	if tool == nil || tool.Handler == nil {
		return toolCallResult{}, fmt.Errorf("unknown tool: %s", params.Name)
	}

	args := params.Arguments
	if len(args) == 0 {
		args = json.RawMessage([]byte(`{}`))
	}

	content, err := tool.Handler(ctx, args)
	if err != nil {
		return toolCallResult{}, err
	}

	switch v := content.(type) {
	case string:
		return toolCallResult{Content: []toolContent{{Type: "text", Text: v}}}, nil
	default:
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return toolCallResult{}, err
		}
		return toolCallResult{Content: []toolContent{{Type: "text", Text: string(b)}}}, nil
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
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Handler     ToolHandler    `json:"-"`
}

type ToolHandler func(context.Context, json.RawMessage) (any, error)

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
