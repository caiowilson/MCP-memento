package mcp

func leafToolDefinitionsFor(root string) []Tool {
	return []Tool{
		cloneToolDefinition(newRepoListFilesTool(root)),
		cloneToolDefinition(newRepoReadFileTool(root)),
		cloneToolDefinition(newRepoSearchTool(root)),
		cloneToolDefinition(newRepoRelatedFilesTool(root)),
		repoContextToolDefinition(),
		repoIndexStatusToolDefinition(),
		repoReindexToolDefinition(),
		repoClearIndexToolDefinition(),
		repoIndexDebugToolDefinition(),
		memoryUpsertToolDefinition(),
		memorySearchToolDefinition(),
		memoryListToolDefinition(),
		memoryDeleteToolDefinition(),
		memoryClearToolDefinition(),
	}
}

func repoContextToolDefinition() Tool {
	return Tool{
		Name:        "repo_context",
		Title:       "Get Repository Context",
		Description: "Return context for a file plus related files. Prefer `intent` for higher-level LLM workflows: `navigate` resolves to `outline`, while `implement` and `review` resolve to `auto`. Use explicit `mode` only when you need to force a low-level behavior.",
		Annotations: readOnlyAnnotations(),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"path"},
			"properties": map[string]any{
				"path":              map[string]any{"type": "string", "description": "Repo-relative path of the active file."},
				"focus":             map[string]any{"type": "string", "description": "Optional query used to prioritize chunks (e.g. function/type name)."},
				"maxFiles":          map[string]any{"type": "integer", "description": "Maximum number of files to include (default 10)."},
				"maxChunksPerFile":  map[string]any{"type": "integer", "description": "Maximum chunks per file (default 2)."},
				"maxTotalBytes":     map[string]any{"type": "integer", "description": "Maximum total bytes across all returned chunks (default 120000)."},
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
				"key":  map[string]any{"type": "string", "description": "Stable identifier for the note (e.g. \"repo-overview\" or \"internal/mcp/server.go\")."},
				"text": map[string]any{"type": "string", "description": "Note content to store."},
				"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional tags for filtering."},
				"path": map[string]any{"type": "string", "description": "Optional repo-relative path this note refers to."},
				"meta": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Optional metadata map."},
			},
		},
	}
}

func memorySearchToolDefinition() Tool {
	return Tool{
		Name:        "memory_search",
		Title:       "Search Memory Notes",
		Description: "Search repo-scoped notes (explicit memory) by substring and/or tags.",
		Annotations: readOnlyAnnotations(),
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
		Description: `List all durable notes stored for the current repository scope. Returns every note with its key, text, tags, path, updatedAt, and meta. Use to enumerate saved context or to find a note's key before calling memory_delete.`,
		Annotations: readOnlyAnnotations(),
		InputSchema: map[string]any{"type": "object"},
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
