package mcp

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var phpGraphCache sync.Map // key: cleaned rootAbs, value: *importGraph

type phpFileRelations struct {
	abs       string
	rel       string
	source    string
	masked    string
	namespace string
	declared  []string
	uses      map[string]string // local alias -> fully-qualified class name
	traitUses []string
}

type composerAutoload struct {
	Autoload struct {
		PSR4 map[string]json.RawMessage `json:"psr-4"`
	} `json:"autoload"`
	AutoloadDev struct {
		PSR4 map[string]json.RawMessage `json:"psr-4"`
	} `json:"autoload-dev"`
}

type psr4Prefix struct {
	namespace string
	dirs      []string
}

func getPHPIncludeGraph(ctx context.Context, rootAbs string) (*importGraph, error) {
	rootAbs = filepath.Clean(rootAbs)
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

func InvalidatePHPIncludeGraphCache(rootAbs string) {
	phpGraphCache.Delete(filepath.Clean(rootAbs))
}

func buildPHPIncludeGraph(ctx context.Context, rootAbs string) (*importGraph, error) {
	g := &importGraph{
		imports:      map[string][]string{},
		importers:    map[string][]string{},
		references:   map[string][]string{},
		referencedBy: map[string][]string{},
	}
	files := make([]phpFileRelations, 0, 128)
	classFiles := map[string]string{}
	ignored := loadGitIgnored(rootAbs)

	walkErr := filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, err error) error {
		rel, relErr := filepath.Rel(rootAbs, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		switch {
		case err != nil:
			return err
		case ctx.Err() != nil:
			return ctx.Err()
		case d.IsDir() && shouldIgnoreDir(d.Name()):
			return filepath.SkipDir
		case d.IsDir() && rel != "." && ignored.Matches(rel):
			return filepath.SkipDir
		case d.IsDir() || shouldIgnoreFile(d.Name()) || !strings.EqualFold(filepath.Ext(d.Name()), ".php"):
			return nil
		case ignored.Matches(rel):
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
		file := parsePHPFileRelations(path, rel, string(b))
		files = append(files, file)
		for _, class := range file.declared {
			classFiles[strings.ToLower(class)] = file.rel
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	psr4 := readComposerPSR4(rootAbs)
	for _, file := range files {
		for _, spec := range parsePHPIncludeSpecifiers(file.source) {
			if rel := resolvePHPIncludeToRel(rootAbs, file.abs, spec); rel != "" {
				addPHPImportEdge(g, file.rel, rel)
			}
		}
		for _, class := range file.uses {
			if rel := resolvePHPClassToRel(rootAbs, class, classFiles, psr4); rel != "" {
				addPHPImportEdge(g, file.rel, rel)
			}
		}
		for _, class := range referencedPHPClasses(file) {
			if rel := resolvePHPClassToRel(rootAbs, class, classFiles, psr4); rel != "" {
				addPHPReferenceEdge(g, file.rel, rel)
			}
		}
		for _, rel := range laravelConventionReferences(rootAbs, file.source) {
			addPHPReferenceEdge(g, file.rel, rel)
		}
	}
	return g, nil
}

func addPHPImportEdge(g *importGraph, from, to string) {
	g.imports[from] = appendUnique(g.imports[from], to)
	g.importers[to] = appendUnique(g.importers[to], from)
}

func addPHPReferenceEdge(g *importGraph, from, to string) {
	g.references[from] = appendUnique(g.references[from], to)
	g.referencedBy[to] = appendUnique(g.referencedBy[to], from)
}

var (
	rePHPInclude       = regexp.MustCompile(`(?im)\b(?:require|include)(?:_once)?\s*\(?\s*['"]([^'"]+)['"]\s*\)?`)
	rePHPTopLevelUse   = regexp.MustCompile(`(?m)^[ \t]*use[ \t]+([^;]+);`)
	rePHPTraitUse      = regexp.MustCompile(`(?m)^[ \t]*use[ \t]+([^;{]+)[ \t]*;`)
	rePHPClassContext  = regexp.MustCompile(`(?i)\b(?:new|instanceof|extends)[ \t\r\n]+(\\?[A-Za-z_][A-Za-z0-9_\\]*)`)
	rePHPStaticContext = regexp.MustCompile(`(\\?[A-Za-z_][A-Za-z0-9_\\]*)[ \t\r\n]*::`)
	rePHPImplements    = regexp.MustCompile(`(?i)\bimplements[ \t\r\n]+([^\{]+)`)
	reLaravelView      = regexp.MustCompile(`(?i)(?:\bview|\bView::make|@include|@extends|@component)[ \t\r\n]*\([ \t\r\n]*['"]([^'"]+)['"]`)
	reLaravelRouteView = regexp.MustCompile(`(?i)\bRoute::view[ \t\r\n]*\([ \t\r\n]*['"][^'"]+['"][ \t\r\n]*,[ \t\r\n]*['"]([^'"]+)['"]`)
	reLaravelConfig    = regexp.MustCompile(`(?i)\bconfig[ \t\r\n]*\([ \t\r\n]*['"]([A-Za-z0-9_-]+)(?:\.[^'"]*)?['"]`)
)

func parsePHPFileRelations(abs, rel, source string) phpFileRelations {
	masked := string(maskPHPNonCode([]byte(source)))
	starts, depths := outlineLineLayout([]byte(masked))
	lines := splitOutlineLines([]byte(source), starts)
	file := phpFileRelations{abs: abs, rel: rel, source: source, masked: masked, uses: map[string]string{}}
	for i, line := range lines {
		if depths[i] != 0 {
			continue
		}
		if match := phpNamespaceRe.FindStringSubmatch(line); match != nil {
			file.namespace = normalizePHPClass(match[1])
		}
		if match := phpTypeDeclRe.FindStringSubmatch(line); match != nil {
			file.declared = append(file.declared, qualifyPHPClass(file.namespace, match[2]))
		}
	}
	for _, match := range rePHPTopLevelUse.FindAllStringSubmatchIndex(masked, -1) {
		line := outlineLineForOffset(starts, match[0])
		if line >= len(depths) || depths[line] != 0 {
			continue // trait imports are references, not namespace imports
		}
		for alias, class := range parsePHPUseClause(source[match[2]:match[3]]) {
			file.uses[alias] = class
		}
	}
	for _, match := range rePHPTraitUse.FindAllStringSubmatchIndex(masked, -1) {
		line := outlineLineForOffset(starts, match[0])
		if line >= len(depths) || depths[line] == 0 {
			continue
		}
		file.traitUses = append(file.traitUses, strings.Split(source[match[2]:match[3]], ",")...)
	}
	return file
}

func parsePHPUseClause(clause string) map[string]string {
	out := map[string]string{}
	clause = strings.TrimSpace(clause)
	if strings.HasPrefix(strings.ToLower(clause), "function ") || strings.HasPrefix(strings.ToLower(clause), "const ") {
		return out
	}
	parts := splitPHPUseList(clause)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if open := strings.Index(part, "{"); open >= 0 && strings.HasSuffix(strings.TrimSpace(part), "}") {
			prefix := strings.TrimSuffix(strings.TrimSpace(part[:open]), "\\")
			inside := strings.TrimSpace(part[open+1 : strings.LastIndex(part, "}")])
			for _, item := range strings.Split(inside, ",") {
				addPHPUse(out, prefix+"\\"+strings.TrimSpace(item))
			}
			continue
		}
		addPHPUse(out, part)
	}
	return out
}

func splitPHPUseList(clause string) []string {
	depth, start := 0, 0
	out := []string{}
	for i, r := range clause {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, clause[start:i])
				start = i + 1
			}
		}
	}
	return append(out, clause[start:])
}

