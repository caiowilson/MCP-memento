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

var phpGraphCache sync.Map // key: rootAbs, value: *importGraph

func getPHPIncludeGraph(ctx context.Context, rootAbs string) (*importGraph, error) {
	if v, ok := phpGraphCache.Load(rootAbs); ok {
		return v.(*importGraph), nil
	}
	g, err := buildPHPIncludeGraph(ctx, rootAbs)
	if err != nil {
		return nil, err
	}
	phpGraphCache.Store(rootAbs, g)
	return g, nil
}

func buildPHPIncludeGraph(ctx context.Context, rootAbs string) (*importGraph, error) {
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
		if strings.ToLower(filepath.Ext(d.Name())) != ".php" {
			return nil
		}

		info, err := d.Info()
		if err != nil || info.Size() > 500_000 {
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

		specs := parsePHPIncludeSpecifiers(string(b))
		for _, spec := range specs {
			resolvedRel := resolvePHPIncludeToRel(rootAbs, path, spec)
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

var rePHPInclude = regexp.MustCompile(`(?im)\b(?:require|include)(?:_once)?\s*\(?\s*['"]([^'"]+)['"]\s*\)?`)

func parsePHPIncludeSpecifiers(src string) []string {
	out := make([]string, 0, 8)
	m := rePHPInclude.FindAllStringSubmatch(src, -1)
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
	return out
}

func resolvePHPIncludeToRel(rootAbs, fromAbs, spec string) string {
	// Only resolve obvious relative includes; anything dynamic is skipped.
	if strings.ContainsAny(spec, "$`") {
		return ""
	}
	if strings.HasPrefix(spec, "/") {
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
	if !strings.HasSuffix(strings.ToLower(cand), ".php") {
		if r := try(cand + ".php"); r != "" {
			return r
		}
	}
	return ""
}
