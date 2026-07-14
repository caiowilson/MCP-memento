package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"memento-mcp/evaluation"
	"memento-mcp/internal/indexing"
	"memento-mcp/internal/testutil/phpcompat"
)

func TestTermSearchTrainingCasesRankFirst(t *testing.T) {
	suite, err := phpcompat.Load(filepath.Join("..", "..", "evaluation", "php-compat", "suite.v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{
		"php80-audit-attribute-declaration":       true,
		"php81-never-throw":                       true,
		"php81-normalize-first-class-callable":    true,
		"laravel-reporting-config-definition":     true,
		"symfony-audit-entity-repository-mapping": true,
	}
	seen := map[string]bool{}
	for _, corpus := range suite.Corpora {
		selected := corpus
		selected.Retrieval = nil
		for _, query := range corpus.Retrieval {
			if wanted[query.ID] {
				selected.Retrieval = append(selected.Retrieval, query)
			}
		}
		if len(selected.Retrieval) == 0 {
			continue
		}
		root, err := suite.CorpusRoot(corpus)
		if err != nil {
			t.Fatal(err)
		}
		report, err := evaluation.ExecuteFixturesWithConfig(
			context.Background(),
			root,
			filepath.Join(t.TempDir(), corpus.ID),
			phpRetrievalFixtures(selected, suite.RetrievalPolicy.K),
			evaluation.ExecuteConfig{TermAware: true, DistinctPaths: true},
		)
		if err != nil {
			t.Fatalf("%s: %v", corpus.ID, err)
		}
		queries := retrievalQueriesByID(selected)
		for _, result := range report.Queries {
			seen[result.ID] = true
			if result.Metrics.MRR != 1 {
				t.Errorf("%s: expected rank-one relevance, metrics=%#v retrieved=%#v", result.ID, result.Metrics, result.Retrieved)
			}
			if wins := hardNegativeWins(queries[result.ID], result.Retrieved); wins != 0 {
				t.Errorf("%s: hard negative outranked relevance: %#v", result.ID, result.Retrieved)
			}
		}
	}
	for id := range wanted {
		if !seen[id] {
			t.Errorf("training case %s was not evaluated", id)
		}
	}
}

func TestTermsV4PostFreezeHoldout(t *testing.T) {
	suite, err := phpcompat.Load(filepath.Join("..", "..", "evaluation", "php-compat", "suite.v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{
		"postv4-php74-voucher-deprecation":        true,
		"postv4-php80-compiler-closure":           true,
		"postv4-php81-beacon-hint-attachment":     true,
		"postv4-php82-courier-retry-default":      true,
		"postv4-php83-wire-revision":              true,
		"postv4-php84-dock-ticket-hook":           true,
		"postv4-laravel-skiff-harbor":             true,
		"postv4-symfony-cloudberry-batch-default": true,
	}
	seen := map[string]bool{}
	var scores scoreAccumulator
	for _, corpus := range suite.Corpora {
		selected := corpus
		selected.Retrieval = nil
		for _, query := range corpus.Retrieval {
			if wanted[query.ID] {
				selected.Retrieval = append(selected.Retrieval, query)
			}
		}
		if len(selected.Retrieval) == 0 {
			continue
		}
		root, err := suite.CorpusRoot(corpus)
		if err != nil {
			t.Fatal(err)
		}
		report, err := evaluation.ExecuteFixturesWithConfig(
			context.Background(),
			root,
			filepath.Join(t.TempDir(), corpus.ID),
			phpRetrievalFixtures(selected, suite.RetrievalPolicy.K),
			evaluation.ExecuteConfig{TermAware: true, DistinctPaths: true},
		)
		if err != nil {
			t.Fatalf("%s: %v", corpus.ID, err)
		}
		queries := retrievalQueriesByID(selected)
		for _, result := range report.Queries {
			seen[result.ID] = true
			wins := hardNegativeWins(queries[result.ID], result.Retrieved)
			scores.add(result.Metrics, wins)
			if wins != 0 {
				t.Errorf("%s: hard negative outranked relevance: %#v", result.ID, result.Retrieved)
			}
		}
	}
	for id := range wanted {
		if !seen[id] {
			t.Errorf("post-freeze holdout case %s was not evaluated", id)
		}
	}
	actual := scores.score(suite.Thresholds)
	t.Logf("post-freeze holdout queries=%d recall@5=%.3f MRR=%.3f nDCG@5=%.3f hard-negative-wins=%d", actual.Queries, actual.Recall, actual.MRR, actual.NDCG, actual.HardNegativeWins)
	if !actual.Passed {
		t.Fatalf("post-freeze holdout missed thresholds: %#v", actual)
	}
}

func TestRetrievalThresholdsPass(t *testing.T) {
	thresholds := phpcompat.Thresholds{
		RetrievalRecallAt5: 0.95,
		RetrievalMRR:       0.90,
		RetrievalNDCGAt5:   0.90,
	}
	if !retrievalThresholdsPass(retrievalScore{Recall: 0.95, MRR: 0.90, NDCG: 0.90}, thresholds) {
		t.Fatal("metrics at each threshold should pass")
	}
	if retrievalThresholdsPass(retrievalScore{Recall: 0.94, MRR: 1, NDCG: 1}, thresholds) {
		t.Fatal("recall below its threshold should fail")
	}
	if retrievalThresholdsPass(retrievalScore{Recall: 1, MRR: 1, NDCG: 1, HardNegativeWins: 1}, thresholds) {
		t.Fatal("a hard negative ahead of relevance should fail")
	}
}

func TestHardNegativeWinsUsesLineBoundedRank(t *testing.T) {
	query := phpcompat.RetrievalExpectation{
		Relevant:      []phpcompat.RetrievalChunkExpectation{{Path: "target.php", StartLine: 20, EndLine: 30}},
		HardNegatives: []phpcompat.RetrievalChunkExpectation{{Path: "noise.php", StartLine: 10, EndLine: 15}},
	}
	negativeFirst := []indexing.Chunk{
		{Path: "noise.php", StartLine: 1, EndLine: 20},
		{Path: "target.php", StartLine: 20, EndLine: 30},
	}
	if got := hardNegativeWins(query, negativeFirst); got != 1 {
		t.Fatalf("hardNegativeWins() = %d, want 1", got)
	}
	if got := hardNegativeWins(query, []indexing.Chunk{negativeFirst[1], negativeFirst[0]}); got != 0 {
		t.Fatalf("hardNegativeWins() = %d when relevance ranks first", got)
	}
}

func TestRetrievalMetricsKeepHoldoutAdvisory(t *testing.T) {
	thresholds := phpcompat.Thresholds{RetrievalRecallAt5: 0.95, RetrievalMRR: 0.9, RetrievalNDCGAt5: 0.9}
	passing := &scoreAccumulator{}
	passing.add(evaluation.Metrics{Precision: 0.2, Recall: 1, MRR: 1, NDCG: 1}, 0)
	failing := &scoreAccumulator{}
	failing.add(evaluation.Metrics{Precision: 0.2, Recall: 1, MRR: 0.5, NDCG: 0.63}, 1)
	accumulator := retrievalAccumulator{
		overall: *passing,
		splits: map[string]*scoreAccumulator{
			phpcompat.RetrievalSplitTrain:    passing,
			phpcompat.RetrievalSplitValidate: passing,
			phpcompat.RetrievalSplitHoldout:  failing,
		},
	}
	policy := phpcompat.RetrievalPolicy{
		Adapter:        indexing.TermSearchVersion,
		K:              5,
		RequiredSplits: []string{phpcompat.RetrievalSplitTrain, phpcompat.RetrievalSplitValidate, phpcompat.RetrievalSplitHoldout},
		BlockingSplits: []string{phpcompat.RetrievalSplitTrain, phpcompat.RetrievalSplitValidate},
	}
	got := accumulator.metrics(policy, thresholds)
	if !got.Passed || got.Splits[phpcompat.RetrievalSplitHoldout].Passed {
		t.Fatalf("unexpected blocking/advisory split result: %#v", got)
	}
}

func TestReportJSONOmitsRetrievalDetails(t *testing.T) {
	value := report{
		Version: reportVersion,
		Suite:   "suite.v2.json",
		Corpora: []corpusReport{{
			ID:       "private-corpus",
			Failures: []string{"private/path.php: structural miss"},
			RetrievalDetails: []retrievalQueryDetail{{
				ID:        "private-query-id",
				Metrics:   evaluation.Metrics{Recall: 1},
				Retrieved: []string{"private/path.php"},
			}},
		}},
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-query-id", "private/path.php", "/Users/private"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("aggregate report leaked %q: %s", private, encoded)
		}
	}
	if !strings.Contains(string(encoded), `"version":3`) {
		t.Fatalf("unexpected report version: %s", encoded)
	}
}
