package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"memento-mcp/internal/indexing"
)

// FixtureSet describes retrieval queries and their relevance judgments.
type FixtureSet struct {
	Version int            `json:"version"`
	K       int            `json:"k"`
	Queries []QueryFixture `json:"queries"`
}

type QueryFixture struct {
	ID       string          `json:"id"`
	Query    string          `json:"query"`
	Relevant []RelevantChunk `json:"relevant"`
}

// RelevantChunk identifies either a whole file or a line-bounded chunk. When
// StartLine and EndLine are omitted, the first retrieved chunk for Path counts.
type RelevantChunk struct {
	Path      string `json:"path"`
	StartLine int    `json:"startLine,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
}

type Metrics struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	MRR       float64 `json:"mrr"`
	NDCG      float64 `json:"ndcg"`
}

type QueryResult struct {
	ID        string           `json:"id"`
	Query     string           `json:"query"`
	K         int              `json:"k"`
	Retrieved []indexing.Chunk `json:"retrieved"`
	Metrics   Metrics          `json:"metrics"`
}

type Report struct {
	K       int           `json:"k"`
	Queries []QueryResult `json:"queries"`
	Metrics Metrics       `json:"metrics"`
}

func LoadFixtures(r io.Reader) (FixtureSet, error) {
	var fixtures FixtureSet
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&fixtures); err != nil {
		return FixtureSet{}, fmt.Errorf("decode fixtures: %w", err)
	}
	if err := fixtures.validate(); err != nil {
		return FixtureSet{}, err
	}
	return fixtures, nil
}

func LoadFixtureFile(path string) (FixtureSet, error) {
	f, err := os.Open(path)
	if err != nil {
		return FixtureSet{}, err
	}
	defer f.Close()
	return LoadFixtures(f)
}

func (f FixtureSet) validate() error {
	if f.Version != 1 {
		return fmt.Errorf("unsupported fixture version %d", f.Version)
	}
	if f.K <= 0 {
		return errors.New("fixture k must be greater than zero")
	}
	if len(f.Queries) == 0 {
		return errors.New("fixtures must contain at least one query")
	}
	ids := make(map[string]struct{}, len(f.Queries))
	for i, q := range f.Queries {
		if strings.TrimSpace(q.ID) == "" {
			return fmt.Errorf("queries[%d].id is required", i)
		}
		if _, exists := ids[q.ID]; exists {
			return fmt.Errorf("duplicate query id %q", q.ID)
		}
		ids[q.ID] = struct{}{}
		if strings.TrimSpace(q.Query) == "" {
			return fmt.Errorf("query %q has an empty query", q.ID)
		}
		if len(q.Relevant) == 0 {
			return fmt.Errorf("query %q has no relevance judgments", q.ID)
		}
		for j, rel := range q.Relevant {
			clean := filepath.ToSlash(filepath.Clean(rel.Path))
			if rel.Path == "" || clean == "." || clean != rel.Path || filepath.IsAbs(rel.Path) || strings.HasPrefix(clean, "../") {
				return fmt.Errorf("query %q relevant[%d] has invalid repo-relative path %q", q.ID, j, rel.Path)
			}
			if (rel.StartLine == 0) != (rel.EndLine == 0) || rel.StartLine < 0 || rel.EndLine < rel.StartLine {
				return fmt.Errorf("query %q relevant[%d] has invalid line range %d-%d", q.ID, j, rel.StartLine, rel.EndLine)
			}
		}
	}
	return nil
}

// Execute indexes root into storeDir and evaluates the fixture file against the
// same deterministic lexical search implementation used by the MCP server.
func Execute(ctx context.Context, root, fixturePath, storeDir string) (Report, error) {
	fixtures, err := LoadFixtureFile(fixturePath)
	if err != nil {
		return Report{}, err
	}
	idx, err := indexing.New(indexing.Config{
		RootAbs:         root,
		StoreDir:        storeDir,
		PollInterval:    0,
		ExtraIgnoreDirs: []string{"evaluation"},
	})
	if err != nil {
		return Report{}, err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	idx.Start(workerCtx)
	if err := idx.IndexAll(ctx); err != nil {
		return Report{}, fmt.Errorf("index corpus: %w", err)
	}

	report := Report{K: fixtures.K, Queries: make([]QueryResult, 0, len(fixtures.Queries))}
	for _, fixture := range fixtures.Queries {
		retrieved, err := idx.Search(fixture.Query, fixtures.K, nil)
		if err != nil {
			return Report{}, fmt.Errorf("search query %q: %w", fixture.ID, err)
		}
		result := Evaluate(fixture, retrieved, fixtures.K)
		report.Queries = append(report.Queries, result)
		report.Metrics.Precision += result.Metrics.Precision
		report.Metrics.Recall += result.Metrics.Recall
		report.Metrics.MRR += result.Metrics.MRR
		report.Metrics.NDCG += result.Metrics.NDCG
	}
	n := float64(len(report.Queries))
	report.Metrics.Precision /= n
	report.Metrics.Recall /= n
	report.Metrics.MRR /= n
	report.Metrics.NDCG /= n
	return report, nil
}

// Evaluate computes binary relevance metrics at k. A relevance judgment may be
// matched at most once, so duplicate retrieved chunks do not inflate recall.
func Evaluate(fixture QueryFixture, retrieved []indexing.Chunk, k int) QueryResult {
	if k < 1 {
		k = 1
	}
	if len(retrieved) > k {
		retrieved = retrieved[:k]
	}
	result := QueryResult{ID: fixture.ID, Query: fixture.Query, K: k, Retrieved: retrieved}
	matched := make([]bool, len(fixture.Relevant))
	hits := 0
	dcg := 0.0
	for rank, chunk := range retrieved {
		judgment := matchingJudgment(chunk, fixture.Relevant, matched)
		if judgment < 0 {
			continue
		}
		matched[judgment] = true
		hits++
		if result.Metrics.MRR == 0 {
			result.Metrics.MRR = 1 / float64(rank+1)
		}
		dcg += 1 / math.Log2(float64(rank+2))
	}
	result.Metrics.Precision = float64(hits) / float64(k)
	result.Metrics.Recall = float64(hits) / float64(len(fixture.Relevant))
	idealHits := min(k, len(fixture.Relevant))
	idcg := 0.0
	for rank := 0; rank < idealHits; rank++ {
		idcg += 1 / math.Log2(float64(rank+2))
	}
	if idcg > 0 {
		result.Metrics.NDCG = dcg / idcg
	}
	return result
}

func matchingJudgment(chunk indexing.Chunk, relevant []RelevantChunk, matched []bool) int {
	for i, judgment := range relevant {
		if matched[i] || chunk.Path != judgment.Path {
			continue
		}
		if judgment.StartLine == 0 || (chunk.StartLine <= judgment.EndLine && chunk.EndLine >= judgment.StartLine) {
			return i
		}
	}
	return -1
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
