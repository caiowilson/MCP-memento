package mcp

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// wordpressHookCallback describes the literal parts of a WordPress hook or
// shortcode registration. Receiver is populated for array callbacks such as
// [Report_Plugin::class, 'boot']; Callback always preserves the quoted name.
type wordpressHookCallback struct {
	Registration string
	Hook         string
	Receiver     string
	Callback     string
}

// symfonyPHPTemplateReferences resolves literal $this->render() calls to the
// nearest templates directory above the PHP source file.
func symfonyPHPTemplateReferences(root, fromRel string, source []byte) []string {
	out := []string{}
	for _, call := range scanPHPFrameworkCalls(source) {
		if call.receiver != "$this" || !strings.EqualFold(call.name, "render") || len(call.args) == 0 {
			continue
		}
		name, ok := phpFrameworkLiteral(call.args[0])
		if !ok || !frameworkSafeRelativeName(name) || strings.HasPrefix(name, "@") {
			continue
		}
		out = append(out, frameworkFindExistingUpward(root, fromRel, filepath.Join("templates", filepath.FromSlash(name)))...)
	}
	return sortedUniqueStrings(out)
}

var (
	twigTagTemplateRE = regexp.MustCompile(`(?is)\{%\s*(?:extends|include)\s+(?:'((?:\\.|[^'\\])*)'|"((?:\\.|[^"\\])*)")`)
	bladeDirectiveRE  = regexp.MustCompile(`(?is)@(?:extends|include|includeIf|includeWhen|includeUnless|component)\s*\(\s*(?:'((?:\\.|[^'\\])*)'|"((?:\\.|[^"\\])*)")`)
	bladeComponentRE  = regexp.MustCompile(`(?i)<\s*x-([A-Za-z0-9_.:-]+)(?:\s|/?>)`)
	yamlPHPClassRE    = regexp.MustCompile(`\\?[A-Za-z_][A-Za-z0-9_]*(?:\\[A-Za-z_][A-Za-z0-9_]*)+(?:::[A-Za-z_][A-Za-z0-9_]*)?`)
)

// twigTemplateReferences resolves literal Twig extends/include tags. Twig
// comments are masked first so examples and disabled tags do not create edges.
func twigTemplateReferences(root, fromRel string, source []byte) []string {
	masked := maskDelimitedTemplateComments(source, []byte("{#"), []byte("#}"))
	out := []string{}
	for _, match := range twigTagTemplateRE.FindAllSubmatch(masked, -1) {
		name := firstFrameworkCapture(match[1], match[2])
		if !frameworkSafeRelativeName(name) || strings.HasPrefix(name, "@") {
			continue
		}
		refs := frameworkFindExistingUpward(root, fromRel, filepath.FromSlash(name))
		if len(refs) == 0 {
			refs = frameworkFindExistingUpward(root, fromRel, filepath.Join("templates", filepath.FromSlash(name)))
		}
		out = append(out, refs...)
	}
	return sortedUniqueStrings(out)
}

// yamlPHPClassReferences extracts fully-qualified PHP class mentions from
// uncommented YAML and asks resolve to map each class to a repository file.
// Resolver results may be relative or absolute, but files outside root (also
// through symlinks) are discarded.
func yamlPHPClassReferences(root string, source []byte, resolve func(string) string) []string {
	if resolve == nil {
		return nil
	}
	out := []string{}
	for _, line := range strings.Split(string(source), "\n") {
		line = stripYAMLComment(line)
		for _, raw := range yamlPHPClassRE.FindAllString(line, -1) {
			class := strings.TrimPrefix(raw, "\\")
			if index := strings.Index(class, "::"); index >= 0 {
				class = class[:index]
			}
			resolved := strings.TrimSpace(resolve(class))
			if resolved == "" {
				continue
			}
			if rel := frameworkExistingRepoFile(root, resolved); rel != "" {
				out = append(out, rel)
			}
		}
	}
	return sortedUniqueStrings(out)
}

