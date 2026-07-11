package mcp

import (
	"regexp"
	"sort"
	"strings"
)

var (
	phpNamespaceRe = regexp.MustCompile(`^\s*namespace\s+([^;]+);`)
	phpUseRe       = regexp.MustCompile(`^\s*use\s+([^;]+);`)
	phpTypeDeclRe  = regexp.MustCompile(`^\s*(?:(?:abstract|final|readonly)\s+)*(class|interface|trait|enum)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	phpFunctionRe  = regexp.MustCompile(`^\s*(?:(?:public|protected|private|static|final|abstract)\s+)*function\s*&?\s*([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	phpPropertyRe  = regexp.MustCompile(`^\s*(?:(?:public|protected|private|static|readonly|var)\s+)+(?:[?\\A-Za-z_][\\A-Za-z0-9_|&?]*\s+)?\$([A-Za-z_][A-Za-z0-9_]*)`)
	phpConstRe     = regexp.MustCompile(`^\s*(?:(?:public|protected|private|final)\s+)*const\s+(?:[?\\A-Za-z_][\\A-Za-z0-9_|&?]*\s+)?([A-Za-z_][A-Za-z0-9_]*)`)
)

func structuredPHPOutline(source []byte) structuredFileOutline {
	raw := source
	masked := maskPHPNonCode(raw)
	starts, depths := outlineLineLayout(masked)
	lines := splitOutlineLines(raw, starts)
	maskedLines := splitOutlineLines(masked, starts)
	out := structuredFileOutline{
		Language: "php",
		Imports:  []string{},
		Header:   []string{},
		Symbols:  []outlineSymbol{},
	}

	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		if depths[lineIndex] != 0 {
			continue
		}
		line := lines[lineIndex]
		maskedLine := strings.ToLower(maskedLines[lineIndex])
		if strings.Contains(maskedLine, "require") || strings.Contains(maskedLine, "include") {
			for _, include := range parsePHPIncludeSpecifiers(line) {
				out.Imports = appendUniqueOutlineValue(out.Imports, include)
			}
		}
		if match := phpNamespaceRe.FindStringSubmatch(line); match != nil {
			out.PackageName = strings.TrimSpace(match[1])
			continue
		}
		if match := phpUseRe.FindStringSubmatch(line); match != nil {
			out.Imports = appendUniqueOutlineValue(out.Imports, strings.TrimSpace(match[1]))
			continue
		}
		start := starts[lineIndex]
		if match := phpTypeDeclRe.FindStringSubmatch(line); match != nil {
			kind, name := match[1], match[2]
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
					out.Symbols = append(out.Symbols, structuredPHPClassMembers(raw, masked, starts, depths, lines, name, openBrace, closeBrace)...)
					lineIndex = outlineLineForOffset(starts, closeBrace)
				}
			}
			continue
		}
		if match := phpFunctionRe.FindStringSubmatch(line); match != nil {
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
		}
	}
	sort.Strings(out.Imports)
	if len(out.Symbols) == 0 {
		fallback := structuredGenericOutline("fallback.php", source)
		fallback.PackageName = out.PackageName
		fallback.Imports = out.Imports
		return fallback
	}
	return out
}

var phpHeredocStartRe = regexp.MustCompile(`<<<\s*['\"]?([A-Za-z_][A-Za-z0-9_]*)['\"]?`)

func maskPHPNonCode(source []byte) []byte {
	masked := maskNonCode(source, true)
	starts, _ := outlineLineLayout(masked)
	lines := splitOutlineLines(source, starts)
	maskedLines := splitOutlineLines(masked, starts)
	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		match := phpHeredocStartRe.FindStringSubmatch(maskedLines[lineIndex])
		if match == nil {
			continue
		}
		for contentLine := lineIndex + 1; contentLine < len(lines); contentLine++ {
			trimmed := strings.TrimSpace(lines[contentLine])
			for offset := starts[contentLine]; offset < starts[contentLine]+len(lines[contentLine]); offset++ {
				masked[offset] = ' '
			}
			if trimmed == match[1] || trimmed == match[1]+";" {
				lineIndex = contentLine
				break
			}
		}
	}
	return masked
}

func structuredPHPClassMembers(raw, masked []byte, starts, depths []int, lines []string, className string, openBrace, closeBrace int) []outlineSymbol {
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
		if match := phpFunctionRe.FindStringSubmatch(line); match != nil {
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
		if match := phpPropertyRe.FindStringSubmatch(line); match != nil {
			signature := stripOutlineInitializer(strings.TrimSpace(line))
			out = append(out, outlineSymbol{Name: match[1], Kind: "property", Signature: signature, Documentation: leadingOutlineDocumentation(lines, lineIndex), Container: className, StartLine: lineIndex + 1, EndLine: lineIndex + 1})
			continue
		}
		if match := phpConstRe.FindStringSubmatch(line); match != nil {
			signature := stripOutlineInitializer(strings.TrimSpace(line))
			out = append(out, outlineSymbol{Name: match[1], Kind: "const", Signature: signature, Documentation: leadingOutlineDocumentation(lines, lineIndex), Container: className, StartLine: lineIndex + 1, EndLine: lineIndex + 1})
		}
	}
	return out
}
