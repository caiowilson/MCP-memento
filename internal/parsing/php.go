package parsing

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

const (
	RelationDeclaration = "declaration"
	RelationFunction    = "function_declaration"
	RelationImport      = "import"
	RelationTraitUse    = "trait_use"
	RelationReference   = "reference"
	RelationInclude     = "include"
)

type phpRelationScope struct {
	namespace string
	aliases   map[string]string
}

// analyzePHPRelations resolves class-like PHP names inside their namespace
// scope before exposing them. This is important for files with multiple
// bracketed namespaces where the same local alias may mean different things.
func analyzePHPRelations(root *gotreesitter.Node, lang *gotreesitter.Language, source []byte) (string, []string, []Relation) {
	packageName := ""
	imports := []string{}
	relations := []Relation{}

	var processNodes func([]*gotreesitter.Node, string)
	processNodes = func(nodes []*gotreesitter.Node, namespace string) {
		scope := phpRelationScope{namespace: normalizePHPName(namespace), aliases: map[string]string{}}
		for _, node := range nodes {
			if node == nil || node.Type(lang) != "namespace_use_declaration" {
				continue
			}
			for _, relation := range phpNamespaceUseRelations(node, lang, source) {
				relations = appendUniqueRelation(relations, relation)
				imports = appendUniqueString(imports, relation.Name)
				scope.aliases[strings.ToLower(relation.Alias)] = relation.Name
			}
		}

		for _, node := range nodes {
			if node == nil || node.Type(lang) == "namespace_use_declaration" {
				continue
			}
			if include := phpStaticInclude(node, lang, source); include != "" {
				imports = appendUniqueString(imports, include)
			}
			walkPHPRelations(node, scope, lang, source, &relations)
		}
	}

	currentNamespace := ""
	segment := []*gotreesitter.Node{}
	flush := func() {
		if len(segment) > 0 {
			processNodes(segment, currentNamespace)
			segment = nil
		}
	}
	for index := 0; index < root.NamedChildCount(); index++ {
		node := root.NamedChild(index)
		if node == nil || node.Type(lang) != "namespace_definition" {
			segment = append(segment, node)
			continue
		}
		flush()
		namespace := phpNodeName(node.ChildByFieldName("name", lang), lang, source)
		if packageName == "" {
			packageName = normalizePHPName(namespace)
		}
		if body := node.ChildByFieldName("body", lang); body != nil {
			processNodes(phpNamedChildren(body), namespace)
			currentNamespace = ""
			continue
		}
		currentNamespace = namespace
	}
	flush()

	sort.Strings(imports)
	return packageName, imports, relations
}

func walkPHPRelations(node *gotreesitter.Node, scope phpRelationScope, lang *gotreesitter.Language, source []byte, relations *[]Relation) {
	if node == nil {
		return
	}
	for _, relation := range phpRelationsForNode(node, scope, lang, source) {
		*relations = appendUniqueRelation(*relations, relation)
	}
	for index := 0; index < node.NamedChildCount(); index++ {
		walkPHPRelations(node.NamedChild(index), scope, lang, source, relations)
	}
}

func phpRelationsForNode(node *gotreesitter.Node, scope phpRelationScope, lang *gotreesitter.Language, source []byte) []Relation {
	if node == nil {
		return nil
	}
	var kind string
	var names []string
	switch node.Type(lang) {
	case "class_declaration", "interface_declaration", "trait_declaration", "enum_declaration":
		name := phpNodeName(node.ChildByFieldName("name", lang), lang, source)
		if name == "" {
			return nil
		}
		return []Relation{{Kind: RelationDeclaration, Name: qualifyPHPName(scope.namespace, name)}}
	case "function_definition":
		name := phpNodeName(node.ChildByFieldName("name", lang), lang, source)
		if name == "" {
			return nil
		}
		return []Relation{{Kind: RelationFunction, Name: qualifyPHPName(scope.namespace, name)}}
	case "namespace_use_declaration":
		return nil // handled once while constructing the enclosing namespace scope
	case "use_declaration":
		kind, names = RelationTraitUse, phpDirectNames(node, lang, source)
	case "base_clause", "class_interface_clause", "named_type", "attribute", "object_creation_expression":
		kind, names = RelationReference, phpDirectNames(node, lang, source)
	case "scoped_call_expression", "scoped_property_access_expression":
		kind, names = RelationReference, phpNamesFromField(node, "scope", lang, source)
	case "class_constant_access_expression":
		kind, names = RelationReference, phpFirstDirectNames(node, lang, source, 1)
	case "binary_expression":
		operator := node.ChildByFieldName("operator", lang)
		if operator == nil || !strings.EqualFold(strings.TrimSpace(operator.Text(source)), "instanceof") {
			return nil
		}
		kind, names = RelationReference, phpNamesFromField(node, "right", lang, source)
	case "include_expression", "include_once_expression", "require_expression", "require_once_expression":
		if include := phpIncludeLiteral(node, lang, source); include != "" {
			return []Relation{{Kind: RelationInclude, Name: include}}
		}
		return nil
	default:
		return nil
	}

	out := make([]Relation, 0, len(names))
	for _, name := range names {
		if resolved := resolvePHPRelationName(scope, name); resolved != "" {
			out = appendUniqueRelation(out, Relation{Kind: kind, Name: resolved})
		}
	}
	return out
}

