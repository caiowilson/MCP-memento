package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
)

type goSemanticIndex struct {
	root string
	pkgs []*packages.Package
}

var goIndexCache sync.Map // key: rootAbs, value: *goSemanticIndex

func getGoSemanticIndex(ctx context.Context, rootAbs string) (*goSemanticIndex, error) {
	if v, ok := goIndexCache.Load(rootAbs); ok {
		return v.(*goSemanticIndex), nil
	}

	cfg := &packages.Config{
		Context: ctx,
		Dir:     rootAbs,
		Mode: packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedCompiledGoFiles |
			packages.NeedName,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}
	idx := &goSemanticIndex{root: rootAbs, pkgs: pkgs}
	goIndexCache.Store(rootAbs, idx)
	return idx, nil
}

func InvalidateGoSemanticCache(rootAbs string) {
	goIndexCache.Delete(filepath.Clean(rootAbs))
}

func WarmGoSemanticCache(ctx context.Context, rootAbs string) {
	_, _ = getGoSemanticIndex(ctx, rootAbs)
}

func addGoTypeSemanticRelated(ctx context.Context, rootAbs, targetAbs, targetRel string, c *relatedCollector) {
	_ = ctx
	idx, err := getGoSemanticIndex(ctx, rootAbs)
	if err != nil || idx == nil {
		return
	}

	rootAbs = filepath.Clean(rootAbs)
	targetAbs = filepath.Clean(targetAbs)

	for _, pkg := range idx.pkgs {
		if pkg == nil || pkg.TypesInfo == nil || pkg.Fset == nil {
			continue
		}
		for i, f := range pkg.Syntax {
			if f == nil {
				continue
			}
			if i >= len(pkg.CompiledGoFiles) {
				continue
			}
			fileAbs := filepath.Clean(pkg.CompiledGoFiles[i])
			if fileAbs == targetAbs {
				continue
			}
			if fileAbs != rootAbs && !strings.HasPrefix(fileAbs, rootAbs+string(filepath.Separator)) {
				continue
			}

			hits := 0
			for ident, obj := range pkg.TypesInfo.Uses {
				if ident == nil || obj == nil {
					continue
				}
				pos := obj.Pos()
				if !pos.IsValid() {
					continue
				}
				defAbs := filepath.Clean(pkg.Fset.Position(pos).Filename)
				if defAbs != targetAbs {
					continue
				}
				hits++
				if hits >= 3 {
					break
				}
			}
			if hits == 0 {
				continue
			}
			rel, err := filepath.Rel(rootAbs, fileAbs)
			if err != nil {
				continue
			}
			c.add(filepath.ToSlash(rel), 12+hits, "go_types_refs_target")
		}
	}

	// Also add files that define objects referenced from the target file.
	for _, pkg := range idx.pkgs {
		if pkg == nil || pkg.TypesInfo == nil || pkg.Fset == nil {
			continue
		}
		for i, f := range pkg.Syntax {
			if f == nil {
				continue
			}
			if i >= len(pkg.CompiledGoFiles) {
				continue
			}
			fileAbs := filepath.Clean(pkg.CompiledGoFiles[i])
			if fileAbs != targetAbs {
				continue
			}

			seen := map[string]int{}
			for ident, obj := range pkg.TypesInfo.Uses {
				if ident == nil || obj == nil {
					continue
				}
				pos := obj.Pos()
				if !pos.IsValid() {
					continue
				}
				defAbs := filepath.Clean(pkg.Fset.Position(pos).Filename)
				if defAbs == "" || defAbs == targetAbs {
					continue
				}
				if defAbs != rootAbs && !strings.HasPrefix(defAbs, rootAbs+string(filepath.Separator)) {
					continue
				}
				seen[defAbs]++
			}

			for defAbs, hits := range seen {
				if hits <= 0 {
					continue
				}
				rel, err := filepath.Rel(rootAbs, defAbs)
				if err != nil {
					continue
				}
				if hits > 5 {
					hits = 5
				}
				c.add(filepath.ToSlash(rel), 7+hits, "go_types_used_by_target")
			}
		}
	}
}
