package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"memento-mcp/internal/gitstate"
)

func newRepoRelatedFilesTool(root string) Tool {
	return Tool{
		Name:        "repo_related_files",
		Title:       "Find Related Files",
		Description: "Given a repo-relative file path, returns related files (same folder, imports, importers, and basic semantic refs).",
		Annotations: readOnlyAnnotations(),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"path"},
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Repo-relative path to the file.",
				},
				"max": map[string]any{
					"type":        "integer",
					"description": "Maximum number of related files to return (default 50).",
				},
				"includeSameDir": map[string]any{
					"type":        "boolean",
					"description": "Include files in the same directory (default true).",
				},
				"includeImports": map[string]any{
					"type":        "boolean",
					"description": "Include packages imported by the file when resolvable to local paths (default true).",
				},
				"includeImporters": map[string]any{
					"type":        "boolean",
					"description": "Include files that import the target (language-aware, default true).",
				},
				"includeReferences": map[string]any{
					"type":        "boolean",
					"description": "Include semantic references where supported (Go and PHP, default true).",
				},
			},
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			args, err := requireArgs(raw)
			if err != nil {
				return nil, err
			}
			rel, ok := asString(args, "path")
			if !ok || strings.TrimSpace(rel) == "" {
				return nil, fmt.Errorf("missing required argument: path")
			}
			max := 50
			if f, ok := asFloat(args, "max"); ok && int(f) > 0 {
				max = int(f)
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

			relClean := filepath.ToSlash(filepath.Clean(rel))
			out, err := computeRelatedFiles(ctx, root, relClean, relatedOptions{
				Max:              max,
				IncludeSameDir:   includeSameDir,
				IncludeImports:   includeImports,
				IncludeImporters: includeImporters,
				IncludeRefs:      includeReferences,
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"path":    relClean,
				"root":    root,
				"count":   len(out),
				"related": out,
			}, nil
		},
	}
}

type relatedOptions struct {
	Max              int
	IncludeSameDir   bool
	IncludeImports   bool
	IncludeImporters bool
	IncludeRefs      bool
}

func computeRelatedFiles(ctx context.Context, root, relClean string, opts relatedOptions) ([]relatedCandidate, error) {
	ignored := loadGitIgnored(root)
	if ignored.Matches(relClean) {
		return nil, fmt.Errorf("path is ignored by Git: %s", relClean)
	}
	targetAbs, err := safeJoin(root, relClean)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(targetAbs)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory, expected file: %s", relClean)
	}

	modulePath, _ := readGoModulePath(root)

	targetDirAbs := filepath.Dir(targetAbs)
	targetDirRel, _ := filepath.Rel(root, targetDirAbs)
	targetDirRel = filepath.ToSlash(filepath.Clean(targetDirRel))

	collector := newRelatedCollector(relClean, opts.Max)

	if opts.IncludeSameDir {
		addSameDirRelated(root, targetDirAbs, relClean, collector, ignored)
	}

	ext := strings.ToLower(filepath.Ext(relClean))
	if ext == ".go" {
		if opts.IncludeImports {
			addGoImportsRelated(root, modulePath, targetAbs, collector, ignored)
		}
		if opts.IncludeImporters && modulePath != "" {
			addGoImportersRelated(ctx, root, modulePath, targetDirRel, relClean, collector, ignored)
		}
		if opts.IncludeRefs {
			addGoTypeSemanticRelated(ctx, root, targetAbs, relClean, collector)
		}
	} else if ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx" || ext == ".mjs" || ext == ".cjs" {
		g, err := getJSImportGraph(ctx, root)
		if err == nil && g != nil {
			if opts.IncludeImports {
				for _, p := range g.imports[relClean] {
					collector.add(p, 9, "imports")
				}
			}
			if opts.IncludeImporters {
				for _, p := range g.importers[relClean] {
					collector.add(p, 10, "imported_by")
				}
			}
		}
	} else if isPHPRelationFile(relClean) || isPHPFrameworkRelationFile(relClean) || relClean == "composer.json" {
		g, err := getPHPIncludeGraph(ctx, root)
		if err == nil && g != nil {
			if opts.IncludeImports {
				for _, p := range g.imports[relClean] {
					collector.add(p, 9, "imports")
				}
				for _, p := range g.autoloads[relClean] {
					collector.add(p, 9, "autoloads")
				}
			}
			if opts.IncludeImporters {
				for _, p := range g.importers[relClean] {
					collector.add(p, 10, "imported_by")
				}
				for _, p := range g.autoloadedBy[relClean] {
					collector.add(p, 10, "autoloaded_by")
				}
			}
			if opts.IncludeRefs {
				for _, p := range g.references[relClean] {
					collector.add(p, 8, "semantic_reference")
				}
				for _, p := range g.referencedBy[relClean] {
					collector.add(p, 8, "referenced_by")
				}
			}
		}
		if isPHPFrameworkRelationFile(relClean) {
			// Preserve generic text-reference discovery for YAML/Twig files that
			// are not part of a PHP project while adding framework-aware edges.
			addGenericMentionsRelated(ctx, root, relClean, collector, ignored)
		}
	} else if ext == ".py" {
		g, err := getPythonImportGraph(ctx, root)
		if err == nil && g != nil {
			if opts.IncludeImports {
				for _, p := range g.imports[relClean] {
					collector.add(p, 9, "imports")
				}
			}
			if opts.IncludeImporters {
				for _, p := range g.importers[relClean] {
					collector.add(p, 10, "imported_by")
				}
			}
		}
	} else {
		addGenericMentionsRelated(ctx, root, relClean, collector, ignored)
	}

	return filterExistingRelated(root, collector.results(), ignored), nil
}

