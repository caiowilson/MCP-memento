package mcp

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// languageForPath returns a language identifier based on file extension.
func languageForPath(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".go":
		return "go"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	default:
		return ""
	}
}

// extractFileOutline returns a compact outline of the file: declaration
// signatures plus doc comments. For Go it uses go/ast; for JS/TS it uses
// regex; for anything else a generic heuristic.
func extractFileOutline(absPath string) (string, error) {
	switch languageForPath(absPath) {
	case "go":
		return goOutline(absPath)
	case "javascript", "typescript":
		return jsOutline(absPath)
	default:
		return genericOutline(absPath)
	}
}

// extractFileSummary returns a very compact summary: one line per symbol
// with line numbers, no doc comments.
func extractFileSummary(absPath string) (string, error) {
	switch languageForPath(absPath) {
	case "go":
		return goSummary(absPath)
	case "javascript", "typescript":
		return jsSummary(absPath)
	default:
		return genericOutline(absPath)
	}
}

// ---------------------------------------------------------------------------
// Go
// ---------------------------------------------------------------------------

func goOutline(absPath string) (string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
	if err != nil {
		return genericOutline(absPath)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n", node.Name.Name)

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			if d.Doc != nil {
				for _, c := range d.Doc.List {
					b.WriteString(c.Text + "\n")
				}
			}
			for _, spec := range d.Specs {
				goSpecOutline(&b, fset, d.Tok, spec)
			}
		case *ast.FuncDecl:
			if d.Doc != nil {
				for _, c := range d.Doc.List {
					b.WriteString(c.Text + "\n")
				}
			}
			goFuncOutline(&b, fset, d)
		}
	}
	return b.String(), nil
}

func goFuncOutline(b *strings.Builder, fset *token.FileSet, d *ast.FuncDecl) {
	clone := *d
	clone.Body = nil
	clone.Doc = nil
	printer.Fprint(b, fset, &clone)
	b.WriteString("\n")
}

func goSpecOutline(b *strings.Builder, fset *token.FileSet, tok token.Token, spec ast.Spec) {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		switch st := s.Type.(type) {
		case *ast.StructType:
			fmt.Fprintf(b, "type %s struct { /* %d fields */ }\n", s.Name.Name, countStructFields(st.Fields))
		case *ast.InterfaceType:
			fmt.Fprintf(b, "type %s interface {\n", s.Name.Name)
			if st.Methods != nil {
				for _, m := range st.Methods.List {
					b.WriteString("\t")
					if len(m.Names) > 0 {
						b.WriteString(m.Names[0].Name)
						var buf strings.Builder
						printer.Fprint(&buf, fset, m.Type)
						b.WriteString(strings.TrimPrefix(buf.String(), "func"))
					} else {
						var buf strings.Builder
						printer.Fprint(&buf, fset, m.Type)
						b.WriteString(buf.String())
					}
					b.WriteString("\n")
				}
			}
			b.WriteString("}\n")
		default:
			b.WriteString("type " + s.Name.Name + " ")
			var buf strings.Builder
			printer.Fprint(&buf, fset, s.Type)
			b.WriteString(buf.String() + "\n")
		}
	case *ast.ValueSpec:
		names := make([]string, len(s.Names))
		for i, n := range s.Names {
			names[i] = n.Name
		}
		fmt.Fprintf(b, "%s %s", tok, strings.Join(names, ", "))
		if s.Type != nil {
			b.WriteString(" ")
			var buf strings.Builder
			printer.Fprint(&buf, fset, s.Type)
			b.WriteString(buf.String())
		}
		b.WriteString("\n")
	}
}

func countStructFields(fl *ast.FieldList) int {
	if fl == nil {
		return 0
	}
	n := 0
	for _, f := range fl.List {
		if len(f.Names) > 0 {
			n += len(f.Names)
		} else {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Go summary (one line per symbol with line number)
// ---------------------------------------------------------------------------

func goSummary(absPath string) (string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
	if err != nil {
		return genericOutline(absPath)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n", node.Name.Name)

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			for _, spec := range d.Specs {
				pos := fset.Position(spec.Pos())
				switch s := spec.(type) {
				case *ast.TypeSpec:
					kind := "type"
					switch s.Type.(type) {
					case *ast.StructType:
						kind = "struct"
					case *ast.InterfaceType:
						kind = "interface"
					}
					fmt.Fprintf(&b, "L%d: type %s %s\n", pos.Line, s.Name.Name, kind)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						fmt.Fprintf(&b, "L%d: %s %s\n", pos.Line, d.Tok, n.Name)
					}
				}
			}
		case *ast.FuncDecl:
			pos := fset.Position(d.Pos())
			recv := ""
			if d.Recv != nil && d.Recv.NumFields() > 0 {
				var buf strings.Builder
				printer.Fprint(&buf, fset, d.Recv.List[0].Type)
				recv = "(" + buf.String() + ") "
			}
			fmt.Fprintf(&b, "L%d: func %s%s\n", pos.Line, recv, d.Name.Name)
		}
	}
	return b.String(), nil
}

// ---------------------------------------------------------------------------
// JS / TS
// ---------------------------------------------------------------------------

var jsDeclRe = regexp.MustCompile(
	`^\s*(export\s+)?(default\s+)?(async\s+)?(function\s*\*?\s+\w+|class\s+\w+|interface\s+\w+|type\s+\w+|enum\s+\w+|const\s+\w+|let\s+\w+|var\s+\w+)`,
)

func jsOutline(absPath string) (string, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	var b strings.Builder
	var docBuf strings.Builder
	inJSDoc := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "/**") {
			inJSDoc = true
			docBuf.Reset()
			docBuf.WriteString(line + "\n")
			if strings.Contains(trimmed, "*/") {
				inJSDoc = false
			}
			continue
		}
		if inJSDoc {
			docBuf.WriteString(line + "\n")
			if strings.Contains(trimmed, "*/") {
				inJSDoc = false
			}
			continue
		}
		if jsDeclRe.MatchString(line) {
			if docBuf.Len() > 0 {
				b.WriteString(docBuf.String())
			}
			b.WriteString(line + "\n")
			docBuf.Reset()
		} else if trimmed != "" {
			docBuf.Reset()
		}
	}

	if b.Len() == 0 {
		return genericOutline(absPath)
	}
	return b.String(), nil
}

func jsSummary(absPath string) (string, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	var b strings.Builder
	for i, line := range lines {
		if jsDeclRe.MatchString(line) {
			fmt.Fprintf(&b, "L%d: %s\n", i+1, strings.TrimSpace(line))
		}
	}
	if b.Len() == 0 {
		return genericOutline(absPath)
	}
	return b.String(), nil
}

// ---------------------------------------------------------------------------
// Generic fallback
// ---------------------------------------------------------------------------

var genericDeclRe = regexp.MustCompile(
	`(?i)^\s*(pub\s+)?(fn|func|function|def|class|interface|struct|enum|module|trait|impl|type|object)\s+\w+`,
)

func genericOutline(absPath string) (string, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	var b strings.Builder

	// Include first few non-empty lines as header context.
	headerLines := 0
	for i := 0; i < len(lines) && headerLines < 3; i++ {
		if strings.TrimSpace(lines[i]) != "" {
			b.WriteString(lines[i] + "\n")
			headerLines++
		}
	}

	for i, line := range lines {
		if genericDeclRe.MatchString(line) {
			fmt.Fprintf(&b, "L%d: %s\n", i+1, strings.TrimSpace(line))
		}
	}
	return b.String(), nil
}
