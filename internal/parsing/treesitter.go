// Package parsing provides bounded, language-neutral source analysis backed by
// a pure-Go tree-sitter runtime. Callers must treat errors as a signal to use
// their existing deterministic fallback.
package parsing

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

const (
	maxSourceBytes     = 1 << 20
	parseTimeoutMicros = 500_000
)

// Symbol is a body-free declaration plus the complete source extent used by
// chunking and durable-note anchors.
type Symbol struct {
	Name            string
	Kind            string
	Signature       string
	Documentation   string
	Container       string
	StartLine       int
	EndLine         int
	ExtentStartLine int
	ExtentEndLine   int
}

// Relation is a parser-backed dependency or declaration used by language-aware
// related-file analysis. PHP class-like names are namespace- and alias-resolved
// so callers can apply project configuration such as Composer autoload mappings.
type Relation struct {
	Kind  string
	Name  string
	Alias string
}

// Analysis is the shared structural result consumed by indexing and MCP
// outline paths.
type Analysis struct {
	Language          string
	PackageName       string
	Imports           []string
	Header            []string
	Symbols           []Symbol
	DeclarationStarts []int
	Relations         []Relation
}

type languageSpec struct {
	name string
	load func() *gotreesitter.Language
}

var parserPools sync.Map   // map[string]*gotreesitter.ParserPool
var symbolQueries sync.Map // map[string]*gotreesitter.Query
var parseGate = make(chan struct{}, 1)

// Analyze parses supported Go, JavaScript/TypeScript, Python, Rust, and PHP source.
// It rejects partial/error trees so callers never receive guessed structure.
func Analyze(path string, source []byte) (analysis Analysis, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			analysis = Analysis{}
			err = fmt.Errorf("tree-sitter panic: %v", recovered)
		}
	}()
	if len(source) == 0 {
		return Analysis{}, errors.New("empty source")
	}
	if len(source) > maxSourceBytes {
		return Analysis{}, fmt.Errorf("source exceeds %d-byte parser limit", maxSourceBytes)
	}
	spec, ok := specForPath(path)
	if !ok {
		return Analysis{}, fmt.Errorf("unsupported tree-sitter language for %s", path)
	}
	lang := spec.load()
	if lang == nil {
		return Analysis{}, fmt.Errorf("tree-sitter grammar %q is unavailable", spec.name)
	}
	poolValue, _ := parserPools.LoadOrStore(spec.name, gotreesitter.NewParserPool(
		lang,
		gotreesitter.WithParserPoolTimeoutMicros(parseTimeoutMicros),
	))
	pool := poolValue.(*gotreesitter.ParserPool)
	// v0.32.0 uses shared GLR forest scratch state across parser instances.
	// Serialize parsing until the pinned runtime makes that state per-parser;
	// immutable tree reads and query execution remain concurrent.
	tree, err := parseStrict(pool, source)
	if tree != nil {
		defer tree.Release()
	}
	if err != nil {
		return Analysis{}, err
	}
	root := tree.RootNode()
	if root == nil {
		return Analysis{}, errors.New("tree-sitter produced an empty tree")
	}
	if invalid := firstSyntaxError(root); invalid != nil {
		return Analysis{}, fmt.Errorf("tree-sitter produced %s at line %d", invalid.Type(lang), invalid.StartPoint().Row+1)
	}

	analysis = Analysis{Language: publicLanguage(path, spec.name)}
	analysis.PackageName, analysis.Imports = sourceMetadata(root, lang, source, spec.name)
	if spec.name == "php" {
		analysis.PackageName, analysis.Imports, analysis.Relations = analyzePHPRelations(root, lang, source)
	}
	analysis.DeclarationStarts = declarationStarts(root, lang, source, spec.name)
	analysis.Symbols, err = collectSymbols(tree, root, lang, source, spec.name)
	if err != nil {
		return Analysis{}, err
	}
	analysis.Header = sourceHeader(source, analysis.DeclarationStarts)
	return analysis, nil
}

func parseStrict(pool *gotreesitter.ParserPool, source []byte) (*gotreesitter.Tree, error) {
	select {
	case parseGate <- struct{}{}:
		defer func() { <-parseGate }()
	case <-time.After(time.Duration(parseTimeoutMicros) * time.Microsecond):
		return nil, errors.New("tree-sitter parser concurrency timeout")
	}
	return pool.ParseStrict(source)
}

