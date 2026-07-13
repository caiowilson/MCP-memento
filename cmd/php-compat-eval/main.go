package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"memento-mcp/evaluation"
	"memento-mcp/internal/indexing"
	"memento-mcp/internal/parsing"
	"memento-mcp/internal/testutil/phpcompat"
)

const (
	reportVersion = 3
)

type metric struct {
	Matched int     `json:"matched"`
	Total   int     `json:"total"`
	Rate    float64 `json:"rate"`
}

type metrics struct {
	ParseSuccess              metric `json:"parseSuccess"`
	SymbolRecall              metric `json:"symbolRecall"`
	SignatureRecall           metric `json:"signatureRecall"`
	DeclarationBoundaryRecall metric `json:"declarationBoundaryRecall"`
	AnchorAccuracy            metric `json:"anchorAccuracy"`
	ForbiddenSymbolViolations int    `json:"forbiddenSymbolViolations"`
}

type judgmentCounts struct {
	Relationships          int `json:"relationships"`
	ForbiddenRelationships int `json:"forbiddenRelationships"`
	ComposerResolutions    int `json:"composerResolutions"`
	RetrievalQueries       int `json:"retrievalQueries"`
}

type corpusReport struct {
	ID               string                 `json:"id"`
	Kind             string                 `json:"kind"`
	PHP              string                 `json:"phpVersion"`
	Metrics          metrics                `json:"metrics"`
	Retrieval        *retrievalMetrics      `json:"retrieval,omitempty"`
	RetrievalDetails []retrievalQueryDetail `json:"-"`
	Judgments        judgmentCounts         `json:"judgments"`
	Failures         []string               `json:"-"`
	Passed           bool                   `json:"passed"`
}

type report struct {
	Version    int                  `json:"version"`
	Suite      string               `json:"suite"`
	Thresholds phpcompat.Thresholds `json:"thresholds"`
	Corpora    []corpusReport       `json:"corpora"`
	Metrics    metrics              `json:"metrics"`
	Retrieval  *retrievalMetrics    `json:"retrieval,omitempty"`
	Judgments  judgmentCounts       `json:"judgments"`
	Passed     bool                 `json:"passed"`
}

type retrievalMetrics struct {
	Adapter        string   `json:"adapter"`
	K              int      `json:"k"`
	BlockingSplits []string `json:"blockingSplits"`
	retrievalScore
	Splits map[string]retrievalScore `json:"splits"`
}

type retrievalScore struct {
	Queries          int     `json:"queries"`
	Precision        float64 `json:"precisionAt5"`
	Recall           float64 `json:"recallAt5"`
	MRR              float64 `json:"mrr"`
	NDCG             float64 `json:"ndcgAt5"`
	HardNegativeWins int     `json:"hardNegativeWins"`
	Passed           bool    `json:"passed"`
}

type retrievalQueryDetail struct {
	ID               string
	Split            string
	Metrics          evaluation.Metrics
	HardNegativeWins int
	Retrieved        []string
}

type scoreAccumulator struct {
	queries          int
	precision        float64
	recall           float64
	mrr              float64
	ndcg             float64
	hardNegativeWins int
}

type retrievalAccumulator struct {
	overall scoreAccumulator
	splits  map[string]*scoreAccumulator
}

type counts struct {
	parsed, files                      int
	symbolsMatched, symbolsTotal       int
	signaturesMatched, signaturesTotal int
	boundariesMatched, boundariesTotal int
	anchorsMatched, anchorsTotal       int
	forbiddenViolations                int
}

