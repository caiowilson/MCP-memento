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

var pyGraphCache sync.Map // key: rootAbs, value: *importGraph

func getPythonImportGraph(ctx context.Context, rootAbs string) (*importGraph, error) {
	if v, ok := pyGraphCache.Load(rootAbs); ok {
		return v.(*importGraph), nil
	}
	g, err := buildPythonImportGraph(ctx, rootAbs)
	if err != nil {
		return nil, err
	}
	pyGraphCache.Store(rootAbs, g)
	return g, nil
}

func buildPythonImportGraph(ctx context.Context, rootAbs string) (*importGraph, error) {
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
		if strings.ToLower(filepath.Ext(d.Name())) != ".py" {
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

		resolved := parseAndResolvePythonImports(rootAbs, path, string(b))
		for _, toRel := range resolved {
			g.imports[fromRel] = appendUnique(g.imports[fromRel], toRel)
			g.importers[toRel] = appendUnique(g.importers[toRel], fromRel)
		}

		return nil
	})

	return g, nil
}

var (
	rePyImportStmt = regexp.MustCompile(`(?m)^\s*import\s+([^#\n]+)`)
	rePyFromStmt   = regexp.MustCompile(`(?m)^\s*from\s+([A-Za-z_][A-Za-z0-9_\.]*|\.+[A-Za-z_][A-Za-z0-9_\.]*|\.+)\s+import\s+([^#\n]+)`)
)

func parseAndResolvePythonImports(rootAbs, fromAbs, src string) []string {
	out := make([]string, 0, 8)

	for _, mm := range rePyImportStmt.FindAllStringSubmatch(src, -1) {
		if len(mm) < 2 {
			continue
		}
		for _, part := range strings.Split(mm[1], ",") {
			module := cleanPythonImportToken(part)
			if module == "" {
				continue
			}
			if rel := resolvePythonModuleToRel(rootAbs, module); rel != "" {
				out = appendUnique(out, rel)
			}
		}
	}

	for _, mm := range rePyFromStmt.FindAllStringSubmatch(src, -1) {
		if len(mm) < 3 {
			continue
		}
		base := strings.TrimSpace(mm[1])
		items := strings.TrimSpace(mm[2])
		if base == "" || items == "" {
			continue
		}

		baseModule := ""
		if strings.HasPrefix(base, ".") {
			resolved, ok := resolvePythonRelativeBase(rootAbs, fromAbs, base)
			if !ok {
				continue
			}
			baseModule = resolved
		} else {
			baseModule = base
		}

		if rel := resolvePythonModuleToRel(rootAbs, baseModule); rel != "" {
			out = appendUnique(out, rel)
		}

		for _, item := range strings.Split(items, ",") {
			token := cleanPythonImportToken(item)
			if token == "" || token == "*" {
				continue
			}
			candidate := baseModule + "." + token
			if rel := resolvePythonModuleToRel(rootAbs, candidate); rel != "" {
				out = appendUnique(out, rel)
			}
		}
	}

	return out
}

func cleanPythonImportToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "()")
	if s == "" {
		return ""
	}
	if i := strings.Index(strings.ToLower(s), " as "); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func resolvePythonRelativeBase(rootAbs, fromAbs, raw string) (string, bool) {
	dotCount := 0
	for dotCount < len(raw) && raw[dotCount] == '.' {
		dotCount++
	}
	suffix := strings.TrimSpace(raw[dotCount:])

	fromDir := filepath.Dir(fromAbs)
	fromDirRel, err := filepath.Rel(rootAbs, fromDir)
	if err != nil {
		return "", false
	}
	fromDirRel = filepath.ToSlash(filepath.Clean(fromDirRel))

	parts := []string{}
	if fromDirRel != "." && fromDirRel != "" {
		parts = strings.Split(fromDirRel, "/")
	}

	up := dotCount - 1
	if up < 0 || up > len(parts) {
		return "", false
	}
	baseParts := append([]string{}, parts[:len(parts)-up]...)
	if suffix != "" {
		baseParts = append(baseParts, strings.Split(suffix, ".")...)
	}
	if len(baseParts) == 0 {
		return "", false
	}
	return strings.Join(baseParts, "."), true
}

func resolvePythonModuleToRel(rootAbs, module string) string {
	module = strings.TrimSpace(module)
	if module == "" || strings.HasPrefix(module, ".") {
		return ""
	}
	modulePath := filepath.FromSlash(strings.ReplaceAll(module, ".", "/"))

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

	if rel := try(filepath.Join(rootAbs, modulePath+".py")); rel != "" {
		return rel
	}
	if rel := try(filepath.Join(rootAbs, modulePath, "__init__.py")); rel != "" {
		return rel
	}
	return ""
}
