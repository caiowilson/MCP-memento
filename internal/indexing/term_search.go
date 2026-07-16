package indexing

import (
	"path/filepath"
	"strings"
	"unicode"
)

// TermSearchVersion fingerprints the deterministic tokenizer, stop words,
// conservative inflection matching, coverage boost, content evidence,
// structural query intent, and bounded relationship-role evidence.
const TermSearchVersion = "terms-v11"

type termSearchIntent struct {
	definition               bool
	attribute                bool
	callable                 bool
	configDefinition         bool
	relationDeclaration      bool
	collectionRelation       bool
	neverTermination         bool
	backedEnumDefinition     bool
	catalogDomainDefinition  bool
	shutdownRegistration     bool
	uninstallRegistration    bool
	preferRelationshipTarget bool
	preferRelationshipSource bool
}

var searchStopWords = map[string]struct{}{
	"a": {}, "all": {}, "an": {}, "and": {}, "are": {}, "be": {},
	"by": {}, "can": {}, "do": {}, "does": {}, "for": {}, "from": {},
	"how": {}, "in": {}, "into": {}, "is": {}, "it": {}, "its": {},
	"of": {}, "on": {}, "or": {}, "that": {}, "the": {}, "this": {},
	"to": {}, "what": {}, "where": {}, "which": {}, "with": {},
}