func main() {
	suitePath := flag.String("suite", "evaluation/php-compat/suite.v2.json", "PHP compatibility suite manifest")
	jsonOut := flag.String("json-out", "", "optional JSON report path")
	retrievalDetails := flag.Bool("retrieval-details", false, "print per-query retrieval metrics and ranked paths")
	flag.Parse()

	suite, err := phpcompat.Load(*suitePath)
	if err != nil {
		fatal(err)
	}
	report, err := evaluate(*suitePath, suite)
	if err != nil {
		fatal(err)
	}
	printReport(report, *retrievalDetails)
	if *jsonOut != "" {
		if err := writeReport(*jsonOut, report); err != nil {
			fatal(err)
		}
	}
	if !report.Passed {
		os.Exit(1)
	}
}

func evaluate(suitePath string, suite phpcompat.Suite) (report, error) {
	if suite.RetrievalPolicy.Adapter != indexing.TermSearchVersion {
		return report{}, fmt.Errorf("retrieval adapter %q does not match runtime %q", suite.RetrievalPolicy.Adapter, indexing.TermSearchVersion)
	}
	out := report{Version: reportVersion, Suite: filepath.Base(suitePath), Thresholds: suite.Thresholds, Passed: true}
	var overall counts
	retrievalStore, err := os.MkdirTemp("", "memento-php-retrieval-")
	if err != nil {
		return report{}, err
	}
	defer os.RemoveAll(retrievalStore)
	var overallRetrieval retrievalAccumulator
	for _, corpus := range suite.Corpora {
		root, err := suite.CorpusRoot(corpus)
		if err != nil {
			return report{}, err
		}
		current := corpusReport{ID: corpus.ID, Kind: corpus.Kind, PHP: corpus.PHPVersion, Passed: true}
		current.Judgments = judgmentCounts{
			Relationships:          len(corpus.Relations),
			ForbiddenRelationships: len(corpus.ForbiddenRelations),
			ComposerResolutions:    len(corpus.ComposerResolutions),
			RetrievalQueries:       len(corpus.Retrieval),
		}
		var corpusCounts counts
		analyses := map[string]parsing.Analysis{}
		for _, file := range corpus.Files {
			corpusCounts.files++
			source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
			if err != nil {
				return report{}, err
			}
			analysis, err := parsing.Analyze(file.Path, source)
			if err != nil {
				current.Failures = append(current.Failures, fmt.Sprintf("%s: parse: %v", file.Path, err))
				continue
			}
			analyses[file.Path] = analysis
			corpusCounts.parsed++
			for _, expected := range file.Symbols {
				corpusCounts.symbolsTotal++
				actual, ok := findSymbol(analysis.Symbols, expected.Name, expected.Kind, expected.Container)
				if !ok {
					current.Failures = append(current.Failures, fmt.Sprintf("%s: missing %s %s.%s", file.Path, expected.Kind, expected.Container, expected.Name))
					corpusCounts.signaturesTotal += len(expected.SignatureContains)
					continue
				}
				corpusCounts.symbolsMatched++
				for _, fragment := range expected.SignatureContains {
					corpusCounts.signaturesTotal++
					if strings.Contains(actual.Signature, fragment) {
						corpusCounts.signaturesMatched++
					} else {
						current.Failures = append(current.Failures, fmt.Sprintf("%s: %s.%s signature misses %q", file.Path, expected.Container, expected.Name, fragment))
					}
				}
			}
			for _, forbidden := range file.ForbiddenSymbols {
				if hasSymbolNamed(analysis.Symbols, forbidden) {
					corpusCounts.forbiddenViolations++
					current.Failures = append(current.Failures, fmt.Sprintf("%s: forbidden body symbol %q leaked", file.Path, forbidden))
				}
			}
			for _, expected := range file.DeclarationStarts {
				corpusCounts.boundariesTotal++
				if containsInt(analysis.DeclarationStarts, expected) {
					corpusCounts.boundariesMatched++
				} else {
					current.Failures = append(current.Failures, fmt.Sprintf("%s: missing declaration start %d (got %v)", file.Path, expected, analysis.DeclarationStarts))
				}
			}
		}
		for _, anchor := range corpus.Anchors {
			corpusCounts.anchorsTotal++
			analysis, ok := analyses[anchor.Path]
			if !ok {
				current.Failures = append(current.Failures, fmt.Sprintf("%s: anchor %s has no parsed file", anchor.Path, anchor.Symbol))
				continue
			}
			container, name := splitSymbol(anchor.Symbol)
			actual, ok := findSymbolByNameContainer(analysis.Symbols, name, container)
			if ok && actual.ExtentStartLine == anchor.StartLine && actual.ExtentEndLine == anchor.EndLine {
				corpusCounts.anchorsMatched++
			} else if ok {
				current.Failures = append(current.Failures, fmt.Sprintf("%s: anchor %s extent %d-%d, want %d-%d", anchor.Path, anchor.Symbol, actual.ExtentStartLine, actual.ExtentEndLine, anchor.StartLine, anchor.EndLine))
			} else {
				current.Failures = append(current.Failures, fmt.Sprintf("%s: anchor symbol %s missing", anchor.Path, anchor.Symbol))
			}
		}
		current.Metrics = corpusCounts.metrics()
		current.Passed = parserThresholdsPass(current.Metrics, suite.Thresholds) && corpusCounts.forbiddenViolations == 0
		if len(corpus.Retrieval) > 0 {
			fixtures := phpRetrievalFixtures(corpus, suite.RetrievalPolicy.K)
			retrievalReport, err := evaluation.ExecuteFixturesWithConfig(
				context.Background(),
				root,
				filepath.Join(retrievalStore, corpus.ID),
				fixtures,
				evaluation.ExecuteConfig{TermAware: true, DistinctPaths: true},
			)
			if err != nil {
				return report{}, fmt.Errorf("%s retrieval: %w", corpus.ID, err)
			}
			var corpusRetrieval retrievalAccumulator
			corpusRetrieval.add(corpus, retrievalReport)
			overallRetrieval.add(corpus, retrievalReport)
			current.Retrieval = corpusRetrieval.metrics(suite.RetrievalPolicy, suite.Thresholds)
			queries := retrievalQueriesByID(corpus)
			for _, query := range retrievalReport.Queries {
				expectation := queries[query.ID]
				detail := retrievalQueryDetail{
					ID:               query.ID,
					Split:            expectation.Split,
					Metrics:          query.Metrics,
					HardNegativeWins: hardNegativeWins(expectation, query.Retrieved),
					Retrieved:        make([]string, 0, len(query.Retrieved)),
				}
				for _, chunk := range query.Retrieved {
					detail.Retrieved = append(detail.Retrieved, fmt.Sprintf("%s:%d-%d", chunk.Path, chunk.StartLine, chunk.EndLine))
				}
				current.RetrievalDetails = append(current.RetrievalDetails, detail)
			}
			current.Passed = current.Passed && current.Retrieval.Passed
		}
		out.Passed = out.Passed && current.Passed
		out.Corpora = append(out.Corpora, current)
		overall.add(corpusCounts)
		out.Judgments.Relationships += current.Judgments.Relationships
		out.Judgments.ForbiddenRelationships += current.Judgments.ForbiddenRelationships
		out.Judgments.ComposerResolutions += current.Judgments.ComposerResolutions
		out.Judgments.RetrievalQueries += current.Judgments.RetrievalQueries
	}
	out.Metrics = overall.metrics()
	if overallRetrieval.overall.queries > 0 {
		out.Retrieval = overallRetrieval.metrics(suite.RetrievalPolicy, suite.Thresholds)
		out.Passed = out.Passed && out.Retrieval.Passed
	}
	out.Passed = out.Passed && parserThresholdsPass(out.Metrics, suite.Thresholds) && overall.forbiddenViolations == 0
	return out, nil
}

