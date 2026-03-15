package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"memento-mcp/internal/indexing"
)

func newRepoContextTool(root string, idx *indexing.Indexer) Tool {
	return Tool{
		Name:        "repo_context",
		Title:       "Get Repository Context",
		Description: "Return context for a file plus related files. Prefer `intent` for higher-level LLM workflows: `navigate` resolves to `outline`, while `implement` and `review` resolve to `auto`. Use explicit `mode` only when you need to force a low-level behavior.",
		Annotations: readOnlyAnnotations(),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"path"},
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Repo-relative path of the active file.",
				},
				"focus": map[string]any{
					"type":        "string",
					"description": "Optional query used to prioritize chunks (e.g. function/type name).",
				},
				"maxFiles": map[string]any{
					"type":        "integer",
					"description": "Maximum number of files to include (default 10).",
				},
				"maxChunksPerFile": map[string]any{
					"type":        "integer",
					"description": "Maximum chunks per file (default 2).",
				},
				"maxTotalBytes": map[string]any{
					"type":        "integer",
					"description": "Maximum total bytes across all returned chunks (default 120000).",
				},
				"excludePaths": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Repo-relative paths to exclude from results. Use this to skip files already in your context from prior calls, avoiding duplicate content.",
				},
				"intent": map[string]any{
					"type":        "string",
					"description": "Optional high-level task intent. `navigate` returns a lighter outline view; `implement` and `review` return `auto`. Ignored when explicit `mode` is provided.",
					"enum":        []any{"navigate", "implement", "review"},
				},
				"mode": map[string]any{
					"type":        "string",
					"description": "Optional low-level output override. `auto` returns full source chunks for the target file and outlines for related files; `full` returns raw source chunks for all files; `outline` returns declaration signatures + doc comments; `summary` returns a compact one-line-per-symbol list with line numbers.",
					"enum":        []any{"full", "auto", "outline", "summary"},
				},
				"includeSameDir": map[string]any{
					"type":        "boolean",
					"description": "Include same-directory files (default true).",
				},
				"includeImports": map[string]any{
					"type":        "boolean",
					"description": "Include imported files (default true).",
				},
				"includeImporters": map[string]any{
					"type":        "boolean",
					"description": "Include importing files (default true).",
				},
				"includeReferences": map[string]any{
					"type":        "boolean",
					"description": "Include semantic references where supported (default true).",
				},
			},
		},
		OutputSchema: repoContextOutputSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			args, err := requireArgs(raw)
			if err != nil {
				return nil, err
			}
			rel, ok := asString(args, "path")
			if !ok || strings.TrimSpace(rel) == "" {
				return nil, fmt.Errorf("missing required argument: path")
			}
			rel = filepath.ToSlash(filepath.Clean(rel))
			abs, err := safeJoin(root, rel)
			if err != nil {
				return nil, err
			}
			info, err := os.Stat(abs)
			if err != nil {
				return nil, err
			}
			if info.IsDir() {
				return nil, fmt.Errorf("path is a directory, expected file: %s", rel)
			}

			focus, _ := asString(args, "focus")
			focusLower := strings.ToLower(strings.TrimSpace(focus))

			maxFiles := 10
			if f, ok := asFloat(args, "maxFiles"); ok && int(f) > 0 {
				maxFiles = int(f)
			}
			maxChunksPerFile := 2
			if f, ok := asFloat(args, "maxChunksPerFile"); ok && int(f) > 0 {
				maxChunksPerFile = int(f)
			}
			maxTotalBytes := 120_000
			if f, ok := asFloat(args, "maxTotalBytes"); ok && int(f) > 0 {
				maxTotalBytes = int(f)
			}

			includeSameDir := true
			if v, ok := args["includeSameDir"].(bool); ok {
				includeSameDir = v
			}
			includeImports := true
			if v, ok := args["includeImports"].(bool); ok {
				includeImports = v
			}
			includeImporters := true
			if v, ok := args["includeImporters"].(bool); ok {
				includeImporters = v
			}
			includeReferences := true
			if v, ok := args["includeReferences"].(bool); ok {
				includeReferences = v
			}

			excludeSet := map[string]struct{}{}
			if ep, ok := asStringSlice(args, "excludePaths"); ok {
				for _, p := range ep {
					p = filepath.ToSlash(filepath.Clean(p))
					if p != "" && p != "." {
						excludeSet[p] = struct{}{}
					}
				}
			}

			intent, _ := asString(args, "intent")
			intent = strings.TrimSpace(intent)
			requestedIntent := any(nil)
			if intent != "" {
				requestedIntent = intent
			}

			requestedMode := any(nil)
			resolvedMode := "auto"
			strategy := "default_auto"
			explicitMode := false
			if m, ok := asString(args, "mode"); ok {
				switch m {
				case "full", "auto", "outline", "summary":
					requestedMode = m
					resolvedMode = m
					strategy = "explicit_mode"
					explicitMode = true
				}
			}
			if !explicitMode {
				switch intent {
				case "navigate":
					resolvedMode = "outline"
					strategy = "intent:navigate"
				case "implement":
					resolvedMode = "auto"
					strategy = "intent:implement"
				case "review":
					resolvedMode = "auto"
					strategy = "intent:review"
				}
			}
			resolved := map[string]any{
				"requestedIntent": requestedIntent,
				"requestedMode":   requestedMode,
				"resolvedMode":    resolvedMode,
				"strategy":        strategy,
			}

			related, err := computeRelatedFiles(ctx, root, rel, relatedOptions{
				Max:              maxFiles * 3,
				IncludeSameDir:   includeSameDir,
				IncludeImports:   includeImports,
				IncludeImporters: includeImporters,
				IncludeRefs:      includeReferences,
			})
			if err != nil {
				related = nil
			}

			reasonByPath := map[string][]string{}
			for _, r := range related {
				reasonByPath[r.Path] = r.Reasons
			}

			paths := make([]string, 0, 1+len(related))
			paths = append(paths, rel)
			for _, r := range related {
				paths = append(paths, r.Path)
			}
			paths = uniqueStrings(paths)
			if len(excludeSet) > 0 {
				filtered := make([]string, 0, len(paths))
				for _, p := range paths {
					if _, excluded := excludeSet[p]; !excluded {
						filtered = append(filtered, p)
					}
				}
				paths = filtered
			}
			if len(paths) > maxFiles {
				paths = paths[:maxFiles]
			}

			if err := idx.EnsureIndexed(ctx, paths); err != nil {
				// best-effort: still try to read whatever is already indexed
			}

			if resolvedMode == "outline" || resolvedMode == "summary" {
				type outlineEntry struct {
					Path    string `json:"path"`
					Outline string `json:"outline"`
					Mode    string `json:"mode"`
				}
				entries := make([]outlineEntry, 0, len(paths))
				totalOutlineBytes := 0
				outlineClamped := false
				for _, p := range paths {
					ap := filepath.Join(root, p)
					var content string
					var oerr error
					if resolvedMode == "summary" {
						content, oerr = extractFileSummary(ap)
					} else {
						content, oerr = extractFileOutline(ap)
					}
					if oerr != nil || content == "" {
						continue
					}
					if maxTotalBytes > 0 && totalOutlineBytes+len(content) > maxTotalBytes {
						outlineClamped = true
						break
					}
					totalOutlineBytes += len(content)
					entries = append(entries, outlineEntry{Path: p, Outline: content, Mode: resolvedMode})
				}
				out := map[string]any{
					"path":     rel,
					"focus":    focus,
					"intent":   requestedIntent,
					"mode":     resolvedMode,
					"resolved": resolved,
					"files":    entries,
					"limits": map[string]any{
						"maxFiles":      maxFiles,
						"maxTotalBytes": maxTotalBytes,
						"usedBytes":     totalOutlineBytes,
						"clamped":       outlineClamped,
					},
				}
				relatedPaths := make([]string, 0, len(entries))
				for _, entry := range entries {
					if entry.Path != rel {
						relatedPaths = append(relatedPaths, entry.Path)
					}
				}
				if next := suggestedRepoContextFollowUp(rel, focus, relatedPaths); next != nil {
					out["suggestedNextCall"] = next
				}
				return out, nil
			}

			// resolvedMode == "auto": full chunks for target, outlines for related files
			if resolvedMode == "auto" {
				type autoFileEntry struct {
					Path    string           `json:"path"`
					Mode    string           `json:"mode"`
					Chunks  []indexing.Chunk `json:"chunks,omitempty"`
					Outline string           `json:"outline,omitempty"`
				}
				entries := make([]autoFileEntry, 0, len(paths))
				totalAutoBytes := 0
				autoClamped := false

				// Target file: full chunks
				for _, p := range paths {
					if p != rel {
						continue
					}
					chunks, cerr := idx.FileChunks(p)
					if cerr != nil || len(chunks) == 0 {
						continue
					}
					selected := selectChunks(chunks, focusLower, maxChunksPerFile)
					for _, ch := range selected {
						totalAutoBytes += len(ch.Content)
					}
					entries = append(entries, autoFileEntry{
						Path:   p,
						Mode:   "full",
						Chunks: selected,
					})
				}

				// Related files: outlines
				for _, p := range paths {
					if p == rel {
						continue
					}
					ap := filepath.Join(root, p)
					outline, oerr := extractFileOutline(ap)
					if oerr != nil || outline == "" {
						continue
					}
					if maxTotalBytes > 0 && totalAutoBytes+len(outline) > maxTotalBytes {
						autoClamped = true
						break
					}
					totalAutoBytes += len(outline)
					entries = append(entries, autoFileEntry{
						Path:    p,
						Mode:    "outline",
						Outline: outline,
					})
				}

				out := map[string]any{
					"path":     rel,
					"focus":    focus,
					"intent":   requestedIntent,
					"mode":     "auto",
					"resolved": resolved,
					"files":    entries,
					"limits": map[string]any{
						"maxFiles":         maxFiles,
						"maxChunksPerFile": maxChunksPerFile,
						"maxTotalBytes":    maxTotalBytes,
						"usedBytes":        totalAutoBytes,
						"clamped":          autoClamped,
					},
				}
				relatedPaths := make([]string, 0, len(entries))
				for _, entry := range entries {
					if entry.Path != rel {
						relatedPaths = append(relatedPaths, entry.Path)
					}
				}
				if next := suggestedRepoContextFollowUp(rel, focus, relatedPaths); next != nil {
					out["suggestedNextCall"] = next
				}
				return out, nil
			}

			type fileCtx struct {
				Path   string           `json:"path"`
				Chunks []indexing.Chunk `json:"chunks"`
			}

			files := make([]fileCtx, 0, len(paths))
			totalBytes := 0
			clamped := false

			targetChunkWeight := 4
			if focusLower != "" {
				targetChunkWeight = 2
			}

			type candidate struct {
				path   string
				chunk  indexing.Chunk
				score  int
				weight int
				bonus  int
			}

			candidates := make([]candidate, 0, len(paths)*maxChunksPerFile)
			for _, p := range paths {
				chunks, err := idx.FileChunks(p)
				if err != nil {
					continue
				}
				selected := selectChunks(chunks, focusLower, maxChunksPerFile)
				if len(selected) == 0 {
					continue
				}
				weight := 1
				if p == rel {
					weight = targetChunkWeight
				}
				relationBonus := 0
				if p != rel {
					relationBonus = relationScore(reasonByPath[p])
				}
				for _, ch := range selected {
					score := chScore(ch, focusLower)
					candidates = append(candidates, candidate{
						path:   p,
						chunk:  ch,
						score:  score,
						weight: weight,
						bonus:  relationBonus,
					})
				}
			}

			sort.Slice(candidates, func(i, j int) bool {
				si := candidates[i].score*candidates[i].weight + candidates[i].bonus
				sj := candidates[j].score*candidates[j].weight + candidates[j].bonus
				if si != sj {
					return si > sj
				}
				if candidates[i].path != candidates[j].path {
					return candidates[i].path < candidates[j].path
				}
				return candidates[i].chunk.StartLine < candidates[j].chunk.StartLine
			})

			type chunkKey struct {
				path      string
				startLine int
			}
			emitted := map[chunkKey]struct{}{}
			perFile := map[string][]indexing.Chunk{}
			for _, c := range candidates {
				if len(perFile[c.path]) >= maxChunksPerFile {
					continue
				}
				ck := chunkKey{path: c.path, startLine: c.chunk.StartLine}
				if _, dup := emitted[ck]; dup {
					continue
				}
				chBytes := len(c.chunk.Content)
				if maxTotalBytes > 0 && totalBytes+chBytes > maxTotalBytes {
					clamped = true
					break
				}
				totalBytes += chBytes
				emitted[ck] = struct{}{}
				perFile[c.path] = append(perFile[c.path], c.chunk)
			}

			if list, ok := perFile[rel]; ok && len(list) > 0 {
				files = append(files, fileCtx{Path: rel, Chunks: list})
				delete(perFile, rel)
			}
			for _, p := range paths {
				list, ok := perFile[p]
				if !ok || len(list) == 0 {
					continue
				}
				files = append(files, fileCtx{Path: p, Chunks: list})
			}

			return map[string]any{
				"path":     rel,
				"focus":    focus,
				"intent":   requestedIntent,
				"mode":     resolvedMode,
				"resolved": resolved,
				"files":    files,
				"limits": map[string]any{
					"maxFiles":         maxFiles,
					"maxChunksPerFile": maxChunksPerFile,
					"maxTotalBytes":    maxTotalBytes,
					"usedBytes":        totalBytes,
					"clamped":          clamped,
				},
			}, nil
		},
	}
}