func firstSyntaxError(node *gotreesitter.Node) *gotreesitter.Node {
	if node == nil {
		return node
	}
	if node.HasError() || node.IsError() || node.IsMissing() {
		for index := 0; index < node.ChildCount(); index++ {
			if invalid := firstSyntaxError(node.Child(index)); invalid != nil {
				return invalid
			}
		}
		return node
	}
	for index := 0; index < node.ChildCount(); index++ {
		if invalid := firstSyntaxError(node.Child(index)); invalid != nil {
			return invalid
		}
	}
	return nil
}

// Supported reports whether path has one of Memento's pinned grammars.
func Supported(path string) bool {
	_, ok := specForPath(path)
	return ok
}

// IsPHPPath reports whether path uses a PHP-bearing extension supported by
// Memento, including Composer classmap and common Drupal source extensions.
func IsPHPPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".php", ".php3", ".php4", ".php5", ".phps", ".phpt", ".phtml", ".inc", ".module", ".install", ".theme", ".profile", ".engine":
		return true
	default:
		return false
	}
}

func specForPath(path string) (languageSpec, bool) {
	if strings.HasSuffix(strings.ToLower(filepath.ToSlash(path)), ".blade.php") {
		return languageSpec{}, false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return languageSpec{name: "go", load: grammars.GoLanguage}, true
	case ".js", ".mjs", ".cjs":
		return languageSpec{name: "javascript", load: grammars.JavascriptLanguage}, true
	case ".jsx":
		return languageSpec{name: "tsx", load: grammars.TsxLanguage}, true
	case ".ts", ".mts", ".cts":
		return languageSpec{name: "typescript", load: grammars.TypescriptLanguage}, true
	case ".tsx":
		return languageSpec{name: "tsx", load: grammars.TsxLanguage}, true
	case ".py":
		return languageSpec{name: "python", load: grammars.PythonLanguage}, true
	case ".rs":
		return languageSpec{name: "rust", load: grammars.RustLanguage}, true
	case ".php", ".php3", ".php4", ".php5", ".phps", ".phpt", ".phtml", ".inc", ".module", ".install", ".theme", ".profile", ".engine":
		return languageSpec{name: "php", load: grammars.PhpLanguage}, true
	default:
		return languageSpec{}, false
	}
}

func publicLanguage(path, parserLanguage string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jsx":
		return "javascript"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	default:
		return parserLanguage
	}
}

func declarationStarts(root *gotreesitter.Node, lang *gotreesitter.Language, source []byte, language string) []int {
	starts := make([]int, 0, root.NamedChildCount())
	lines := strings.Split(string(source), "\n")
	for _, child := range topLevelDeclarationNodes(root, lang, language) {
		if child == nil || !isTopLevelDeclaration(child, lang, language) {
			continue
		}
		line := int(child.StartPoint().Row) + 1
		starts = append(starts, leadingDeclarationLine(lines, line, language))
	}
	if len(starts) == 0 {
		return nil
	}
	sort.Ints(starts)
	out := starts[:0]
	for _, line := range starts {
		if line < 1 || len(out) > 0 && out[len(out)-1] == line {
			continue
		}
		out = append(out, line)
	}
	return out
}

func topLevelDeclarationNodes(root *gotreesitter.Node, lang *gotreesitter.Language, language string) []*gotreesitter.Node {
	out := make([]*gotreesitter.Node, 0, root.NamedChildCount())
	var appendChildren func(*gotreesitter.Node)
	appendChildren = func(parent *gotreesitter.Node) {
		for index := 0; index < parent.NamedChildCount(); index++ {
			child := parent.NamedChild(index)
			if child == nil {
				continue
			}
			if isTopLevelDeclaration(child, lang, language) {
				out = append(out, child)
				continue
			}
			if language == "php" && child.Type(lang) == "namespace_definition" {
				if body := child.ChildByFieldName("body", lang); body != nil {
					appendChildren(body)
				}
			}
		}
	}
	appendChildren(root)
	return out
}