func phpRetrievalFixtures(corpus phpcompat.Corpus, k int) evaluation.FixtureSet {
	fixtures := evaluation.FixtureSet{Version: 1, K: k, Queries: make([]evaluation.QueryFixture, 0, len(corpus.Retrieval))}
	for _, query := range corpus.Retrieval {
		fixture := evaluation.QueryFixture{ID: query.ID, Query: query.Query, Relevant: make([]evaluation.RelevantChunk, 0, len(query.Relevant))}
		for _, relevant := range query.Relevant {
			fixture.Relevant = append(fixture.Relevant, evaluation.RelevantChunk{
				Path: relevant.Path, StartLine: relevant.StartLine, EndLine: relevant.EndLine,
			})
		}
		fixtures.Queries = append(fixtures.Queries, fixture)
	}
	return fixtures
}

func (a *retrievalAccumulator) add(corpus phpcompat.Corpus, value evaluation.Report) {
	if a.splits == nil {
		a.splits = map[string]*scoreAccumulator{}
	}
	queries := retrievalQueriesByID(corpus)
	for _, result := range value.Queries {
		expectation := queries[result.ID]
		wins := hardNegativeWins(expectation, result.Retrieved)
		a.overall.add(result.Metrics, wins)
		if a.splits[expectation.Split] == nil {
			a.splits[expectation.Split] = &scoreAccumulator{}
		}
		a.splits[expectation.Split].add(result.Metrics, wins)
	}
}

