package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

func newMemoryUpsertTool(store *NoteStore) Tool {
	return Tool{
		Name:        "memory_upsert",
		Title:       "Save Memory Note",
		Description: "Upsert a repo-scoped note (explicit memory) keyed by `key`.",
		Annotations: mutatingAnnotations(),
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
		Title:       "Search Memory Notes",
		Description: "Search repo-scoped notes (explicit memory) by substring and/or tags.",
		Annotations: readOnlyAnnotations(),
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

func newMemoryListTool(store *NoteStore) Tool {
	return Tool{
		Name:        "memory_list",
		Title:       "List Memory Notes",
		Description: `List all durable notes stored for the current repository scope. Returns every note with its key, text, tags, path, updatedAt, and meta. Use to enumerate saved context or to find a note's key before calling memory_delete.`,
		Annotations: readOnlyAnnotations(),
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			_ = ctx
			notes, err := store.List()
			if err != nil {
				return nil, err
			}
			if notes == nil {
				notes = []Note{}
			}
			return map[string]any{"notes": notes}, nil
		},
	}
}

func newMemoryDeleteTool(store *NoteStore) Tool {
	return Tool{
		Name:        "memory_delete",
		Title:       "Delete Memory Note",
		Description: `Delete a single durable note by its key. Use memory_list first to find the exact key if unsure. Returns {"deleted": true, "key": "..."} on success; errors if the key does not exist.`,
		Annotations: destructiveAnnotations(),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"key"},
			"properties": map[string]any{
				"key": map[string]any{
					"type":        "string",
					"description": "Exact key of the note to delete.",
				},
			},
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			_ = ctx
			args, err := requireArgs(raw)
			if err != nil {
				return nil, err
			}
			key, _ := asString(args, "key")
			if key == "" {
				return nil, fmt.Errorf("key is required")
			}
			if err := store.Delete(key); err != nil {
				return nil, err
			}
			return map[string]any{"deleted": true, "key": key}, nil
		},
	}
}

func newMemoryClearTool(store *NoteStore) Tool {
	return Tool{
		Name:        "memory_clear",
		Title:       "Clear Memory Notes",
		Description: "Clear all repo-scoped notes.",
		Annotations: destructiveAnnotations(),
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