func meaningfulSearchTerms(query string) []string {
	tokens := identifierSearchTokens(positiveSearchClause(query))
	out := make([]string, 0, len(tokens))
	seen := map[string]struct{}{}
	for _, token := range tokens {
		token = canonicalSearchTerm(token)
		if len(token) < 3 {
			continue
		}
		if _, stop := searchStopWords[token]; stop {
			continue
		}
		if _, duplicate := seen[token]; duplicate {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func meaningfulSearchTermsForIntent(query string, intent termSearchIntent) []string {
	if intent.definition && (intent.configDefinition || intent.relationDeclaration) {
		query = definitionSearchClause(query)
	}
	return meaningfulSearchTerms(query)
}

func classifyTermSearchIntent(query string) termSearchIntent {
	lower := strings.ToLower(positiveSearchClause(query))
	specificationAction := containsAnySearchToken(lower, "specify", "specifies", "specified", "specifying")
	fixAction := containsAnySearchToken(lower, "fix", "fixes", "fixed", "fixing")
	definitionLocus := containsAnySearchToken(lower, "source", "declaration", "definition", "enum", "type")
	consumerLocus := containsAnySearchToken(lower,
		"function", "method", "serializer", "presenter", "renderer", "formatter", "encoder", "decoder", "mapper",
		"render", "renders", "rendered", "rendering", "serialize", "serializes", "serialized", "serializing", "present", "presents", "presented", "presenting",
	)
	fixDefinitionAction := fixAction && definitionLocus && !consumerLocus
	callableIntent := containsAny(lower,
		"callable", "closure", "arrow function", "anonymous function", "passed around", "run later", "invoked later", "stored for later", "executed later",
	)
	explicitDefinition := containsAny(lower,
		" defin", " declar", " assign", " map", " register", " restrict", " annotat", " marked with ", " attach", " bind", " establish", " enumerat",
	) || specificationAction
	definition := explicitDefinition || strings.Contains(lower, " wire")
	configConcept := containsAny(lower, "config", "setting", "service container")
	configDefinition := configConcept && (definition || containsAny(lower,
		"configuration line", "configuration entry", "configuration value", "config line", "config entry", "config key",
	))
	relationConcept := containsAny(lower,
		"relationship", "repository class", "repository mapping", "entity mapping", "belongs-to", "belongs to", "has-many", "has many", "has-one", "has one", "many-to-one", "one-to-many",
	)
	collectionRelation := containsAny(lower, "parent record", "parent model", "one parent") && containsAny(lower,
		"collection of dependent", "collection of child", "collection of related", "many dependent records", "many child records",
	)
	enumConcept := containsAny(lower, "phase", "state", "status", "outcome", "verdict", "option", "choice")
	serializedValueConcept := containsAny(lower, "persist", "string", "integer", "int code", "code", "value")
	eachDomainMember := containsAnySearchToken(lower, "each")
	closedDomainConcept := containsAny(lower, "allowed", "canonical", "every", "all ", " enum", "case", "set of") || eachDomainMember
	durableCatalogRepresentation := containsAnySearchToken(lower, "durable") && containsAnySearchToken(lower, "catalog")
	serializedRepresentationConcept := containsAny(lower, "wire", "serializ", "persist", "stored", "storage", "database") || durableCatalogRepresentation
	catalogSpelling := containsAnySearchToken(lower, "spelling", "spellings")
	domainValueConcept := containsAny(lower, "label", "code", "value", "identifier", "literal", "string", "integer") || catalogSpelling
	serializedDomainDefinitionAction := containsAny(lower, " defin", " declar", " establish", " enumerat") || specificationAction || fixDefinitionAction
	serializedDomainValueDefinition := serializedDomainDefinitionAction && closedDomainConcept && serializedRepresentationConcept && domainValueConcept
	catalogDomainDefinition := fixDefinitionAction && eachDomainMember && durableCatalogRepresentation && catalogSpelling
	backedEnumDefinition := definition && enumConcept && serializedValueConcept && containsAny(lower, "allowed", "canonical", "persist", "case", "every") || serializedDomainValueDefinition
	terminationConcept := containsAny(lower,
		"script termination", "process termination", "process terminates", "process exits", "shutdown", "after termination", "early exit", "last-chance", "finalization",
	)
	bindingConcept := containsAny(lower, "install", "register", "registration", "implementation", "attach", "bind", "hook", "wire")
	shutdownRegistration := terminationConcept && containsAny(lower, "callback", "function", "hook") && bindingConcept
	pluginDeletion := strings.Contains(lower, "plugin") && containsAny(lower,
		"is deleted", "deletes the plugin", "delete the plugin", "plugin deletion", "deleted plugin", "uninstall", "is removed", "removes the plugin",
	)
	uninstallRegistration := pluginDeletion && bindingConcept
	intent := termSearchIntent{
		definition:              definition,
		attribute:               containsAny(lower, "attribute", "annotation", "annotated", "metadata", "marked with "),
		callable:                callableIntent,
		configDefinition:        configDefinition,
		relationDeclaration:     relationConcept && definition || collectionRelation,
		collectionRelation:      collectionRelation,
		neverTermination:        strings.Contains(lower, "never") && containsAny(lower, "throw", "terminat", "does not return", "never return"),
		backedEnumDefinition:    backedEnumDefinition,
		catalogDomainDefinition: catalogDomainDefinition,
		shutdownRegistration:    shutdownRegistration,
		uninstallRegistration:   uninstallRegistration,
	}
	intent.preferRelationshipTarget = backedEnumDefinition || shutdownRegistration || configDefinition
	intent.preferRelationshipSource = relationConcept && definition || collectionRelation || uninstallRegistration
	return intent
}

func (intent termSearchIntent) usesRelationships() bool {
	return intent.preferRelationshipTarget || intent.preferRelationshipSource
}

func termSearchRelationshipBonuses(candidatePaths []string, edges []RelationshipEdge, intent termSearchIntent) map[string]int {
	const relationshipUnit = 20
	candidates := make(map[string]struct{}, len(candidatePaths))
	for _, path := range candidatePaths {
		candidates[filepath.ToSlash(filepath.Clean(path))] = struct{}{}
	}
	bonuses := map[string]int{}
	for _, edge := range edges {
		from := filepath.ToSlash(filepath.Clean(edge.FromPath))
		to := filepath.ToSlash(filepath.Clean(edge.ToPath))
		if from == to {
			continue
		}
		if _, ok := candidates[from]; !ok {
			continue
		}
		if _, ok := candidates[to]; !ok {
			continue
		}
		if intent.preferRelationshipSource {
			bonuses[from] = relationshipUnit
		}
		if intent.preferRelationshipTarget {
			bonuses[to] = relationshipUnit
		}
	}
	return bonuses
}

func definitionSearchClause(query string) string {
	lower := strings.ToLower(query)
	end := len(query)
	for _, marker := range []string{" consumed by ", " used by ", " read by ", " referenced by ", " injected into "} {
		if index := strings.Index(lower, marker); index >= 0 && index < end {
			end = index
		}
	}
	return query[:end]
}

func positiveSearchClause(query string) string {
	lower := strings.ToLower(query)
	end := len(query)
	for _, marker := range []string{" instead of ", " rather than "} {
		if index := strings.Index(lower, marker); index >= 0 && index < end {
			end = index
		}
	}
	return query[:end]
}

func termAwareChunkScore(chunk Chunk, queryTerms []string) int {
	return termAwareChunkScoreWithIntent(chunk, queryTerms, termSearchIntent{})
}

func termAwareChunkScoreWithIntent(chunk Chunk, queryTerms []string, intent termSearchIntent) int {
	contentTokens := identifierSearchTokens(chunk.Content)
	pathTokens := identifierSearchTokens(filepath.ToSlash(chunk.Path))
	declarationTokens := phpDeclarationHeaderTokens(chunk)
	matched, contentMatches, declarationMatches, score := 0, 0, 0, 0
	for _, query := range queryTerms {
		contentQuality := bestSearchTermQuality(query, contentTokens)
		pathQuality := bestSearchTermQuality(query, pathTokens)
		if pathQuality > 0 && contentQuality == 0 {
			pathQuality++
		}
		quality := max(contentQuality, pathQuality)
		if quality == 0 {
			continue
		}
		matched++
		if contentQuality > 0 {
			contentMatches++
		}
		if bestSearchTermQuality(query, declarationTokens) > 0 {
			declarationMatches++
		}
		score += quality
	}
	if matched == 0 {
		return 0
	}
	// Coverage dominates repeated common words so multi-concept matches rank
	// ahead of chunks that only repeat a ubiquitous project noun.
	// Content evidence then resolves chunks from the same path whose file-name
	// concepts are identical, keeping namespace/import headers below the member
	// that actually answers the query.
	score += matched*matched*6 + contentMatches*4 + declarationMatches*8
	if isPHPHeaderOnlyChunk(chunk) && !queryTargetsPHPHeader(queryTerms) {
		// Namespace/import headers share file-path concepts with every member.
		// Keep them discoverable for direct namespace queries without allowing
		// boilerplate to outrank the declaration that carries the answer.
		score /= 4
	}
	score += termSearchStructuralScore(chunk, intent)
	if score < 1 {
		return 1
	}
	return score
}

func termSearchStructuralScore(chunk Chunk, intent termSearchIntent) int {
	const exactTermUnit = 20

	content := strings.ToLower(chunk.Content)
	path := strings.ToLower(filepath.ToSlash(chunk.Path))
	score := 0

	if intent.attribute && strings.Contains(content, "#[") {
		score += exactTermUnit
		if intent.definition && strings.Contains(content, "#[attribute") {
			score += exactTermUnit
		}
	}
	if intent.callable {
		compact := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "").Replace(content)
		if (strings.Contains(compact, "->") || strings.Contains(compact, "::")) && strings.Contains(compact, "(...)") {
			score += 2 * exactTermUnit
		}
		if strings.Contains(compact, "fn(") || strings.Contains(compact, "staticfunction(") || strings.Contains(compact, "function(") {
			score += exactTermUnit
		}
	}
	if intent.configDefinition {
		if isConfigurationPath(path) {
			score += 2 * exactTermUnit
			if strings.Contains(content, "=>") || strings.Contains(content, ":") || strings.Contains(content, "=") {
				score += exactTermUnit
			}
		}
		if containsAny(content, "config(", "getenv(", "->getparameter(") {
			score -= exactTermUnit
		}
	}
	if intent.relationDeclaration {
		relationSyntax := containsAny(content, "belongsto(", "hasmany(", "hasone(", "manytoone", "onetomany")
		if strings.Contains(content, "repositoryclass") {
			score += 2 * exactTermUnit
		}
		if relationSyntax {
			score += 2 * exactTermUnit
		}
		if (!intent.collectionRelation || relationSyntax) && (strings.Contains(path, "/entity/") || strings.Contains(path, "/models/") || strings.HasPrefix(path, "entity/") || strings.HasPrefix(path, "models/")) {
			score += exactTermUnit
		}
		if strings.Contains(path, "/service/") || strings.Contains(path, "/services/") || strings.Contains(path, "/controller/") || strings.Contains(path, "/handler/") {
			score -= exactTermUnit
		}
	}
	if intent.neverTermination {
		compact := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "").Replace(content)
		if strings.Contains(compact, ":never") && strings.Contains(content, "throw") {
			score += 2 * exactTermUnit
		}
	}
	if intent.backedEnumDefinition {
		if strings.Contains(content, "enum ") && containsAny(content, ": string", ": int") {
			score += 3 * exactTermUnit
		}
		if strings.Contains(content, "match (") || strings.Contains(content, "match(") {
			score -= exactTermUnit
		}
	}
	if intent.catalogDomainDefinition && strings.Contains(content, "enum ") && containsAny(content, ": string", ": int") {
		// High-confidence synonym intent needs one additional unit because PHP
		// declaration chunks keep enum cases separate from the enum header.
		score += exactTermUnit
	}
	if intent.shutdownRegistration {
		if strings.Contains(content, "register_shutdown_function(") {
			score += 3 * exactTermUnit
		} else if strings.Contains(content, "exit(") {
			score -= exactTermUnit
		}
	}
	if intent.uninstallRegistration {
		if strings.Contains(content, "register_uninstall_hook(") {
			score += 3 * exactTermUnit
		} else if strings.Contains(content, "register_deactivation_hook(") {
			score -= exactTermUnit
		}
	}
	maxStructuralScore := 3 * exactTermUnit
	if intent.catalogDomainDefinition {
		maxStructuralScore = 4 * exactTermUnit
	}
	if score > maxStructuralScore {
		return maxStructuralScore
	}
	if score < -exactTermUnit {
		return -exactTermUnit
	}
	return score
}

func isConfigurationPath(path string) bool {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(path))
	return strings.Contains(path, "/config/") || strings.HasPrefix(path, "config/") || strings.HasPrefix(base, "config.") || ext == ".yaml" || ext == ".yml" || ext == ".json" || ext == ".toml"
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func containsAnySearchToken(value string, needles ...string) bool {
	for _, token := range identifierSearchTokens(value) {
		for _, needle := range needles {
			if token == needle {
				return true
			}
		}
	}
	return false
}

func phpDeclarationHeaderTokens(chunk Chunk) []string {
	if !usesPHPDeclarationChunks(chunk.Path, chunk.Language) {
		return nil
	}
	var header strings.Builder
	for _, line := range strings.Split(chunk.Content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "<?php" || line == "?>" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#[") || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "*") {
			continue
		}
		if strings.HasPrefix(line, "declare(") || strings.HasPrefix(line, "require") {
			continue
		}
		delimiter := strings.IndexAny(line, "{;")
		if delimiter >= 0 {
			line = line[:delimiter]
		}
		header.WriteString(line)
		header.WriteByte(' ')
		if delimiter >= 0 {
			break
		}
	}
	return identifierSearchTokens(header.String())
}