func addPHPUse(out map[string]string, item string) {
	fields := regexp.MustCompile(`(?i)[ \t]+as[ \t]+`).Split(strings.TrimSpace(item), 2)
	class := normalizePHPClass(fields[0])
	if class == "" {
		return
	}
	alias := class[strings.LastIndex(class, "\\")+1:]
	if len(fields) == 2 {
		alias = strings.TrimSpace(fields[1])
	}
	out[strings.ToLower(alias)] = class
}

func referencedPHPClasses(file phpFileRelations) []string {
	names := append([]string{}, file.traitUses...)
	for _, match := range rePHPClassContext.FindAllStringSubmatch(file.masked, -1) {
		names = append(names, match[1])
	}
	for _, match := range rePHPStaticContext.FindAllStringSubmatch(file.masked, -1) {
		names = append(names, match[1])
	}
	for _, match := range rePHPImplements.FindAllStringSubmatch(file.masked, -1) {
		for _, name := range strings.Split(match[1], ",") {
			names = append(names, strings.TrimSpace(name))
		}
	}
	// A namespace import is a real import edge. If its alias appears again, it is
	// also a symbol-level reference (including type hints and attributes).
	for alias, class := range file.uses {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(alias) + `\b`)
		if len(re.FindAllStringIndex(file.masked, 2)) > 1 {
			names = append(names, class)
		}
	}
	out := []string{}
	for _, name := range names {
		if class := resolvePHPClassName(file, name); class != "" {
			out = appendUnique(out, class)
		}
	}
	return out
}