func isTopLevelDeclaration(node *gotreesitter.Node, lang *gotreesitter.Language, language string) bool {
	typ := node.Type(lang)
	switch language {
	case "go":
		switch typ {
		case "import_declaration", "const_declaration", "var_declaration", "type_declaration", "function_declaration", "method_declaration":
			return true
		}
	case "javascript", "typescript", "tsx":
		switch typ {
		case "import_statement", "function_declaration", "generator_function_declaration", "class_declaration", "lexical_declaration", "variable_declaration", "interface_declaration", "type_alias_declaration", "enum_declaration", "ambient_declaration", "module":
			return true
		case "export_statement":
			return hasDeclarationDescendant(node, lang, language)
		}
	case "python":
		switch typ {
		case "function_definition", "class_definition", "decorated_definition":
			return true
		}
	case "rust":
		switch typ {
		case "use_declaration", "function_item", "struct_item", "enum_item", "trait_item", "impl_item", "type_item", "const_item", "static_item", "mod_item", "union_item", "macro_definition":
			return true
		}
	case "php":
		switch typ {
		case "namespace_use_declaration", "const_declaration", "class_declaration", "interface_declaration", "trait_declaration", "enum_declaration", "function_definition":
			return true
		}
	}
	return false
}

func hasDeclarationDescendant(node *gotreesitter.Node, lang *gotreesitter.Language, language string) bool {
	for index := 0; index < node.NamedChildCount(); index++ {
		child := node.NamedChild(index)
		if child != nil && (isTopLevelDeclaration(child, lang, language) || hasDeclarationDescendant(child, lang, language)) {
			return true
		}
	}
	return false
}

func collectSymbols(tree *gotreesitter.Tree, root *gotreesitter.Node, lang *gotreesitter.Language, source []byte, language string) ([]Symbol, error) {
	lines := strings.Split(string(source), "\n")
	result := make([]Symbol, 0, 32)
	queryValue, ok := symbolQueries.Load(language)
	if !ok {
		query, err := gotreesitter.NewQuery(symbolQuery(language), lang)
		if err != nil {
			return nil, fmt.Errorf("compile %s symbol query: %w", language, err)
		}
		queryValue, _ = symbolQueries.LoadOrStore(language, query)
	}
	query := queryValue.(*gotreesitter.Query)
	for _, match := range query.Execute(tree) {
		for _, capture := range match.Captures {
			node := capture.Node
			if node == nil || !visibleSymbol(node, lang) {
				continue
			}
			if kind := symbolKind(node, lang, language); kind != "" {
				if symbol, ok := symbolFromNode(node, lang, source, lines, language, kind); ok {
					result = append(result, symbol)
				}
			}
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].StartLine == result[right].StartLine {
			return result[left].Name < result[right].Name
		}
		return result[left].StartLine < result[right].StartLine
	})
	return deduplicateSymbols(result), nil
}

func symbolQuery(language string) string {
	switch language {
	case "go":
		return `[(function_declaration) (method_declaration) (type_spec) (const_spec) (var_spec)] @symbol`
	case "javascript":
		return `[(function_declaration) (generator_function_declaration) (class_declaration) (method_definition) (variable_declarator)] @symbol`
	case "typescript", "tsx":
		return `[(function_declaration) (generator_function_declaration) (class_declaration) (method_definition) (method_signature) (interface_declaration) (type_alias_declaration) (enum_declaration) (variable_declarator) (public_field_definition) (property_signature)] @symbol`
	case "python":
		return `[(function_definition) (class_definition)] @symbol`
	case "rust":
		return `[(function_item) (function_signature_item) (struct_item) (enum_item) (trait_item) (impl_item) (union_item) (type_item) (const_item) (static_item) (mod_item) (field_declaration)] @symbol`
	case "php":
		return `[(class_declaration) (interface_declaration) (trait_declaration) (enum_declaration) (function_definition) (method_declaration) (property_element) (property_promotion_parameter) (const_element) (enum_case)] @symbol`
	default:
		return `(_) @symbol`
	}
}

func visibleSymbol(node *gotreesitter.Node, lang *gotreesitter.Language) bool {
	if node != nil && node.Type(lang) == "property_promotion_parameter" {
		return true
	}
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Type(lang) {
		case "function_declaration", "generator_function_declaration", "function_expression", "arrow_function", "method_declaration", "method_definition", "function_definition", "function_item", "closure_expression":
			return false
		}
	}
	return true
}

