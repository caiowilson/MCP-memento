package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"memento-mcp/internal/indexing"
)

type diffContextFile struct {
	Path           string           `json:"path"`
	TotalChunks    int              `json:"totalChunks"`
	IncludedChunks int              `json:"includedChunks"`
	Chunks         []indexing.Chunk `json:"chunks"`
}

type diffContextSkippedPath struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func newRepoDiffContextTool(root string, idx *indexing.Indexer) Tool {
	tool := repoDiffContextToolDefinition()
	tool.Handler = func(ctx context.Context, raw json.RawMessage) (any, error) {
		if idx == nil {
			return nil, fmt.Errorf("repository index is unavailable")
		}
		args, err := requireArgs(raw)
		if err != nil {
			return nil, err
		}
		requestedPaths, ok := asStringSlice(args, "paths")
		if !ok || len(requestedPaths) == 0 {
			return nil, fmt.Errorf("missing required argument: paths")
		}
		if len(requestedPaths) > defaultRepoDiffContextMaxPaths {
			return nil, fmt.Errorf("paths exceeds maximum of %d", defaultRepoDiffContextMaxPaths)
		}
		paths, rejectedPaths, validationErr := validateDiffContextPaths(root, requestedPaths)
		if len(rejectedPaths) > 0 {
			if err := idx.RemovePaths(rejectedPaths); err != nil {
				return nil, fmt.Errorf("remove stale chunks for rejected paths %s: %w", strings.Join(rejectedPaths, ", "), err)
			}
		}
		if len(paths) > defaultRepoDiffContextMaxPaths {
			return nil, fmt.Errorf("paths exceeds maximum of %d", defaultRepoDiffContextMaxPaths)
		}

		maxChunksPerFile := defaultRepoDiffContextMaxChunks
		if value, ok := asFloat(args, "maxChunksPerFile"); ok && int(value) > 0 {
			maxChunksPerFile = int(value)
		}
		maxTokens := defaultRepoDiffContextMaxTokens
		if value, ok := asFloat(args, "maxTokens"); ok && int(value) > 0 {
			maxTokens = int(value)
		}
		maxTotalBytes := defaultRepoDiffContextMaxBytes
		if value, ok := asFloat(args, "maxTotalBytes"); ok && int(value) > 0 {
			maxTotalBytes = int(value)
		}
		focus, _ := asString(args, "focus")
		focusLower := strings.ToLower(strings.TrimSpace(focus))

		if err := idx.ReloadIgnoreRules(); err != nil {
			return nil, fmt.Errorf("reload ignore rules: %w", err)
		}
		ignored := loadGitIgnored(root)
		ignoredPaths := make([]string, 0)
		for _, rel := range paths {
			if ignored.Matches(rel) {
				ignoredPaths = append(ignoredPaths, rel)
			}
		}
		if len(ignoredPaths) > 0 {
			if err := idx.RemovePaths(ignoredPaths); err != nil {
				return nil, fmt.Errorf("remove stale chunks for Git-ignored paths %s: %w", strings.Join(ignoredPaths, ", "), err)
			}
		}
		if validationErr != nil {
			return nil, validationErr
		}
		if len(ignoredPaths) > 0 {
			return nil, fmt.Errorf("paths are ignored by Git: %s", strings.Join(ignoredPaths, ", "))
		}
		if err := idx.EnsureIndexed(ctx, paths); err != nil {
			return nil, fmt.Errorf("index requested paths: %w", err)
		}

		budget := newContextBudget(maxTokens, maxTotalBytes)
		files := make([]diffContextFile, 0, len(paths))
		skipped := make([]diffContextSkippedPath, 0)
		omittedPaths := make([]diffContextSkippedPath, 0)
		totalChunks := 0
		includedChunks := 0
		indexedPaths := 0
		for _, rel := range paths {
			chunks, err := idx.FileChunks(rel)
			if errors.Is(err, os.ErrNotExist) {
				skipped = append(skipped, diffContextSkippedPath{Path: rel, Reason: "not_indexed"})
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("read indexed chunks for %s: %w", rel, err)
			}
			indexedPaths++
			totalChunks += len(chunks)

			selected := selectChunks(chunks, focusLower, maxChunksPerFile)
			packed := make([]indexing.Chunk, 0, len(selected))
			for _, chunk := range selected {
				if budget.tryAdd(chunk.Content) {
					packed = append(packed, chunk)
				}
			}
			if len(packed) == 0 {
				reason := "budget"
				if len(selected) == 0 {
					reason = "no_chunks"
				}
				omittedPaths = append(omittedPaths, diffContextSkippedPath{Path: rel, Reason: reason})
				continue
			}
			sort.Slice(packed, func(left, right int) bool {
				return packed[left].StartLine < packed[right].StartLine
			})
			includedChunks += len(packed)
			files = append(files, diffContextFile{
				Path:           rel,
				TotalChunks:    len(chunks),
				IncludedChunks: len(packed),
				Chunks:         packed,
			})
		}

		omittedChunks := totalChunks - includedChunks
		summary := map[string]any{
			"requestedPaths": len(paths),
			"indexedPaths":   indexedPaths,
			"includedPaths":  len(files),
			"skippedPaths":   len(skipped),
			"omittedPaths":   len(omittedPaths),
			"totalChunks":    totalChunks,
			"includedChunks": includedChunks,
			"omittedChunks":  omittedChunks,
			"text": fmt.Sprintf(
				"Requested %d paths; returned %d paths and %d of %d indexed chunks; skipped %d paths and omitted %d indexed paths.",
				len(paths), len(files), includedChunks, totalChunks, len(skipped), len(omittedPaths),
			),
		}

		return map[string]any{
			"paths":        paths,
			"focus":        focus,
			"summary":      summary,
			"files":        files,
			"skippedPaths": skipped,
			"omittedPaths": omittedPaths,
			"limits":       budget.limits(len(paths), maxChunksPerFile),
		}, nil
	}
	return tool
}

