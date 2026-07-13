package evaluation

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"memento-mcp/internal/embedding"
	"memento-mcp/internal/indexing"
)

type evaluationEmbedder struct{}

func (evaluationEmbedder) Embed(_ context.Context, task embedding.Task, inputs []string) ([][]float32, error) {
	out := make([][]float32, 0, len(inputs))
	for _, input := range inputs {
		lower := strings.ToLower(input)
		if task == embedding.TaskQuery || strings.Contains(lower, "auth.go") {
			out = append(out, []float32{1, 0})
		} else {
			out = append(out, []float32{0, 1})
		}
	}
	return out, nil
}

func (evaluationEmbedder) Fingerprint() string { return "evaluation-semantic-v1" }
func (evaluationEmbedder) Name() string        { return "test/evaluation-semantic" }

type unavailableEvaluationEmbedder struct{}

func (unavailableEvaluationEmbedder) Embed(context.Context, embedding.Task, []string) ([][]float32, error) {
	return nil, errors.New("model unavailable")
}

func (unavailableEvaluationEmbedder) Fingerprint() string { return "evaluation-unavailable-v1" }
func (unavailableEvaluationEmbedder) Name() string        { return "test/evaluation-unavailable" }

func TestEvaluateMetrics(t *testing.T) {
	fixture := QueryFixture{
		ID:    "metrics",
		Query: "needle",
		Relevant: []RelevantChunk{
			{Path: "a.go", StartLine: 10, EndLine: 10},
			{Path: "b.go"},
		},
	}
	retrieved := []indexing.Chunk{
		{Path: "noise.go", StartLine: 1, EndLine: 20},
		{Path: "a.go", StartLine: 1, EndLine: 12},
		{Path: "a.go", StartLine: 8, EndLine: 15}, // duplicate judgment
		{Path: "b.go", StartLine: 30, EndLine: 40},
	}

	got := Evaluate(fixture, retrieved, 4).Metrics
	assertNear(t, "precision@4", got.Precision, 0.5)
	assertNear(t, "recall@4", got.Recall, 1)
	assertNear(t, "MRR", got.MRR, 0.5)
	wantNDCG := (1/math.Log2(3) + 1/math.Log2(5)) / (1 + 1/math.Log2(3))
	assertNear(t, "nDCG@4", got.NDCG, wantNDCG)
}

func TestEvaluateDoesNotCreditWrongRangeInCorrectPath(t *testing.T) {
	fixture := QueryFixture{
		ID:       "range-sensitive",
		Query:    "target declaration",
		Relevant: []RelevantChunk{{Path: "target.php", StartLine: 20, EndLine: 25}},
	}
	retrieved := []indexing.Chunk{{Path: "target.php", StartLine: 1, EndLine: 10}}

	got := Evaluate(fixture, retrieved, 1).Metrics
	if got.Precision != 0 || got.Recall != 0 || got.MRR != 0 || got.NDCG != 0 {
		t.Fatalf("wrong range in correct path received relevance credit: %#v", got)
	}
}

func TestExecuteIsDeterministic(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "alpha.go"), "package fixture\n\n// StableToken marks alpha.\nfunc StableToken() {}\n")
	mustWrite(t, filepath.Join(root, "beta.go"), "package fixture\n\n// StableToken marks beta.\nfunc Beta() {}\n")
	fixturePath := filepath.Join(t.TempDir(), "retrieval.json")
	mustWrite(t, fixturePath, `{
  "version": 1,
  "k": 2,
  "queries": [{
    "id": "stable-token",
    "query": "StableToken",
    "relevant": [
      {"path": "alpha.go", "startLine": 3, "endLine": 4},
      {"path": "beta.go", "startLine": 3, "endLine": 3}
    ]
  }]
}`)

	first, err := Execute(context.Background(), root, fixturePath, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Execute(context.Background(), root, fixturePath, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("fixture execution changed between runs:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	if first.Metrics.Recall != 1 || first.Metrics.MRR != 1 {
		t.Fatalf("unexpected deterministic report metrics: %#v", first.Metrics)
	}
}

func TestExecuteWithConfigMeasuresSemanticRetrieval(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "auth.go"), "package fixture\n\n// Guard validates sessions.\nfunc Guard() {}\n")
	mustWrite(t, filepath.Join(root, "database.go"), "package fixture\n\n// Migrate updates schemas.\nfunc Migrate() {}\n")
	fixturePath := filepath.Join(t.TempDir(), "retrieval.json")
	mustWrite(t, fixturePath, `{
  "version": 1,
  "k": 1,
  "queries": [{
    "id": "conceptual-auth",
    "query": "login security",
    "relevant": [{"path": "auth.go"}]
  }]
}`)

	lexical, err := Execute(context.Background(), root, fixturePath, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := ExecuteWithConfig(context.Background(), root, fixturePath, t.TempDir(), ExecuteConfig{
		Embedder:       evaluationEmbedder{},
		SemanticWeight: 0.65,
	})
	if err != nil {
		t.Fatal(err)
	}
	if lexical.Metrics.Recall != 0 {
		t.Fatalf("expected lexical baseline to miss conceptual query, got %#v", lexical.Metrics)
	}
	if semantic.Metrics.Recall != 1 || semantic.Metrics.MRR != 1 {
		t.Fatalf("expected semantic retrieval to find auth.go, got %#v", semantic.Metrics)
	}
}

