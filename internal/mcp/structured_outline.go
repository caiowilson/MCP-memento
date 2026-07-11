package mcp

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"memento-mcp/internal/redact"
)

type outlineSymbol struct {
	Name                   string `json:"name"`
	Kind                   string `json:"kind"`
	Signature              string `json:"signature"`
	Documentation          string `json:"documentation,omitempty"`
	DocumentationTruncated bool   `json:"documentationTruncated,omitempty"`
	Container              string `json:"container,omitempty"`
	StartLine              int    `json:"startLine"`
	EndLine                int    `json:"endLine"`
}

type structuredFileOutline struct {
	Language    string          `json:"language"`
	PackageName string          `json:"package,omitempty"`
	Imports     []string        `json:"imports"`
	Header      []string        `json:"header"`
	Symbols     []outlineSymbol `json:"symbols"`
	Fallback    bool            `json:"fallback"`
}

func extractStructuredFileOutline(path string, source []byte) structuredFileOutline {
	switch languageForStructuredOutline(path) {
	case "go":
		if outline, err := structuredGoOutline(path, source); err == nil {
			return outline
		}
	case "javascript", "typescript":
		return structuredJSOutline(path, source)
	case "php":
		return structuredPHPOutline(source)
	}
	return structuredGenericOutline(path, source)
}

func languageForStructuredOutline(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	case ".php":
		return "php"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".cs":
		return "csharp"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "cpp"
	default:
		return "text"
	}
}

var genericStructuredDeclRe = regexp.MustCompile(`(?i)^\s*(?:pub\s+)?(fn|func|function|def|class|interface|struct|enum|module|trait|impl|type|object)\s+([A-Za-z_][A-Za-z0-9_]*)`)

func structuredGenericOutline(path string, source []byte) structuredFileOutline {
	language := languageForStructuredOutline(path)
	lines := strings.Split(string(source), "\n")
	out := structuredFileOutline{Language: language, Imports: []string{}, Header: []string{}, Symbols: []outlineSymbol{}, Fallback: true}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		out.Header = append(out.Header, safeOutlineHeader(trimmed))
		if genericStructuredDeclRe.MatchString(line) || len(out.Header) == 3 {
			break
		}
	}
	for index, line := range lines {
		match := genericStructuredDeclRe.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		signature := strings.TrimSpace(line)
		if brace := strings.Index(signature, "{"); brace >= 0 {
			signature = strings.TrimSpace(signature[:brace])
		}
		if (strings.EqualFold(match[1], "def") || strings.EqualFold(match[1], "class")) && strings.Contains(signature, ":") {
			colon := strings.LastIndex(signature, ":")
			signature = strings.TrimSpace(signature[:colon+1])
		}
		out.Symbols = append(out.Symbols, outlineSymbol{
			Name:      match[2],
			Kind:      strings.ToLower(match[1]),
			Signature: signature,
			StartLine: index + 1,
			EndLine:   index + 1,
		})
	}
	return out
}

func safeOutlineHeader(line string) string {
	if brace := strings.Index(line, "{"); brace >= 0 {
		line = strings.TrimSpace(line[:brace])
	}
	if match := genericStructuredDeclRe.FindStringSubmatch(line); match != nil && (strings.EqualFold(match[1], "def") || strings.EqualFold(match[1], "class")) {
		if colon := strings.LastIndex(line, ":"); colon >= 0 {
			line = strings.TrimSpace(line[:colon+1])
		}
	}
	line, _ = truncateStringBytes(line, 240)
	return line
}

func maskNonCode(source []byte, hashComments bool) []byte {
	masked := append([]byte(nil), source...)
	inLineComment := false
	inBlockComment := false
	var quote byte
	escaped := false
	for index := 0; index < len(source); index++ {
		current := source[index]
		if inLineComment {
			if current == '\n' {
				inLineComment = false
				masked[index] = '\n'
			} else {
				masked[index] = ' '
			}
			continue
		}
		if inBlockComment {
			if current == '\n' {
				masked[index] = '\n'
			} else {
				masked[index] = ' '
			}
			if current == '*' && index+1 < len(source) && source[index+1] == '/' {
				masked[index+1] = ' '
				index++
				inBlockComment = false
			}
			continue
		}
		if quote != 0 {
			if current == '\n' {
				masked[index] = '\n'
			} else {
				masked[index] = ' '
			}
			if escaped {
				escaped = false
				continue
			}
			if current == '\\' {
				escaped = true
				continue
			}
			if current == quote {
				quote = 0
			}
			continue
		}
		if current == '/' && index+1 < len(source) && source[index+1] == '/' {
			masked[index], masked[index+1] = ' ', ' '
			index++
			inLineComment = true
			continue
		}
		if current == '/' && index+1 < len(source) && source[index+1] == '*' {
			masked[index], masked[index+1] = ' ', ' '
			index++
			inBlockComment = true
			continue
		}
		if hashComments && current == '#' {
			masked[index] = ' '
			inLineComment = true
			continue
		}
		if current == '\'' || current == '"' || current == '`' {
			quote = current
			masked[index] = ' '
		}
	}
	return masked
}

