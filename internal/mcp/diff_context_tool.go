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

	"memento-mcp/internal/gitstate"
	"memento-mcp/internal/indexing"
	"memento-mcp/internal/redact"
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

type diffContextPathSelection struct {
	PathSource         string
	Paths              []string
	Changes            []gitstate.WorktreeChange
	DeletedPaths       []string
	DetectedPaths      int
	FilteredPaths      int
	PathLimitOmissions int
	SkippedPaths       []diffContextSkippedPath
}

func newRepoDiffContextTool(root string, idx *indexing.Indexer, redactors ...*redact.Redactor) Tool {
	redactor := toolRedactor(redactors)
	tool := repoDiffContextToolDefinition()
	tool.Handler = func(ctx context.Context, raw json.RawMessage) (any, error) {
		if ctx == nil {
			ctx = context.Background()
		}
		if idx == nil {
			return nil, fmt.Errorf("repository index is unavailable")
		}
		args, err := requireArgs(raw)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
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
		maxDiffBytes := defaultRepoDiffContextDiffBytes
		if value, ok := asFloat(args, "maxDiffBytes"); ok && int(value) > 0 {
			maxDiffBytes = min(int(value), maximumRepoDiffContextDiffBytes)
		}
		diffContextLines := defaultRepoDiffContextDiffLines
		if value, ok := asFloat(args, "diffContextLines"); ok && int(value) >= 0 {
			diffContextLines = min(int(value), maximumRepoDiffContextDiffLines)
		}
		focus, _ := asString(args, "focus")
		focusLower := strings.ToLower(strings.TrimSpace(focus))

		if err := idx.RefreshIgnoreRules(ctx); err != nil {
			return nil, fmt.Errorf("reload ignore rules: %w", err)
		}
		if _, err := idx.PurgeDisallowedPaths(); err != nil {
			return nil, fmt.Errorf("purge chunks excluded by current rules: %w", err)
		}

		selection, err := resolveDiffContextPaths(ctx, root, idx, args)
		if err != nil {
			return nil, err
		}
		if err := idx.EnsureIndexed(ctx, selection.Paths); err != nil {
			return nil, fmt.Errorf("index requested paths: %w", err)
		}
		diffSummary, err := buildDiffContextDiffSummary(ctx, root, selection.Changes, maxDiffBytes, diffContextLines, redactor)
		if err != nil {
			return nil, fmt.Errorf("build unified diff summary: %w", err)
		}

		budget := newContextBudget(maxTokens, maxTotalBytes)
		files := make([]diffContextFile, 0, len(selection.Paths))
		skipped := make([]diffContextSkippedPath, len(selection.SkippedPaths))
		copy(skipped, selection.SkippedPaths)
		omittedPaths := make([]diffContextSkippedPath, 0)
		totalChunks := 0
		includedChunks := 0
		indexedPaths := 0
		for _, rel := range selection.Paths {
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
		requestedPaths := len(selection.Paths)
		if selection.PathSource == "git_status" {
			requestedPaths = 0
		}
		summary := map[string]any{
			"requestedPaths":     requestedPaths,
			"detectedPaths":      selection.DetectedPaths,
			"selectedChanges":    len(selection.Changes),
			"deletedPaths":       len(selection.DeletedPaths),
			"filteredPaths":      selection.FilteredPaths,
			"pathLimitOmissions": selection.PathLimitOmissions,
			"indexedPaths":       indexedPaths,
			"includedPaths":      len(files),
			"skippedPaths":       len(skipped),
			"omittedPaths":       len(omittedPaths),
			"totalChunks":        totalChunks,
			"includedChunks":     includedChunks,
			"omittedChunks":      omittedChunks,
			"text": fmt.Sprintf(
				"Selected %d safe changes from %s; loaded %d chunk-eligible paths and returned %d paths with %d of %d indexed chunks; deleted %d paths, filtered %d paths, skipped %d paths, omitted %d indexed paths, and capped %d detected changes.",
				len(selection.Changes), selection.PathSource, len(selection.Paths), len(files), includedChunks, totalChunks, len(selection.DeletedPaths), selection.FilteredPaths, len(skipped), len(omittedPaths), selection.PathLimitOmissions,
			),
		}

		return map[string]any{
			"pathSource":   selection.PathSource,
			"paths":        selection.Paths,
			"changes":      selection.Changes,
			"deletedPaths": selection.DeletedPaths,
			"focus":        focus,
			"summary":      summary,
			"diffSummary":  diffSummary,
			"files":        files,
			"skippedPaths": skipped,
			"omittedPaths": omittedPaths,
			"limits":       budget.limits(len(selection.Paths), maxChunksPerFile),
		}, nil
	}
	return tool
}

func resolveDiffContextPaths(ctx context.Context, root string, idx *indexing.Indexer, args map[string]any) (diffContextPathSelection, error) {
	if err := ctx.Err(); err != nil {
		return diffContextPathSelection{}, err
	}
	rawPaths, pathsProvided := args["paths"]
	if pathsProvided {
		requestedPaths, ok := asStringSlice(args, "paths")
		if !ok || rawPaths == nil || len(requestedPaths) == 0 {
			return diffContextPathSelection{}, fmt.Errorf("paths must be a non-empty array when provided")
		}
		if len(requestedPaths) > defaultRepoDiffContextMaxPaths {
			return diffContextPathSelection{}, fmt.Errorf("paths exceeds maximum of %d", defaultRepoDiffContextMaxPaths)
		}
		paths, rejectedPaths, validationErr := validateDiffContextPaths(root, requestedPaths)
		if len(rejectedPaths) > 0 {
			if err := idx.RemovePaths(rejectedPaths); err != nil {
				return diffContextPathSelection{}, fmt.Errorf("remove stale chunks for rejected paths %s: %w", strings.Join(rejectedPaths, ", "), err)
			}
		}
		ignored, err := gitstate.LoadIgnoredPathsContext(ctx, root)
		if err != nil {
			return diffContextPathSelection{}, fmt.Errorf("inspect Git ignore rules: %w", err)
		}
		ignoredPaths := make([]string, 0)
		for _, rel := range paths {
			if ignored.Matches(rel) {
				ignoredPaths = append(ignoredPaths, rel)
			}
		}
		if len(ignoredPaths) > 0 {
			if err := idx.RemovePaths(ignoredPaths); err != nil {
				return diffContextPathSelection{}, fmt.Errorf("remove stale chunks for Git-ignored paths %s: %w", strings.Join(ignoredPaths, ", "), err)
			}
		}
		if validationErr != nil {
			return diffContextPathSelection{}, validationErr
		}
		if len(ignoredPaths) > 0 {
			return diffContextPathSelection{}, fmt.Errorf("paths are ignored by Git: %s", strings.Join(ignoredPaths, ", "))
		}

		selection := diffContextPathSelection{
			PathSource:   "explicit",
			Paths:        paths,
			Changes:      make([]gitstate.WorktreeChange, 0),
			DeletedPaths: make([]string, 0),
			SkippedPaths: make([]diffContextSkippedPath, 0),
		}
		gitAvailable, err := diffSummaryGitAvailable(ctx, root)
		if err != nil {
			return diffContextPathSelection{}, fmt.Errorf("inspect Git worktree: %w", err)
		}
		if gitAvailable {
			changes, err := gitstate.LoadWorktreeChanges(ctx, root)
			if err != nil {
				return diffContextPathSelection{}, fmt.Errorf("inspect Git worktree: %w", err)
			}
			selection.Changes, selection.FilteredPaths = explicitDiffContextChanges(idx, changes, paths)
			renameSources := make([]string, 0)
			for _, change := range selection.Changes {
				if change.Renamed && change.PreviousPath != "" {
					renameSources = append(renameSources, change.PreviousPath)
				}
			}
			if len(renameSources) > 0 {
				if err := idx.RemovePaths(renameSources); err != nil {
					return diffContextPathSelection{}, fmt.Errorf("remove stale chunks for rename sources: %w", err)
				}
			}
		}
		return selection, nil
	}

	gitAvailable, err := diffSummaryGitAvailable(ctx, root)
	if err != nil {
		return diffContextPathSelection{}, fmt.Errorf("auto-detect changed paths: %w", err)
	}
	if !gitAvailable {
		return diffContextPathSelection{}, fmt.Errorf("cannot auto-detect changed paths: workspace is not a Git worktree")
	}
	changes, err := gitstate.LoadWorktreeChanges(ctx, root)
	if err != nil {
		return diffContextPathSelection{}, fmt.Errorf("auto-detect changed paths: %w", err)
	}
	selection := diffContextPathSelection{
		PathSource:    "git_status",
		Paths:         make([]string, 0),
		Changes:       make([]gitstate.WorktreeChange, 0),
		DeletedPaths:  make([]string, 0),
		DetectedPaths: len(changes),
		SkippedPaths:  make([]diffContextSkippedPath, 0),
	}
	if len(changes) == 0 {
		return selection, nil
	}

	candidates := make([]gitstate.WorktreeChange, 0, len(changes))
	stalePaths := make([]string, 0)
	for _, change := range changes {
		if change.Renamed && change.PreviousPath != "" {
			stalePaths = append(stalePaths, change.PreviousPath)
		}
		if !idx.PathSafeForSummary(change.Path) || change.PreviousPath != "" && !idx.PathSafeForSummary(change.PreviousPath) {
			selection.FilteredPaths++
			stalePaths = append(stalePaths, change.Path)
			continue
		}

		err := validateResolvedDiffContextPath(root, change.Path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			selection.SkippedPaths = append(selection.SkippedPaths, diffContextSkippedPath{Path: change.Path, Reason: autoDiffContextSkipReason(err)})
			stalePaths = append(stalePaths, change.Path)
			continue
		}
		if errors.Is(err, os.ErrNotExist) && !change.Deleted {
			selection.SkippedPaths = append(selection.SkippedPaths, diffContextSkippedPath{Path: change.Path, Reason: "not_found"})
			stalePaths = append(stalePaths, change.Path)
			continue
		}
		if errors.Is(err, os.ErrNotExist) {
			stalePaths = append(stalePaths, change.Path)
		}
		candidates = append(candidates, change)
	}
	if len(stalePaths) > 0 {
		if err := idx.RemovePaths(stalePaths); err != nil {
			return diffContextPathSelection{}, fmt.Errorf("remove stale chunks for changed paths: %w", err)
		}
	}

	selectedCount := min(len(candidates), defaultRepoDiffContextMaxPaths)
	selection.Changes = append(selection.Changes, candidates[:selectedCount]...)
	selection.PathLimitOmissions = len(candidates) - selectedCount
	seenPaths := make(map[string]struct{}, selectedCount)
	seenDeleted := make(map[string]struct{}, selectedCount)
	for _, change := range selection.Changes {
		if err := validateResolvedDiffContextPath(root, change.Path); errors.Is(err, os.ErrNotExist) {
			if _, exists := seenDeleted[change.Path]; !exists {
				seenDeleted[change.Path] = struct{}{}
				selection.DeletedPaths = append(selection.DeletedPaths, change.Path)
			}
			continue
		} else if err != nil {
			selection.SkippedPaths = append(selection.SkippedPaths, diffContextSkippedPath{Path: change.Path, Reason: autoDiffContextSkipReason(err)})
			continue
		}
		if !idx.PathAllowed(change.Path) {
			continue
		}
		if _, exists := seenPaths[change.Path]; exists {
			continue
		}
		seenPaths[change.Path] = struct{}{}
		selection.Paths = append(selection.Paths, change.Path)
	}
	return selection, nil
}

func explicitDiffContextChanges(idx *indexing.Indexer, changes []gitstate.WorktreeChange, paths []string) ([]gitstate.WorktreeChange, int) {
	requested := make(map[string]struct{}, len(paths))
	for _, rel := range paths {
		requested[rel] = struct{}{}
	}
	out := make([]gitstate.WorktreeChange, 0)
	filtered := 0
	for _, change := range changes {
		_, currentMatch := requested[change.Path]
		_, previousMatch := requested[change.PreviousPath]
		if !currentMatch && !previousMatch {
			continue
		}
		if !idx.PathSafeForSummary(change.Path) || change.PreviousPath != "" && !idx.PathSafeForSummary(change.PreviousPath) {
			filtered++
			continue
		}
		out = append(out, change)
	}
	return out, filtered
}

func validateResolvedDiffContextPath(root, rel string) error {
	abs, err := safeJoin(root, rel)
	if err != nil {
		return err
	}
	if err := rejectSymlinkedPath(root, rel); err != nil {
		return err
	}
	inside, err := resolvedPathWithinRoot(root, abs)
	if err != nil {
		return err
	}
	if !inside {
		return fmt.Errorf("path resolves outside workspace: %s", rel)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, expected file: %s", rel)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular file: %s", rel)
	}
	return nil
}