func resolvePHPClassName(file phpFileRelations, name string) string {
	name = strings.TrimSpace(strings.Trim(name, "?&|()[]"))
	if name == "" {
		return ""
	}
	lower := strings.ToLower(strings.TrimPrefix(name, "\\"))
	switch lower {
	case "self", "static", "parent", "class", "string", "int", "float", "bool", "array", "object", "callable", "iterable", "mixed", "void", "never", "null", "false", "true":
		return ""
	}
	if strings.HasPrefix(name, "\\") {
		return normalizePHPClass(name)
	}
	first, rest := name, ""
	if slash := strings.Index(name, "\\"); slash >= 0 {
		first, rest = name[:slash], name[slash:]
	}
	if imported, ok := file.uses[strings.ToLower(first)]; ok {
		return normalizePHPClass(imported + rest)
	}
	return qualifyPHPClass(file.namespace, name)
}

func normalizePHPClass(name string) string {
	return strings.Trim(strings.TrimSpace(name), "\\")
}

func qualifyPHPClass(namespace, name string) string {
	name = normalizePHPClass(name)
	if namespace == "" {
		return name
	}
	return normalizePHPClass(namespace) + "\\" + name
}

func readComposerPSR4(root string) []psr4Prefix {
	b, err := os.ReadFile(filepath.Join(root, "composer.json"))
	if err != nil {
		return nil
	}
	var composer composerAutoload
	if json.Unmarshal(b, &composer) != nil {
		return nil
	}
	out := []psr4Prefix{}
	add := func(values map[string]json.RawMessage) {
		for namespace, raw := range values {
			dirs := []string{}
			var one string
			if json.Unmarshal(raw, &one) == nil {
				dirs = append(dirs, one)
			} else {
				_ = json.Unmarshal(raw, &dirs)
			}
			out = append(out, psr4Prefix{namespace: normalizePHPClass(namespace), dirs: dirs})
		}
	}
	add(composer.Autoload.PSR4)
	add(composer.AutoloadDev.PSR4)
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].namespace) > len(out[j].namespace) })
	return out
}

func resolvePHPClassToRel(root, class string, classFiles map[string]string, prefixes []psr4Prefix) string {
	class = normalizePHPClass(class)
	if rel := classFiles[strings.ToLower(class)]; rel != "" {
		return rel
	}
	for _, prefix := range prefixes {
		if !strings.EqualFold(class, prefix.namespace) && !strings.HasPrefix(strings.ToLower(class), strings.ToLower(prefix.namespace+"\\")) {
			continue
		}
		suffix := strings.TrimPrefix(class[len(prefix.namespace):], "\\")
		for _, dir := range prefix.dirs {
			candidate := filepath.Join(root, filepath.FromSlash(dir), filepath.FromSlash(strings.ReplaceAll(suffix, "\\", "/"))) + ".php"
			if rel := existingRepoFile(root, candidate); rel != "" {
				return rel
			}
		}
	}
	return ""
}

func laravelConventionReferences(root, source string) []string {
	out := []string{}
	for _, re := range []*regexp.Regexp{reLaravelView, reLaravelRouteView} {
		for _, match := range re.FindAllStringSubmatch(source, -1) {
			if strings.Contains(match[1], "::") { // package view namespace
				continue
			}
			name := strings.TrimSuffix(strings.ReplaceAll(match[1], ".", "/"), ".blade.php")
			candidate := filepath.Join(root, "resources", "views", filepath.FromSlash(name+".blade.php"))
			if rel := existingRepoFile(root, candidate); rel != "" {
				out = appendUnique(out, rel)
			}
		}
	}
	for _, match := range reLaravelConfig.FindAllStringSubmatch(source, -1) {
		if rel := existingRepoFile(root, filepath.Join(root, "config", match[1]+".php")); rel != "" {
			out = appendUnique(out, rel)
		}
	}
	return out
}

func existingRepoFile(root, abs string) string {
	abs = filepath.Clean(abs)
	root = filepath.Clean(root)
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return ""
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return ""
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

func parsePHPIncludeSpecifiers(src string) []string {
	out := make([]string, 0, 8)
	for _, match := range rePHPInclude.FindAllStringSubmatch(src, -1) {
		if len(match) > 1 && strings.TrimSpace(match[1]) != "" {
			out = append(out, strings.TrimSpace(match[1]))
		}
	}
	return out
}

func resolvePHPIncludeToRel(rootAbs, fromAbs, spec string) string {
	if strings.ContainsAny(spec, "$`") || strings.HasPrefix(spec, "/") {
		return ""
	}
	candidate := filepath.Clean(filepath.Join(filepath.Dir(fromAbs), filepath.FromSlash(spec)))
	if rel := existingRepoFile(rootAbs, candidate); rel != "" {
		return rel
	}
	if !strings.HasSuffix(strings.ToLower(candidate), ".php") {
		return existingRepoFile(rootAbs, candidate+".php")
	}
	return ""
}