// drupalPHPTemplateReferences resolves both render-array #theme values and
// hook_theme() template declarations to the nearest module templates folder.
func drupalPHPTemplateReferences(root, fromRel string, source []byte) []string {
	tokens := tokenizePHPFramework(source)
	out := []string{}
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].kind != phpFrameworkString || tokens[i+1].text != "=>" || tokens[i+2].kind != phpFrameworkString {
			continue
		}
		key := strings.ToLower(tokens[i].text)
		name := tokens[i+2].text
		switch key {
		case "#theme":
			name = strings.ReplaceAll(name, "_", "-")
		case "template":
			// hook_theme() template values already use filename separators.
		default:
			continue
		}
		if !frameworkSafeRelativeName(name) {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".html.twig") {
			name += ".html.twig"
		}
		out = append(out, frameworkFindExistingUpward(root, fromRel, filepath.Join("templates", filepath.FromSlash(name)))...)
	}
	return sortedUniqueStrings(out)
}

// laravelBladeTemplateReferences resolves Blade view directives and anonymous
// component tags such as <x-alert> or <x-forms.input>. Package namespaces and
// dynamic component expressions are intentionally left unresolved.
func laravelBladeTemplateReferences(root, fromRel string, source []byte) []string {
	masked := maskBladeComments(source)
	out := []string{}
	for _, match := range bladeDirectiveRE.FindAllSubmatch(masked, -1) {
		name := firstFrameworkCapture(match[1], match[2])
		if strings.Contains(name, "::") || !frameworkSafeRelativeName(name) {
			continue
		}
		name = strings.TrimSuffix(name, ".blade.php")
		name = strings.ReplaceAll(name, ".", "/")
		out = append(out, frameworkFindExistingUpward(root, fromRel, filepath.Join("resources", "views", filepath.FromSlash(name+".blade.php")))...)
	}
	for _, match := range bladeComponentRE.FindAllSubmatch(masked, -1) {
		name := string(match[1])
		if strings.EqualFold(name, "dynamic-component") || strings.Contains(name, "::") || !frameworkSafeRelativeName(name) {
			continue
		}
		name = strings.ReplaceAll(name, ".", "/")
		out = append(out, frameworkFindExistingUpward(root, fromRel, filepath.Join("resources", "views", "components", filepath.FromSlash(name+".blade.php")))...)
	}
	return sortedUniqueStrings(out)
}

// wordpressTemplatePartReferences resolves literal get_template_part() calls.
// When both the specialized and fallback files exist, both are returned because
// WordPress can execute either depending on the selected template part.
func wordpressTemplatePartReferences(root, fromRel string, source []byte) []string {
	out := []string{}
	for _, call := range scanPHPFrameworkCalls(source) {
		if call.receiver != "" || !strings.EqualFold(call.name, "get_template_part") || len(call.args) == 0 {
			continue
		}
		slug, ok := phpFrameworkLiteral(call.args[0])
		if !ok || !frameworkSafeRelativeName(slug) {
			continue
		}
		slug = strings.TrimSuffix(slug, ".php")
		candidates := []string{}
		if len(call.args) > 1 {
			if name, literal := phpFrameworkLiteral(call.args[1]); literal && name != "" && frameworkSafeRelativeName(name) && !strings.Contains(name, "/") {
				candidates = append(candidates, filepath.FromSlash(slug+"-"+name+".php"))
			}
		}
		candidates = append(candidates, filepath.FromSlash(slug+".php"))
		out = append(out, frameworkFindExistingUpward(root, fromRel, candidates...)...)
	}
	return sortedUniqueStrings(out)
}

