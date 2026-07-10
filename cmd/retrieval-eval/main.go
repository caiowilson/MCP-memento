package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"memento-mcp/evaluation"
	"memento-mcp/internal/embedding"
)

func main() {
	root := flag.String("root", ".", "repository root to evaluate")
	fixtures := flag.String("fixtures", "evaluation/fixtures/retrieval.json", "retrieval fixture file")
	flag.Parse()

	rootAbs, err := filepath.Abs(*root)
	if err != nil {
		fatal(err)
	}
	fixturePath := *fixtures
	if !filepath.IsAbs(fixturePath) {
		fixturePath = filepath.Join(rootAbs, fixturePath)
	}
	storeDir, err := os.MkdirTemp("", "memento-retrieval-eval-")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(storeDir)

	semantic, err := embedding.FromEnv()
	if err != nil {
		fatal(err)
	}
	report, err := evaluation.ExecuteWithConfig(context.Background(), rootAbs, fixturePath, storeDir, evaluation.ExecuteConfig{
		Embedder:           semantic.Embedder,
		SemanticWeight:     semantic.SemanticWeight,
		EmbeddingBatchSize: semantic.BatchSize,
	})
	if err != nil {
		fatal(err)
	}
	if semantic.Enabled {
		fmt.Printf("MODE hybrid model=%s semantic-weight=%.2f\n", semantic.Embedder.Name(), semantic.SemanticWeight)
	} else {
		fmt.Println("MODE lexical")
	}
	for _, result := range report.Queries {
		fmt.Printf("%-28s precision@%d=%.3f recall@%d=%.3f MRR=%.3f nDCG@%d=%.3f\n",
			result.ID, report.K, result.Metrics.Precision, report.K, result.Metrics.Recall,
			result.Metrics.MRR, report.K, result.Metrics.NDCG)
	}
	fmt.Printf("OVERALL (%d queries) precision@%d=%.3f recall@%d=%.3f MRR=%.3f nDCG@%d=%.3f\n",
		len(report.Queries), report.K, report.Metrics.Precision, report.K, report.Metrics.Recall,
		report.Metrics.MRR, report.K, report.Metrics.NDCG)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "retrieval evaluation:", err)
	os.Exit(1)
}