func autoDiffContextSkipReason(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "not_found"
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "symbolic link") || strings.Contains(message, "outside workspace") {
		return "unsafe_path"
	}
	return "not_regular"
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
		if err := validateResolvedDiffContextPath(root, rel); err != nil {
			reject(rel, err)
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
	raw := strings.ReplaceAll(value, "\\", "/")
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
		Description: "Return compact exact-file chunks plus a bounded, redacted unified diff summary. Omit paths to auto-detect staged, unstaged, and untracked Git changes, or provide ordered repo-relative paths to constrain the result. Deleted files are summarized but never chunk-loaded; related files are never added.",
		Annotations: readOnlyAnnotations(),
		Meta:        largeResultToolMeta(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"paths": map[string]any{
					"type":        "array",
					"minItems":    1,
					"maxItems":    defaultRepoDiffContextMaxPaths,
					"items":       map[string]any{"type": "string"},
					"description": "Optional ordered repo-relative changed-file paths. Duplicates are removed after normalization. When omitted, the tool auto-detects the dirty Git worktree.",
				},
				"focus":            map[string]any{"type": "string", "description": "Optional text used to prioritize chunks within each requested file."},
				"maxChunksPerFile": map[string]any{"type": "integer", "minimum": 1, "description": "Maximum chunks returned per requested file (default 3)."},
				"maxTokens":        map[string]any{"type": "integer", "minimum": 1, "description": "Approximate content-token budget (default 4000)."},
				"maxTotalBytes":    map[string]any{"type": "integer", "minimum": 1, "description": "Hard content byte budget (default 16000)."},
				"maxDiffBytes":     map[string]any{"type": "integer", "minimum": 1, "maximum": maximumRepoDiffContextDiffBytes, "description": "Hard byte budget for redacted unified diff sections (default 12000, maximum 64000)."},
				"diffContextLines": map[string]any{"type": "integer", "minimum": 0, "maximum": maximumRepoDiffContextDiffLines, "description": "Unified diff context lines (default 3, maximum 10)."},
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
		"requestedPaths":     map[string]any{"type": "integer"},
		"detectedPaths":      map[string]any{"type": "integer"},
		"selectedChanges":    map[string]any{"type": "integer"},
		"deletedPaths":       map[string]any{"type": "integer"},
		"filteredPaths":      map[string]any{"type": "integer"},
		"pathLimitOmissions": map[string]any{"type": "integer"},
		"indexedPaths":       map[string]any{"type": "integer"},
		"includedPaths":      map[string]any{"type": "integer"},
		"skippedPaths":       map[string]any{"type": "integer"},
		"omittedPaths":       map[string]any{"type": "integer"},
		"totalChunks":        map[string]any{"type": "integer"},
		"includedChunks":     map[string]any{"type": "integer"},
		"omittedChunks":      map[string]any{"type": "integer"},
		"text":               map[string]any{"type": "string"},
	}
	changeProperties := map[string]any{
		"path":           map[string]any{"type": "string"},
		"previousPath":   map[string]any{"type": "string"},
		"indexStatus":    map[string]any{"type": "string"},
		"worktreeStatus": map[string]any{"type": "string"},
		"kind":           map[string]any{"type": "string"},
		"staged":         map[string]any{"type": "boolean"},
		"unstaged":       map[string]any{"type": "boolean"},
		"untracked":      map[string]any{"type": "boolean"},
		"deleted":        map[string]any{"type": "boolean"},
		"renamed":        map[string]any{"type": "boolean"},
		"copied":         map[string]any{"type": "boolean"},
	}
	diffSectionProperties := map[string]any{
		"scope":     map[string]any{"type": "string", "enum": []any{"staged", "unstaged", "untracked"}},
		"text":      map[string]any{"type": "string"},
		"usedBytes": map[string]any{"type": "integer"},
		"truncated": map[string]any{"type": "boolean"},
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pathSource": map[string]any{"type": "string", "enum": []any{"explicit", "git_status"}},
			"paths":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"changes": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":       "object",
					"properties": changeProperties,
					"required":   []any{"path", "indexStatus", "worktreeStatus", "kind", "staged", "unstaged", "untracked", "deleted", "renamed", "copied"},
				},
			},
			"deletedPaths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"focus":        map[string]any{"type": "string"},
			"summary": map[string]any{
				"type":       "object",
				"properties": countProperties,
				"required":   []any{"requestedPaths", "detectedPaths", "selectedChanges", "deletedPaths", "filteredPaths", "pathLimitOmissions", "indexedPaths", "includedPaths", "skippedPaths", "omittedPaths", "totalChunks", "includedChunks", "omittedChunks", "text"},
			},
			"diffSummary": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"available":    map[string]any{"type": "boolean"},
					"format":       map[string]any{"type": "string"},
					"contextLines": map[string]any{"type": "integer"},
					"maxBytes":     map[string]any{"type": "integer"},
					"usedBytes":    map[string]any{"type": "integer"},
					"truncated":    map[string]any{"type": "boolean"},
					"sections": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":       "object",
							"properties": diffSectionProperties,
							"required":   []any{"scope", "text", "usedBytes", "truncated"},
						},
					},
				},
				"required": []any{"available", "format", "contextLines", "maxBytes", "usedBytes", "truncated", "sections"},
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
		"required": []any{"pathSource", "paths", "changes", "deletedPaths", "focus", "summary", "diffSummary", "files", "skippedPaths", "omittedPaths", "limits"},
	}
}