// wordpressHookCallbacks extracts literal callbacks registered with the common
// action, filter, and shortcode APIs. Dynamic callbacks are ignored.
func wordpressHookCallbacks(source []byte) []wordpressHookCallback {
	out := []wordpressHookCallback{}
	seen := map[wordpressHookCallback]struct{}{}
	for _, call := range scanPHPFrameworkCalls(source) {
		registration := strings.ToLower(call.name)
		if call.receiver != "" || (registration != "add_action" && registration != "add_filter" && registration != "add_shortcode") || len(call.args) < 2 {
			continue
		}
		hook, ok := phpFrameworkLiteral(call.args[0])
		if !ok || hook == "" {
			continue
		}
		receiver, callback, ok := wordpressLiteralCallback(call.args[1])
		if !ok || callback == "" {
			continue
		}
		item := wordpressHookCallback{Registration: registration, Hook: hook, Receiver: receiver, Callback: callback}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if left.Registration != right.Registration {
			return left.Registration < right.Registration
		}
		if left.Hook != right.Hook {
			return left.Hook < right.Hook
		}
		if left.Receiver != right.Receiver {
			return left.Receiver < right.Receiver
		}
		return left.Callback < right.Callback
	})
	return out
}

type phpFrameworkTokenKind uint8

const (
	phpFrameworkPunctuation phpFrameworkTokenKind = iota
	phpFrameworkIdentifier
	phpFrameworkVariable
	phpFrameworkString
)

type phpFrameworkToken struct {
	kind phpFrameworkTokenKind
	text string
}

type phpFrameworkCall struct {
	receiver string
	name     string
	args     [][]phpFrameworkToken
}

// tokenizePHPFramework is deliberately small: it retains only tokens needed by
// framework conventions, while fully skipping PHP comments and quoted content.
// Calls written inside comments or strings therefore cannot become relations.
func tokenizePHPFramework(source []byte) []phpFrameworkToken {
	tokens := []phpFrameworkToken{}
	for i := 0; i < len(source); {
		switch {
		case isFrameworkSpace(source[i]):
			i++
		case i+1 < len(source) && source[i] == '/' && source[i+1] == '/':
			i = skipFrameworkLine(source, i+2)
		case source[i] == '#' && (i+1 >= len(source) || source[i+1] != '['):
			i = skipFrameworkLine(source, i+1)
		case i+1 < len(source) && source[i] == '/' && source[i+1] == '*':
			i = skipFrameworkBlockComment(source, i+2)
		case source[i] == '\'' || source[i] == '"':
			value, next := scanPHPFrameworkString(source, i)
			tokens = append(tokens, phpFrameworkToken{kind: phpFrameworkString, text: value})
			i = next
		case i+2 < len(source) && source[i] == '<' && source[i+1] == '<' && source[i+2] == '<':
			i = skipPHPFrameworkHeredoc(source, i+3)
		case source[i] == '$' && i+1 < len(source) && isFrameworkIdentStart(source[i+1]):
			start := i
			i += 2
			for i < len(source) && isFrameworkIdentContinue(source[i]) {
				i++
			}
			tokens = append(tokens, phpFrameworkToken{kind: phpFrameworkVariable, text: string(source[start:i])})
		case isFrameworkIdentStart(source[i]):
			start := i
			i++
			for i < len(source) && isFrameworkIdentContinue(source[i]) {
				i++
			}
			tokens = append(tokens, phpFrameworkToken{kind: phpFrameworkIdentifier, text: string(source[start:i])})
		default:
			text := string(source[i : i+1])
			if i+1 < len(source) {
				pair := string(source[i : i+2])
				if pair == "->" || pair == "::" || pair == "=>" {
					text = pair
					i++
				}
			}
			tokens = append(tokens, phpFrameworkToken{kind: phpFrameworkPunctuation, text: text})
			i++
		}
	}
	return tokens
}

func scanPHPFrameworkCalls(source []byte) []phpFrameworkCall {
	tokens := tokenizePHPFramework(source)
	out := []phpFrameworkCall{}
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].kind != phpFrameworkIdentifier || tokens[i+1].text != "(" {
			continue
		}
		receiver := ""
		if i >= 2 && tokens[i-1].text == "->" && (tokens[i-2].kind == phpFrameworkVariable || tokens[i-2].kind == phpFrameworkIdentifier) {
			receiver = tokens[i-2].text
		} else if i >= 1 && (tokens[i-1].text == "->" || tokens[i-1].text == "::") {
			continue
		}
		close := frameworkMatchingToken(tokens, i+1, "(", ")")
		if close < 0 {
			continue
		}
		out = append(out, phpFrameworkCall{receiver: receiver, name: tokens[i].text, args: splitPHPFrameworkArguments(tokens[i+2 : close])})
	}
	return out
}

