package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"memento-mcp/internal/parsing"
	"memento-mcp/internal/testutil/phpcompat"
)

const reportVersion = 1

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
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	PHP       string         `json:"phpVersion"`
	Metrics   metrics        `json:"metrics"`
	Judgments judgmentCounts `json:"judgments"`
	Failures  []string       `json:"failures,omitempty"`
	Passed    bool           `json:"passed"`
}

type report struct {
	Version    int                  `json:"version"`
	Suite      string               `json:"suite"`
	Thresholds phpcompat.Thresholds `json:"thresholds"`
	Corpora    []corpusReport       `json:"corpora"`
	Metrics    metrics              `json:"metrics"`
	Judgments  judgmentCounts       `json:"judgments"`
	Passed     bool                 `json:"passed"`
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
	suitePath := flag.String("suite", "evaluation/php-compat/suite.v1.json", "PHP compatibility suite manifest")
	jsonOut := flag.String("json-out", "", "optional JSON report path")
	flag.Parse()

	suite, err := phpcompat.Load(*suitePath)
	if err != nil {
		fatal(err)
	}
	report, err := evaluate(*suitePath, suite)
	if err != nil {
		fatal(err)
	}
	printReport(report)
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
	out := report{Version: reportVersion, Suite: filepath.ToSlash(suitePath), Thresholds: suite.Thresholds, Passed: true}
	var overall counts
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
		out.Passed = out.Passed && current.Passed
		out.Corpora = append(out.Corpora, current)
		overall.add(corpusCounts)
		out.Judgments.Relationships += current.Judgments.Relationships
		out.Judgments.ForbiddenRelationships += current.Judgments.ForbiddenRelationships
		out.Judgments.ComposerResolutions += current.Judgments.ComposerResolutions
		out.Judgments.RetrievalQueries += current.Judgments.RetrievalQueries
	}
	out.Metrics = overall.metrics()
	out.Passed = out.Passed && parserThresholdsPass(out.Metrics, suite.Thresholds) && overall.forbiddenViolations == 0
	return out, nil
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

func printReport(report report) {
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
