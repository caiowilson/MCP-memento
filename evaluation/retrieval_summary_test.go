package evaluation

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetrievalSummaryRoundTrip(t *testing.T) {
	fixtures := FixtureSet{Version: 1, K: 5, Queries: []QueryFixture{{ID: "one", Query: "query", Relevant: []RelevantChunk{{Path: "README.md"}}}}}
	report := Report{K: 5, Queries: []QueryResult{{ID: "one"}}, Metrics: Metrics{Precision: .2, Recall: .5, MRR: .3, NDCG: .4}}
	summary, err := NewRetrievalSummary(fixtures, report, RetrievalSummaryConfig{Mode: "lexical"})
	if err != nil {
		t.Fatal(err)
	}
	if summary.QueryCount != 1 || summary.FixtureFingerprint == "" || summary.ConfigurationFingerprint == "" {
		t.Fatalf("summary = %#v", summary)
	}
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"query"`) || strings.Contains(string(data), "README.md") {
		t.Fatalf("privacy-safe summary leaked query or path: %s", data)
	}
	path := filepath.Join(t.TempDir(), "retrieval.json")
	if err := WriteRetrievalSummary(path, summary); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRetrievalSummaryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != summary {
		t.Fatalf("loaded = %#v, want %#v", loaded, summary)
	}
}

func TestRetrievalSummaryFingerprintIncludesChunkLimits(t *testing.T) {
	fixtures := FixtureSet{Version: 1, K: 5, Queries: []QueryFixture{{ID: "one", Query: "query", Relevant: []RelevantChunk{{Path: "README.md"}}}}}
	report := Report{K: 5, Queries: []QueryResult{{ID: "one"}}}
	defaults, err := NewRetrievalSummary(fixtures, report, RetrievalSummaryConfig{Mode: "lexical"})
	if err != nil {
		t.Fatal(err)
	}
	custom, err := NewRetrievalSummary(fixtures, report, RetrievalSummaryConfig{Mode: "lexical", MaxChunkLines: 100})
	if err != nil {
		t.Fatal(err)
	}
	if defaults.ConfigurationFingerprint == custom.ConfigurationFingerprint {
		t.Fatal("expected effective chunking limits to affect the retrieval configuration fingerprint")
	}
}