func frameworkMatchingToken(tokens []phpFrameworkToken, open int, left, right string) int {
	depth := 0
	for i := open; i < len(tokens); i++ {
		switch tokens[i].text {
		case left:
			depth++
		case right:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func splitPHPFrameworkArguments(tokens []phpFrameworkToken) [][]phpFrameworkToken {
	if len(tokens) == 0 {
		return nil
	}
	out := [][]phpFrameworkToken{}
	start := 0
	paren, bracket, brace := 0, 0, 0
	for i, token := range tokens {
		switch token.text {
		case "(":
			paren++
		case ")":
			paren--
		case "[":
			bracket++
		case "]":
			bracket--
		case "{":
			brace++
		case "}":
			brace--
		case ",":
			if paren == 0 && bracket == 0 && brace == 0 {
				out = append(out, tokens[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, tokens[start:])
	return out
}

func phpFrameworkLiteral(tokens []phpFrameworkToken) (string, bool) {
	if len(tokens) != 1 || tokens[0].kind != phpFrameworkString {
		return "", false
	}
	return tokens[0].text, true
}

func wordpressLiteralCallback(tokens []phpFrameworkToken) (receiver, callback string, ok bool) {
	if value, literal := phpFrameworkLiteral(tokens); literal {
		return "", value, true
	}
	if len(tokens) < 5 {
		return "", "", false
	}
	var inside []phpFrameworkToken
	switch {
	case tokens[0].text == "[" && tokens[len(tokens)-1].text == "]":
		inside = tokens[1 : len(tokens)-1]
	case len(tokens) >= 4 && strings.EqualFold(tokens[0].text, "array") && tokens[1].text == "(" && tokens[len(tokens)-1].text == ")":
		inside = tokens[2 : len(tokens)-1]
	default:
		return "", "", false
	}
	parts := splitPHPFrameworkArguments(inside)
	if len(parts) != 2 {
		return "", "", false
	}
	callback, ok = phpFrameworkLiteral(parts[1])
	if !ok {
		return "", "", false
	}
	if value, literal := phpFrameworkLiteral(parts[0]); literal {
		return value, callback, true
	}
	if len(parts[0]) == 1 && (parts[0][0].kind == phpFrameworkVariable || parts[0][0].kind == phpFrameworkIdentifier) {
		return parts[0][0].text, callback, true
	}
	if len(parts[0]) == 3 && parts[0][1].text == "::" && strings.EqualFold(parts[0][2].text, "class") {
		return parts[0][0].text, callback, true
	}
	return "", "", false
}

func scanPHPFrameworkString(source []byte, start int) (string, int) {
	quote := source[start]
	var value strings.Builder
	for i := start + 1; i < len(source); i++ {
		if source[i] == quote {
			return value.String(), i + 1
		}
		if source[i] == '\\' && i+1 < len(source) {
			next := source[i+1]
			if next == quote || next == '\\' {
				value.WriteByte(next)
				i++
				continue
			}
		}
		value.WriteByte(source[i])
	}
	return value.String(), len(source)
}

func skipPHPFrameworkHeredoc(source []byte, start int) int {
	lineEnd := start
	for lineEnd < len(source) && source[lineEnd] != '\n' && source[lineEnd] != '\r' {
		lineEnd++
	}
	header := strings.TrimSpace(string(source[start:lineEnd]))
	header = strings.Trim(header, "'\"")
	if header == "" {
		return lineEnd
	}
	for i := lineEnd; i < len(source); {
		if source[i] == '\r' || source[i] == '\n' {
			i++
			continue
		}
		end := skipFrameworkLine(source, i)
		line := strings.TrimSpace(string(source[i:end]))
		if line == header || line == header+";" {
			return end
		}
		i = end
	}
	return len(source)
}

func stripYAMLComment(line string) string {
	single, double, escaped := false, false, false
	for i := 0; i < len(line); i++ {
		char := line[i]
		if double {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == '"' {
				double = false
			}
			continue
		}
		if single {
			if char == '\'' {
				if i+1 < len(line) && line[i+1] == '\'' {
					i++
					continue
				}
				single = false
			}
			continue
		}
		switch char {
		case '\'':
			single = true
		case '"':
			double = true
		case '#':
			return line[:i]
		}
	}
	return line
}

func maskBladeComments(source []byte) []byte {
	masked := maskDelimitedTemplateComments(source, []byte("{{--"), []byte("--}}"))
	return maskDelimitedTemplateComments(masked, []byte("<!--"), []byte("-->"))
}

func maskDelimitedTemplateComments(source, open, close []byte) []byte {
	out := append([]byte(nil), source...)
	for start := 0; start < len(out); {
		index := bytesIndex(out[start:], open)
		if index < 0 {
			break
		}
		index += start
		endRel := bytesIndex(out[index+len(open):], close)
		end := len(out)
		if endRel >= 0 {
			end = index + len(open) + endRel + len(close)
		}
		for i := index; i < end; i++ {
			if out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
			}
		}
		start = end
	}
	return out
}

func bytesIndex(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return i
		}
	}
	return -1
}

func firstFrameworkCapture(left, right []byte) string {
	if left != nil {
		return unescapeFrameworkTemplateLiteral(string(left), '\'')
	}
	return unescapeFrameworkTemplateLiteral(string(right), '"')
}

func unescapeFrameworkTemplateLiteral(value string, quote byte) string {
	value = strings.ReplaceAll(value, "\\\\", "\\")
	value = strings.ReplaceAll(value, "\\"+string(quote), string(quote))
	return value
}

func frameworkSafeRelativeName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsRune(name, 0) || strings.ContainsAny(name, "$`{}") {
		return false
	}
	slash := strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(slash, "/") || strings.Contains(slash, "://") {
		return false
	}
	for _, part := range strings.Split(slash, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

func frameworkFindExistingUpward(root, fromRel string, children ...string) []string {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	rootAbs = filepath.Clean(rootAbs)
	fromAbs := filepath.Clean(filepath.Join(rootAbs, filepath.FromSlash(fromRel)))
	if !frameworkPathWithin(rootAbs, fromAbs) {
		return nil
	}
	dir := fromAbs
	if fromRel != "" && fromRel != "." {
		dir = filepath.Dir(fromAbs)
	}
	for {
		found := []string{}
		for _, child := range children {
			if rel := frameworkExistingRepoFile(rootAbs, filepath.Join(dir, child)); rel != "" {
				found = append(found, rel)
			}
		}
		if len(found) > 0 {
			return sortedUniqueStrings(found)
		}
		if dir == rootAbs {
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir || !frameworkPathWithin(rootAbs, parent) {
			return nil
		}
		dir = parent
	}
}

func frameworkExistingRepoFile(root, candidate string) string {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	rootAbs = filepath.Clean(rootAbs)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(rootAbs, filepath.FromSlash(candidate))
	}
	candidate = filepath.Clean(candidate)
	if !frameworkPathWithin(rootAbs, candidate) {
		return ""
	}
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return ""
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return ""
	}
	resolvedCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil || !frameworkPathWithin(resolvedRoot, resolvedCandidate) {
		return ""
	}
	rel, err := filepath.Rel(rootAbs, candidate)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

func frameworkPathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sortedUniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func isFrameworkSpace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\r' || char == '\n' || char == '\f'
}

func isFrameworkIdentStart(char byte) bool {
	return char == '_' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= 0x80
}

func isFrameworkIdentContinue(char byte) bool {
	return isFrameworkIdentStart(char) || char >= '0' && char <= '9'
}

func skipFrameworkLine(source []byte, start int) int {
	for start < len(source) && source[start] != '\n' && source[start] != '\r' {
		start++
	}
	return start
}

func skipFrameworkBlockComment(source []byte, start int) int {
	for start+1 < len(source) {
		if source[start] == '*' && source[start+1] == '/' {
			return start + 2
		}
		start++
	}
	return len(source)
}
