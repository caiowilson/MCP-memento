package indexing

import (
	"path/filepath"
	"strings"
	"unicode"
)

// TermSearchVersion fingerprints the deterministic tokenizer, stop words,
// conservative inflection matching, coverage boost, content evidence, and a
// bounded path tie-break.
const TermSearchVersion = "terms-v2"

var searchStopWords = map[string]struct{}{
	"a": {}, "all": {}, "an": {}, "and": {}, "are": {}, "be": {},
	"by": {}, "can": {}, "do": {}, "does": {}, "for": {}, "from": {},
	"how": {}, "in": {}, "into": {}, "is": {}, "it": {}, "its": {},
	"of": {}, "on": {}, "or": {}, "that": {}, "the": {}, "this": {},
	"to": {}, "what": {}, "where": {}, "which": {}, "with": {},
}

func meaningfulSearchTerms(query string) []string {
	tokens := identifierSearchTokens(query)
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

func termAwareChunkScore(chunk Chunk, queryTerms []string) int {
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
	return score
}

func phpDeclarationHeaderTokens(chunk Chunk) []string {
	if !usesPHPDeclarationChunks(chunk.Path, chunk.Language) {
		return nil
	}
	var header strings.Builder
	for _, line := range strings.Split(chunk.Content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "<?php" || line == "?>" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "*") {
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