func validateDiffContextPaths(root string, requested []string) ([]string, []string, error) {
	paths := make([]string, 0, len(requested))
	rejected := make([]string, 0)
	seen := make(map[string]struct{}, len(requested))
	var firstErr error
	reject := func(rel string, err error) {
		rejected = append(rejected, rel)
		if firstErr == nil {
			firstErr = err
		}
	}
	for _, requestedPath := range requested {
		rel, err := normalizeDiffContextPath(requestedPath)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if _, duplicate := seen[rel]; duplicate {
			continue
		}
		seen[rel] = struct{}{}
		abs, err := safeJoin(root, rel)
		if err != nil {
			reject(rel, err)
			continue
		}
		if err := rejectSymlinkedPath(root, rel); err != nil {
			reject(rel, err)
			continue
		}
		inside, err := resolvedPathWithinRoot(root, abs)
		if err != nil {
			reject(rel, err)
			continue
		}
		if !inside {
			reject(rel, fmt.Errorf("path resolves outside workspace: %s", rel))
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			reject(rel, err)
			continue
		}
		if info.IsDir() {
			reject(rel, fmt.Errorf("path is a directory, expected file: %s", rel))
			continue
		}
		if !info.Mode().IsRegular() {
			reject(rel, fmt.Errorf("path is not a regular file: %s", rel))
			continue
		}
		paths = append(paths, rel)
	}
	return paths, rejected, firstErr
}

func rejectSymlinkedPath(root, rel string) error {
	current := root
	for _, component := range strings.Split(rel, "/") {
		current = filepath.Join(current, filepath.FromSlash(component))
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path contains a symbolic link: %s", rel)
		}
	}
	return nil
}

func resolvedPathWithinRoot(root, target string) (bool, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil {
		return false, err
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel), nil
}

func normalizeDiffContextPath(value string) (string, error) {
	raw := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if raw == "" {
		return "", fmt.Errorf("paths must contain non-empty repo-relative paths")
	}
	if strings.HasPrefix(raw, "/") || filepath.IsAbs(filepath.FromSlash(raw)) || looksLikeWindowsAbsolutePath(raw) {
		return "", fmt.Errorf("path must be repo-relative: %s", value)
	}
	rel := path.Clean(raw)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("path resolves outside workspace: %s", value)
	}
	return rel, nil
}

func looksLikeWindowsAbsolutePath(value string) bool {
	return len(value) >= 3 && value[1] == ':' && value[2] == '/' &&
		(value[0] >= 'A' && value[0] <= 'Z' || value[0] >= 'a' && value[0] <= 'z')
}

