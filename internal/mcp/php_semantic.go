package mcp

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"memento-mcp/internal/parsing"
)

type phpGraphCacheEntry struct {
	ready chan struct{}
	graph *importGraph
	err   error
}

var phpGraphCache sync.Map // key: cleaned rootAbs, value: *phpGraphCacheEntry

type phpFileRelations struct {
	abs        string
	rel        string
	source     string
	masked     string
	namespace  string
	declared   []string
	functions  []string
	uses       map[string]string // local alias -> fully-qualified class name
	imports    []string          // canonical parser-backed namespace imports
	traitUses  []string
	references []string
	includes   []string
	parsed     bool
}

func getPHPIncludeGraph(ctx context.Context, rootAbs string) (*importGraph, error) {
	rootAbs = filepath.Clean(rootAbs)
	pending := &phpGraphCacheEntry{ready: make(chan struct{})}
	value, loaded := phpGraphCache.LoadOrStore(rootAbs, pending)
	entry := value.(*phpGraphCacheEntry)
	if loaded {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-entry.ready:
			return entry.graph, entry.err
		}
	}
	entry.graph, entry.err = buildPHPIncludeGraph(ctx, rootAbs)
	if entry.err != nil {
		phpGraphCache.CompareAndDelete(rootAbs, entry)
	}
	close(entry.ready)
	return entry.graph, entry.err
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
		autoloads:    map[string][]string{},
		autoloadedBy: map[string][]string{},
	}
	files := make([]phpFileRelations, 0, 128)
	filesByRel := map[string]phpFileRelations{}
	frameworkSources := map[string][]byte{}
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
		case d.Type()&os.ModeSymlink != 0:
			return nil
		case d.IsDir() || shouldIgnoreFile(d.Name()) || (!isPHPRelationFile(rel) && !isPHPFrameworkRelationFile(rel)):
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
		if !isPHPRelationFile(rel) {
			frameworkSources[rel] = b
			return nil
		}
		file := parsePHPFileRelations(path, rel, string(b))
		files = append(files, file)
		filesByRel[file.rel] = file
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	composer := readComposerAutoload(rootAbs)
	resolver := buildComposerResolver(rootAbs, composer, filesByRel)
	classFiles := map[string]string{}
	functionFiles := map[string]string{}
	for _, file := range files {
		if _, excluded := resolver.excluded[file.rel]; excluded {
			continue
		}
		for _, class := range file.declared {
			key := strings.ToLower(class)
			if _, exists := classFiles[key]; !exists {
				classFiles[key] = file.rel
			}
		}
		for _, function := range file.functions {
			key := strings.ToLower(function)
			if _, exists := functionFiles[key]; !exists {
				functionFiles[key] = file.rel
			}
		}
	}
	for _, rel := range composerAutoloadFiles(rootAbs, composer.files, ignored) {
		addPHPAutoloadEdge(g, "composer.json", rel)
	}
	for _, file := range files {
		includeSpecifiers := file.includes
		if !file.parsed {
			includeSpecifiers = parsePHPIncludeSpecifiers(file.source)
		}
		for _, spec := range includeSpecifiers {
			if rel := resolvePHPIncludeToRel(rootAbs, file.abs, spec); rel != "" {
				addPHPImportEdge(g, file.rel, rel)
			}
		}
		importClasses := file.imports
		if !file.parsed {
			for _, class := range file.uses {
				importClasses = appendUnique(importClasses, class)
			}
		}
		for _, class := range importClasses {
			if rel := resolvePHPClassToRel(class, classFiles, resolver); rel != "" {
				addPHPImportEdge(g, file.rel, rel)
			}
		}
		for _, class := range referencedPHPClasses(file) {
			if rel := resolvePHPClassToRel(class, classFiles, resolver); rel != "" {
				addPHPReferenceEdge(g, file.rel, rel)
			}
		}
		for _, rel := range laravelConventionReferences(rootAbs, file.source) {
			addPHPReferenceEdge(g, file.rel, rel)
		}
		for _, rel := range symfonyPHPTemplateReferences(rootAbs, file.rel, []byte(file.source)) {
			addPHPReferenceEdge(g, file.rel, rel)
		}
		for _, rel := range drupalPHPTemplateReferences(rootAbs, file.rel, []byte(file.source)) {
			addPHPReferenceEdge(g, file.rel, rel)
		}
		for _, rel := range laravelBladeTemplateReferences(rootAbs, file.rel, []byte(file.source)) {
			addPHPReferenceEdge(g, file.rel, rel)
		}
		for _, rel := range wordpressTemplatePartReferences(rootAbs, file.rel, []byte(file.source)) {
			addPHPReferenceEdge(g, file.rel, rel)
		}
		for _, callback := range wordpressHookCallbacks([]byte(file.source)) {
			if callback.Receiver != "" {
				continue // class receivers are already parser-backed references
			}
			if rel := functionFiles[strings.ToLower(callback.Callback)]; rel != "" {
				addPHPReferenceEdge(g, file.rel, rel)
			}
		}
	}
	for rel, source := range frameworkSources {
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".twig":
			for _, target := range twigTemplateReferences(rootAbs, rel, source) {
				addPHPReferenceEdge(g, rel, target)
			}
		case ".yaml", ".yml":
			resolve := func(class string) string {
				return resolvePHPClassToRel(class, classFiles, resolver)
			}
			for _, target := range yamlPHPClassReferences(rootAbs, source, resolve) {
				addPHPReferenceEdge(g, rel, target)
			}
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

func addPHPAutoloadEdge(g *importGraph, from, to string) {
	g.autoloads[from] = appendUnique(g.autoloads[from], to)
	g.autoloadedBy[to] = appendUnique(g.autoloadedBy[to], from)
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
	file := phpFileRelations{abs: abs, rel: rel, source: source, masked: masked, uses: map[string]string{}}
	analysis, err := parsing.Analyze(rel, []byte(source))
	if err == nil && analysis.Language == "php" {
		file.parsed = true
		file.namespace = normalizePHPClass(analysis.PackageName)
		for _, relation := range analysis.Relations {
			switch relation.Kind {
			case parsing.RelationDeclaration:
				file.declared = appendUnique(file.declared, normalizePHPClass(relation.Name))
			case parsing.RelationFunction:
				file.functions = appendUnique(file.functions, normalizePHPClass(relation.Name))
			case parsing.RelationImport:
				class := normalizePHPClass(relation.Name)
				if class != "" {
					file.imports = appendUnique(file.imports, class)
				}
			case parsing.RelationTraitUse:
				file.traitUses = appendUnique(file.traitUses, relation.Name)
			case parsing.RelationReference:
				file.references = appendUnique(file.references, relation.Name)
			case parsing.RelationInclude:
				file.includes = appendUnique(file.includes, relation.Name)
			}
		}
		return file
	}
	return parsePHPFileRelationsFallback(file)
}

func parsePHPFileRelationsFallback(file phpFileRelations) phpFileRelations {
	source := file.source
	masked := file.masked
	starts, depths := outlineLineLayout([]byte(masked))
	lines := splitOutlineLines([]byte(source), starts)
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
		if match := phpFunctionRe.FindStringSubmatch(line); match != nil {
			file.functions = appendUnique(file.functions, qualifyPHPClass(file.namespace, match[1]))
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
	if file.parsed {
		out := []string{}
		for _, name := range append(append([]string{}, file.traitUses...), file.references...) {
			if class := normalizePHPClass(name); class != "" {
				out = appendUnique(out, class)
			}
		}
		return out
	}
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
	if strings.HasPrefix(strings.ToLower(name), "namespace\\") {
		return qualifyPHPClass(file.namespace, name[len("namespace\\"):])
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

func resolvePHPClassToRel(class string, classFiles map[string]string, resolver *composerResolver) string {
	class = normalizePHPClass(class)
	if resolver != nil {
		if rel, claimed := resolver.resolveClass(class); rel != "" || claimed {
			return rel
		}
	}
	return classFiles[strings.ToLower(class)]
}

func isPHPRelationFile(path string) bool {
	return parsing.IsPHPPath(path)
}

func isPHPFrameworkRelationFile(path string) bool {
	switch strings.ToLower(filepath.Ext(filepath.ToSlash(filepath.Clean(path)))) {
	case ".twig", ".yaml", ".yml":
		return true
	default:
		return false
	}
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
