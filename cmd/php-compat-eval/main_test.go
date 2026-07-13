package main

import (
	"encoding/json"
	"strings"
	"testing"

	"memento-mcp/evaluation"
	"memento-mcp/internal/testutil/phpcompat"
)

func TestRetrievalThresholdsPass(t *testing.T) {
	thresholds := phpcompat.Thresholds{
		RetrievalRecallAt5: 0.95,
		RetrievalMRR:       0.90,
		RetrievalNDCGAt5:   0.90,
	}
	if !retrievalThresholdsPass(retrievalMetrics{Recall: 0.95, MRR: 0.90, NDCG: 0.90}, thresholds) {
		t.Fatal("metrics at each threshold should pass")
	}
	if retrievalThresholdsPass(retrievalMetrics{Recall: 0.94, MRR: 1, NDCG: 1}, thresholds) {
		t.Fatal("recall below its threshold should fail")
	}
}

func TestReportJSONOmitsRetrievalDetails(t *testing.T) {
	value := report{
		Version: reportVersion,
		Corpora: []corpusReport{{
			ID: "private-corpus",
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
	for _, private := range []string{"private-query-id", "private/path.php"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("aggregate report leaked %q: %s", private, encoded)
		}
	}
	if !strings.Contains(string(encoded), `"version":2`) {
		t.Fatalf("unexpected report version: %s", encoded)
	}
}
