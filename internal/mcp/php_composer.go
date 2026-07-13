package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"memento-mcp/internal/gitstate"
)

type composerAutoloadSection struct {
	PSR4                map[string]json.RawMessage `json:"psr-4"`
	PSR0                map[string]json.RawMessage `json:"psr-0"`
	Classmap            []string                   `json:"classmap"`
	Files               []string                   `json:"files"`
	ExcludeFromClassmap []string                   `json:"exclude-from-classmap"`
}

type composerManifest struct {
	Autoload    composerAutoloadSection `json:"autoload"`
	AutoloadDev composerAutoloadSection `json:"autoload-dev"`
}

type composerAutoloadConfig struct {
	psr4                map[string][]string
	psr0                map[string][]string
	classmap            []string
	files               []string
	excludeFromClassmap []*regexp.Regexp
}

type composerResolver struct {
	files     map[string]phpFileRelations
	psr4      map[string][]string
	psr0      []composerPrefix
	psr4Empty []string
	psr0Empty []string
	classmap  map[string]string
	excluded  map[string]struct{}
}

type composerPrefix struct {
	prefix string
	dirs   []string
}

func readComposerAutoload(root string) composerAutoloadConfig {
	config := composerAutoloadConfig{
		psr4: map[string][]string{},
		psr0: map[string][]string{},
	}
	b, err := os.ReadFile(filepath.Join(root, "composer.json"))
	if err != nil {
		return config
	}
	var manifest composerManifest
	if json.Unmarshal(b, &manifest) != nil {
		return config
	}

	add := func(section composerAutoloadSection) {
		for prefix, raw := range section.PSR4 {
			prefix = strings.TrimLeft(strings.TrimSpace(prefix), "\\")
			if prefix != "" && !strings.HasSuffix(prefix, "\\") {
				continue
			}
			dirs := composerPathList(raw)
			if len(dirs) == 0 {
				continue
			}
			config.psr4[prefix] = append(config.psr4[prefix], dirs...)
		}
		for prefix, raw := range section.PSR0 {
			prefix = strings.TrimLeft(strings.TrimSpace(prefix), "\\")
			dirs := composerPathList(raw)
			if len(dirs) == 0 {
				continue
			}
			config.psr0[prefix] = append(config.psr0[prefix], dirs...)
		}
		config.classmap = append(config.classmap, section.Classmap...)
		config.files = append(config.files, section.Files...)
	}
	add(manifest.Autoload)
	add(manifest.AutoloadDev)

	for _, section := range []composerAutoloadSection{manifest.Autoload, manifest.AutoloadDev} {
		for _, pattern := range section.ExcludeFromClassmap {
			if compiled := compileComposerExclusion(pattern); compiled != nil {
				config.excludeFromClassmap = append(config.excludeFromClassmap, compiled)
			}
		}
	}
	return config
}

func composerPathList(raw json.RawMessage) []string {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var many []string
	if json.Unmarshal(raw, &many) != nil {
		return nil
	}
	return many
}

func buildComposerResolver(root string, config composerAutoloadConfig, files map[string]phpFileRelations) *composerResolver {
	resolver := &composerResolver{
		files:    files,
		psr4:     map[string][]string{},
		classmap: map[string]string{},
		excluded: map[string]struct{}{},
	}
	for prefix, dirs := range config.psr4 {
		if prefix == "" {
			resolver.psr4Empty = append(resolver.psr4Empty, dirs...)
			continue
		}
		resolver.psr4[prefix] = dirs
	}
	for prefix, dirs := range config.psr0 {
		if prefix == "" {
			resolver.psr0Empty = append(resolver.psr0Empty, dirs...)
			continue
		}
		resolver.psr0 = append(resolver.psr0, composerPrefix{prefix: prefix, dirs: dirs})
	}
	// Composer krsort()s PSR-0 prefixes before registering them. Reverse lexical
	// order also puts a child prefix before its parent when both share a stem.
	sort.Slice(resolver.psr0, func(i, j int) bool { return resolver.psr0[i].prefix > resolver.psr0[j].prefix })

	for _, rel := range composerClassmapFiles(root, config.classmap, files) {
		if composerExcluded(rel, config.excludeFromClassmap) {
			resolver.excluded[rel] = struct{}{}
			continue
		}
		file := files[rel]
		for _, class := range file.declared {
			class = normalizePHPClass(class)
			if class != "" {
				if _, exists := resolver.classmap[class]; !exists {
					resolver.classmap[class] = rel
				}
			}
		}
	}

	return resolver
}

func (resolver *composerResolver) resolveClass(class string) (string, bool) {
	class = normalizePHPClass(class)
	if class == "" {
		return "", false
	}
	if rel := resolver.classmap[class]; rel != "" {
		return rel, true
	}
	rel, psr4Matched := resolver.resolvePSR4(class)
	if rel != "" {
		return rel, true
	}
	rel, psr0Matched := resolver.resolvePSR0(class)
	return rel, psr4Matched || psr0Matched
}

func (resolver *composerResolver) resolvePSR4(class string) (string, bool) {
	matched := false
	search := class
	for {
		index := strings.LastIndex(search, "\\")
		if index < 0 {
			break
		}
		prefix := class[:index+1]
		if dirs, ok := resolver.psr4[prefix]; ok {
			matched = true
			logical := strings.ReplaceAll(class[len(prefix):], "\\", "/") + ".php"
			if rel := resolver.resolveMappedFile(dirs, logical); rel != "" {
				return rel, true
			}
		}
		search = search[:index]
	}
	if len(resolver.psr4Empty) > 0 {
		matched = true
		logical := strings.ReplaceAll(class, "\\", "/") + ".php"
		if rel := resolver.resolveMappedFile(resolver.psr4Empty, logical); rel != "" {
			return rel, true
		}
	}
	return "", matched
}

