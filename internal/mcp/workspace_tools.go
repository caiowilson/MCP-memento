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