func selectChunks(chunks []indexing.Chunk, focusLower string, maxChunks int) []indexing.Chunk {
	if maxChunks <= 0 {
		maxChunks = 2
	}
	if len(chunks) == 0 {
		return nil
	}

	// Prefer chunks that match focus; always include the first chunk as a fallback.
	type scored struct {
		ch indexing.Chunk
		s  int
	}
	scoredChunks := make([]scored, 0, len(chunks))
	for _, ch := range chunks {
		scoredChunks = append(scoredChunks, scored{ch: ch, s: chScore(ch, focusLower)})
	}
	sort.Slice(scoredChunks, func(i, j int) bool {
		if scoredChunks[i].s != scoredChunks[j].s {
			return scoredChunks[i].s > scoredChunks[j].s
		}
		return scoredChunks[i].ch.StartLine < scoredChunks[j].ch.StartLine
	})

	selected := make([]indexing.Chunk, 0, min(maxChunks, len(scoredChunks)))
	for _, s := range scoredChunks {
		if len(selected) >= maxChunks {
			break
		}
		selected = append(selected, s.ch)
	}

	// If no focus match and we didn't pick the first chunk, include it.
	if focusLower == "" && len(chunks) > 0 && (len(selected) == 0 || selected[0].StartLine != chunks[0].StartLine) {
		selected = append([]indexing.Chunk{chunks[0]}, selected...)
		if len(selected) > maxChunks {
			selected = selected[:maxChunks]
		}
	}
	return selected
}

