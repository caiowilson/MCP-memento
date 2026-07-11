package mcp

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strconv"
	"strings"
)

func structuredGoOutline(path string, source []byte) (structuredFileOutline, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, parser.ParseComments)
	if err != nil {
		return structuredFileOutline{}, err
	}
	out := structuredFileOutline{
		Language:    "go",
		PackageName: file.Name.Name,
		Imports:     []string{},
		Header:      []string{},
		Symbols:     []outlineSymbol{},
	}
	for _, spec := range file.Imports {
		pathValue, unquoteErr := strconv.Unquote(spec.Path.Value)
		if unquoteErr != nil {
			pathValue = strings.Trim(spec.Path.Value, "\"")
		}
		if spec.Name != nil {
			pathValue = spec.Name.Name + " " + pathValue
		}
		out.Imports = append(out.Imports, pathValue)
	}

	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.GenDecl:
			if typed.Tok == token.IMPORT {
				continue
			}
			for _, spec := range typed.Specs {
				switch value := spec.(type) {
				case *ast.TypeSpec:
					kind := "type"
					switch value.Type.(type) {
					case *ast.StructType:
						kind = "struct"
					case *ast.InterfaceType:
						kind = "interface"
					}
					out.Symbols = append(out.Symbols, outlineSymbol{
						Name:          value.Name.Name,
						Kind:          kind,
						Signature:     printGoTypeSignature(fset, typed.Tok, value),
						Documentation: goSpecDocumentation(typed, value.Doc, value.Comment),
						StartLine:     fset.Position(value.Pos()).Line,
						EndLine:       fset.Position(value.End()).Line,
					})
				case *ast.ValueSpec:
					for _, name := range value.Names {
						out.Symbols = append(out.Symbols, outlineSymbol{
							Name:          name.Name,
							Kind:          strings.ToLower(typed.Tok.String()),
							Signature:     printGoValueSignature(fset, typed.Tok, name.Name, value.Type),
							Documentation: goSpecDocumentation(typed, value.Doc, value.Comment),
							StartLine:     fset.Position(value.Pos()).Line,
							EndLine:       fset.Position(value.End()).Line,
						})
					}
				}
			}
		case *ast.FuncDecl:
			clone := *typed
			clone.Body = nil
			clone.Doc = nil
			var signature strings.Builder
			_ = printer.Fprint(&signature, fset, &clone)
			kind := "function"
			container := ""
			if typed.Recv != nil && len(typed.Recv.List) > 0 {
				kind = "method"
				var receiver strings.Builder
				_ = printer.Fprint(&receiver, fset, typed.Recv.List[0].Type)
				container = receiver.String()
			}
			out.Symbols = append(out.Symbols, outlineSymbol{
				Name:          typed.Name.Name,
				Kind:          kind,
				Signature:     normalizeOutlineSignature(signature.String()),
				Documentation: commentGroupText(typed.Doc),
				Container:     container,
				StartLine:     fset.Position(typed.Pos()).Line,
				EndLine:       fset.Position(typed.Type.End()).Line,
			})
		}
	}
	return out, nil
}

func printGoTypeSignature(fset *token.FileSet, tok token.Token, spec *ast.TypeSpec) string {
	clone := *spec
	clone.Doc = nil
	clone.Comment = nil
	decl := &ast.GenDecl{Tok: tok, Specs: []ast.Spec{&clone}}
	var signature strings.Builder
	_ = printer.Fprint(&signature, fset, decl)
	return normalizeOutlineSignature(signature.String())
}

func printGoValueSignature(fset *token.FileSet, tok token.Token, name string, valueType ast.Expr) string {
	signature := tok.String() + " " + name
	if valueType != nil {
		var printed strings.Builder
		_ = printer.Fprint(&printed, fset, valueType)
		signature += " " + printed.String()
	}
	return normalizeOutlineSignature(signature)
}

func goSpecDocumentation(decl *ast.GenDecl, doc, trailing *ast.CommentGroup) string {
	if text := commentGroupText(doc); text != "" {
		return text
	}
	if len(decl.Specs) == 1 {
		if text := commentGroupText(decl.Doc); text != "" {
			return text
		}
	}
	return commentGroupText(trailing)
}

func commentGroupText(group *ast.CommentGroup) string {
	if group == nil {
		return ""
	}
	return strings.TrimSpace(group.Text())
}
