package mcp

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"memento-mcp/internal/testutil/phpcompat"
)

func TestPHPCompatibilityRelationshipMetrics(t *testing.T) {
	suite, err := phpcompat.Load(filepath.Join("..", "..", "evaluation", "php-compat", "suite.v1.json"))
	if err != nil {
		t.Fatal(err)
	}

	relationHits, relationTotal := 0, 0
	precisionHits, precisionTotal := 0, 0
	composerHits, composerTotal := 0, 0
	forbiddenHits := 0

	for _, corpus := range suite.Corpora {
		root, err := suite.CorpusRoot(corpus)
		if err != nil {
			t.Fatalf("%s: %v", corpus.ID, err)
		}
		if len(corpus.Relations) > 0 || len(corpus.ForbiddenRelations) > 0 {
			hitsBefore, totalBefore := relationHits, relationTotal
			precisionHitsBefore, precisionTotalBefore := precisionHits, precisionTotal
			graph, err := buildPHPIncludeGraph(context.Background(), root)
			if err != nil {
				t.Fatalf("%s graph: %v", corpus.ID, err)
			}
			expected := map[string]bool{}
			judgedSources := map[string]bool{}
			for _, relation := range corpus.Relations {
				judgedSources[relation.From] = true
				for _, reason := range relation.Reasons {
					key := phpCompatRelationKey(relation.From, relation.To, reason)
					expected[key] = true
					relationTotal++
					if phpCompatGraphHasReason(graph, relation.From, relation.To, reason) {
						relationHits++
					} else {
						t.Logf("%s: missing relationship %s", corpus.ID, key)
					}
				}
			}
			for _, relation := range corpus.ForbiddenRelations {
				if !phpCompatGraphHasAnyForwardEdge(graph, relation.From, relation.To) {
					forbiddenHits++
				} else {
					t.Errorf("%s: forbidden relationship %s -> %s", corpus.ID, relation.From, relation.To)
				}
			}
			for source := range judgedSources {
				for _, actual := range phpCompatForwardRelations(graph, source) {
					precisionTotal++
					if expected[actual] {
						precisionHits++
					} else {
						t.Logf("%s: unjudged relationship %s", corpus.ID, actual)
					}
				}
			}
			corpusRecall := phpCompatRatio(relationHits-hitsBefore, relationTotal-totalBefore)
			corpusPrecision := phpCompatRatio(precisionHits-precisionHitsBefore, precisionTotal-precisionTotalBefore)
			t.Logf("PHP_COMPAT_RELATIONSHIP corpus=%s recall=%.4f precision=%.4f", corpus.ID, corpusRecall, corpusPrecision)
			if corpusRecall < suite.Thresholds.RelationshipRecall {
				t.Errorf("%s relationship recall %.4f is below threshold %.4f", corpus.ID, corpusRecall, suite.Thresholds.RelationshipRecall)
			}
			if corpusPrecision < suite.Thresholds.RelationshipPrecision {
				t.Errorf("%s relationship precision %.4f is below threshold %.4f", corpus.ID, corpusPrecision, suite.Thresholds.RelationshipPrecision)
			}
		}

		if len(corpus.ComposerResolutions) > 0 || len(corpus.AutoloadFiles) > 0 {
			composerHitsBefore, composerTotalBefore := composerHits, composerTotal
			filesByRel := phpCompatFilesByRel(t, root)
			composer := readComposerAutoload(root)
			resolver := buildComposerResolver(root, composer, filesByRel)
			classFiles := map[string]string{}
			for _, file := range filesByRel {
				if _, excluded := resolver.excluded[file.rel]; excluded {
					continue
				}
				for _, class := range file.declared {
					key := strings.ToLower(class)
					if _, exists := classFiles[key]; !exists {
						classFiles[key] = file.rel
					}
				}
			}
			for _, expectation := range corpus.ComposerResolutions {
				composerTotal++
				got := resolvePHPClassToRel(expectation.Class, classFiles, resolver)
				if (expectation.Missing && got == "") || (!expectation.Missing && got == expectation.Path) {
					composerHits++
				} else {
					t.Errorf("%s: resolve %q = %q; want path=%q missing=%v", corpus.ID, expectation.Class, got, expectation.Path, expectation.Missing)
				}
			}
			gotAutoload := composerAutoloadFiles(root, composer.files, loadGitIgnored(root))
			sort.Strings(gotAutoload)
			wantAutoload := append([]string(nil), corpus.AutoloadFiles...)
			sort.Strings(wantAutoload)
			composerTotal++
			if strings.Join(gotAutoload, "\x00") == strings.Join(wantAutoload, "\x00") {
				composerHits++
			} else {
				t.Errorf("%s: autoload files = %v; want %v", corpus.ID, gotAutoload, wantAutoload)
			}
			corpusAccuracy := phpCompatRatio(composerHits-composerHitsBefore, composerTotal-composerTotalBefore)
			t.Logf("PHP_COMPAT_COMPOSER corpus=%s accuracy=%.4f", corpus.ID, corpusAccuracy)
			if corpusAccuracy < suite.Thresholds.ComposerResolutionAccuracy {
				t.Errorf("%s Composer accuracy %.4f is below threshold %.4f", corpus.ID, corpusAccuracy, suite.Thresholds.ComposerResolutionAccuracy)
			}
		}
	}

	recall := phpCompatRatio(relationHits, relationTotal)
	precision := phpCompatRatio(precisionHits, precisionTotal)
	composerAccuracy := phpCompatRatio(composerHits, composerTotal)
	t.Logf("PHP_COMPAT_RELATIONSHIP recall=%.4f precision=%.4f forbidden=%d/%d composer=%.4f", recall, precision, forbiddenHits, phpCompatForbiddenCount(suite), composerAccuracy)
	if recall < suite.Thresholds.RelationshipRecall {
		t.Errorf("relationship recall %.4f is below threshold %.4f", recall, suite.Thresholds.RelationshipRecall)
	}
	if precision < suite.Thresholds.RelationshipPrecision {
		t.Errorf("relationship precision %.4f is below threshold %.4f", precision, suite.Thresholds.RelationshipPrecision)
	}
	if composerAccuracy < suite.Thresholds.ComposerResolutionAccuracy {
		t.Errorf("Composer accuracy %.4f is below threshold %.4f", composerAccuracy, suite.Thresholds.ComposerResolutionAccuracy)
	}
}