type relatedCandidate struct {
	Path    string   `json:"path"`
	Score   int      `json:"score"`
	Reasons []string `json:"reasons"`
}

type relatedCollector struct {
	target string
	max    int
	m      map[string]*relatedCandidate
}

func newRelatedCollector(target string, max int) *relatedCollector {
	return &relatedCollector{
		target: target,
		max:    max,
		m:      map[string]*relatedCandidate{},
	}
}

func (c *relatedCollector) add(path string, score int, reason string) {
	path = filepath.ToSlash(filepath.Clean(path))
	if path == "" || path == "." || path == c.target {
		return
	}
	existing := c.m[path]
	if existing == nil {
		c.m[path] = &relatedCandidate{Path: path, Score: score, Reasons: []string{reason}}
		return
	}
	existing.Score += score
	for _, r := range existing.Reasons {
		if r == reason {
			return
		}
	}
	existing.Reasons = append(existing.Reasons, reason)
}

func (c *relatedCollector) results() []relatedCandidate {
	out := make([]relatedCandidate, 0, len(c.m))
	for _, v := range c.m {
		sort.Strings(v.Reasons)
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Path < out[j].Path
	})
	if c.max > 0 && len(out) > c.max {
		out = out[:c.max]
	}
	return out
}

func addSameDirRelated(root, dirAbs, targetRel string, c *relatedCollector, ignored *gitstate.IgnoredPaths) {
	entries, err := os.ReadDir(dirAbs)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if shouldIgnoreFile(e.Name()) {
			continue
		}
		abs := filepath.Join(dirAbs, e.Name())
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if rel == targetRel || ignored.Matches(rel) {
			continue
		}
		score := 5
		if strings.HasSuffix(rel, "_test.go") {
			score = 6
		}
		c.add(rel, score, "same_dir")
	}
}

func addGenericMentionsRelated(ctx context.Context, root, targetRel string, c *relatedCollector, ignored *gitstate.IgnoredPaths) {
	base := filepath.Base(targetRel)
	if base == "" {
		return
	}
	needle1 := strings.ToLower(base)
	needle2 := strings.ToLower(targetRel)

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
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
		info, err := d.Info()
		if err != nil || info.Size() > 300_000 {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		hay := strings.ToLower(string(b))
		if strings.Contains(hay, needle2) || strings.Contains(hay, needle1) {
			c.add(rel, 2, "mention")
		}
		return nil
	})
}

func readGoModulePath(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", fmt.Errorf("module path not found in go.mod")
}

func addGoImportsRelated(root, modulePath, targetAbs string, c *relatedCollector, ignored *gitstate.IgnoredPaths) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, targetAbs, nil, parser.ImportsOnly)
	if err != nil {
		return
	}
	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, "\"")
		if modulePath == "" || !strings.HasPrefix(p, modulePath) {
			continue
		}
		dirRel := strings.TrimPrefix(p, modulePath)
		dirRel = strings.TrimPrefix(dirRel, "/")
		if dirRel == "" {
			continue
		}
		dirAbs := filepath.Join(root, filepath.FromSlash(dirRel))
		if fi, err := os.Stat(dirAbs); err == nil && fi.IsDir() {
			entries, err := os.ReadDir(dirAbs)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				if !strings.HasSuffix(e.Name(), ".go") {
					continue
				}
				rel, err := filepath.Rel(root, filepath.Join(dirAbs, e.Name()))
				if err != nil {
					continue
				}
				rel = filepath.ToSlash(rel)
				if !ignored.Matches(rel) {
					c.add(rel, 8, "imported_package")
				}
			}
		}
	}
}

func addGoImportersRelated(ctx context.Context, root, modulePath, targetDirRel, targetFileRel string, c *relatedCollector, ignored *gitstate.IgnoredPaths) {
	targetImportPath := modulePath
	if targetDirRel != "." && targetDirRel != "" {
		targetImportPath = modulePath + "/" + strings.TrimPrefix(targetDirRel, "./")
	}
	targetImportPath = strings.TrimSuffix(targetImportPath, "/")

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
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
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") || ignored.Matches(rel) {
			return nil
		}
		if rel == targetFileRel {
			return nil
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return nil
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, "\"")
			if p == targetImportPath {
				c.add(rel, 10, "imports_target_package")
				return nil
			}
		}
		return nil
	})
}

func filterExistingRelated(root string, related []relatedCandidate, ignored *gitstate.IgnoredPaths) []relatedCandidate {
	if len(related) == 0 {
		return nil
	}

	filtered := make([]relatedCandidate, 0, len(related))
	for _, cand := range related {
		if ignored.Matches(cand.Path) {
			continue
		}
		abs, err := safeJoin(root, cand.Path)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		filtered = append(filtered, cand)
	}
	return filtered
}
