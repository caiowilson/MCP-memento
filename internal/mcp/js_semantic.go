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
	if v, ok := jsGraphCache.Load(rootAbs); ok {
		return v.(*importGraph), nil
	}
	g, err := buildJSImportGraph(ctx, rootAbs)
	if err != nil {
		return nil, err
	}
	jsGraphCache.Store(rootAbs, g)
	return g, nil
}

func buildJSImportGraph(ctx context.Context, rootAbs string) (*importGraph, error) {
	g := &importGraph{
		imports:   map[string][]string{},
		importers: map[string][]string{},
	}

	_ = filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
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
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".ts" && ext != ".tsx" && ext != ".js" && ext != ".jsx" && ext != ".mjs" && ext != ".cjs" {
			return nil
		}

		info, err := d.Info()
		if err != nil || info.Size() > 400_000 {
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
	})

	return g, nil
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
