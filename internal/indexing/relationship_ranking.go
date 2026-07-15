package indexing

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
)

// RelationshipEdge is a directed file-level relationship between two paths
// already eligible for retrieval. Providers return repository-relative paths.
type RelationshipEdge struct {
	FromPath string
	ToPath   string
}

// RelationshipProvider supplies repository relationships for a bounded set of
// existing lexical candidates. It cannot introduce retrieval candidates.
type RelationshipProvider interface {
	Fingerprint() string
	Relationships(context.Context, []string) ([]RelationshipEdge, error)
}

type relationshipInvalidator interface {
	InvalidateRelationships()
}

// TermSearchAdapterVersion identifies both deterministic term scoring and the
// optional relationship evidence used by a concrete indexer.
func TermSearchAdapterVersion(provider RelationshipProvider) string {
	if provider == nil {
		return TermSearchVersion
	}
	fingerprint := strings.TrimSpace(provider.Fingerprint())
	if fingerprint == "" {
		return TermSearchVersion + "+unknown-relationships"
	}
	return TermSearchVersion + "+" + fingerprint
}

func relationshipRankingWindow(results []scoredSearchResult, maxResults int) []string {
	indices := make([]int, len(results))
	for index := range results {
		indices[index] = index
	}
	sort.Slice(indices, func(left, right int) bool {
		a, b := results[indices[left]], results[indices[right]]
		if a.lexical != b.lexical {
			return a.lexical > b.lexical
		}
		if a.semantic != b.semantic {
			return a.semantic > b.semantic
		}
		if a.chunk.Path != b.chunk.Path {
			return a.chunk.Path < b.chunk.Path
		}
		return a.chunk.StartLine < b.chunk.StartLine
	})
	limit := max(20, maxResults*4)
	limit = min(100, limit)
	paths := make([]string, 0, min(limit, len(indices)))
	seen := map[string]struct{}{}
	for _, index := range indices {
		if results[index].lexical <= 0 {
			continue
		}
		path := filepath.ToSlash(filepath.Clean(results[index].chunk.Path))
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
		if len(paths) == limit {
			break
		}
	}
	return paths
}
