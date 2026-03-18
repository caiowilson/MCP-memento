package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func newRepoSwitchWorkspaceTool(s *Server) Tool {
	return Tool{
		Name:        "repo_switch_workspace",
		Title:       "Switch Workspace",
		Description: "Switch the active workspace root at runtime without restarting the MCP process.",
		Annotations: mutatingAnnotations(),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"path"},
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Absolute or relative path to the new workspace root.",
				},
				"reindexNow": map[string]any{
					"type":        "boolean",
					"description": "When true, waits for a full index pass before returning.",
				},
			},
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			args, err := requireArgs(raw)
			if err != nil {
				return nil, err
			}
			path, ok := asString(args, "path")
			if !ok || strings.TrimSpace(path) == "" {
				return nil, fmt.Errorf("missing required argument: path")
			}
			reindexNow := false
			if v, ok := args["reindexNow"].(bool); ok {
				reindexNow = v
			}
			return s.switchWorkspace(ctx, path, reindexNow)
		},
	}
}

func (s *Server) switchWorkspace(ctx context.Context, root string, reindexNow bool) (any, error) {
	absRoot, err := normalizeWorkspaceRoot(root)
	if err != nil {
		return nil, err
	}

	previousRoot := s.currentRoot()
	_, spawned, err := s.ensureChild(ctx, absRoot)
	if err != nil {
		return nil, err
	}

	if reindexNow {
		if _, err := s.callChildTool(ctx, absRoot, "repo_reindex", json.RawMessage([]byte(`{}`))); err != nil {
			return nil, err
		}
	}
	s.setCurrentRoot(absRoot)

	indexStatus, err := s.callChildTool(ctx, absRoot, "repo_index_status", json.RawMessage([]byte(`{}`)))
	if err != nil {
		return nil, err
	}
	indexDebug, err := s.callChildTool(ctx, absRoot, "repo_index_debug", json.RawMessage([]byte(`{}`)))
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"switched":     absRoot != previousRoot,
		"spawned":      spawned,
		"previousRoot": previousRoot,
		"root":         absRoot,
		"indexDebug":   indexDebug.StructuredContent,
		"indexStatus":  indexStatus.StructuredContent,
	}, nil
}