func chScore(ch indexing.Chunk, focusLower string) int {
	s := 0
	if focusLower != "" {
		hay := strings.ToLower(ch.Content)
		if strings.Contains(hay, focusLower) {
			s += 10 + strings.Count(hay, focusLower)
		}
	}
	// small bias for top-of-file context
	if ch.StartLine <= 5 {
		s += 1
	}
	return s
}

func relationScore(reasons []string) int {
	if len(reasons) == 0 {
		return 0
	}
	score := 0
	for _, r := range reasons {
		switch r {
		case "go_types_refs_target", "go_types_used_by_target":
			score += 6
		case "imports_target_package", "imported_by":
			score += 4
		case "imports", "imported_package", "includes", "included_by":
			score += 3
		case "same_dir":
			score += 2
		default:
			score += 1
		}
	}
	return score
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = filepath.ToSlash(filepath.Clean(s))
		if s == "" || s == "." {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func suggestedRepoContextFollowUp(rel, focus string, relatedPaths []string) map[string]any {
	if len(relatedPaths) == 0 {
		return nil
	}
	target := relatedPaths[0]
	excludePaths := []string{rel}
	for _, p := range relatedPaths[1:] {
		excludePaths = append(excludePaths, p)
	}
	args := map[string]any{
		"path":         target,
		"mode":         "full",
		"excludePaths": excludePaths,
	}
	if strings.TrimSpace(focus) != "" {
		args["focus"] = focus
	}
	return map[string]any{
		"name":      "repo_context",
		"arguments": args,
		"reason":    "Use `mode=full` on the most relevant related file for a deeper read without re-sending files already in context.",
	}
}

func repoContextOutputSchema() map[string]any {
	fileEntry := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"mode":    map[string]any{"type": "string"},
			"outline": map[string]any{"type": "string"},
			"chunks": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":      map[string]any{"type": "string"},
						"language":  map[string]any{"type": "string"},
						"startLine": map[string]any{"type": "integer"},
						"endLine":   map[string]any{"type": "integer"},
						"content":   map[string]any{"type": "string"},
					},
					"required": []any{"path", "language", "startLine", "endLine", "content"},
				},
			},
		},
		"required": []any{"path"},
	}

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":   map[string]any{"type": "string"},
			"focus":  map[string]any{"type": "string"},
			"intent": map[string]any{"type": []any{"string", "null"}},
			"mode":   map[string]any{"type": "string"},
			"resolved": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"requestedIntent": map[string]any{"type": []any{"string", "null"}},
					"requestedMode":   map[string]any{"type": []any{"string", "null"}},
					"resolvedMode":    map[string]any{"type": "string"},
					"strategy":        map[string]any{"type": "string"},
				},
				"required": []any{"requestedIntent", "requestedMode", "resolvedMode", "strategy"},
			},
			"files": map[string]any{
				"type":  "array",
				"items": fileEntry,
			},
			"limits": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"maxFiles":         map[string]any{"type": "integer"},
					"maxChunksPerFile": map[string]any{"type": "integer"},
					"maxTotalBytes":    map[string]any{"type": "integer"},
					"usedBytes":        map[string]any{"type": "integer"},
					"clamped":          map[string]any{"type": "boolean"},
				},
				"required": []any{"maxFiles", "maxTotalBytes", "usedBytes", "clamped"},
			},
			"suggestedNextCall": map[string]any{
				"type": []any{"object", "null"},
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
					"arguments": map[string]any{
						"type": "object",
					},
					"reason": map[string]any{"type": "string"},
				},
				"required": []any{"name", "arguments", "reason"},
			},
		},
		"required": []any{"path", "mode", "resolved", "files", "limits"},
	}
}