func queryTargetsPHPHeader(queryTerms []string) bool {
	for _, term := range queryTerms {
		switch canonicalSearchTerm(term) {
		case "import", "namespace", "use":
			return true
		}
	}
	return false
}

func isPHPHeaderOnlyChunk(chunk Chunk) bool {
	if !usesPHPDeclarationChunks(chunk.Path, chunk.Language) {
		return false
	}
	sawHeader := false
	for _, line := range strings.Split(chunk.Content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "<?php" || line == "?>" {
			continue
		}
		if strings.HasPrefix(line, "declare(") || strings.HasPrefix(line, "namespace ") || strings.HasPrefix(line, "use ") {
			sawHeader = true
			continue
		}
		return false
	}
	return sawHeader
}

func termAwarePathScore(path string, queryTerms []string) int {
	matched, score := termMatchScore(queryTerms, identifierSearchTokens(filepath.ToSlash(path)))
	if matched == 0 {
		return 0
	}
	return score + matched*8
}

func termMatchScore(queryTerms, documentTokens []string) (int, int) {
	matched, score := 0, 0
	for _, query := range queryTerms {
		best := bestSearchTermQuality(query, documentTokens)
		if best > 0 {
			matched++
			score += best
		}
	}
	return matched, score
}

func bestSearchTermQuality(query string, documentTokens []string) int {
	best := 0
	for _, token := range documentTokens {
		quality := searchTermMatchQuality(query, token)
		if quality > best {
			best = quality
		}
		if best == 20 {
			break
		}
	}
	return best
}