func TestExecuteWithConfigMeasuresTermAwareRetrieval(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "ReportHandler.php"), "<?php final class ReportHandler { public function handle(): void {} }\n")
	mustWrite(t, filepath.Join(root, "Repository.php"), "<?php final class Repository {}\n")
	fixtures := FixtureSet{Version: 1, K: 1, Queries: []QueryFixture{{
		ID:       "report-handler",
		Query:    "where is a report handled",
		Relevant: []RelevantChunk{{Path: "ReportHandler.php"}},
	}}}

	exact, err := ExecuteFixturesWithConfig(context.Background(), root, t.TempDir(), fixtures, ExecuteConfig{})
	if err != nil {
		t.Fatal(err)
	}
	terms, err := ExecuteFixturesWithConfig(context.Background(), root, t.TempDir(), fixtures, ExecuteConfig{TermAware: true})
	if err != nil {
		t.Fatal(err)
	}
	if exact.Metrics.Recall != 0 {
		t.Fatalf("exact baseline unexpectedly matched natural-language query: %#v", exact)
	}
	if terms.Metrics.Recall != 1 || terms.Metrics.MRR != 1 {
		t.Fatalf("term-aware retrieval missed handler: %#v", terms)
	}
}

func TestExecuteFixturesRejectsDistinctPathsWithoutTermAwareRetrieval(t *testing.T) {
	fixtures := FixtureSet{Version: 1, K: 1, Queries: []QueryFixture{{
		ID:       "report-handler",
		Query:    "report handler",
		Relevant: []RelevantChunk{{Path: "ReportHandler.php"}},
	}}}
	_, err := ExecuteFixturesWithConfig(context.Background(), t.TempDir(), t.TempDir(), fixtures, ExecuteConfig{DistinctPaths: true})
	if err == nil || !strings.Contains(err.Error(), "requires term-aware retrieval") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteWithConfigRejectsSemanticFallbackMetrics(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "auth.go"), "package fixture\n\nfunc Guard() {}\n")
	fixturePath := filepath.Join(t.TempDir(), "retrieval.json")
	mustWrite(t, fixturePath, `{
  "version": 1,
  "k": 1,
  "queries": [{
    "id": "auth",
    "query": "auth",
    "relevant": [{"path": "auth.go"}]
  }]
}`)

	_, err := ExecuteWithConfig(context.Background(), root, fixturePath, t.TempDir(), ExecuteConfig{
		Embedder: unavailableEvaluationEmbedder{},
	})
	if err == nil || !strings.Contains(err.Error(), "semantic evaluation could not build vectors") {
		t.Fatalf("expected semantic evaluator to reject fallback metrics, got %v", err)
	}
}

func TestLoadFixturesRejectsInvalidChunkRange(t *testing.T) {
	_, err := LoadFixtures(strings.NewReader(`{
  "version": 1,
  "k": 5,
  "queries": [{
    "id": "bad-range",
    "query": "needle",
    "relevant": [{"path": "a.go", "startLine": 10}]
  }]
}`))
	if err == nil || !strings.Contains(err.Error(), "invalid line range") {
		t.Fatalf("expected invalid line range error, got %v", err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("%s = %.12f, want %.12f", name, got, want)
	}
}