func repoDiffContextToolDefinition() Tool {
	return Tool{
		Name:        "repo_diff_context",
		Title:       "Get Changed-File Context",
		Description: "Return compact indexed chunks for an explicit ordered list of changed repo-relative paths. Results never expand to related files. Automatic Git changed-file detection and unified diff summaries are reserved for a later workflow.",
		Annotations: readOnlyAnnotations(),
		Meta:        largeResultToolMeta(),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"paths"},
			"properties": map[string]any{
				"paths": map[string]any{
					"type":        "array",
					"minItems":    1,
					"maxItems":    defaultRepoDiffContextMaxPaths,
					"items":       map[string]any{"type": "string"},
					"description": "Ordered repo-relative changed-file paths. Duplicates are removed after normalization.",
				},
				"focus":            map[string]any{"type": "string", "description": "Optional text used to prioritize chunks within each requested file."},
				"maxChunksPerFile": map[string]any{"type": "integer", "minimum": 1, "description": "Maximum chunks returned per requested file (default 3)."},
				"maxTokens":        map[string]any{"type": "integer", "minimum": 1, "description": "Approximate content-token budget (default 4000)."},
				"maxTotalBytes":    map[string]any{"type": "integer", "minimum": 1, "description": "Hard content byte budget (default 16000)."},
			},
		},
		OutputSchema: repoDiffContextOutputSchema(),
	}
}

func repoDiffContextOutputSchema() map[string]any {
	chunk := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":      map[string]any{"type": "string"},
			"language":  map[string]any{"type": "string"},
			"startLine": map[string]any{"type": "integer"},
			"endLine":   map[string]any{"type": "integer"},
			"content":   map[string]any{"type": "string"},
		},
		"required": []any{"path", "language", "startLine", "endLine", "content"},
	}
	countProperties := map[string]any{
		"requestedPaths": map[string]any{"type": "integer"},
		"indexedPaths":   map[string]any{"type": "integer"},
		"includedPaths":  map[string]any{"type": "integer"},
		"skippedPaths":   map[string]any{"type": "integer"},
		"omittedPaths":   map[string]any{"type": "integer"},
		"totalChunks":    map[string]any{"type": "integer"},
		"includedChunks": map[string]any{"type": "integer"},
		"omittedChunks":  map[string]any{"type": "integer"},
		"text":           map[string]any{"type": "string"},
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"focus": map[string]any{"type": "string"},
			"summary": map[string]any{
				"type":       "object",
				"properties": countProperties,
				"required":   []any{"requestedPaths", "indexedPaths", "includedPaths", "skippedPaths", "omittedPaths", "totalChunks", "includedChunks", "omittedChunks", "text"},
			},
			"files": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":           map[string]any{"type": "string"},
						"totalChunks":    map[string]any{"type": "integer"},
						"includedChunks": map[string]any{"type": "integer"},
						"chunks":         map[string]any{"type": "array", "items": chunk},
					},
					"required": []any{"path", "totalChunks", "includedChunks", "chunks"},
				},
			},
			"skippedPaths": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":       "object",
					"properties": map[string]any{"path": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"}},
					"required":   []any{"path", "reason"},
				},
			},
			"omittedPaths": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":       "object",
					"properties": map[string]any{"path": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"}},
					"required":   []any{"path", "reason"},
				},
			},
			"limits": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"maxFiles":         map[string]any{"type": "integer"},
					"maxChunksPerFile": map[string]any{"type": "integer"},
					"maxTokens":        map[string]any{"type": "integer"},
					"usedTokens":       map[string]any{"type": "integer"},
					"tokenEstimator":   map[string]any{"type": "string"},
					"maxTotalBytes":    map[string]any{"type": "integer"},
					"usedBytes":        map[string]any{"type": "integer"},
					"clamped":          map[string]any{"type": "boolean"},
				},
				"required": []any{"maxFiles", "maxChunksPerFile", "maxTokens", "usedTokens", "tokenEstimator", "maxTotalBytes", "usedBytes", "clamped"},
			},
		},
		"required": []any{"paths", "focus", "summary", "files", "skippedPaths", "omittedPaths", "limits"},
	}
}
