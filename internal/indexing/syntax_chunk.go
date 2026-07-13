package indexing

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
)

var jsTopLevelDeclaration = regexp.MustCompile(`^[\t ]*(?:(?:export|default|declare|async|abstract)\s+)*(?:function\s*\*?\s+[A-Za-z_$]|class\s+[A-Za-z_$]|interface\s+[A-Za-z_$]|type\s+[A-Za-z_$]|enum\s+[A-Za-z_$]|namespace\s+[A-Za-z_$]|module\s+[A-Za-z_$]|(?:const|let|var)\s+[A-Za-z_$])`)
var jsTopLevelExport = regexp.MustCompile(`^[\t ]*export\b`)
var jsModifierOnly = regexp.MustCompile(`^[\t ]*(?:export(?:\s+default)?|default|declare|abstract|async)[\t ]*;?[\t ]*$`)

func syntaxChunkStarts(path, language, source string, lines []string) ([]int, bool) {
	switch chunkLanguage(path, language) {
	case "go":
		return goChunkStarts(path, source)
	case "javascript":
		return jsChunkStarts([]byte(source), lines)
	default:
		return nil, false
	}
}

func chunkLanguage(path, language string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jsx", ".tsx":
		// JSX text needs a tag-aware lexer. Preserve the exact line fallback
		// until that grammar can be handled without false string/comment states.
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "go", "golang":
		return "go"
	case "ts/js", "javascript", "typescript", "js", "ts":
		return "javascript"
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".js", ".mjs", ".cjs", ".ts", ".mts", ".cts":
		return "javascript"
	default:
		return ""
	}
}

func goChunkStarts(path, source string) ([]int, bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil || file == nil || len(file.Decls) == 0 {
		return nil, false
	}
	starts := make([]int, 0, len(file.Decls))
	for _, declaration := range file.Decls {
		position := declaration.Pos()
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			if typed.Doc != nil {
				position = typed.Doc.Pos()
			}
		case *ast.GenDecl:
			if typed.Doc != nil {
				position = typed.Doc.Pos()
			}
		}
		starts = append(starts, fset.PositionFor(position, false).Line)
	}
	return starts, true
}

func jsChunkStarts(source []byte, lines []string) ([]int, bool) {
	masked, ok := maskJSChunkSource(source)
	if !ok {
		return nil, false
	}
	topLevel, ok := jsTopLevelLayout(masked, len(lines))
	if !ok {
		return nil, false
	}
	maskedLines := splitChunkLines(string(masked))
	if len(maskedLines) != len(lines) {
		return nil, false
	}
	starts := []int{}
	for index, line := range maskedLines {
		if index >= len(topLevel) || !topLevel[index] {
			continue
		}
		if !jsTopLevelDeclaration.MatchString(line) && !jsTopLevelExport.MatchString(line) {
			continue
		}
		starts = append(starts, leadingJSChunkBoundary(lines, maskedLines, topLevel, index+1))
	}
	if len(starts) == 0 {
		return nil, false
	}
	return normalizedChunkStarts(starts, len(lines)), true
}

func maskJSChunkSource(source []byte) ([]byte, bool) {
	masked := append([]byte(nil), source...)
	inLineComment := false
	inBlockComment := false
	inRegex := false
	inRegexClass := false
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
		if inRegex {
			if current == '\n' {
				return nil, false
			}
			masked[index] = ' '
			if escaped {
				escaped = false
				continue
			}
			if current == '\\' {
				escaped = true
				continue
			}
			if current == '[' {
				inRegexClass = true
				continue
			}
			if current == ']' {
				inRegexClass = false
				continue
			}
			if current == '/' && !inRegexClass {
				inRegex = false
				for index+1 < len(source) && isASCIIAlpha(source[index+1]) {
					index++
					masked[index] = ' '
				}
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
		if current == '/' && jsChunkRegexCanStart(masked, index) {
			masked[index] = ' '
			inRegex = true
			inRegexClass = false
			escaped = false
			continue
		}
		if current == '\'' || current == '"' || current == '`' {
			quote = current
			masked[index] = ' '
		}
	}
	if inBlockComment || inRegex || quote != 0 {
		return nil, false
	}
	return masked, true
}

func jsTopLevelLayout(masked []byte, lineCount int) ([]bool, bool) {
	topLevel := make([]bool, lineCount)
	if lineCount > 0 {
		topLevel[0] = true
	}
	braceDepth, parenDepth, bracketDepth := 0, 0, 0
	line := 0
	for _, current := range masked {
		switch current {
		case '{':
			braceDepth++
		case '}':
			braceDepth--
		case '(':
			parenDepth++
		case ')':
			parenDepth--
		case '[':
			bracketDepth++
		case ']':
			bracketDepth--
		case '\n':
			line++
			if line < len(topLevel) {
				topLevel[line] = braceDepth == 0 && parenDepth == 0 && bracketDepth == 0
			}
		}
		if braceDepth < 0 || parenDepth < 0 || bracketDepth < 0 {
			return nil, false
		}
	}
	if braceDepth != 0 || parenDepth != 0 || bracketDepth != 0 {
		return nil, false
	}
	return topLevel, true
}

func leadingJSChunkBoundary(lines, maskedLines []string, topLevel []bool, declarationLine int) int {
	boundary := declarationLine - 1
	for {
		previous := boundary
		for boundary > 0 && jsModifierOnly.MatchString(maskedLines[boundary-1]) {
			boundary--
		}
		for {
			index := boundary - 1
			for index >= 0 && !topLevel[index] {
				index--
			}
			if index < 0 || !strings.HasPrefix(strings.TrimSpace(maskedLines[index]), "@") {
				break
			}
			boundary = index
		}
		boundary = leadingJSCommentBoundary(lines, boundary)
		if boundary == previous {
			return boundary + 1
		}
	}
}

func leadingJSCommentBoundary(lines []string, boundary int) int {
	index := boundary - 1
	if index < 0 || strings.TrimSpace(lines[index]) == "" {
		return boundary
	}
	trimmed := strings.TrimSpace(lines[index])
	if strings.HasPrefix(trimmed, "//") {
		for index >= 0 && strings.HasPrefix(strings.TrimSpace(lines[index]), "//") {
			index--
		}
		return index + 1
	}
	if !strings.HasSuffix(trimmed, "*/") {
		return boundary
	}
	for index >= 0 {
		line := strings.TrimSpace(lines[index])
		if opener := strings.Index(lines[index], "/*"); opener >= 0 {
			if strings.TrimSpace(lines[index][:opener]) == "" {
				return index
			}
			break
		}
		if line == "" {
			break
		}
		index--
	}
	return boundary
}

func jsChunkRegexCanStart(masked []byte, slash int) bool {
	previous := slash - 1
	for previous >= 0 && (masked[previous] == ' ' || masked[previous] == '\t' || masked[previous] == '\r' || masked[previous] == '\n') {
		previous--
	}
	if previous < 0 || strings.ContainsRune("=(:,[!&|?;{}", rune(masked[previous])) {
		return true
	}
	if masked[previous] == '>' {
		before := previous - 1
		for before >= 0 && (masked[before] == ' ' || masked[before] == '\t') {
			before--
		}
		if before >= 0 && masked[before] == '=' {
			return true
		}
	}
	if isASCIIAlpha(masked[previous]) {
		start := previous
		for start > 0 && isASCIIAlpha(masked[start-1]) {
			start--
		}
		switch string(masked[start : previous+1]) {
		case "await", "case", "delete", "in", "instanceof", "of", "return", "throw", "typeof", "void", "yield":
			return true
		}
	}
	return false
}

func isASCIIAlpha(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}
