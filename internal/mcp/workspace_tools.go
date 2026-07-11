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
					"description": "Workspace changes always trigger a full reindex. When true, waits for that pass before returning.",
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
	changed := absRoot != previousRoot
	_, spawned, err := s.ensureChild(ctx, absRoot)
	if err != nil {
		return nil, err
	}

	reindexTriggered := changed || reindexNow
	// A newly spawned child starts with a full index pass. Reused children need
	// an explicit refresh when they become active again.
	if reindexNow || (changed && !spawned) {
		if err := s.triggerWorkspaceReindex(ctx, absRoot, reindexNow); err != nil {
			return nil, err
		}
	}
	s.setCurrentRoot(absRoot)
	s.rootSource = workspaceRootSourceManual
	s.logWorkspaceRootSelection()

	indexStatus, err := s.callChildTool(ctx, absRoot, "repo_index_status", json.RawMessage([]byte(`{}`)))
	if err != nil {
		return nil, err
	}
	indexDebug, err := s.callChildTool(ctx, absRoot, "repo_index_debug", json.RawMessage([]byte(`{}`)))
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"switched":         changed,
		"spawned":          spawned,
		"reindexTriggered": reindexTriggered,
		"reindexWaited":    reindexNow,
		"previousRoot":     previousRoot,
		"root":             absRoot,
		"indexDebug":       indexDebug.StructuredContent,
		"indexStatus":      indexStatus.StructuredContent,
	}, nil
}

func (s *Server) triggerWorkspaceReindex(ctx context.Context, root string, wait bool) error {
	if wait {
		_, err := s.callChildTool(ctx, root, "repo_reindex", json.RawMessage([]byte(`{}`)))
		return err
	}

	backgroundCtx := s.backgroundParentCtx
	if backgroundCtx == nil {
		backgroundCtx = context.Background()
	}
	go func() {
		if _, err := s.callChildTool(backgroundCtx, root, "repo_reindex", json.RawMessage([]byte(`{}`))); err != nil {
			s.logf("workspace root %q automatic reindex failed: %v", root, err)
		}
	}()
	return nil
}