func symbolKind(node *gotreesitter.Node, lang *gotreesitter.Language, language string) string {
	typ := node.Type(lang)
	switch language {
	case "go":
		switch typ {
		case "function_declaration":
			return "function"
		case "method_declaration":
			return "method"
		case "type_spec":
			if child := node.ChildByFieldName("type", lang); child != nil {
				switch child.Type(lang) {
				case "struct_type":
					return "struct"
				case "interface_type":
					return "interface"
				}
			}
			return "type"
		case "const_spec":
			return "constant"
		case "var_spec":
			return "variable"
		}
	case "javascript", "typescript", "tsx":
		switch typ {
		case "function_declaration", "generator_function_declaration":
			return "function"
		case "class_declaration":
			return "class"
		case "method_definition", "method_signature":
			return "method"
		case "interface_declaration":
			return "interface"
		case "type_alias_declaration":
			return "type"
		case "enum_declaration":
			return "enum"
		case "variable_declarator":
			if value := node.ChildByFieldName("value", lang); value != nil && value.Type(lang) == "arrow_function" {
				return "function"
			}
			return "variable"
		case "public_field_definition", "property_signature":
			return "property"
		}
	case "python":
		switch typ {
		case "function_definition":
			if hasContainer(node.Parent(), lang) {
				return "method"
			}
			return "function"
		case "class_definition":
			return "class"
		}
	case "rust":
		switch typ {
		case "function_item", "function_signature_item":
			if hasContainer(node.Parent(), lang) {
				return "method"
			}
			return "function"
		case "struct_item":
			return "struct"
		case "enum_item":
			return "enum"
		case "trait_item":
			return "trait"
		case "impl_item":
			return "impl"
		case "union_item":
			return "union"
		case "type_item":
			return "type"
		case "const_item":
			return "constant"
		case "static_item":
			return "variable"
		case "mod_item":
			return "module"
		case "field_declaration":
			return "property"
		}
	case "php":
		switch typ {
		case "class_declaration":
			return "class"
		case "interface_declaration":
			return "interface"
		case "trait_declaration":
			return "trait"
		case "enum_declaration":
			return "enum"
		case "function_definition":
			return "function"
		case "method_declaration":
			return "method"
		case "property_element", "property_promotion_parameter":
			return "property"
		case "const_element":
			return "const"
		case "enum_case":
			return "case"
		}
	}
	return ""
}

func symbolFromNode(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, lines []string, language, kind string) (Symbol, bool) {
	nameNode := node.ChildByFieldName("name", lang)
	if nameNode == nil && kind == "impl" {
		nameNode = node.ChildByFieldName("type", lang)
	}
	if nameNode == nil {
		nameNode = firstNamedDescendant(node, lang, "identifier", "type_identifier", "field_identifier", "property_identifier", "private_property_identifier", "name", "variable_name")
	}
	if nameNode == nil {
		return Symbol{}, false
	}
	name := strings.TrimSpace(nameNode.Text(source))
	if language == "php" {
		name = strings.TrimLeft(name, "&$")
	}
	if name == "" {
		return Symbol{}, false
	}
	extent := node
	if language == "php" {
		switch node.Type(lang) {
		case "property_element", "const_element":
			if parent := node.Parent(); parent != nil {
				extent = parent
			}
		}
	}
	if parent := node.Parent(); parent != nil && parent.Type(lang) == "decorated_definition" {
		extent = parent
	}
	startLine := int(node.StartPoint().Row) + 1
	extentStart := leadingDeclarationLine(lines, int(extent.StartPoint().Row)+1, language)
	extentEnd := int(extent.EndPoint().Row) + 1
	signature, endLine := declarationSignature(node, lang, source, kind)
	if signature == "" {
		return Symbol{}, false
	}
	return Symbol{
		Name:            name,
		Kind:            kind,
		Signature:       signature,
		Documentation:   leadingDocumentation(lines, startLine, language),
		Container:       symbolContainer(node, lang, source, language, kind),
		StartLine:       startLine,
		EndLine:         endLine,
		ExtentStartLine: extentStart,
		ExtentEndLine:   extentEnd,
	}, true
}

