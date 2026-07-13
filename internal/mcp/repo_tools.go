package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"memento-mcp/internal/indexing"
	"memento-mcp/internal/redact"
	"memento-mcp/internal/safefs"
)

func newRepoListFilesTool(root string) Tool {
	return Tool{
		Name:        "repo_list_files",
		Title:       "List Repository Files",
		Description: "List files under the workspace root, excluding Git-ignored and built-in sensitive paths.",
		Annotations: readOnlyAnnotations(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"glob": map[string]any{
					"type":        "string",
					"description": "Optional filepath.Match pattern applied to relative paths.",
				},
				"max": map[string]any{
					"type":        "integer",
					"description": "Maximum number of files to return (default 2000).",
				},
			},
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			args, err := requireArgs(raw)
			if err != nil {
				return nil, err
			}
			glob, _ := asString(args, "glob")
			max := 2000
			if f, ok := asFloat(args, "max"); ok && int(f) > 0 {
				max = int(f)
			}

			ignored := loadGitIgnored(root)
			paths := make([]string, 0, 256)
			err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return nil
				}
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return nil
				}
				rel = filepath.ToSlash(rel)
				if d.IsDir() {
					if shouldIgnoreDir(d.Name()) {
						return filepath.SkipDir
					}
					if rel != "." && ignored.Matches(rel) {
						return filepath.SkipDir
					}
					return nil
				}
				if shouldIgnoreFile(d.Name()) || ignored.Matches(rel) {
					return nil
				}
				if glob != "" {
					ok, err := filepath.Match(glob, rel)
					if err != nil || !ok {
						return nil
					}
				}
				paths = append(paths, rel)
				if len(paths) >= max {
					return fs.SkipAll
				}
				return nil
			})
			if err != nil && err != fs.SkipAll {
				return nil, err
			}

			return map[string]any{
				"root":  root,
				"count": len(paths),
				"files": paths,
			}, nil
		},
	}
}

func newRepoReadFileTool(root string, redactors ...*redact.Redactor) Tool {
	redactor := toolRedactor(redactors)
	return Tool{
		Name:        "repo_read_file",
		Title:       "Read Repository File",
		Description: "Read a file from the workspace root (optionally line-bounded).",
		Annotations: readOnlyAnnotations(),
		Meta:        largeResultToolMeta(),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"path"},
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Repo-relative path to read.",
				},
				"startLine": map[string]any{
					"type":        "integer",
					"description": "1-based start line (optional).",
				},
				"endLine": map[string]any{
					"type":        "integer",
					"description": "1-based end line (optional, inclusive).",
				},
				"maxBytes": map[string]any{
					"type":        "integer",
					"description": "Maximum bytes to return (default 32000).",
				},
			},
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			args, err := requireArgs(raw)
			if err != nil {
				return nil, err
			}
			rel, ok := asString(args, "path")
			if !ok || rel == "" {
				return nil, fmt.Errorf("missing required argument: path")
			}

			startLine := 0
			endLine := 0
			if f, ok := asFloat(args, "startLine"); ok && int(f) > 0 {
				startLine = int(f)
			}
			if f, ok := asFloat(args, "endLine"); ok && int(f) > 0 {
				endLine = int(f)
			}

			maxBytes := defaultRepoReadFileMaxBytes
			if f, ok := asFloat(args, "maxBytes"); ok && int(f) > 0 {
				maxBytes = int(f)
			}

			abs, err := safeJoin(root, rel)
			if err != nil {
				return nil, err
			}
			rel = filepath.ToSlash(filepath.Clean(rel))
			if loadGitIgnored(root).Matches(rel) {
				return nil, fmt.Errorf("path is ignored by Git: %s", rel)
			}

			fh, err := os.Open(abs)
			if err != nil {
				return nil, err
			}
			defer fh.Close()

			content, err := readFileContent(ctx, fh, startLine, endLine, maxBytes+64*1024)
			if err != nil {
				return nil, err
			}
			content = prefixStringBytes(redactor.Redact(content), maxBytes)

			return map[string]any{
				"path":      filepath.ToSlash(filepath.Clean(rel)),
				"startLine": startLine,
				"endLine":   endLine,
				"content":   content,
			}, nil
		},
	}
}

