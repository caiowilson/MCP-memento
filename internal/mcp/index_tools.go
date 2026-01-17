package mcp

import (
	"context"
	"encoding/json"

	"memento-mcp/internal/indexing"
)

func newRepoIndexStatusTool(idx *indexing.Indexer) Tool {
	return Tool{
		Name:        "repo.index_status",
		Description: "Return the current automatic indexer status.",
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
		Name:        "repo.reindex",
		Description: "Trigger a full re-index of the workspace (automatic memory).",
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