func phpCompatFilesByRel(t *testing.T, root string) map[string]phpFileRelations {
	t.Helper()
	files := map[string]phpFileRelations{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !isPHPRelationFile(path) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = parsePHPFileRelations(path, rel, string(source))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func phpCompatForwardRelations(graph *importGraph, source string) []string {
	out := []string{}
	for reason, targets := range map[string][]string{
		"imports":            graph.imports[source],
		"semantic_reference": graph.references[source],
		"autoloads":          graph.autoloads[source],
	} {
		for _, target := range targets {
			out = append(out, phpCompatRelationKey(source, target, reason))
		}
	}
	sort.Strings(out)
	return out
}

func phpCompatGraphHasReason(graph *importGraph, from, to, reason string) bool {
	var targets []string
	switch reason {
	case "imports":
		targets = graph.imports[from]
	case "semantic_reference":
		targets = graph.references[from]
	case "autoloads":
		targets = graph.autoloads[from]
	default:
		return false
	}
	for _, target := range targets {
		if target == to {
			return true
		}
	}
	return false
}

func phpCompatGraphHasAnyForwardEdge(graph *importGraph, from, to string) bool {
	return phpCompatGraphHasReason(graph, from, to, "imports") ||
		phpCompatGraphHasReason(graph, from, to, "semantic_reference") ||
		phpCompatGraphHasReason(graph, from, to, "autoloads")
}

func phpCompatRelationKey(from, to, reason string) string {
	return from + " -> " + to + " [" + reason + "]"
}

func phpCompatRatio(hit, total int) float64 {
	if total == 0 {
		return 1
	}
	return float64(hit) / float64(total)
}

func phpCompatForbiddenCount(suite phpcompat.Suite) int {
	total := 0
	for _, corpus := range suite.Corpora {
		total += len(corpus.ForbiddenRelations)
	}
	return total
}