func readFileContent(ctx context.Context, r io.Reader, startLine, endLine, maxBytes int) (string, error) {
	var b strings.Builder
	b.Grow(min(maxBytes, 32_768))

	buf := make([]byte, 32*1024)
	lineNo := 1
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		if endLine > 0 && lineNo > endLine {
			break
		}
		if maxBytes > 0 && b.Len() >= maxBytes {
			break
		}

		n, readErr := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			for len(chunk) > 0 {
				if endLine > 0 && lineNo > endLine {
					break
				}
				if maxBytes > 0 && b.Len() >= maxBytes {
					break
				}

				segment := chunk
				nextLine := false
				if idx := bytes.IndexByte(chunk, '\n'); idx >= 0 {
					segment = chunk[:idx+1]
					nextLine = true
				}

				if startLine <= 0 || lineNo >= startLine {
					remaining := maxBytes - b.Len()
					if remaining <= 0 {
						return b.String(), nil
					}
					if len(segment) > remaining {
						b.WriteString(prefixStringBytes(string(segment), remaining))
						return b.String(), nil
					}
					_, _ = b.Write(segment)
				}

				if !nextLine {
					break
				}
				lineNo++
				chunk = chunk[len(segment):]
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}

	return b.String(), nil
}

func newRepoSearchTool(root string, redactors ...*redact.Redactor) Tool {
	return newRepoSearchToolWithIndexer(root, nil, redactors...)
}

