package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func newRepoListFilesTool(root string) Tool {
	return Tool{
		Name:        "repo_list_files",
		Description: "List files under the workspace root (basic ignores).",
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

			paths := make([]string, 0, 256)
			err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return nil
				}
				if d.IsDir() {
					if shouldIgnoreDir(d.Name()) {
						return filepath.SkipDir
					}
					return nil
				}
				if shouldIgnoreFile(d.Name()) {
					return nil
				}

				rel, err := filepath.Rel(root, path)
				if err != nil {
					return nil
				}
				rel = filepath.ToSlash(rel)
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

func newRepoReadFileTool(root string) Tool {
	return Tool{
		Name:        "repo_read_file",
		Description: "Read a file from the workspace root (optionally line-bounded).",
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
					"description": "Maximum bytes to return (default 200000).",
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

			maxBytes := 200_000
			if f, ok := asFloat(args, "maxBytes"); ok && int(f) > 0 {
				maxBytes = int(f)
			}

			abs, err := safeJoin(root, rel)
			if err != nil {
				return nil, err
			}

			fh, err := os.Open(abs)
			if err != nil {
				return nil, err
			}
			defer fh.Close()

			var b strings.Builder
			b.Grow(min(maxBytes, 32_768))

			sc := bufio.NewScanner(fh)
			sc.Buffer(make([]byte, 1024), maxBytes)

			lineNo := 0
			for sc.Scan() {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
				}

				lineNo++
				if startLine > 0 && lineNo < startLine {
					continue
				}
				if endLine > 0 && lineNo > endLine {
					break
				}
				line := sc.Text()
				if b.Len()+len(line)+1 > maxBytes {
					break
				}
				b.WriteString(line)
				b.WriteByte('\n')
			}
			if err := sc.Err(); err != nil {
				return nil, err
			}

			return map[string]any{
				"path":      filepath.ToSlash(filepath.Clean(rel)),
				"startLine": startLine,
				"endLine":   endLine,
				"content":   b.String(),
			}, nil
		},
	}
}

func newRepoSearchTool(root string) Tool {
	return Tool{
		Name:        "repo_search",
		Description: "Search for a substring across files in the workspace root (basic ignores).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"query"},
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Substring query to search for.",
				},
				"glob": map[string]any{
					"type":        "string",
					"description": "Optional filepath.Match pattern applied to relative paths.",
				},
				"caseSensitive": map[string]any{
					"type":        "boolean",
					"description": "Default false.",
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

			needle := query
			if !caseSensitive {
				needle = strings.ToLower(query)
			}

			type match struct {
				Path    string `json:"path"`
				Line    int    `json:"line"`
				Snippet string `json:"snippet"`
			}
			matches := make([]match, 0, min(maxResults, 32))

			walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() {
					if shouldIgnoreDir(d.Name()) {
						return filepath.SkipDir
					}
					return nil
				}
				if shouldIgnoreFile(d.Name()) {
					return nil
				}

				rel, err := filepath.Rel(root, path)
				if err != nil {
					return nil
				}
				rel = filepath.ToSlash(rel)
				if glob != "" {
					ok, err := filepath.Match(glob, rel)
					if err != nil || !ok {
						return nil
					}
				}

				info, err := d.Info()
				if err != nil {
					return nil
				}
				if info.Size() > maxFileBytes {
					return nil
				}

				fh, err := os.Open(path)
				if err != nil {
					return nil
				}
				defer fh.Close()

				sc := bufio.NewScanner(fh)
				sc.Buffer(make([]byte, 1024), int(min64(int64(10_000_000), maxFileBytes+1024)))

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
					found := strings.Contains(hay, needle)
					if !found {
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
						snippet = strings.Join(prev, "\n") + "\n" + line
					}

					matches = append(matches, match{
						Path:    rel,
						Line:    lineNo,
						Snippet: snippet,
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
				out = append(out, map[string]any{
					"path":    m.Path,
					"line":    m.Line,
					"snippet": m.Snippet,
				})
			}
			return map[string]any{
				"query":   query,
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
	return false
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