func searchTermMatchQuality(left, right string) int {
	left = canonicalSearchTerm(left)
	right = canonicalSearchTerm(right)
	if left == right {
		return 20
	}
	if len(left) < 4 || len(right) < 4 {
		return 0
	}
	common := commonSearchPrefix(left, right)
	shorter := min(len(left), len(right))
	if common == shorter {
		longer := left
		if len(right) > len(left) {
			longer = right
		}
		suffix := longer[common:]
		if searchInflectionSuffix(suffix) || searchDoubledInflection(longer[:common], suffix) {
			return 14
		}
		return 0
	}
	if common >= 4 && common >= shorter-2 && searchInflectionSuffix(left[common:]) && searchInflectionSuffix(right[common:]) {
		return 10
	}
	return 0
}

func canonicalSearchTerm(value string) string {
	switch value {
	case "bound", "binding", "bindings", "binds":
		return "bind"
	case "const", "constants":
		return "constant"
	case "iterable", "iterator", "iteration", "iterations":
		return "iterate"
	case "itself":
		return "this"
	case "located", "location", "locations":
		return "locate"
	default:
		return value
	}
}

func termCandidateProbes(term string) []string {
	if term == "bind" {
		// "bound" canonicalizes to "bind" for scoring but does not share its
		// raw trigram prefix, so include both forms in the conservative filter.
		return []string{"bind", "boun"}
	}
	return []string{term}
}