func (resolver *composerResolver) resolvePSR0(class string) (string, bool) {
	logical := composerPSR0LogicalPath(class)
	matched := false
	for _, mapping := range resolver.psr0 {
		if !strings.HasPrefix(class, mapping.prefix) {
			continue
		}
		matched = true
		if rel := resolver.resolveMappedFile(mapping.dirs, logical); rel != "" {
			return rel, true
		}
	}
	if len(resolver.psr0Empty) > 0 {
		matched = true
		if rel := resolver.resolveMappedFile(resolver.psr0Empty, logical); rel != "" {
			return rel, true
		}
	}
	return "", matched
}

func composerPSR0LogicalPath(class string) string {
	if index := strings.LastIndex(class, "\\"); index >= 0 {
		namespace := strings.ReplaceAll(class[:index+1], "\\", "/")
		short := strings.ReplaceAll(class[index+1:], "_", "/")
		return namespace + short + ".php"
	}
	return strings.ReplaceAll(class, "_", "/") + ".php"
}

func (resolver *composerResolver) resolveMappedFile(dirs []string, logical string) string {
	for _, dir := range dirs {
		rel, ok := composerJoinRel(dir, logical)
		if !ok {
			continue
		}
		if _, exists := resolver.files[rel]; exists {
			return rel
		}
	}
	return ""
}

func composerClassmapFiles(root string, entries []string, files map[string]phpFileRelations) []string {
	allFiles := make([]string, 0, len(files))
	for rel := range files {
		if isComposerClassmapFile(rel) {
			allFiles = append(allFiles, rel)
		}
	}
	sort.Strings(allFiles)

	out := []string{}
	seen := map[string]struct{}{}
	for _, entry := range entries {
		pattern, ok := composerSafePattern(entry)
		if !ok {
			continue
		}
		absPattern := filepath.Join(root, filepath.FromSlash(pattern))
		matches := []string{absPattern}
		if strings.Contains(pattern, "*") {
			matches, _ = filepath.Glob(absPattern)
			sort.Strings(matches)
		}
		for _, match := range matches {
			info, err := os.Lstat(match)
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			matchRel, err := filepath.Rel(root, match)
			if err != nil {
				continue
			}
			matchRel = filepath.ToSlash(filepath.Clean(matchRel))
			if info.IsDir() {
				prefix := ""
				if matchRel != "." {
					prefix = strings.TrimSuffix(matchRel, "/") + "/"
				}
				for _, rel := range allFiles {
					if strings.HasPrefix(rel, prefix) {
						if _, exists := seen[rel]; !exists {
							seen[rel] = struct{}{}
							out = append(out, rel)
						}
					}
				}
				continue
			}
			if _, exists := files[matchRel]; !exists || !isComposerClassmapFile(matchRel) {
				continue
			}
			if _, exists := seen[matchRel]; !exists {
				seen[matchRel] = struct{}{}
				out = append(out, matchRel)
			}
		}
	}
	return out
}

func isComposerClassmapFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".php", ".inc":
		return true
	default:
		return false
	}
}

func composerSafePattern(value string) (string, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		return ".", true
	}
	if strings.HasPrefix(value, "/") || looksLikeWindowsAbsolutePath(value) || strings.ContainsAny(value, "?[") {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

func composerJoinRel(base, suffix string) (string, bool) {
	base, ok := composerSafeRelativePath(base, true)
	if !ok {
		return "", false
	}
	suffix, ok = composerSafeRelativePath(suffix, false)
	if !ok {
		return "", false
	}
	rel := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.FromSlash(base), filepath.FromSlash(suffix))))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

func composerSafeRelativePath(value string, allowEmpty bool) (string, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		return "", allowEmpty
	}
	if strings.HasPrefix(value, "/") || looksLikeWindowsAbsolutePath(value) {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

func compileComposerExclusion(value string) *regexp.Regexp {
	value = strings.Trim(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")), "/")
	if value == "" {
		return nil
	}
	parts := strings.Split(value, "/")
	var pattern strings.Builder
	pattern.WriteString("^")
	for index, part := range parts {
		if index > 0 {
			pattern.WriteString("/")
		}
		for offset := 0; offset < len(part); {
			if part[offset] == '*' {
				if offset+1 < len(part) && part[offset+1] == '*' {
					pattern.WriteString(".+?")
					offset += 2
					continue
				}
				pattern.WriteString("[^/]+?")
				offset++
				continue
			}
			start := offset
			for offset < len(part) && part[offset] != '*' {
				offset++
			}
			pattern.WriteString(regexp.QuoteMeta(part[start:offset]))
		}
	}
	pattern.WriteString("(?:$|/)")
	compiled, err := regexp.Compile(pattern.String())
	if err != nil {
		return nil
	}
	return compiled
}

func composerExcluded(rel string, patterns []*regexp.Regexp) bool {
	rel = strings.Trim(filepath.ToSlash(filepath.Clean(rel)), "/")
	for _, pattern := range patterns {
		if pattern.MatchString(rel) {
			return true
		}
	}
	return false
}

func composerAutoloadFiles(root string, entries []string, ignored *gitstate.IgnoredPaths) []string {
	out := []string{}
	for _, entry := range entries {
		rel, ok := composerSafeRelativePath(entry, false)
		if !ok || ignored.Matches(rel) || pathHasIgnoredDirectory(rel) {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Lstat(abs)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		out = appendUnique(out, rel)
	}
	return out
}

func pathHasIgnoredDirectory(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts[:max(0, len(parts)-1)] {
		if shouldIgnoreDir(part) {
			return true
		}
	}
	return false
}
