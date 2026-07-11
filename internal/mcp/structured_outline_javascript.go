package mcp

import (
	"regexp"
	"strings"
)

var (
	jsFunctionDeclRe = regexp.MustCompile(`^\s*(?:(?:export|default|declare|async)\s+)*function\s*\*?\s*([A-Za-z_$][A-Za-z0-9_$]*)`)
	jsTypeDeclRe     = regexp.MustCompile(`^\s*(?:(?:export|default|declare|abstract)\s+)*(class|interface|type|enum)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	jsVariableDeclRe = regexp.MustCompile(`^\s*(?:(?:export|default|declare)\s+)*(const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	jsMethodDeclRe   = regexp.MustCompile(`^\s*(?:(?:public|protected|private|static|readonly|abstract|override|async|get|set)\s+)*(constructor|#?[A-Za-z_$][A-Za-z0-9_$]*)\s*(?:<[^>]*>)?\s*\(`)
	jsPropertyDeclRe = regexp.MustCompile(`^\s*(?:(?:public|protected|private|static|readonly|declare|abstract|override)\s+)*(#?[A-Za-z_$][A-Za-z0-9_$]*)(?:[!?])?(?:\s*:[^=;]+)?(?:\s*=.*)?;\s*$`)
	jsModuleRe       = regexp.MustCompile(`(?m)(?:\bfrom\s+|\bimport\s+|\brequire\s*\(\s*)["']([^"']+)["']`)
)

func structuredJSOutline(path string, source []byte) structuredFileOutline {
	language := languageForStructuredOutline(path)
	raw := source
	masked := maskJSNonCode(raw)
	starts, depths := outlineLineLayout(masked)
	lines := splitOutlineLines(raw, starts)
	maskedLines := splitOutlineLines(masked, starts)
	out := structuredFileOutline{
		Language: language,
		Imports:  structuredJSModules(lines, maskedLines, depths),
		Header:   []string{},
		Symbols:  []outlineSymbol{},
	}

	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		if depths[lineIndex] != 0 {
			continue
		}
		line := lines[lineIndex]
		start := starts[lineIndex]
		if match := jsFunctionDeclRe.FindStringSubmatch(line); match != nil {
			signature, endOffset, _ := outlineHeaderBeforeBody(raw, masked, start, len(raw))
			out.Symbols = append(out.Symbols, outlineSymbol{
				Name:          match[1],
				Kind:          "function",
				Signature:     signature,
				Documentation: leadingOutlineDocumentation(lines, lineIndex),
				StartLine:     lineIndex + 1,
				EndLine:       outlineLineForOffset(starts, endOffset) + 1,
			})
			lineIndex = outlineLineForOffset(starts, endOffset)
			continue
		}
		if match := jsTypeDeclRe.FindStringSubmatch(line); match != nil {
			kind, name := match[1], match[2]
			if kind == "class" {
				signature, endOffset, openBrace := outlineHeaderBeforeBody(raw, masked, start, len(raw))
				out.Symbols = append(out.Symbols, outlineSymbol{
					Name:          name,
					Kind:          kind,
					Signature:     signature,
					Documentation: leadingOutlineDocumentation(lines, lineIndex),
					StartLine:     lineIndex + 1,
					EndLine:       outlineLineForOffset(starts, endOffset) + 1,
				})
				if openBrace >= 0 {
					if closeBrace := matchingOutlineBrace(masked, openBrace); closeBrace >= 0 {
						out.Symbols = append(out.Symbols, structuredJSClassMembers(raw, masked, starts, depths, lines, name, openBrace, closeBrace)...)
						lineIndex = outlineLineForOffset(starts, closeBrace)
					}
				}
				continue
			}
			signature, endOffset := outlineStructuralDeclaration(raw, masked, start, len(raw))
			out.Symbols = append(out.Symbols, outlineSymbol{
				Name:          name,
				Kind:          kind,
				Signature:     signature,
				Documentation: leadingOutlineDocumentation(lines, lineIndex),
				StartLine:     lineIndex + 1,
				EndLine:       outlineLineForOffset(starts, endOffset) + 1,
			})
			lineIndex = outlineLineForOffset(starts, endOffset)
			continue
		}
		if match := jsVariableDeclRe.FindStringSubmatch(line); match != nil {
			signature, endOffset, _ := outlineHeaderBeforeBody(raw, masked, start, len(raw))
			kind := "variable"
			if arrow := strings.Index(signature, "=>"); arrow >= 0 {
				kind = "function"
				signature = strings.TrimSpace(signature[:arrow+2])
			} else if assignment := strings.Index(signature, "="); assignment >= 0 {
				signature = strings.TrimSpace(signature[:assignment])
			}
			out.Symbols = append(out.Symbols, outlineSymbol{
				Name:          match[2],
				Kind:          kind,
				Signature:     signature,
				Documentation: leadingOutlineDocumentation(lines, lineIndex),
				StartLine:     lineIndex + 1,
				EndLine:       outlineLineForOffset(starts, endOffset) + 1,
			})
			lineIndex = outlineLineForOffset(starts, endOffset)
		}
	}
	if len(out.Symbols) == 0 {
		fallback := structuredGenericOutline(path, source)
		fallback.Imports = out.Imports
		return fallback
	}
	return out
}

func maskJSNonCode(source []byte) []byte {
	masked := maskNonCode(source, false)
	for index := 0; index < len(source); index++ {
		if source[index] != '/' || masked[index] != '/' || !jsRegexCanStart(masked, index) {
			continue
		}
		inClass := false
		escaped := false
		for end := index + 1; end < len(source) && source[end] != '\n'; end++ {
			masked[end] = ' '
			if escaped {
				escaped = false
				continue
			}
			if source[end] == '\\' {
				escaped = true
				continue
			}
			if source[end] == '[' {
				inClass = true
				continue
			}
			if source[end] == ']' {
				inClass = false
				continue
			}
			if source[end] == '/' && !inClass {
				masked[index] = ' '
				for end++; end < len(source) && ((source[end] >= 'a' && source[end] <= 'z') || (source[end] >= 'A' && source[end] <= 'Z')); end++ {
					masked[end] = ' '
				}
				index = end - 1
				break
			}
		}
	}
	return masked
}

func jsRegexCanStart(masked []byte, slash int) bool {
	previous := slash - 1
	for previous >= 0 && (masked[previous] == ' ' || masked[previous] == '\t' || masked[previous] == '\r' || masked[previous] == '\n') {
		previous--
	}
	if previous < 0 || strings.ContainsRune("=(:,[!&|?;{}", rune(masked[previous])) {
		return true
	}
	if (masked[previous] >= 'A' && masked[previous] <= 'Z') || (masked[previous] >= 'a' && masked[previous] <= 'z') {
		start := previous
		for start > 0 && ((masked[start-1] >= 'A' && masked[start-1] <= 'Z') || (masked[start-1] >= 'a' && masked[start-1] <= 'z')) {
			start--
		}
		switch string(masked[start : previous+1]) {
		case "await", "case", "delete", "in", "instanceof", "of", "return", "throw", "typeof", "void", "yield":
			return true
		}
	}
	return false
}

func structuredJSModules(lines, maskedLines []string, depths []int) []string {
	modules := []string{}
	for index, line := range lines {
		if depths[index] != 0 || strings.TrimSpace(maskedLines[index]) == "" {
			continue
		}
		for _, match := range jsModuleRe.FindAllStringSubmatch(line, -1) {
			if len(match) > 1 {
				modules = appendUniqueOutlineValue(modules, match[1])
			}
		}
	}
	return modules
}

func structuredJSClassMembers(raw, masked []byte, starts, depths []int, lines []string, className string, openBrace, closeBrace int) []outlineSymbol {
	baseDepth := depths[outlineLineForOffset(starts, openBrace)] + 1
	startLine := outlineLineForOffset(starts, openBrace) + 1
	endLine := outlineLineForOffset(starts, closeBrace)
	out := []outlineSymbol{}
	for lineIndex := startLine; lineIndex < endLine; lineIndex++ {
		if depths[lineIndex] != baseDepth {
			continue
		}
		line := lines[lineIndex]
		start := starts[lineIndex]
		if match := jsMethodDeclRe.FindStringSubmatch(line); match != nil {
			signature, endOffset, _ := outlineHeaderBeforeBody(raw, masked, start, closeBrace)
			out = append(out, outlineSymbol{
				Name:          match[1],
				Kind:          "method",
				Signature:     signature,
				Documentation: leadingOutlineDocumentation(lines, lineIndex),
				Container:     className,
				StartLine:     lineIndex + 1,
				EndLine:       outlineLineForOffset(starts, endOffset) + 1,
			})
			lineIndex = outlineLineForOffset(starts, endOffset)
			continue
		}
		if match := jsPropertyDeclRe.FindStringSubmatch(line); match != nil {
			signature := strings.TrimSpace(line)
			if assignment := strings.Index(signature, "="); assignment >= 0 {
				signature = strings.TrimSpace(signature[:assignment])
			}
			signature = strings.TrimSuffix(signature, ";")
			out = append(out, outlineSymbol{
				Name:          match[1],
				Kind:          "property",
				Signature:     signature,
				Documentation: leadingOutlineDocumentation(lines, lineIndex),
				Container:     className,
				StartLine:     lineIndex + 1,
				EndLine:       lineIndex + 1,
			})
		}
	}
	return out
}