func phpNamespaceUseRelations(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte) []Relation {
	declarationText := strings.TrimSpace(node.Text(source))
	afterUse := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(declarationText), "use"))
	if strings.HasPrefix(afterUse, "function ") || strings.HasPrefix(afterUse, "const ") {
		return nil
	}

	prefix := ""
	grouped := false
	for index := 0; index < node.NamedChildCount(); index++ {
		child := node.NamedChild(index)
		if child == nil {
			continue
		}
		switch child.Type(lang) {
		case "namespace_name":
			prefix = phpNodeName(child, lang, source)
		case "namespace_use_group":
			grouped = true
		}
	}

	relations := []Relation{}
	var collectClauses func(*gotreesitter.Node)
	collectClauses = func(parent *gotreesitter.Node) {
		for index := 0; index < parent.NamedChildCount(); index++ {
			child := parent.NamedChild(index)
			if child == nil {
				continue
			}
			if child.Type(lang) != "namespace_use_clause" {
				collectClauses(child)
				continue
			}
			clauseText := strings.ToLower(strings.TrimSpace(child.Text(source)))
			if strings.HasPrefix(clauseText, "function ") || strings.HasPrefix(clauseText, "const ") {
				continue
			}
			name := phpFirstDirectName(child, lang, source)
			if grouped && prefix != "" {
				name = normalizePHPName(prefix) + "\\" + normalizePHPName(name)
			}
			name = normalizePHPName(name)
			if name == "" {
				continue
			}
			alias := phpNodeName(child.ChildByFieldName("alias", lang), lang, source)
			if alias == "" {
				alias = phpShortName(name)
			}
			relations = appendUniqueRelation(relations, Relation{Kind: RelationImport, Name: name, Alias: alias})
		}
	}
	collectClauses(node)
	return relations
}

func phpStaticInclude(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte) string {
	if node == nil || node.Type(lang) != "expression_statement" {
		return ""
	}
	for index := 0; index < node.NamedChildCount(); index++ {
		child := node.NamedChild(index)
		if child == nil {
			continue
		}
		switch child.Type(lang) {
		case "include_expression", "include_once_expression", "require_expression", "require_once_expression":
			return phpIncludeLiteral(child, lang, source)
		}
	}
	return ""
}

func phpIncludeLiteral(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte) string {
	for index := 0; index < node.NamedChildCount(); index++ {
		child := node.NamedChild(index)
		if child == nil || child.Type(lang) != "string" {
			continue
		}
		value := strings.TrimSpace(child.Text(source))
		if len(value) >= 2 && (value[0] == '\'' && value[len(value)-1] == '\'' || value[0] == '"' && value[len(value)-1] == '"') {
			return value[1 : len(value)-1]
		}
	}
	return phpStaticIncludeExpression(node.Text(source))
}

var phpDirnameIncludeRe = regexp.MustCompile(`(?i)^dirname\s*\(\s*__DIR__\s*(?:,\s*([0-9]+)\s*)?\)$`)

