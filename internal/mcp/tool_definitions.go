package mcp

func leafToolDefinitionsFor(root string, feedbackEnabled bool) []Tool {
	tools := []Tool{
		cloneToolDefinition(newRepoListFilesTool(root)),
		cloneToolDefinition(newRepoReadFileTool(root)),
		cloneToolDefinition(newRepoSearchTool(root)),
		cloneToolDefinition(newRepoRelatedFilesTool(root)),
		repoOutlineToolDefinition(),
		repoContextToolDefinition(),
		repoIndexStatusToolDefinition(),
		repoReindexToolDefinition(),
		repoClearIndexToolDefinition(),
		repoIndexDebugToolDefinition(),
		memoryUpsertToolDefinition(),
		memorySearchToolDefinition(),
		memoryListToolDefinition(),
		memoryMarkStaleToolDefinition(),
		memoryVerifyToolDefinition(),
		memoryTombstoneToolDefinition(),
		memoryGCToolDefinition(),
		memoryDeleteToolDefinition(),
		memoryClearToolDefinition(),
	}
	if feedbackEnabled {
		tools = append(tools, feedbackSubmitToolDefinition())
	}
	return tools
}

func repoContextToolDefinition() Tool {
	return Tool{
		Name:        "repo_context",
		Title:       "Get Repository Context",
		Description: "Return context for a file plus related files. Prefer `intent` for higher-level LLM workflows: `navigate` resolves to `outline`, while `implement` and `review` resolve to `auto`. Use explicit `mode` only when you need to force a low-level behavior.",
		Annotations: readOnlyAnnotations(),
		Meta:        largeResultToolMeta(),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"path"},
			"properties": map[string]any{
				"path":              map[string]any{"type": "string", "description": "Repo-relative path of the active file."},
				"focus":             map[string]any{"type": "string", "description": "Optional query used to prioritize chunks (e.g. function/type name)."},
				"maxFiles":          map[string]any{"type": "integer", "description": "Maximum number of files to include (default 10)."},
				"maxChunksPerFile":  map[string]any{"type": "integer", "description": "Maximum chunks per file (default 2)."},
				"maxTokens":         map[string]any{"type": "integer", "description": "Approximate content-token budget (default 7000, configurable with MEMENTO_CONTEXT_MAX_TOKENS)."},
				"maxTotalBytes":     map[string]any{"type": "integer", "description": "Maximum total bytes across all returned chunks (default 32000)."},
				"excludePaths":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Repo-relative paths to exclude from results. Use this to skip files already in your context from prior calls, avoiding duplicate content."},
				"intent":            map[string]any{"type": "string", "description": "Optional high-level task intent. `navigate` returns a lighter outline view; `implement` and `review` return `auto`. Ignored when explicit `mode` is provided.", "enum": []any{"navigate", "implement", "review"}},
				"mode":              map[string]any{"type": "string", "description": "Optional low-level output override. `auto` returns full source chunks for the target file and outlines for related files; `full` returns raw source chunks for all files; `outline` returns declaration signatures + doc comments; `summary` returns a compact one-line-per-symbol list with line numbers.", "enum": []any{"full", "auto", "outline", "summary"}},
				"includeSameDir":    map[string]any{"type": "boolean", "description": "Include same-directory files (default true)."},
				"includeImports":    map[string]any{"type": "boolean", "description": "Include imported files (default true)."},
				"includeImporters":  map[string]any{"type": "boolean", "description": "Include importing files (default true)."},
				"includeReferences": map[string]any{"type": "boolean", "description": "Include semantic references where supported (default true)."},
			},
		},
		OutputSchema: repoContextOutputSchema(),
	}
}

func repoIndexStatusToolDefinition() Tool {
	return Tool{
		Name:        "repo_index_status",
		Title:       "Get Index Status",
		Description: "Return the current automatic indexer status.",
		Annotations: readOnlyAnnotations(),
		InputSchema: map[string]any{"type": "object"},
	}
}

func repoReindexToolDefinition() Tool {
	return Tool{
		Name:        "repo_reindex",
		Title:       "Reindex Repository",
		Description: "Trigger a full re-index of the workspace (automatic memory).",
		Annotations: mutatingAnnotations(),
		InputSchema: map[string]any{"type": "object"},
	}
}

func repoClearIndexToolDefinition() Tool {
	return Tool{
		Name:        "repo_clear_index",
		Title:       "Clear Index",
		Description: "Remove all indexed chunks and reset the index manifest.",
		Annotations: destructiveAnnotations(),
		InputSchema: map[string]any{"type": "object"},
	}
}

func repoIndexDebugToolDefinition() Tool {
	return Tool{
		Name:        "repo_index_debug",
		Title:       "Get Index Debug Info",
		Description: "Return index debug information (paths count, filters, last error).",
		Annotations: readOnlyAnnotations(),
		InputSchema: map[string]any{"type": "object"},
	}
}

func memoryUpsertToolDefinition() Tool {
	return Tool{
		Name:        "memory_upsert",
		Title:       "Save Memory Note",
		Description: "Upsert a repo-scoped note (explicit memory) keyed by `key`.",
		Annotations: mutatingAnnotations(),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"key", "text"},
			"properties": map[string]any{
				"key":     map[string]any{"type": "string", "description": "Stable identifier for the note (e.g. \"repo-overview\" or \"internal/mcp/server.go\")."},
				"text":    map[string]any{"type": "string", "description": "Note content to store."},
				"tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional tags for filtering."},
				"path":    map[string]any{"type": "string", "description": "Optional repo-relative path this note refers to."},
				"meta":    map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Optional metadata map."},
				"anchors": map[string]any{"type": "array", "items": memoryAnchorSchema(), "description": "Optional code anchors. Current hashes, Git commit, branch, and symbol lines are captured on save."},
			},
		},
	}
}

