package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func (s *NoteStore) snapshotAnchors(anchors []NoteAnchor) ([]NoteAnchor, error) {
	if len(anchors) == 0 {
		return nil, nil
	}
	git := currentNoteGitState(s.repo)
	out := make([]NoteAnchor, 0, len(anchors))
	for index, anchor := range anchors {
		anchor.Path = cleanNoteAnchorPath(anchor.Path)
		anchor.Symbol = strings.TrimSpace(anchor.Symbol)
		if anchor.Path == "" {
			if anchor.Symbol != "" || anchor.ContentHash != "" {
				return nil, fmt.Errorf("anchor %d requires path for symbol or content hash", index)
			}
			if git.Head != "" {
				anchor.CommitSHA = git.Head
				anchor.Branch = git.Branch
			}
			if anchor.CommitSHA == "" {
				return nil, fmt.Errorf("anchor %d requires path or commitSha", index)
			}
			out = append(out, anchor)
			continue
		}
		hash, startLine, endLine, err := s.currentAnchorHash(anchor)
		if err != nil {
			return nil, fmt.Errorf("anchor %d: %w", index, err)
		}
		anchor.ContentHash = hash
		anchor.StartLine = startLine
		anchor.EndLine = endLine
		if git.Head != "" {
			anchor.CommitSHA = git.Head
		} else {
			anchor.CommitSHA = strings.TrimSpace(anchor.CommitSHA)
		}
		if git.Branch != "" {
			anchor.Branch = git.Branch
		} else {
			anchor.Branch = strings.TrimSpace(anchor.Branch)
		}
		out = append(out, anchor)
	}
	return out, nil
}

func (s *NoteStore) currentAnchorHash(anchor NoteAnchor) (string, int, int, error) {
	abs, err := safeJoin(s.repo, cleanNoteAnchorPath(anchor.Path))
	if err != nil {
		return "", 0, 0, err
	}
	source, err := os.ReadFile(abs)
	if err != nil {
		return "", 0, 0, err
	}
	startLine, endLine := anchor.StartLine, anchor.EndLine
	if anchor.Symbol != "" {
		outline := extractStructuredFileOutline(abs, source)
		symbol, ok := findAnchorSymbol(outline.Symbols, anchor.Symbol)
		if !ok {
			return "", 0, 0, fmt.Errorf("symbol %q not found in %s", anchor.Symbol, anchor.Path)
		}
		startLine, endLine = anchorSymbolExtent(abs, source, symbol)
	}
	content := source
	if startLine > 0 || endLine > 0 {
		if startLine <= 0 || endLine < startLine {
			return "", 0, 0, fmt.Errorf("invalid line range %d-%d", startLine, endLine)
		}
		content, err = noteLineRange(source, startLine, endLine)
		if err != nil {
			return "", 0, 0, err
		}
	}
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:]), startLine, endLine, nil
}

func anchorSymbolExtent(path string, source []byte, symbol outlineSymbol) (int, int) {
	if languageForStructuredOutline(path) == "go" {
		fset := token.NewFileSet()
		if file, err := parser.ParseFile(fset, path, source, 0); err == nil {
			for _, decl := range file.Decls {
				switch typed := decl.(type) {
				case *ast.FuncDecl:
					container := ""
					if typed.Recv != nil && len(typed.Recv.List) > 0 {
						container = goReceiverName(typed.Recv.List[0].Type)
					}
					if typed.Name.Name == symbol.Name && (symbol.Container == "" || strings.TrimPrefix(container, "*") == strings.TrimPrefix(symbol.Container, "*")) {
						return fset.Position(typed.Pos()).Line, fset.Position(typed.End()).Line
					}
				case *ast.GenDecl:
					for _, spec := range typed.Specs {
						name := ""
						switch value := spec.(type) {
						case *ast.TypeSpec:
							name = value.Name.Name
						case *ast.ValueSpec:
							for _, candidate := range value.Names {
								if candidate.Name == symbol.Name {
									name = candidate.Name
									break
								}
							}
						}
						if name == symbol.Name {
							return fset.Position(spec.Pos()).Line, fset.Position(spec.End()).Line
						}
					}
				}
			}
		}
	}
	if symbol.Kind != "function" && symbol.Kind != "method" && symbol.Kind != "class" {
		return symbol.StartLine, symbol.EndLine
	}
	masked := maskNonCode(source, languageForStructuredOutline(path) == "php")
	if language := languageForStructuredOutline(path); language == "javascript" || language == "typescript" {
		masked = maskJSNonCode(source)
	} else if language == "php" {
		masked = maskPHPNonCode(source)
	}
	starts, _ := outlineLineLayout(masked)
	if symbol.StartLine <= 0 || symbol.StartLine > len(starts) {
		return symbol.StartLine, symbol.EndLine
	}
	_, _, openBrace := outlineHeaderBeforeBody(source, masked, starts[symbol.StartLine-1], len(source))
	if openBrace >= 0 {
		if closeBrace := matchingOutlineBrace(masked, openBrace); closeBrace >= 0 {
			return symbol.StartLine, outlineLineForOffset(starts, closeBrace) + 1
		}
	}
	return symbol.StartLine, symbol.EndLine
}

func goReceiverName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return "*" + goReceiverName(typed.X)
	case *ast.IndexExpr:
		return goReceiverName(typed.X)
	case *ast.IndexListExpr:
		return goReceiverName(typed.X)
	default:
		return ""
	}
}

func findAnchorSymbol(symbols []outlineSymbol, requested string) (outlineSymbol, bool) {
	container := ""
	name := requested
	if dot := strings.LastIndex(requested, "."); dot >= 0 {
		container, name = requested[:dot], requested[dot+1:]
	}
	for _, symbol := range symbols {
		if symbol.Name == name && (container == "" || strings.TrimPrefix(symbol.Container, "*") == strings.TrimPrefix(container, "*")) {
			return symbol, true
		}
	}
	return outlineSymbol{}, false
}

func noteLineRange(source []byte, startLine, endLine int) ([]byte, error) {
	lines := bytes.SplitAfter(source, []byte{'\n'})
	if startLine > len(lines) {
		return nil, fmt.Errorf("start line %d exceeds file length %d", startLine, len(lines))
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	return bytes.Join(lines[startLine-1:endLine], nil), nil
}

func cleanNoteAnchorPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}