func phpStaticIncludeExpression(expression string) string {
	value := strings.TrimSpace(expression)
	lower := strings.ToLower(value)
	for _, keyword := range []string{"require_once", "include_once", "require", "include"} {
		if strings.HasPrefix(lower, keyword) {
			value = strings.TrimSpace(value[len(keyword):])
			break
		}
	}
	if len(value) >= 2 && value[0] == '(' && value[len(value)-1] == ')' {
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	quote := strings.IndexAny(value, "'\"")
	if quote < 0 {
		return ""
	}
	prefix := strings.TrimSpace(value[:quote])
	if !strings.HasSuffix(prefix, ".") {
		return ""
	}
	prefix = strings.TrimSpace(strings.TrimSuffix(prefix, "."))
	delimiter := value[quote]
	end := strings.LastIndexByte(value, delimiter)
	if end <= quote || strings.TrimSpace(value[end+1:]) != "" {
		return ""
	}
	suffix := value[quote+1 : end]
	if suffix == "" || strings.ContainsAny(suffix, "$`") {
		return ""
	}
	levels := 0
	if strings.EqualFold(prefix, "__DIR__") {
		levels = 0
	} else if match := phpDirnameIncludeRe.FindStringSubmatch(prefix); match != nil {
		levels = 1
		if match[1] != "" {
			parsed, err := strconv.Atoi(match[1])
			if err != nil || parsed < 1 || parsed > 16 {
				return ""
			}
			levels = parsed
		}
	} else {
		return ""
	}
	return strings.Repeat("../", levels) + strings.TrimLeft(suffix, "/\\")
}

func resolvePHPRelationName(scope phpRelationScope, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	absolute := strings.HasPrefix(raw, "\\")
	name := normalizePHPName(raw)
	if name == "" || isPHPBuiltinName(name) {
		return ""
	}
	if absolute {
		return name
	}
	if len(name) > len("namespace\\") && strings.EqualFold(name[:len("namespace\\")], "namespace\\") {
		return qualifyPHPName(scope.namespace, name[len("namespace\\"):])
	}
	first, rest := name, ""
	if slash := strings.Index(name, "\\"); slash >= 0 {
		first, rest = name[:slash], name[slash:]
	}
	if imported := scope.aliases[strings.ToLower(first)]; imported != "" {
		return normalizePHPName(imported + rest)
	}
	return qualifyPHPName(scope.namespace, name)
}

func isPHPBuiltinName(name string) bool {
	switch strings.ToLower(normalizePHPName(name)) {
	case "self", "static", "parent", "class", "string", "int", "float", "bool", "array", "object", "callable", "iterable", "mixed", "void", "never", "null", "false", "true", "resource":
		return true
	default:
		return false
	}
}

func phpNamesFromField(node *gotreesitter.Node, field string, lang *gotreesitter.Language, source []byte) []string {
	child := node.ChildByFieldName(field, lang)
	if child == nil {
		return nil
	}
	if name := phpNodeName(child, lang, source); name != "" {
		return []string{name}
	}
	return phpDirectNames(child, lang, source)
}

func phpDirectNames(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte) []string {
	out := []string{}
	for index := 0; index < node.NamedChildCount(); index++ {
		child := node.NamedChild(index)
		if child == nil {
			continue
		}
		if name := phpNodeName(child, lang, source); name != "" {
			out = appendUniqueString(out, name)
		}
	}
	return out
}

func phpFirstDirectNames(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, limit int) []string {
	out := []string{}
	for index := 0; index < node.NamedChildCount() && len(out) < limit; index++ {
		child := node.NamedChild(index)
		if name := phpNodeName(child, lang, source); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func phpFirstDirectName(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte) string {
	names := phpFirstDirectNames(node, lang, source, 1)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func phpNodeName(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Type(lang) {
	case "name", "qualified_name", "relative_name", "namespace_name":
		return strings.TrimSpace(node.Text(source))
	default:
		return ""
	}
}

func phpNamedChildren(node *gotreesitter.Node) []*gotreesitter.Node {
	out := make([]*gotreesitter.Node, 0, node.NamedChildCount())
	for index := 0; index < node.NamedChildCount(); index++ {
		out = append(out, node.NamedChild(index))
	}
	return out
}

func qualifyPHPName(namespace, name string) string {
	name = normalizePHPName(name)
	if name == "" || namespace == "" {
		return name
	}
	return normalizePHPName(namespace) + "\\" + name
}

func normalizePHPName(name string) string {
	return strings.Trim(strings.TrimSpace(name), "\\")
}

func phpShortName(name string) string {
	name = normalizePHPName(name)
	if index := strings.LastIndex(name, "\\"); index >= 0 {
		return name[index+1:]
	}
	return name
}

func appendUniqueRelation(relations []Relation, relation Relation) []Relation {
	if relation.Name == "" {
		return relations
	}
	for _, existing := range relations {
		if existing == relation {
			return relations
		}
	}
	return append(relations, relation)
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