func declarationSignature(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, kind string) (string, int) {
	signatureNode := node
	var body *gotreesitter.Node
	if node.Type(lang) == "variable_declarator" && kind == "function" {
		if parent := node.Parent(); parent != nil {
			signatureNode = parent
			if outer := parent.Parent(); outer != nil && outer.Type(lang) == "export_statement" {
				signatureNode = outer
			}
		}
		if value := node.ChildByFieldName("value", lang); value != nil {
			body = value.ChildByFieldName("body", lang)
		}
	}
	if node.Type(lang) == "property_element" || node.Type(lang) == "const_element" {
		if parent := node.Parent(); parent != nil {
			signatureNode = parent
		}
	}
	start, end := int(signatureNode.StartByte()), int(signatureNode.EndByte())
	if node.Type(lang) == "property_element" {
		end = int(node.EndByte())
		if value := node.ChildByFieldName("default_value", lang); value != nil {
			end = int(value.StartByte())
		}
	}
	if node.Type(lang) == "const_element" {
		if name := firstNamedDescendant(node, lang, "name"); name != nil {
			end = int(name.EndByte())
		}
	}
	if body == nil {
		body = node.ChildByFieldName("body", lang)
	}
	if body != nil {
		end = int(body.StartByte())
	} else if body := firstDirectChild(node, lang, "block", "statement_block", "class_body", "declaration_list", "field_declaration_list", "interface_body", "enum_body", "enum_declaration_list", "property_hook_list"); body != nil {
		end = int(body.StartByte())
	}
	if start < 0 || end <= start || end > len(source) {
		return "", 0
	}
	rawSignature := strings.TrimRightFunc(string(source[start:end]), unicode.IsSpace)
	signature := normalizeSignature(rawSignature)
	if signature == "" {
		return "", 0
	}
	endLine := int(signatureNode.StartPoint().Row) + strings.Count(rawSignature, "\n") + 1
	return signature, endLine
}

func symbolContainer(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, language, kind string) string {
	if language == "go" && kind == "method" {
		if receiver := node.ChildByFieldName("receiver", lang); receiver != nil {
			raw := receiver.Text(source)
			parts := strings.FieldsFunc(raw, func(value rune) bool {
				return !(unicode.IsLetter(value) || unicode.IsDigit(value) || value == '_')
			})
			if len(parts) > 0 {
				name := parts[len(parts)-1]
				if strings.Contains(raw, "*"+name) {
					return "*" + name
				}
				return name
			}
		}
	}
	return nearestContainer(node.Parent(), lang, source, language)
}

func nearestContainer(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, language string) string {
	for current := node; current != nil; current = current.Parent() {
		typ := current.Type(lang)
		isContainer := typ == "class_declaration" || typ == "class_definition" || typ == "interface_declaration" || typ == "trait_declaration" || typ == "enum_declaration" || typ == "trait_item" || typ == "impl_item" || typ == "struct_item"
		if !isContainer {
			continue
		}
		nameNode := current.ChildByFieldName("name", lang)
		if nameNode == nil && language == "rust" && typ == "impl_item" {
			nameNode = current.ChildByFieldName("type", lang)
		}
		if nameNode != nil {
			return strings.TrimSpace(nameNode.Text(source))
		}
	}
	return ""
}

func hasContainer(node *gotreesitter.Node, lang *gotreesitter.Language) bool {
	for current := node; current != nil; current = current.Parent() {
		switch current.Type(lang) {
		case "class_declaration", "class_definition", "interface_declaration", "trait_declaration", "enum_declaration", "trait_item", "impl_item", "struct_item":
			return true
		}
	}
	return false
}

func firstNamedDescendant(node *gotreesitter.Node, lang *gotreesitter.Language, types ...string) *gotreesitter.Node {
	wanted := make(map[string]struct{}, len(types))
	for _, typ := range types {
		wanted[typ] = struct{}{}
	}
	var find func(*gotreesitter.Node) *gotreesitter.Node
	find = func(current *gotreesitter.Node) *gotreesitter.Node {
		for index := 0; index < current.NamedChildCount(); index++ {
			child := current.NamedChild(index)
			if child == nil {
				continue
			}
			if _, ok := wanted[child.Type(lang)]; ok {
				return child
			}
			if nested := find(child); nested != nil {
				return nested
			}
		}
		return nil
	}
	return find(node)
}