func memorySearchToolDefinition() Tool {
	return Tool{
		Name:        "memory_search",
		Title:       "Search Memory Notes",
		Description: "Search active repo-scoped notes by substring and/or tags. Reconcile anchors first and return stale notes after fresh notes; omit tombstones.",
		Annotations: mutatingAnnotations(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Substring query (optional)."},
				"tags":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Require all tags (optional)."},
				"limit": map[string]any{"type": "integer", "description": "Default 20."},
			},
		},
	}
}

func memoryListToolDefinition() Tool {
	return Tool{
		Name:        "memory_list",
		Title:       "List Memory Notes",
		Description: `List all durable notes, including stale and tombstoned notes with lifecycle metadata. Reconcile anchors before return.`,
		Annotations: mutatingAnnotations(),
		InputSchema: map[string]any{"type": "object"},
	}
}

func memoryAnchorSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string", "description": "Repo-relative anchored file path."},
			"symbol":      map[string]any{"type": "string", "description": "Optional symbol name or Container.Symbol name."},
			"commitSha":   map[string]any{"type": "string", "description": "Optional Git commit; refreshed on upsert or verify."},
			"contentHash": map[string]any{"type": "string", "description": "Optional content hash; refreshed on upsert or verify."},
			"branch":      map[string]any{"type": "string", "description": "Optional branch identity; refreshed on upsert or verify."},
			"startLine":   map[string]any{"type": "integer", "minimum": 1},
			"endLine":     map[string]any{"type": "integer", "minimum": 1},
		},
		"anyOf": []any{
			map[string]any{"required": []any{"path"}},
			map[string]any{"required": []any{"commitSha"}},
		},
	}
}

func memoryMarkStaleToolDefinition() Tool {
	return Tool{
		Name:        "memory_mark_stale",
		Title:       "Mark Memory Note Stale",
		Description: "Confirm that a note contradicts current reality. Stale notes remain searchable and are downranked. Set failedAdjudication only after an attempted reconciliation could not salvage the note.",
		Annotations: mutatingAnnotations(),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"key", "reason"},
			"properties": map[string]any{
				"key":                map[string]any{"type": "string"},
				"reason":             map[string]any{"type": "string"},
				"failedAdjudication": map[string]any{"type": "boolean", "description": "Increment the failed-reconciliation count (default false)."},
				"orphaned":           map[string]any{"type": "boolean", "description": "Confirm that the note's referent no longer exists (default false)."},
			},
		},
	}
}

func memoryVerifyToolDefinition() Tool {
	return Tool{
		Name:        "memory_verify",
		Title:       "Verify Memory Note",
		Description: "Confirm a note against current code, refresh its anchors, and reset its lifecycle to fresh. Fails if an anchor cannot be resolved.",
		Annotations: mutatingAnnotations(),
		InputSchema: map[string]any{"type": "object", "required": []any{"key"}, "properties": map[string]any{"key": map[string]any{"type": "string"}}},
	}
}

func memoryTombstoneToolDefinition() Tool {
	return Tool{
		Name:        "memory_tombstone",
		Title:       "Tombstone Memory Note",
		Description: "Soft-evict a confirmed obsolete note from active search while retaining it for inspection and recovery through memory_list or memory_verify.",
		Annotations: mutatingAnnotations(),
		InputSchema: map[string]any{"type": "object", "required": []any{"key", "reason"}, "properties": map[string]any{"key": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"}}},
	}
}

func memoryGCToolDefinition() Tool {
	return Tool{
		Name:        "memory_gc",
		Title:       "Garbage Collect Memory Notes",
		Description: "Permanently delete only notes that are tombstoned, orphaned, aged out, repeatedly unsalvageable, and below the retrieval threshold. Explicit memory_delete remains the direct override.",
		Annotations: destructiveAnnotations(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"olderThanDays":          map[string]any{"type": "integer", "minimum": minimumMemoryGCAgeDays, "description": "Tombstone and last-use age threshold (default 90 days; minimum 30)."},
				"minFailedAdjudications": map[string]any{"type": "integer", "minimum": 1, "description": "Required failed reconciliations (default 2)."},
				"maximumRetrievalCount":  map[string]any{"type": "integer", "minimum": 1, "maximum": defaultMemoryGCMaxRetrievals, "description": "Notes retrieved this many times or more survive (default and maximum 3)."},
			},
		},
	}
}

func memoryDeleteToolDefinition() Tool {
	return Tool{
		Name:        "memory_delete",
		Title:       "Delete Memory Note",
		Description: `Delete a single durable note by its key. Use memory_list first to find the exact key if unsure. Returns {"deleted": true, "key": "..."} on success; errors if the key does not exist.`,
		Annotations: destructiveAnnotations(),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"key"},
			"properties": map[string]any{
				"key": map[string]any{"type": "string", "description": "Exact key of the note to delete."},
			},
		},
	}
}

func memoryClearToolDefinition() Tool {
	return Tool{
		Name:        "memory_clear",
		Title:       "Clear Memory Notes",
		Description: "Clear all repo-scoped notes.",
		Annotations: destructiveAnnotations(),
		InputSchema: map[string]any{"type": "object"},
	}
}
