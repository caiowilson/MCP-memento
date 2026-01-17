package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

func newMemoryUpsertTool(store *NoteStore) Tool {
	return Tool{
		Name:        "memory_upsert",
		Description: "Upsert a repo-scoped note (explicit memory) keyed by `key`.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"key", "text"},
			"properties": map[string]any{
				"key": map[string]any{
					"type":        "string",
					"description": "Stable identifier for the note (e.g. \"repo-overview\" or \"internal/mcp/server.go\").",
				},
				"text": map[string]any{
					"type":        "string",
					"description": "Note content to store.",
				},
				"tags": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional tags for filtering.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Optional repo-relative path this note refers to.",
				},
				"meta": map[string]any{
					"type":                 "object",
					"additionalProperties": map[string]any{"type": "string"},
					"description":          "Optional metadata map.",
				},
			},
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			args, err := requireArgs(raw)
			if err != nil {
				return nil, err
			}
			key, _ := asString(args, "key")
			text, _ := asString(args, "text")
			path, _ := asString(args, "path")
			tags, _ := asStringSlice(args, "tags")

			meta := map[string]string(nil)
			if v, ok := args["meta"].(map[string]any); ok {
				meta = map[string]string{}
				for k, vv := range v {
					s, ok := vv.(string)
					if !ok {
						continue
					}
					meta[k] = s
				}
			}

			n, err := store.Upsert(Note{
				Key:  key,
				Text: text,
				Tags: tags,
				Path: path,
				Meta: meta,
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"stored": n,
			}, nil
		},
	}
}

func newMemorySearchTool(store *NoteStore) Tool {
	return Tool{
		Name:        "memory_search",
		Description: "Search repo-scoped notes (explicit memory) by substring and/or tags.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Substring query (optional).",
				},
				"tags": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Require all tags (optional).",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Default 20.",
				},
			},
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			_ = ctx
			args, err := requireArgs(raw)
			if err != nil {
				return nil, err
			}
			query, _ := asString(args, "query")
			tags, _ := asStringSlice(args, "tags")

			limit := 20
			if f, ok := asFloat(args, "limit"); ok && int(f) > 0 {
				limit = int(f)
			}
			if query == "" && len(tags) == 0 {
				return nil, fmt.Errorf("provide at least one of: query, tags")
			}

			notes, err := store.Search(query, tags, limit)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"query": query,
				"tags":  tags,
				"notes": notes,
			}, nil
		},
	}
}

func newMemoryClearTool(store *NoteStore) Tool {
	return Tool{
		Name:        "memory_clear",
		Description: "Clear all repo-scoped notes.",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			_ = ctx
			_ = raw
			if err := store.Clear(); err != nil {
				return nil, err
			}
			return map[string]any{"cleared": true}, nil
		},
	}
}
