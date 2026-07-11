package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"memento-mcp/internal/redact"
)

const (
	defaultRepoOutlineMaxSymbols   = 200
	maximumRepoOutlineMaxSymbols   = 1_000
	defaultRepoOutlineMaxFileBytes = 1 * 1024 * 1024
)

type repoOutlineResult struct {
	Path         string          `json:"path"`
	Language     string          `json:"language"`
	PackageName  string          `json:"package,omitempty"`
	Imports      []string        `json:"imports"`
	Header       []string        `json:"header"`
	Symbols      []outlineSymbol `json:"symbols"`
	SymbolCount  int             `json:"symbolCount"`
	TotalSymbols int             `json:"totalSymbols"`
	Fallback     bool            `json:"fallback"`
	Truncated    bool            `json:"truncated"`
	SourceBytes  int64           `json:"sourceBytes"`
	OutlineBytes int             `json:"outlineBytes"`
}

func newRepoOutlineTool(root string, redactors ...*redact.Redactor) Tool {
	tool := repoOutlineToolDefinition()
	redactor := toolRedactor(redactors)
	tool.Handler = func(ctx context.Context, raw json.RawMessage) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
		maxFileBytes := envInt("MEMENTO_OUTLINE_MAX_FILE_BYTES", defaultRepoOutlineMaxFileBytes)
		if maxFileBytes <= 0 {
			maxFileBytes = defaultRepoOutlineMaxFileBytes
		}
		if info.Size() > int64(maxFileBytes) {
			return nil, fmt.Errorf("file exceeds outline size limit (%d bytes): %s", maxFileBytes, rel)
		}
		source, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		includeDocumentation := true
		if value, ok := args["includeDocumentation"].(bool); ok {
			includeDocumentation = value
		}
		includeImports := true
		if value, ok := args["includeImports"].(bool); ok {
			includeImports = value
		}
		maxSymbols := defaultRepoOutlineMaxSymbols
		if value, ok := asFloat(args, "maxSymbols"); ok && int(value) > 0 {
			maxSymbols = int(value)
		}
		if maxSymbols > maximumRepoOutlineMaxSymbols {
			maxSymbols = maximumRepoOutlineMaxSymbols
		}

		outline := extractStructuredFileOutline(abs, source)
		if err := validateStructuredOutline(outline); err != nil {
			return nil, err
		}
		if !includeImports {
			outline.Imports = []string{}
		}
		redactStructuredFileOutline(&outline, redactor, includeDocumentation)
		totalSymbols := len(outline.Symbols)
		truncated := totalSymbols > maxSymbols
		if truncated {
			outline.Symbols = outline.Symbols[:maxSymbols]
		}

		return repoOutlineResult{
			Path:         rel,
			Language:     outline.Language,
			PackageName:  outline.PackageName,
			Imports:      outline.Imports,
			Header:       outline.Header,
			Symbols:      outline.Symbols,
			SymbolCount:  len(outline.Symbols),
			TotalSymbols: totalSymbols,
			Fallback:     outline.Fallback,
			Truncated:    truncated,
			SourceBytes:  int64(len(source)),
			OutlineBytes: structuredOutlineContentBytes(outline),
		}, nil
	}
	return tool
}

func repoOutlineToolDefinition() Tool {
	return Tool{
		Name:        "repo_outline",
		Title:       "Get Repository Outline",
		Description: "Return a compact structural outline for one repository file: package/import metadata plus symbol names, kinds, signatures, documentation, and line ranges. Function and method bodies are excluded. Use before repo_read_file or full repo_context when you need to map a file cheaply.",
		Annotations: readOnlyAnnotations(),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"path"},
			"properties": map[string]any{
				"path":                 map[string]any{"type": "string", "description": "Repo-relative path of the file to outline."},
				"includeDocumentation": map[string]any{"type": "boolean", "description": "Include declaration documentation (default true)."},
				"includeImports":       map[string]any{"type": "boolean", "description": "Include package/module dependencies (default true)."},
				"maxSymbols":           map[string]any{"type": "integer", "minimum": 1, "maximum": maximumRepoOutlineMaxSymbols, "description": "Maximum symbols to return in source order (default 200, maximum 1000)."},
			},
		},
		OutputSchema: repoOutlineOutputSchema(),
	}
}

func repoOutlineOutputSchema() map[string]any {
	symbol := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":                   map[string]any{"type": "string"},
			"kind":                   map[string]any{"type": "string"},
			"signature":              map[string]any{"type": "string"},
			"documentation":          map[string]any{"type": "string"},
			"documentationTruncated": map[string]any{"type": "boolean"},
			"container":              map[string]any{"type": "string"},
			"startLine":              map[string]any{"type": "integer"},
			"endLine":                map[string]any{"type": "integer"},
		},
		"required": []any{"name", "kind", "signature", "startLine", "endLine"},
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":         map[string]any{"type": "string"},
			"language":     map[string]any{"type": "string"},
			"package":      map[string]any{"type": "string"},
			"imports":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"header":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"symbols":      map[string]any{"type": "array", "items": symbol},
			"symbolCount":  map[string]any{"type": "integer"},
			"totalSymbols": map[string]any{"type": "integer"},
			"fallback":     map[string]any{"type": "boolean"},
			"truncated":    map[string]any{"type": "boolean"},
			"sourceBytes":  map[string]any{"type": "integer"},
			"outlineBytes": map[string]any{"type": "integer"},
		},
		"required": []any{"path", "language", "imports", "header", "symbols", "symbolCount", "totalSymbols", "fallback", "truncated", "sourceBytes", "outlineBytes"},
	}
}
