package mcp

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var jsGraphCache sync.Map // key: rootAbs, value: *importGraph

func getJSImportGraph(ctx context.Context, rootAbs string) (*importGraph, error) {

	if cached, found := jsGraphCache.Load(rootAbs); found {
		return cached.(*importGraph), nil
	}

	graph, err := buildJSImportGraph(ctx, rootAbs)
	if err != nil {
		return nil, err
	}

	jsGraphCache.Store(rootAbs, graph)
	return graph, nil
}

func InvalidateJSImportGraphCache(rootAbs string) {
	jsGraphCache.Delete(filepath.Clean(rootAbs))
}

const maxJSFileSize = 400_000 // Maximum JS file size to parse (in bytes)

func buildJSImportGraph(ctx context.Context, rootAbs string) (*importGraph, error) {
	g := &importGraph{
		imports:   map[string][]string{},
		importers: map[string][]string{},
	}

	walkErr := filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case ctx.Err() != nil:
			return ctx.Err()
		case shouldSkipDir(d):
			return filepath.SkipDir
		case shouldSkipFile(d):
			return nil
		}
		return processJSFile(g, rootAbs, path, d)
	})

	if walkErr != nil {
		return nil, walkErr
	}
	return g, nil
}

func shouldSkipDir(d fs.DirEntry) bool {
	return d.IsDir() && shouldIgnoreDir(d.Name())
}

func shouldSkipFile(d fs.DirEntry) bool {
	if d.IsDir() || shouldIgnoreFile(d.Name()) {
		return true
	}
	ext := strings.ToLower(filepath.Ext(d.Name()))
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return false
	default:
		return true
	}
}

func processJSFile(g *importGraph, rootAbs, path string, d fs.DirEntry) error {
	info, err := d.Info()
	if err != nil || info.Size() > maxJSFileSize {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	fromRel, err := filepath.Rel(rootAbs, path)
	if err != nil {
		return nil
	}
	fromRel = filepath.ToSlash(fromRel)

	specs := parseJSImportSpecifiers(string(b))
	for _, spec := range specs {
		resolvedRel := resolveJSModuleToRel(rootAbs, path, spec)
		if resolvedRel == "" {
			continue
		}
		g.imports[fromRel] = appendUnique(g.imports[fromRel], resolvedRel)
		g.importers[resolvedRel] = appendUnique(g.importers[resolvedRel], fromRel)
	}
	return nil
}

var (
	reJSImportFrom = regexp.MustCompile(`(?m)^\s*(?:import|export)\s+[^;]*?\sfrom\s+['"]([^'"]+)['"]`)
	reJSImportBare = regexp.MustCompile(`(?m)^\s*import\s+['"]([^'"]+)['"]`)
	reJSRequire    = regexp.MustCompile(`(?m)\brequire\s*\(\s*['"]([^'"]+)['"]\s*\)`)
	reJSDynImport  = regexp.MustCompile(`(?m)\bimport\s*\(\s*['"]([^'"]+)['"]\s*\)`)
)

func parseJSImportSpecifiers(src string) []string {
	out := make([]string, 0, 8)
	for _, re := range []*regexp.Regexp{reJSImportFrom, reJSImportBare, reJSRequire, reJSDynImport} {
		m := re.FindAllStringSubmatch(src, -1)
		for _, mm := range m {
			if len(mm) < 2 {
				continue
			}
			spec := strings.TrimSpace(mm[1])
			if spec == "" {
				continue
			}
			out = append(out, spec)
		}
	}
	return out
}

func resolveJSModuleToRel(rootAbs, fromAbs, spec string) string {
	if !strings.HasPrefix(spec, ".") {
		return ""
	}
	fromDir := filepath.Dir(fromAbs)
	cand := filepath.Clean(filepath.Join(fromDir, filepath.FromSlash(spec)))

	try := func(abs string) string {
		abs = filepath.Clean(abs)
		if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) {
			return ""
		}
		fi, err := os.Stat(abs)
		if err != nil || fi.IsDir() {
			return ""
		}
		rel, err := filepath.Rel(rootAbs, abs)
		if err != nil {
			return ""
		}
		return filepath.ToSlash(rel)
	}

	if r := try(cand); r != "" {
		return r
	}

	exts := []string{".ts", ".tsx", ".js", ".jsx", ".d.ts", ".mjs", ".cjs"}
	for _, ext := range exts {
		if r := try(cand + ext); r != "" {
			return r
		}
	}

	fi, err := os.Stat(cand)
	if err == nil && fi.IsDir() {
		for _, ext := range exts {
			if r := try(filepath.Join(cand, "index"+ext)); r != "" {
				return r
			}
		}
	}
	return ""
}
