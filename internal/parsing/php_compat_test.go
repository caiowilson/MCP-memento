package parsing

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"memento-mcp/internal/testutil/phpcompat"
)

func TestPHPCompatibilityCorpusStructuralMetrics(t *testing.T) {
	suite := loadPHPCompatibilitySuite(t)
	var files, parsed, expectedSymbols, foundSymbols, signatureChecks, signatureHits, boundaryChecks, boundaryHits, anchorChecks, anchorHits int

	for _, corpus := range suite.Corpora {
		root, err := suite.CorpusRoot(corpus)
		if err != nil {
			t.Fatal(err)
		}
		analyses := map[string]Analysis{}
		for _, expectation := range corpus.Files {
			files++
			source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(expectation.Path)))
			if err != nil {
				t.Fatal(err)
			}
			analysis, err := Analyze(expectation.Path, source)
			if err != nil {
				t.Errorf("%s/%s parse: %v", corpus.ID, expectation.Path, err)
				continue
			}
			parsed++
			analyses[expectation.Path] = analysis
			for _, expected := range expectation.Symbols {
				expectedSymbols++
				symbol, ok := phpCompatibilitySymbol(analysis.Symbols, expected.Name, expected.Kind, expected.Container)
				if !ok {
					t.Errorf("%s/%s missing %s %s.%s in %#v", corpus.ID, expectation.Path, expected.Kind, expected.Container, expected.Name, analysis.Symbols)
					continue
				}
				foundSymbols++
				for _, fragment := range expected.SignatureContains {
					signatureChecks++
					if strings.Contains(symbol.Signature, fragment) {
						signatureHits++
					} else {
						t.Errorf("%s/%s %s signature %q missing %q", corpus.ID, expectation.Path, expected.Name, symbol.Signature, fragment)
					}
				}
			}
			for _, forbidden := range expectation.ForbiddenSymbols {
				if _, ok := phpCompatibilitySymbolByName(analysis.Symbols, forbidden); ok {
					t.Errorf("%s/%s leaked forbidden symbol %q", corpus.ID, expectation.Path, forbidden)
				}
			}
			for _, line := range expectation.DeclarationStarts {
				boundaryChecks++
				if containsLine(analysis.DeclarationStarts, line) {
					boundaryHits++
				} else {
					t.Errorf("%s/%s declaration starts %#v missing %d", corpus.ID, expectation.Path, analysis.DeclarationStarts, line)
				}
			}
		}
		for _, anchor := range corpus.Anchors {
			anchorChecks++
			analysis, ok := analyses[anchor.Path]
			if !ok {
				continue
			}
			container, name := "", anchor.Symbol
			if dot := strings.LastIndex(anchor.Symbol, "."); dot >= 0 {
				container, name = anchor.Symbol[:dot], anchor.Symbol[dot+1:]
			}
			symbol, found := phpCompatibilitySymbol(analysis.Symbols, name, "", container)
			if found && symbol.ExtentStartLine == anchor.StartLine && symbol.ExtentEndLine == anchor.EndLine {
				anchorHits++
			} else {
				t.Errorf("%s/%s anchor %s = %#v, want lines %d-%d", corpus.ID, anchor.Path, anchor.Symbol, symbol, anchor.StartLine, anchor.EndLine)
			}
		}
	}

	parseRate := ratio(parsed, files)
	symbolRecall := ratio(foundSymbols, expectedSymbols)
	signatureRecall := ratio(signatureHits, signatureChecks)
	boundaryRecall := ratio(boundaryHits, boundaryChecks)
	anchorAccuracy := ratio(anchorHits, anchorChecks)
	t.Logf("PHP_COMPAT parse_success=%.4f symbol_recall=%.4f signature_recall=%.4f declaration_boundary_recall=%.4f anchor_accuracy=%.4f files=%d symbols=%d", parseRate, symbolRecall, signatureRecall, boundaryRecall, anchorAccuracy, files, expectedSymbols)
	if parseRate < suite.Thresholds.ParseSuccess || symbolRecall < suite.Thresholds.SymbolRecall || signatureRecall < suite.Thresholds.SignatureRecall || boundaryRecall < suite.Thresholds.DeclarationBoundaryRecall || anchorAccuracy < suite.Thresholds.AnchorAccuracy {
		t.Fatalf("PHP compatibility metrics below thresholds: parse=%.4f symbols=%.4f signatures=%.4f boundaries=%.4f anchors=%.4f", parseRate, symbolRecall, signatureRecall, boundaryRecall, anchorAccuracy)
	}
}

func loadPHPCompatibilitySuite(t *testing.T) phpcompat.Suite {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(current), "../../evaluation/php-compat/suite.v1.json")
	suite, err := phpcompat.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return suite
}

func phpCompatibilitySymbol(symbols []Symbol, name, kind, container string) (Symbol, bool) {
	for _, symbol := range symbols {
		if symbol.Name == name && (kind == "" || symbol.Kind == kind) && symbol.Container == container {
			return symbol, true
		}
	}
	return Symbol{}, false
}

func phpCompatibilitySymbolByName(symbols []Symbol, name string) (Symbol, bool) {
	for _, symbol := range symbols {
		if symbol.Name == name {
			return symbol, true
		}
	}
	return Symbol{}, false
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 1
	}
	return float64(numerator) / float64(denominator)
}
