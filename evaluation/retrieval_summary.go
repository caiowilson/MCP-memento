package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
)

const RetrievalSummaryVersion = 1

// RetrievalSummary is the privacy-safe CI form of a retrieval report. It
// deliberately excludes queries, repository paths, and retrieved chunks.
type RetrievalSummary struct {
	Version                  int     `json:"version"`
	Mode                     string  `json:"mode"`
	ConfigurationFingerprint string  `json:"configurationFingerprint"`
	FixtureFingerprint       string  `json:"fixtureFingerprint"`
	QueryCount               int     `json:"queryCount"`
	K                        int     `json:"k"`
	Metrics                  Metrics `json:"metrics"`
}

type RetrievalSummaryConfig struct {
	Mode           string
	Embedder       string
	SemanticWeight float64
}

func NewRetrievalSummary(fixtures FixtureSet, report Report, cfg RetrievalSummaryConfig) (RetrievalSummary, error) {
	if cfg.Mode != "lexical" && cfg.Mode != "semantic" {
		return RetrievalSummary{}, fmt.Errorf("retrieval summary mode must be lexical or semantic")
	}
	return RetrievalSummary{
		Version: RetrievalSummaryVersion,
		Mode:    cfg.Mode,
		ConfigurationFingerprint: fingerprint(struct {
			Mode           string  `json:"mode"`
			Embedder       string  `json:"embedder,omitempty"`
			SemanticWeight float64 `json:"semanticWeight,omitempty"`
			K              int     `json:"k"`
		}{cfg.Mode, cfg.Embedder, cfg.SemanticWeight, report.K}),
		FixtureFingerprint: fingerprint(fixtures),
		QueryCount:         len(report.Queries),
		K:                  report.K,
		Metrics:            report.Metrics,
	}, nil
}

func LoadRetrievalSummaryFile(path string) (RetrievalSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return RetrievalSummary{}, err
	}
	defer f.Close()
	var summary RetrievalSummary
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&summary); err != nil {
		return RetrievalSummary{}, fmt.Errorf("decode retrieval summary: %w", err)
	}
	if err := summary.Validate(); err != nil {
		return RetrievalSummary{}, err
	}
	return summary, nil
}

func (s RetrievalSummary) Validate() error {
	if s.Version != RetrievalSummaryVersion {
		return fmt.Errorf("unsupported retrieval summary version %d", s.Version)
	}
	if s.Mode != "lexical" && s.Mode != "semantic" {
		return fmt.Errorf("retrieval summary mode must be lexical or semantic")
	}
	if s.ConfigurationFingerprint == "" || s.FixtureFingerprint == "" {
		return fmt.Errorf("retrieval summary configuration and fixture fingerprints are required")
	}
	if s.QueryCount <= 0 || s.K <= 0 {
		return fmt.Errorf("retrieval summary query count and k must be positive")
	}
	for _, metric := range []float64{s.Metrics.Precision, s.Metrics.Recall, s.Metrics.MRR, s.Metrics.NDCG} {
		if metric < 0 || metric > 1 {
			return fmt.Errorf("retrieval summary metrics must be between zero and one")
		}
	}
	return nil
}

func WriteRetrievalSummary(path string, summary RetrievalSummary) error {
	if err := summary.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
