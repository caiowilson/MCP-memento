package evaluation

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"memento-mcp/internal/indexing"
)

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