func newRepoSearchToolWithIndexer(root string, idx *indexing.Indexer, redactors ...*redact.Redactor) Tool {
	redactor := toolRedactor(redactors)
	return Tool{
		Name:        "repo_search",
		Title:       "Search Repository",
		Description: "Search for a substring or optional regular expression across non-ignored files in the workspace root.",
		Annotations: readOnlyAnnotations(),
		Meta:        largeResultToolMeta(),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"query"},
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Literal substring query by default, or a regular expression when regex is true.",
				},
				"glob": map[string]any{
					"type":        "string",
					"description": "Optional filepath.Match pattern applied to relative paths.",
				},
				"caseSensitive": map[string]any{
					"type":        "boolean",
					"description": "Default false.",
				},
				"regex": map[string]any{
					"type":        "boolean",
					"description": "Interpret query as a regular expression (default false).",
				},
				"maxResults": map[string]any{
					"type":        "integer",
					"description": "Default 50.",
				},
				"maxFileBytes": map[string]any{
					"type":        "integer",
					"description": "Skip files larger than this many bytes (default 1000000).",
				},
				"contextLines": map[string]any{
					"type":        "integer",
					"description": "Context lines included before/after match (default 0).",
				},
				"maxSnippetBytes": map[string]any{
					"type":        "integer",
					"description": "Maximum bytes per returned snippet, including context lines (default 500).",
				},
			},
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			args, err := requireArgs(raw)
			if err != nil {
				return nil, err
			}
			query, ok := asString(args, "query")
			if !ok || query == "" {
				return nil, fmt.Errorf("missing required argument: query")
			}
			glob, _ := asString(args, "glob")

			caseSensitive := false
			if v, ok := args["caseSensitive"].(bool); ok {
				caseSensitive = v
			}
			regexMode := false
			if v, ok := args["regex"].(bool); ok {
				regexMode = v
			}

			maxResults := 50
			if f, ok := asFloat(args, "maxResults"); ok && int(f) > 0 {
				maxResults = int(f)
			}
			maxFileBytes := int64(1_000_000)
			if f, ok := asFloat(args, "maxFileBytes"); ok && int64(f) > 0 {
				maxFileBytes = int64(f)
			}
			contextLines := 0
			if f, ok := asFloat(args, "contextLines"); ok && int(f) >= 0 {
				contextLines = int(f)
			}
			maxSnippetBytes := defaultRepoSearchMaxSnippetBytes
			if f, ok := asFloat(args, "maxSnippetBytes"); ok && int(f) > 0 {
				maxSnippetBytes = int(f)
			}

			needle := query
			if !caseSensitive {
				needle = strings.ToLower(query)
			}
			var expression *regexp.Regexp
			if regexMode {
				pattern := query
				if !caseSensitive {
					pattern = "(?i:" + pattern + ")"
				}
				expression, err = regexp.Compile(pattern)
				if err != nil {
					return nil, fmt.Errorf("invalid regular expression: %w", err)
				}
			}
			var candidateSnapshot indexing.SubstringCandidateSnapshot
			if idx != nil && !regexMode {
				candidateSnapshot = idx.SubstringCandidates(query)
			}

			type match struct {
				Path             string `json:"path"`
				Line             int    `json:"line"`
				Snippet          string `json:"snippet"`
				SnippetTruncated bool   `json:"snippetTruncated,omitempty"`
			}
			matches := make([]match, 0, min(maxResults, 32))

			ignored := loadGitIgnored(root)
			walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return nil
				}
				rel = filepath.ToSlash(rel)
				if d.IsDir() {
					if shouldIgnoreDir(d.Name()) {
						return filepath.SkipDir
					}
					if rel != "." && ignored.Matches(rel) {
						return filepath.SkipDir
					}
					return nil
				}
				if shouldIgnoreFile(d.Name()) || ignored.Matches(rel) {
					return nil
				}
				if glob != "" {
					ok, err := filepath.Match(glob, rel)
					if err != nil || !ok {
						return nil
					}
				}

				if d.Type()&os.ModeSymlink != 0 {
					return nil
				}

				fh, err := safefs.OpenRegular(root, rel)
				if err != nil {
					return nil
				}
				defer fh.Close()
				info, err := fh.Stat()
				if err != nil {
					return nil
				}
				if info.Size() > maxFileBytes || !info.Mode().IsRegular() {
					return nil
				}
				if idx != nil && !regexMode && !candidateSnapshot.MayContain(rel, info.Size(), info.ModTime()) {
					return nil
				}

				var reader io.Reader = fh
				if maxFileBytes < math.MaxInt64 {
					reader = io.LimitReader(fh, maxFileBytes+1)
				}
				content, err := io.ReadAll(reader)
				if err != nil || int64(len(content)) > maxFileBytes {
					return nil
				}
				redactedContent := redactor.Redact(string(content))
				sc := bufio.NewScanner(strings.NewReader(redactedContent))
				maxScannerBytes := int64(10_000_000)
				if maxFileBytes < maxScannerBytes-1024 {
					maxScannerBytes = maxFileBytes + 1024
				}
				sc.Buffer(make([]byte, 1024), int(maxScannerBytes))

				var prev []string
				lineNo := 0
				for sc.Scan() {
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}

					lineNo++
					line := sc.Text()
					hay := line
					if !caseSensitive {
						hay = strings.ToLower(line)
					}
					matchStart := -1
					matchLength := len(query)
					if expression != nil {
						if location := expression.FindStringIndex(line); location != nil {
							matchStart = location[0]
							matchLength = location[1] - location[0]
						}
					} else {
						matchStart = strings.Index(hay, needle)
					}
					if matchStart < 0 {
						if contextLines > 0 {
							prev = append(prev, line)
							if len(prev) > contextLines {
								prev = prev[len(prev)-contextLines:]
							}
						}
						continue
					}

					snippet := line
					if contextLines > 0 && len(prev) > 0 {
						prefix := strings.Join(prev, "\n") + "\n"
						snippet = prefix + line
						matchStart += len(prefix)
					}
					snippet, snippetTruncated := truncateStringAroundBytes(snippet, matchStart, matchLength, maxSnippetBytes)

					snippet = redactor.Redact(snippet)
					matches = append(matches, match{
						Path:             rel,
						Line:             lineNo,
						Snippet:          snippet,
						SnippetTruncated: snippetTruncated,
					})
					if len(matches) >= maxResults {
						return fs.SkipAll
					}

					prev = nil
				}
				if err := sc.Err(); err != nil {
					return nil
				}
				return nil
			})
			if walkErr != nil && walkErr != fs.SkipAll {
				return nil, walkErr
			}

			out := make([]map[string]any, 0, len(matches))
			for _, m := range matches {
				item := map[string]any{
					"path":    m.Path,
					"line":    m.Line,
					"snippet": m.Snippet,
				}
				if m.SnippetTruncated {
					item["snippetTruncated"] = true
				}
				out = append(out, item)
			}
			return map[string]any{
				"query":   redactor.Redact(query),
				"matches": out,
			}, nil
		},
	}
}

func safeJoin(root, rel string) (string, error) {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "/")
	rel = strings.TrimPrefix(rel, "./")

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(rootAbs, filepath.FromSlash(rel))
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	rootAbs = filepath.Clean(rootAbs)
	abs = filepath.Clean(abs)

	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) {
		return "", errOutsideRoot
	}
	return abs, nil
}

func shouldIgnoreDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", "out", ".vscode", ".idea":
		return true
	default:
		return strings.HasPrefix(name, ".git")
	}
}

func shouldIgnoreFile(name string) bool {
	if name == "" {
		return true
	}
	if strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") || strings.HasSuffix(name, ".gif") || strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".pdf") {
		return true
	}
	if name == "server" {
		return true
	}
	if strings.HasPrefix(strings.ToLower(name), ".env") {
		return true
	}
	return false
}

func toolRedactor(redactors []*redact.Redactor) *redact.Redactor {
	if len(redactors) > 0 && redactors[0] != nil {
		return redactors[0]
	}
	return redact.Default()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