func (a *scoreAccumulator) add(metrics evaluation.Metrics, hardNegativeWins int) {
	a.queries++
	a.precision += metrics.Precision
	a.recall += metrics.Recall
	a.mrr += metrics.MRR
	a.ndcg += metrics.NDCG
	a.hardNegativeWins += hardNegativeWins
}

func (a retrievalAccumulator) metrics(policy phpcompat.RetrievalPolicy, thresholds phpcompat.Thresholds) *retrievalMetrics {
	out := &retrievalMetrics{
		Adapter:        policy.Adapter,
		K:              policy.K,
		BlockingSplits: append([]string(nil), policy.BlockingSplits...),
		retrievalScore: a.overall.score(thresholds),
		Splits:         make(map[string]retrievalScore, len(policy.RequiredSplits)),
	}
	for _, split := range policy.RequiredSplits {
		out.Splits[split] = a.splits[split].score(thresholds)
	}
	out.Passed = true
	for _, split := range policy.BlockingSplits {
		out.Passed = out.Passed && out.Splits[split].Passed
	}
	return out
}

func (a *scoreAccumulator) score(thresholds phpcompat.Thresholds) retrievalScore {
	if a == nil || a.queries == 0 {
		return retrievalScore{}
	}
	n := float64(a.queries)
	out := retrievalScore{
		Queries:          a.queries,
		Precision:        a.precision / n,
		Recall:           a.recall / n,
		MRR:              a.mrr / n,
		NDCG:             a.ndcg / n,
		HardNegativeWins: a.hardNegativeWins,
	}
	out.Passed = retrievalThresholdsPass(out, thresholds)
	return out
}

func retrievalThresholdsPass(actual retrievalScore, threshold phpcompat.Thresholds) bool {
	return actual.Recall >= threshold.RetrievalRecallAt5 &&
		actual.MRR >= threshold.RetrievalMRR &&
		actual.NDCG >= threshold.RetrievalNDCGAt5 &&
		actual.HardNegativeWins == 0
}

func retrievalQueriesByID(corpus phpcompat.Corpus) map[string]phpcompat.RetrievalExpectation {
	out := make(map[string]phpcompat.RetrievalExpectation, len(corpus.Retrieval))
	for _, query := range corpus.Retrieval {
		out[query.ID] = query
	}
	return out
}

func hardNegativeWins(query phpcompat.RetrievalExpectation, retrieved []indexing.Chunk) int {
	firstRelevant := len(retrieved)
	for rank, chunk := range retrieved {
		if matchesRetrievalChunk(chunk, query.Relevant) {
			firstRelevant = rank
			break
		}
	}
	wins := 0
	for _, negative := range query.HardNegatives {
		for rank, chunk := range retrieved {
			if rank >= firstRelevant {
				break
			}
			if matchesRetrievalChunk(chunk, []phpcompat.RetrievalChunkExpectation{negative}) {
				wins++
				break
			}
		}
	}
	return wins
}

