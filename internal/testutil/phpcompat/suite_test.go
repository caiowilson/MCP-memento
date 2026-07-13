package phpcompat

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestCheckedInSuiteIsStrictAndComplete(t *testing.T) {
	suite := loadCheckedInSuite(t)
	if suite.Version != 2 || suite.RetrievalPolicy.Adapter != "terms-v2" || suite.RetrievalPolicy.K != 5 {
		t.Fatalf("unexpected retrieval policy: version=%d policy=%#v", suite.Version, suite.RetrievalPolicy)
	}
	wantIDs := []string{
		"composer-autoload",
		"drupal-module",
		"laravel-app",
		"php-7.4",
		"php-8.0",
		"php-8.1",
		"php-8.2",
		"php-8.3",
		"php-8.4",
		"symfony-app",
		"wordpress-plugin-theme",
	}
	if got := SortedCorpusIDs(suite); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("corpus ids = %#v, want %#v", got, wantIDs)
	}

	versions := map[string]bool{}
	frameworks := map[string]bool{}
	retrievalSplits := map[string]int{}
	retrievalQueries := 0
	retrievalJudgments := 0
	for _, corpus := range suite.Corpora {
		if corpus.Kind == "language" {
			versions[corpus.PHPVersion] = true
		}
		if corpus.Kind == "framework" {
			frameworks[corpus.ID] = true
		}
		for _, query := range corpus.Retrieval {
			retrievalQueries++
			retrievalSplits[query.Split]++
			retrievalJudgments += len(query.Relevant)
			for _, relevant := range query.Relevant {
				if relevant.StartLine <= 0 || relevant.EndLine < relevant.StartLine {
					t.Errorf("query %s has unbounded relevance: %#v", query.ID, relevant)
				}
			}
			if query.Split != RetrievalSplitTrain && len(query.HardNegatives) == 0 {
				t.Errorf("%s query %s has no hard negative", query.Split, query.ID)
			}
		}
	}
	for _, version := range []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4"} {
		if !versions[version] {
			t.Errorf("missing PHP %s corpus", version)
		}
	}
	for _, framework := range []string{"laravel-app", "symfony-app", "wordpress-plugin-theme", "drupal-module"} {
		if !frameworks[framework] {
			t.Errorf("missing framework corpus %s", framework)
		}
	}
	if retrievalQueries != 30 || retrievalJudgments != 35 || retrievalSplits[RetrievalSplitTrain] != 19 || retrievalSplits[RetrievalSplitValidate] != 11 {
		t.Fatalf("unexpected retrieval corpus: queries=%d judgments=%d splits=%v", retrievalQueries, retrievalJudgments, retrievalSplits)
	}
}

func TestSuiteRejectsOverlappingRetrievalHardNegative(t *testing.T) {
	suite := loadCheckedInSuite(t)
	query := &suite.Corpora[0].Retrieval[0]
	query.HardNegatives = []RetrievalChunkExpectation{query.Relevant[0]}
	if err := suite.Validate(); err == nil || !strings.Contains(err.Error(), "overlaps relevant") {
		t.Fatalf("expected overlapping hard-negative failure, got %v", err)
	}
}

func TestSuiteRejectsUnboundedRetrievalJudgment(t *testing.T) {
	suite := loadCheckedInSuite(t)
	suite.Corpora[0].Retrieval[0].Relevant[0].StartLine = 0
	if err := suite.Validate(); err == nil || !strings.Contains(err.Error(), "invalid line range") {
		t.Fatalf("expected unbounded relevance failure, got %v", err)
	}
}

func TestSuiteRejectsRepeatedRetrievalPaths(t *testing.T) {
	for _, mutate := range []func(*RetrievalExpectation){
		func(query *RetrievalExpectation) { query.Relevant = append(query.Relevant, query.Relevant[0]) },
		func(query *RetrievalExpectation) {
			query.HardNegatives = append(query.HardNegatives, query.HardNegatives[0])
		},
	} {
		suite := loadCheckedInSuite(t)
		query := &suite.Corpora[0].Retrieval[0]
		mutate(query)
		if err := suite.Validate(); err == nil || !strings.Contains(err.Error(), "repeats path") {
			t.Fatalf("expected repeated-path failure, got %v", err)
		}
	}
}

func TestCheckedInCorpusIsDependencyFreeAndUsesLF(t *testing.T) {
	suite := loadCheckedInSuite(t)
	phpSourceCount := 0
	for _, corpus := range suite.Corpora {
		root, err := suite.CorpusRoot(corpus)
		if err != nil {
			t.Fatal(err)
		}
		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				t.Errorf("fixture corpus contains symlink: %s", path)
				return nil
			}
			if entry.IsDir() {
				if entry.Name() == "vendor" || entry.Name() == "node_modules" {
					t.Errorf("fixture corpus contains dependency directory: %s", path)
				}
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(b), "\r\n") {
				t.Errorf("fixture must use LF line endings: %s", path)
			}
			if IsPHPSourcePath(path) {
				phpSourceCount++
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if phpSourceCount < 50 {
		t.Fatalf("PHP source fixture count = %d, want at least 50", phpSourceCount)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "suite.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"thresholds":{},"corpora":[],"unknown":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected strict unknown-field failure, got %v", err)
	}
}

func TestPHPSourceExtensions(t *testing.T) {
	for _, path := range []string{"a.php", "x.blade.php", "report.module", "report.install", "report.theme", "report.inc", "report.profile", "report.engine"} {
		if !IsPHPSourcePath(path) {
			t.Errorf("expected PHP source path: %s", path)
		}
	}
	if IsPHPSourcePath("report.services.yml") {
		t.Fatal("YAML must not be classified as PHP source")
	}
}

func loadCheckedInSuite(t *testing.T) Suite {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(current), "../../.."))
	suite, err := Load(filepath.Join(repoRoot, "evaluation/php-compat/suite.v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	return suite
}

func TestSortedCorpusIDsReturnsCopy(t *testing.T) {
	suite := Suite{Corpora: []Corpus{{ID: "z"}, {ID: "a"}}}
	got := SortedCorpusIDs(suite)
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"a", "z"}) || suite.Corpora[0].ID != "z" {
		t.Fatalf("unexpected sorted ids or mutation: %#v %#v", got, suite.Corpora)
	}
}