func outlineLineLayout(masked []byte) ([]int, []int) {
	starts := []int{0}
	depths := []int{0}
	depth := 0
	for index, value := range masked {
		switch value {
		case '{':
			if !outlineBraceEscaped(masked, index) {
				depth++
			}
		case '}':
			if !outlineBraceEscaped(masked, index) && depth > 0 {
				depth--
			}
		case '\n':
			if index+1 < len(masked) {
				starts = append(starts, index+1)
				depths = append(depths, depth)
			}
		}
	}
	return starts, depths
}

func splitOutlineLines(source []byte, starts []int) []string {
	lines := make([]string, len(starts))
	for index, start := range starts {
		end := len(source)
		if index+1 < len(starts) {
			end = starts[index+1] - 1
		}
		lines[index] = string(bytes.TrimSuffix(source[start:end], []byte{'\r'}))
	}
	return lines
}

func outlineHeaderBeforeBody(raw, masked []byte, start, limit int) (string, int, int) {
	if limit > len(raw) {
		limit = len(raw)
	}
	parentheses := 0
	brackets := 0
	for index := start; index < limit; index++ {
		switch masked[index] {
		case '(':
			parentheses++
		case ')':
			if parentheses > 0 {
				parentheses--
			}
		case '[':
			brackets++
		case ']':
			if brackets > 0 {
				brackets--
			}
		case '{':
			if !outlineBraceEscaped(masked, index) {
				if parentheses > 0 || brackets > 0 || outlineBraceStartsStructuralType(masked, start, index) {
					if closeBrace := matchingOutlineBrace(masked, index); closeBrace >= 0 && closeBrace < limit {
						index = closeBrace
						continue
					}
				}
				return normalizeOutlineSignature(string(raw[start:index])), index, index
			}
		case ';':
			return normalizeOutlineSignature(string(raw[start : index+1])), index, -1
		}
	}
	end := limit
	if newline := bytes.IndexByte(raw[start:limit], '\n'); newline >= 0 {
		end = start + newline
	}
	return normalizeOutlineSignature(string(raw[start:end])), end, -1
}

func outlineBraceStartsStructuralType(masked []byte, start, brace int) bool {
	for index := brace - 1; index >= start; index-- {
		switch masked[index] {
		case ' ', '\t', '\r', '\n':
			continue
		case ':', '=', '|', '&', ',', '(', '[', '<':
			return true
		default:
			return false
		}
	}
	return false
}

func outlineStructuralDeclaration(raw, masked []byte, start, limit int) (string, int) {
	if limit > len(raw) {
		limit = len(raw)
	}
	openBrace := -1
	semicolon := -1
	for index := start; index < limit; index++ {
		if masked[index] == '{' && !outlineBraceEscaped(masked, index) {
			openBrace = index
			break
		}
		if masked[index] == ';' {
			semicolon = index
			break
		}
	}
	if openBrace >= 0 {
		if closeBrace := matchingOutlineBrace(masked, openBrace); closeBrace >= 0 && closeBrace < limit {
			end := closeBrace + 1
			for end < limit && (masked[end] == ' ' || masked[end] == '\t' || masked[end] == '\r') {
				end++
			}
			if end < limit && masked[end] == ';' {
				end++
			}
			return normalizeOutlineSignature(string(raw[start:end])), end - 1
		}
	}
	if semicolon >= 0 {
		return normalizeOutlineSignature(string(raw[start : semicolon+1])), semicolon
	}
	return outlineHeaderBeforeBodyNoOpen(raw, start, limit)
}

func outlineHeaderBeforeBodyNoOpen(raw []byte, start, limit int) (string, int) {
	end := limit
	if newline := bytes.IndexByte(raw[start:limit], '\n'); newline >= 0 {
		end = start + newline
	}
	return normalizeOutlineSignature(string(raw[start:end])), end
}