func searchInflectionSuffix(value string) bool {
	switch value {
	case "d", "r", "ed", "er", "ers", "es", "ing", "ly", "s", "y", "ies", "able", "ible", "al", "ation", "ations", "ment", "ments", "ize", "ized", "ization":
		return true
	default:
		return false
	}
}

func searchDoubledInflection(stem, suffix string) bool {
	if len(stem) == 0 || len(suffix) < 2 || suffix[0] != stem[len(stem)-1] {
		return false
	}
	return searchInflectionSuffix(suffix[1:])
}

func commonSearchPrefix(left, right string) int {
	limit := min(len(left), len(right))
	for index := 0; index < limit; index++ {
		if left[index] != right[index] {
			return index
		}
	}
	return limit
}

func identifierSearchTokens(value string) []string {
	out := []string{}
	current := []rune{}
	flush := func() {
		if len(current) == 0 {
			return
		}
		out = append(out, strings.ToLower(string(current)))
		current = current[:0]
	}
	var previous rune
	runes := []rune(value)
	for index, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			previous = 0
			continue
		}
		nextIsLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
		if len(current) > 0 && unicode.IsUpper(r) && (unicode.IsLower(previous) || unicode.IsDigit(previous) || unicode.IsUpper(previous) && nextIsLower) {
			flush()
		}
		current = append(current, r)
		previous = r
	}
	flush()
	return out
}
