package mcp

import (
	"context"
	"encoding/json"

	"memento-mcp/internal/indexing"
)

func newRepoIndexStatusTool(idx *indexing.Indexer) Tool {
	return Tool{
		Name:        "repo_index_status",
		Title:       "Get Index Status",
		Description: "Return the current automatic indexer status.",
		Annotations: readOnlyAnnotations(),
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			_ = ctx
			_ = raw
			return idx.Status(), nil
		},
	}
}

func newRepoReindexTool(idx *indexing.Indexer) Tool {
	return Tool{
		Name:        "repo_reindex",
		Title:       "Reindex Repository",
		Description: "Trigger a full re-index of the workspace (automatic memory).",
		Annotations: mutatingAnnotations(),
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			_ = raw
			if err := idx.IndexAll(ctx); err != nil {
				return nil, err
			}
			return idx.Status(), nil
		},
	}
}

func newRepoClearIndexTool(idx *indexing.Indexer) Tool {
	return Tool{
		Name:        "repo_clear_index",
		Title:       "Clear Index",
		Description: "Remove all indexed chunks and reset the index manifest.",
		Annotations: destructiveAnnotations(),
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			_ = ctx
			_ = raw
			if err := idx.Clear(); err != nil {
				return nil, err
			}
			return idx.Status(), nil
		},
	}
}

func newRepoIndexDebugTool(idx *indexing.Indexer) Tool {
	return Tool{
		Name:        "repo_index_debug",
		Title:       "Get Index Debug Info",
		Description: "Return index debug information (paths count, filters, last error).",
		Annotations: readOnlyAnnotations(),
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			_ = ctx
			_ = raw
			return idx.DebugInfo(), nil
		},
	}
}