func matchingOutlineBrace(masked []byte, open int) int {
	depth := 0
	for index := open; index < len(masked); index++ {
		switch masked[index] {
		case '{':
			if !outlineBraceEscaped(masked, index) {
				depth++
			}
		case '}':
			if !outlineBraceEscaped(masked, index) {
				depth--
				if depth == 0 {
					return index
				}
			}
		}
	}
	return -1
}

func outlineBraceEscaped(value []byte, index int) bool {
	backslashes := 0
	for index--; index >= 0 && value[index] == '\\'; index-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func outlineLineForOffset(starts []int, offset int) int {
	if len(starts) == 0 || offset <= 0 {
		return 0
	}
	line := sort.Search(len(starts), func(index int) bool { return starts[index] > offset }) - 1
	if line < 0 {
		return 0
	}
	return line
}

func normalizeOutlineSignature(signature string) string {
	lines := strings.Split(signature, "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		line = strings.ReplaceAll(line, "\t", " ")
		if line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " ")
}

func leadingOutlineDocumentation(lines []string, declarationLine int) string {
	index := declarationLine - 1
	if index < 0 || strings.TrimSpace(lines[index]) == "" {
		return ""
	}
	trimmed := strings.TrimSpace(lines[index])
	if strings.HasPrefix(trimmed, "///") {
		start := index
		for start > 0 && strings.HasPrefix(strings.TrimSpace(lines[start-1]), "///") {
			start--
		}
		parts := make([]string, 0, index-start+1)
		for lineIndex := start; lineIndex <= index; lineIndex++ {
			parts = append(parts, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[lineIndex]), "///")))
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	if !strings.HasSuffix(trimmed, "*/") {
		return ""
	}
	start := index
	for start >= 0 && !strings.Contains(lines[start], "/**") {
		start--
	}
	if start < 0 {
		return ""
	}
	parts := make([]string, 0, index-start+1)
	for lineIndex := start; lineIndex <= index; lineIndex++ {
		part := strings.TrimSpace(lines[lineIndex])
		part = strings.TrimPrefix(part, "/**")
		part = strings.TrimSuffix(part, "*/")
		part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "*"))
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func stripOutlineInitializer(signature string) string {
	if assignment := strings.Index(signature, "="); assignment >= 0 {
		signature = strings.TrimSpace(signature[:assignment])
	}
	return strings.TrimSuffix(signature, ";")
}

func appendUniqueOutlineValue(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func redactStructuredFileOutline(outline *structuredFileOutline, redactor *redact.Redactor, includeDocumentation bool) {
	outline.PackageName = redactor.Redact(outline.PackageName)
	for index := range outline.Imports {
		outline.Imports[index] = redactor.Redact(outline.Imports[index])
	}
	for index := range outline.Header {
		outline.Header[index] = redactor.Redact(outline.Header[index])
	}
	for index := range outline.Symbols {
		symbol := &outline.Symbols[index]
		symbol.Name = redactor.Redact(symbol.Name)
		symbol.Signature = redactor.Redact(symbol.Signature)
		symbol.Container = redactor.Redact(symbol.Container)
		if includeDocumentation {
			symbol.Documentation = redactor.Redact(symbol.Documentation)
			symbol.Documentation, symbol.DocumentationTruncated = truncateStringBytes(symbol.Documentation, 2_048)
		} else {
			symbol.Documentation = ""
			symbol.DocumentationTruncated = false
		}
	}
}

func structuredOutlineContentBytes(outline structuredFileOutline) int {
	total := len(outline.PackageName)
	for _, value := range outline.Imports {
		total += len(value)
	}
	for _, value := range outline.Header {
		total += len(value)
	}
	for _, symbol := range outline.Symbols {
		total += len(symbol.Name) + len(symbol.Kind) + len(symbol.Signature) + len(symbol.Documentation) + len(symbol.Container)
	}
	return total
}

func validateStructuredOutline(outline structuredFileOutline) error {
	for index, symbol := range outline.Symbols {
		if symbol.Name == "" || symbol.Kind == "" || symbol.Signature == "" || symbol.StartLine < 1 || symbol.EndLine < symbol.StartLine {
			return fmt.Errorf("invalid outline symbol %d: %#v", index, symbol)
		}
	}
	return nil
}