func firstDirectChild(node *gotreesitter.Node, lang *gotreesitter.Language, types ...string) *gotreesitter.Node {
	for index := 0; index < node.NamedChildCount(); index++ {
		child := node.NamedChild(index)
		if child == nil {
			continue
		}
		for _, typ := range types {
			if child.Type(lang) == typ {
				return child
			}
		}
	}
	return nil
}

func sourceMetadata(root *gotreesitter.Node, lang *gotreesitter.Language, source []byte, language string) (string, []string) {
	packageName := ""
	imports := []string{}
	for index := 0; index < root.NamedChildCount(); index++ {
		child := root.NamedChild(index)
		if child == nil {
			continue
		}
		typ := child.Type(lang)
		text := strings.TrimSpace(child.Text(source))
		if language == "go" && typ == "package_clause" {
			packageName = strings.TrimSpace(strings.TrimPrefix(text, "package"))
		}
		switch typ {
		case "import_declaration", "import_statement", "import_from_statement", "use_declaration":
			imports = append(imports, normalizeSignature(text))
		}
	}
	return packageName, imports
}

func sourceHeader(source []byte, starts []int) []string {
	limit := len(strings.Split(string(source), "\n"))
	if len(starts) > 0 {
		limit = starts[0] - 1
	}
	lines := strings.Split(string(source), "\n")
	if limit > len(lines) {
		limit = len(lines)
	}
	header := make([]string, 0, 3)
	for _, line := range lines[:limit] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		header = append(header, trimmed)
		if len(header) == 3 {
			break
		}
	}
	return header
}

func leadingDeclarationLine(lines []string, line int, language string) int {
	if line < 1 {
		return 1
	}
	start := line
	for start > 1 {
		previousIndex := start - 2
		previousRaw := lines[previousIndex]
		previous := strings.TrimSpace(previousRaw)
		if previous == "" {
			break
		}
		if strings.HasSuffix(previous, "*/") {
			blockStart := previousIndex
			for blockStart >= 0 && !strings.Contains(lines[blockStart], "/*") {
				blockStart--
			}
			if blockStart < 0 {
				break
			}
			marker := strings.Index(lines[blockStart], "/*")
			if strings.TrimSpace(lines[blockStart][:marker]) != "" {
				break
			}
			start = blockStart + 1
			continue
		}
		isLeading := strings.HasPrefix(previous, "//")
		if language == "python" {
			isLeading = isLeading || strings.HasPrefix(previous, "#") && !strings.HasPrefix(previous, "#!") || strings.HasPrefix(previous, "@")
		}
		if language == "javascript" || language == "typescript" || language == "tsx" {
			isLeading = isLeading || strings.HasPrefix(previous, "@") || previous == "export" || previous == "export default"
		}
		if language == "rust" {
			isLeading = isLeading || strings.HasPrefix(previous, "#[") || strings.HasPrefix(previous, "#![")
		}
		if language == "php" {
			isLeading = isLeading || strings.HasPrefix(previous, "#[")
		}
		if !isLeading {
			break
		}
		start--
	}
	return start
}

func leadingDocumentation(lines []string, declarationLine int, language string) string {
	start := leadingDeclarationLine(lines, declarationLine, language)
	if start == declarationLine {
		return ""
	}
	parts := make([]string, 0, declarationLine-start)
	for index := start - 1; index < declarationLine-1; index++ {
		part := strings.TrimSpace(lines[index])
		part = strings.TrimPrefix(part, "/**")
		part = strings.TrimPrefix(part, "/*")
		part = strings.TrimPrefix(part, "///")
		part = strings.TrimPrefix(part, "//")
		part = strings.TrimPrefix(part, "#")
		part = strings.TrimPrefix(part, "*")
		part = strings.TrimSuffix(part, "*/")
		part = strings.TrimSpace(part)
		if part != "" && !strings.HasPrefix(part, "@") && !strings.HasPrefix(part, "[") {
			parts = append(parts, part)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func normalizeSignature(value string) string {
	parts := strings.FieldsFunc(value, unicode.IsSpace)
	return strings.Join(parts, " ")
}

func deduplicateSymbols(symbols []Symbol) []Symbol {
	seen := make(map[string]struct{}, len(symbols))
	out := symbols[:0]
	for _, symbol := range symbols {
		key := fmt.Sprintf("%s\x00%s\x00%s\x00%d", symbol.Container, symbol.Name, symbol.Kind, symbol.StartLine)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, symbol)
	}
	return out
}
