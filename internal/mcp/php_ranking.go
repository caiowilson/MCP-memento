package mcp

import (
	"context"
	"path/filepath"
	"sort"

	"memento-mcp/internal/indexing"
)

type phpRelationshipProvider struct {
	rootAbs string
}

const PHPRelationshipProviderVersion = "php-relationships-v1"

// NewPHPRelationshipProvider adapts the cached parser-, Composer-, and
// framework-backed PHP graph for term-aware ranking.
func NewPHPRelationshipProvider(rootAbs string) indexing.RelationshipProvider {
	return phpRelationshipProvider{rootAbs: filepath.Clean(rootAbs)}
}

func (phpRelationshipProvider) Fingerprint() string {
	return PHPRelationshipProviderVersion
}

func (provider phpRelationshipProvider) InvalidateRelationships() {
	InvalidatePHPIncludeGraphCache(provider.rootAbs)
}

func (provider phpRelationshipProvider) Relationships(ctx context.Context, candidatePaths []string) ([]indexing.RelationshipEdge, error) {
	graph, err := getPHPIncludeGraph(ctx, provider.rootAbs)
	if err != nil {
		return nil, err
	}
	candidates := make(map[string]struct{}, len(candidatePaths))
	for _, path := range candidatePaths {
		candidates[filepath.ToSlash(filepath.Clean(path))] = struct{}{}
	}
	edges := make([]indexing.RelationshipEdge, 0)
	seen := map[[2]string]struct{}{}
	appendEdges := func(forward map[string][]string) {
		for from := range candidates {
			for _, to := range forward[from] {
				if from == to {
					continue
				}
				if _, ok := candidates[to]; !ok {
					continue
				}
				key := [2]string{from, to}
				if _, duplicate := seen[key]; duplicate {
					continue
				}
				seen[key] = struct{}{}
				edges = append(edges, indexing.RelationshipEdge{FromPath: from, ToPath: to})
			}
		}
	}
	appendEdges(graph.imports)
	appendEdges(graph.references)
	appendEdges(graph.autoloads)
	sort.Slice(edges, func(left, right int) bool {
		if edges[left].FromPath != edges[right].FromPath {
			return edges[left].FromPath < edges[right].FromPath
		}
		return edges[left].ToPath < edges[right].ToPath
	})
	return edges, nil
}