func matchesRetrievalChunk(chunk indexing.Chunk, judgments []phpcompat.RetrievalChunkExpectation) bool {
	for _, judgment := range judgments {
		if chunk.Path == judgment.Path && chunk.StartLine <= judgment.EndLine && chunk.EndLine >= judgment.StartLine {
			return true
		}
	}
	return false
}

func (c counts) metrics() metrics {
	return metrics{
		ParseSuccess:              newMetric(c.parsed, c.files),
		SymbolRecall:              newMetric(c.symbolsMatched, c.symbolsTotal),
		SignatureRecall:           newMetric(c.signaturesMatched, c.signaturesTotal),
		DeclarationBoundaryRecall: newMetric(c.boundariesMatched, c.boundariesTotal),
		AnchorAccuracy:            newMetric(c.anchorsMatched, c.anchorsTotal),
		ForbiddenSymbolViolations: c.forbiddenViolations,
	}
}

func (c *counts) add(other counts) {
	c.parsed += other.parsed
	c.files += other.files
	c.symbolsMatched += other.symbolsMatched
	c.symbolsTotal += other.symbolsTotal
	c.signaturesMatched += other.signaturesMatched
	c.signaturesTotal += other.signaturesTotal
	c.boundariesMatched += other.boundariesMatched
	c.boundariesTotal += other.boundariesTotal
	c.anchorsMatched += other.anchorsMatched
	c.anchorsTotal += other.anchorsTotal
	c.forbiddenViolations += other.forbiddenViolations
}

func newMetric(matched, total int) metric {
	rate := 1.0
	if total > 0 {
		rate = float64(matched) / float64(total)
	}
	return metric{Matched: matched, Total: total, Rate: rate}
}

func parserThresholdsPass(actual metrics, threshold phpcompat.Thresholds) bool {
	return actual.ParseSuccess.Rate >= threshold.ParseSuccess &&
		actual.SymbolRecall.Rate >= threshold.SymbolRecall &&
		actual.SignatureRecall.Rate >= threshold.SignatureRecall &&
		actual.DeclarationBoundaryRecall.Rate >= threshold.DeclarationBoundaryRecall &&
		actual.AnchorAccuracy.Rate >= threshold.AnchorAccuracy
}

func findSymbol(symbols []parsing.Symbol, name, kind, container string) (parsing.Symbol, bool) {
	for _, symbol := range symbols {
		if symbol.Name == name && symbol.Kind == kind && symbol.Container == container {
			return symbol, true
		}
	}
	return parsing.Symbol{}, false
}

func findSymbolByNameContainer(symbols []parsing.Symbol, name, container string) (parsing.Symbol, bool) {
	for _, symbol := range symbols {
		if symbol.Name == name && symbol.Container == container {
			return symbol, true
		}
	}
	return parsing.Symbol{}, false
}

func hasSymbolNamed(symbols []parsing.Symbol, name string) bool {
	for _, symbol := range symbols {
		if symbol.Name == name {
			return true
		}
	}
	return false
}

func containsInt(values []int, wanted int) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func splitSymbol(value string) (string, string) {
	if dot := strings.LastIndex(value, "."); dot >= 0 {
		return value[:dot], value[dot+1:]
	}
	return "", value
}

