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
				"anchors": map[string]any{
					"type":        "array",
					"items":       memoryAnchorSchema(),
					"description": "Optional code anchors. Current hashes, Git commit, branch, and symbol lines are captured on save.",
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
			anchors, err := parseMemoryAnchors(args["anchors"])
			if err != nil {
				return nil, err
			}

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
				Key:     key,
				Text:    text,
				Tags:    tags,
				Path:    path,
				Meta:    meta,
				Anchors: anchors,
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
		Description: "Search active repo-scoped notes by substring and/or tags. Anchors are reconciled first; stale notes remain visible with status and reason, ranked after fresh notes. Tombstoned notes are omitted.",
		Annotations: mutatingAnnotations(),
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
		Description: `List all durable notes for the current repository scope, including stale and tombstoned notes with lifecycle metadata. Anchors are reconciled before return.`,
		Annotations: mutatingAnnotations(),
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			_ = ctx
			notes, err := store.List()
			if err != nil {
				return nil, err
			}
			return map[string]any{"notes": notes}, nil
		},
	}
}

func newMemoryMarkStaleTool(store *NoteStore) Tool {
	tool := memoryMarkStaleToolDefinition()
	tool.Handler = func(ctx context.Context, raw json.RawMessage) (any, error) {
		_ = ctx
		args, err := requireArgs(raw)
		if err != nil {
			return nil, err
		}
		key, _ := asString(args, "key")
		reason, _ := asString(args, "reason")
		failed, _ := args["failedAdjudication"].(bool)
		orphaned, _ := args["orphaned"].(bool)
		note, err := store.MarkStale(key, reason, failed, orphaned)
		if err != nil {
			return nil, err
		}
		return map[string]any{"note": note}, nil
	}
	return tool
}

func newMemoryVerifyTool(store *NoteStore) Tool {
	tool := memoryVerifyToolDefinition()
	tool.Handler = func(ctx context.Context, raw json.RawMessage) (any, error) {
		_ = ctx
		args, err := requireArgs(raw)
		if err != nil {
			return nil, err
		}
		key, _ := asString(args, "key")
		note, err := store.Verify(key)
		if err != nil {
			return nil, err
		}
		return map[string]any{"note": note}, nil
	}
	return tool
}

func newMemoryTombstoneTool(store *NoteStore) Tool {
	tool := memoryTombstoneToolDefinition()
	tool.Handler = func(ctx context.Context, raw json.RawMessage) (any, error) {
		_ = ctx
		args, err := requireArgs(raw)
		if err != nil {
			return nil, err
		}
		key, _ := asString(args, "key")
		reason, _ := asString(args, "reason")
		note, err := store.Tombstone(key, reason)
		if err != nil {
			return nil, err
		}
		return map[string]any{"note": note}, nil
	}
	return tool
}

func newMemoryGCTool(store *NoteStore) Tool {
	tool := memoryGCToolDefinition()
	tool.Handler = func(ctx context.Context, raw json.RawMessage) (any, error) {
		_ = ctx
		args, err := requireArgs(raw)
		if err != nil {
			return nil, err
		}
		result, err := store.GarbageCollect(memoryGCRules{
			OlderThanDays:          parsePositiveInt(args["olderThanDays"], defaultMemoryGCAgeDays),
			MinFailedAdjudications: parsePositiveInt(args["minFailedAdjudications"], defaultMemoryGCFailedAdjudications),
			MaximumRetrievalCount:  parsePositiveInt(args["maximumRetrievalCount"], defaultMemoryGCMaxRetrievals),
		})
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	return tool
}

func parseMemoryAnchors(value any) ([]NoteAnchor, error) {
	if value == nil {
		return nil, nil
	}
	values, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("anchors must be an array")
	}
	anchors := make([]NoteAnchor, 0, len(values))
	for index, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("anchor %d must be an object", index)
		}
		path, _ := asString(item, "path")
		symbol, _ := asString(item, "symbol")
		commitSHA, _ := asString(item, "commitSha")
		contentHash, _ := asString(item, "contentHash")
		branch, _ := asString(item, "branch")
		startLine, endLine := 0, 0
		if value, ok := asFloat(item, "startLine"); ok {
			startLine = int(value)
		}
		if value, ok := asFloat(item, "endLine"); ok {
			endLine = int(value)
		}
		anchors = append(anchors, NoteAnchor{Path: path, Symbol: symbol, CommitSHA: commitSHA, ContentHash: contentHash, Branch: branch, StartLine: startLine, EndLine: endLine})
	}
	return anchors, nil
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