func printReport(report report, retrievalDetails bool) {
	for _, corpus := range report.Corpora {
		fmt.Printf("%-24s parse=%s symbols=%s signatures=%s boundaries=%s anchors=%s forbidden=%d passed=%t\n",
			corpus.ID,
			formatMetric(corpus.Metrics.ParseSuccess),
			formatMetric(corpus.Metrics.SymbolRecall),
			formatMetric(corpus.Metrics.SignatureRecall),
			formatMetric(corpus.Metrics.DeclarationBoundaryRecall),
			formatMetric(corpus.Metrics.AnchorAccuracy),
			corpus.Metrics.ForbiddenSymbolViolations,
			corpus.Passed,
		)
		for _, failure := range corpus.Failures {
			fmt.Printf("  - %s\n", failure)
		}
		if corpus.Retrieval != nil {
			fmt.Printf("  retrieval adapter=%s blocking=%s queries=%d precision@5=%.3f recall@5=%.3f MRR=%.3f nDCG@5=%.3f hard-negative-wins=%d passed=%t\n",
				corpus.Retrieval.Adapter, strings.Join(corpus.Retrieval.BlockingSplits, ","), corpus.Retrieval.Queries, corpus.Retrieval.Precision, corpus.Retrieval.Recall,
				corpus.Retrieval.MRR, corpus.Retrieval.NDCG, corpus.Retrieval.HardNegativeWins, corpus.Retrieval.Passed)
			printRetrievalSplits("    ", corpus.Retrieval.Splits)
			if retrievalDetails {
				for _, detail := range corpus.RetrievalDetails {
					fmt.Printf("    %-28s split=%-7s recall=%.3f MRR=%.3f nDCG=%.3f hard-negative-wins=%d chunks=%s\n",
						detail.ID, detail.Split, detail.Metrics.Recall, detail.Metrics.MRR, detail.Metrics.NDCG,
						detail.HardNegativeWins, strings.Join(detail.Retrieved, ","))
				}
			}
		}
	}
	fmt.Printf("OVERALL parse=%s symbols=%s signatures=%s boundaries=%s anchors=%s forbidden=%d judgments=relationships:%d composer:%d retrieval:%d passed=%t\n",
		formatMetric(report.Metrics.ParseSuccess),
		formatMetric(report.Metrics.SymbolRecall),
		formatMetric(report.Metrics.SignatureRecall),
		formatMetric(report.Metrics.DeclarationBoundaryRecall),
		formatMetric(report.Metrics.AnchorAccuracy),
		report.Metrics.ForbiddenSymbolViolations,
		report.Judgments.Relationships,
		report.Judgments.ComposerResolutions,
		report.Judgments.RetrievalQueries,
		report.Passed,
	)
	if report.Retrieval != nil {
		fmt.Printf("RETRIEVAL adapter=%s blocking=%s queries=%d precision@5=%.3f recall@5=%.3f MRR=%.3f nDCG@5=%.3f hard-negative-wins=%d passed=%t\n",
			report.Retrieval.Adapter, strings.Join(report.Retrieval.BlockingSplits, ","), report.Retrieval.Queries, report.Retrieval.Precision, report.Retrieval.Recall,
			report.Retrieval.MRR, report.Retrieval.NDCG, report.Retrieval.HardNegativeWins, report.Retrieval.Passed)
		printRetrievalSplits("  ", report.Retrieval.Splits)
	}
}

func printRetrievalSplits(prefix string, splits map[string]retrievalScore) {
	for _, split := range []string{phpcompat.RetrievalSplitTrain, phpcompat.RetrievalSplitValidate, phpcompat.RetrievalSplitHoldout} {
		metrics, ok := splits[split]
		if !ok {
			continue
		}
		fmt.Printf("%ssplit=%-7s queries=%d precision@5=%.3f recall@5=%.3f MRR=%.3f nDCG@5=%.3f hard-negative-wins=%d passed=%t\n",
			prefix, split, metrics.Queries, metrics.Precision, metrics.Recall, metrics.MRR, metrics.NDCG,
			metrics.HardNegativeWins, metrics.Passed)
	}
}

func formatMetric(value metric) string {
	return fmt.Sprintf("%.3f(%d/%d)", value.Rate, value.Matched, value.Total)
}

func writeReport(path string, report report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "php compatibility evaluation:", err)
	os.Exit(2)
}
