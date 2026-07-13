// Package phpcompat loads the checked-in PHP compatibility corpus used by
// parser, indexing, MCP relationship, anchor, and retrieval tests.
package phpcompat

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	SuiteVersion           = 2
	RetrievalSplitTrain    = "train"
	RetrievalSplitValidate = "validation"
	RetrievalSplitHoldout  = "holdout"
)

var supportedPHPVersion = regexp.MustCompile(`^(?:7\.4|8\.[0-4])$`)

type Thresholds struct {
	ParseSuccess               float64 `json:"parseSuccess"`
	SymbolRecall               float64 `json:"symbolRecall"`
	SignatureRecall            float64 `json:"signatureRecall"`
	DeclarationBoundaryRecall  float64 `json:"declarationBoundaryRecall"`
	AnchorAccuracy             float64 `json:"anchorAccuracy"`
	ComposerResolutionAccuracy float64 `json:"composerResolutionAccuracy"`
	RelationshipRecall         float64 `json:"relationshipRecall"`
	RelationshipPrecision      float64 `json:"relationshipPrecision"`
	RetrievalRecallAt5         float64 `json:"retrievalRecallAt5"`
	RetrievalMRR               float64 `json:"retrievalMRR"`
	RetrievalNDCGAt5           float64 `json:"retrievalNDCGAt5"`
}

type Suite struct {
	Version         int             `json:"version"`
	Thresholds      Thresholds      `json:"thresholds"`
	RetrievalPolicy RetrievalPolicy `json:"retrievalPolicy"`
	Corpora         []Corpus        `json:"corpora"`
	suitePath       string
}

type RetrievalPolicy struct {
	Adapter        string   `json:"adapter"`
	K              int      `json:"k"`
	RequiredSplits []string `json:"requiredSplits"`
	BlockingSplits []string `json:"blockingSplits"`
}

type Corpus struct {
	ID                  string                 `json:"id"`
	Kind                string                 `json:"kind"`
	PHPVersion          string                 `json:"phpVersion"`
	Root                string                 `json:"root"`
	ModelledOn          string                 `json:"modelledOn,omitempty"`
	Files               []FileExpectation      `json:"files"`
	Anchors             []AnchorExpectation    `json:"anchors,omitempty"`
	Relations           []RelationExpectation  `json:"relations,omitempty"`
	ForbiddenRelations  []ForbiddenRelation    `json:"forbiddenRelations,omitempty"`
	ComposerResolutions []ComposerResolution   `json:"composerResolutions,omitempty"`
	AutoloadFiles       []string               `json:"autoloadFiles,omitempty"`
	Retrieval           []RetrievalExpectation `json:"retrieval,omitempty"`
}

type FileExpectation struct {
	Path              string              `json:"path"`
	Language          string              `json:"language"`
	DeclarationStarts []int               `json:"declarationStarts,omitempty"`
	Symbols           []SymbolExpectation `json:"symbols"`
	ForbiddenSymbols  []string            `json:"forbiddenSymbols,omitempty"`
}

type SymbolExpectation struct {
	Name              string   `json:"name"`
	Kind              string   `json:"kind"`
	Container         string   `json:"container,omitempty"`
	SignatureContains []string `json:"signatureContains"`
}

type AnchorExpectation struct {
	Path      string `json:"path"`
	Symbol    string `json:"symbol"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}

type RelationExpectation struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	Reasons []string `json:"reasons"`
}

type ForbiddenRelation struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type ComposerResolution struct {
	Class   string `json:"class"`
	Path    string `json:"path,omitempty"`
	Missing bool   `json:"missing,omitempty"`
}

type RetrievalExpectation struct {
	ID            string                      `json:"id"`
	Split         string                      `json:"split"`
	Query         string                      `json:"query"`
	Relevant      []RetrievalChunkExpectation `json:"relevant"`
	HardNegatives []RetrievalChunkExpectation `json:"hardNegatives,omitempty"`
}

type RetrievalChunkExpectation struct {
	Path      string `json:"path"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}

func Load(path string) (Suite, error) {
	f, err := os.Open(path)
	if err != nil {
		return Suite{}, err
	}
	defer f.Close()

	var suite Suite
	decoder := json.NewDecoder(bufio.NewReader(f))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&suite); err != nil {
		return Suite{}, fmt.Errorf("decode PHP compatibility suite: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Suite{}, errors.New("decode PHP compatibility suite: trailing JSON value")
		}
		return Suite{}, fmt.Errorf("decode PHP compatibility suite trailing data: %w", err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Suite{}, err
	}
	suite.suitePath = filepath.Clean(absolute)
	if err := suite.Validate(); err != nil {
		return Suite{}, err
	}
	return suite, nil
}

func (s Suite) Validate() error {
	if s.Version != SuiteVersion {
		return fmt.Errorf("unsupported PHP compatibility suite version %d", s.Version)
	}
	for name, value := range map[string]float64{
		"parseSuccess":               s.Thresholds.ParseSuccess,
		"symbolRecall":               s.Thresholds.SymbolRecall,
		"signatureRecall":            s.Thresholds.SignatureRecall,
		"declarationBoundaryRecall":  s.Thresholds.DeclarationBoundaryRecall,
		"anchorAccuracy":             s.Thresholds.AnchorAccuracy,
		"composerResolutionAccuracy": s.Thresholds.ComposerResolutionAccuracy,
		"relationshipRecall":         s.Thresholds.RelationshipRecall,
		"relationshipPrecision":      s.Thresholds.RelationshipPrecision,
		"retrievalRecallAt5":         s.Thresholds.RetrievalRecallAt5,
		"retrievalMRR":               s.Thresholds.RetrievalMRR,
		"retrievalNDCGAt5":           s.Thresholds.RetrievalNDCGAt5,
	} {
		if value <= 0 || value > 1 {
			return fmt.Errorf("threshold %s must be greater than zero and at most one", name)
		}
	}
	if strings.TrimSpace(s.RetrievalPolicy.Adapter) == "" {
		return errors.New("retrievalPolicy.adapter is required")
	}
	if s.RetrievalPolicy.K != 5 {
		return errors.New("retrievalPolicy.k must be 5 for the configured thresholds")
	}
	requiredSplits := map[string]bool{}
	for index, split := range s.RetrievalPolicy.RequiredSplits {
		if split != RetrievalSplitTrain && split != RetrievalSplitValidate && split != RetrievalSplitHoldout || requiredSplits[split] {
			return fmt.Errorf("retrievalPolicy.requiredSplits[%d] is invalid or duplicate", index)
		}
		requiredSplits[split] = true
	}
	if !requiredSplits[RetrievalSplitTrain] || !requiredSplits[RetrievalSplitValidate] {
		return errors.New("retrievalPolicy.requiredSplits must include train and validation")
	}
	blockingSplits := map[string]bool{}
	for index, split := range s.RetrievalPolicy.BlockingSplits {
		if !requiredSplits[split] || blockingSplits[split] {
			return fmt.Errorf("retrievalPolicy.blockingSplits[%d] is not required or is duplicate", index)
		}
		blockingSplits[split] = true
	}
	if !blockingSplits[RetrievalSplitTrain] || !blockingSplits[RetrievalSplitValidate] || blockingSplits[RetrievalSplitHoldout] {
		return errors.New("retrievalPolicy.blockingSplits must include train and validation but not holdout")
	}
	if len(s.Corpora) == 0 {
		return errors.New("PHP compatibility suite has no corpora")
	}
	seen := map[string]bool{}
	for index := range s.Corpora {
		corpus := &s.Corpora[index]
		if strings.TrimSpace(corpus.ID) == "" || seen[corpus.ID] {
			return fmt.Errorf("corpus %d has blank or duplicate id %q", index, corpus.ID)
		}
		seen[corpus.ID] = true
		if err := s.validateCorpus(*corpus); err != nil {
			return fmt.Errorf("corpus %q: %w", corpus.ID, err)
		}
	}
	return nil
}

func (s Suite) validateCorpus(corpus Corpus) error {
	switch corpus.Kind {
	case "language", "composer", "framework":
	default:
		return fmt.Errorf("unsupported kind %q", corpus.Kind)
	}
	if !supportedPHPVersion.MatchString(corpus.PHPVersion) {
		return fmt.Errorf("unsupported phpVersion %q", corpus.PHPVersion)
	}
	root, err := s.CorpusRoot(corpus)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("root %q is not a directory", corpus.Root)
	}
	if corpus.Kind != "language" && strings.TrimSpace(corpus.ModelledOn) == "" {
		return errors.New("non-language corpus requires modelledOn provenance")
	}

	filePaths := map[string]bool{}
	for index, file := range corpus.Files {
		if err := validateRelativePath(file.Path); err != nil {
			return fmt.Errorf("files[%d]: %w", index, err)
		}
		if filePaths[file.Path] {
			return fmt.Errorf("duplicate file expectation %q", file.Path)
		}
		filePaths[file.Path] = true
		if file.Language != "php" {
			return fmt.Errorf("file %q has unsupported language %q", file.Path, file.Language)
		}
		lineCount, err := validateFixtureFile(root, file.Path)
		if err != nil {
			return err
		}
		previous := 0
		for _, line := range file.DeclarationStarts {
			if line <= previous || line > lineCount {
				return fmt.Errorf("file %q has invalid declaration start %d", file.Path, line)
			}
			previous = line
		}
		seenSymbols := map[string]bool{}
		for symbolIndex, symbol := range file.Symbols {
			key := symbol.Kind + "\x00" + symbol.Container + "\x00" + symbol.Name
			if strings.TrimSpace(symbol.Name) == "" || strings.TrimSpace(symbol.Kind) == "" || seenSymbols[key] {
				return fmt.Errorf("file %q symbol %d is blank or duplicate", file.Path, symbolIndex)
			}
			seenSymbols[key] = true
			if len(symbol.SignatureContains) == 0 {
				return fmt.Errorf("file %q symbol %q has no signature fragments", file.Path, symbol.Name)
			}
			for _, fragment := range symbol.SignatureContains {
				if strings.TrimSpace(fragment) == "" {
					return fmt.Errorf("file %q symbol %q has blank signature fragment", file.Path, symbol.Name)
				}
			}
		}
		if hasDuplicateStrings(file.ForbiddenSymbols) {
			return fmt.Errorf("file %q has duplicate forbidden symbols", file.Path)
		}
	}

	for index, anchor := range corpus.Anchors {
		if err := validateCorpusPath(root, anchor.Path); err != nil {
			return fmt.Errorf("anchors[%d]: %w", index, err)
		}
		if strings.TrimSpace(anchor.Symbol) == "" || anchor.StartLine <= 0 || anchor.EndLine < anchor.StartLine {
			return fmt.Errorf("anchors[%d] has invalid symbol or range", index)
		}
		lineCount, _ := validateFixtureFile(root, anchor.Path)
		if anchor.EndLine > lineCount {
			return fmt.Errorf("anchors[%d] end line exceeds %s", index, anchor.Path)
		}
	}
	for index, relation := range corpus.Relations {
		if err := validateRelationPaths(root, relation.From, relation.To); err != nil {
			return fmt.Errorf("relations[%d]: %w", index, err)
		}
		if len(relation.Reasons) == 0 || hasDuplicateStrings(relation.Reasons) {
			return fmt.Errorf("relations[%d] requires unique reasons", index)
		}
	}
	for index, relation := range corpus.ForbiddenRelations {
		if err := validateRelationPaths(root, relation.From, relation.To); err != nil {
			return fmt.Errorf("forbiddenRelations[%d]: %w", index, err)
		}
	}

	classes := map[string]bool{}
	for index, resolution := range corpus.ComposerResolutions {
		if strings.TrimSpace(resolution.Class) == "" || classes[strings.ToLower(resolution.Class)] {
			return fmt.Errorf("composerResolutions[%d] has blank or duplicate class", index)
		}
		classes[strings.ToLower(resolution.Class)] = true
		if resolution.Missing {
			if resolution.Path != "" {
				return fmt.Errorf("composerResolutions[%d] cannot set path when missing", index)
			}
		} else if err := validateCorpusPath(root, resolution.Path); err != nil {
			return fmt.Errorf("composerResolutions[%d]: %w", index, err)
		}
	}
	for index, path := range corpus.AutoloadFiles {
		if err := validateCorpusPath(root, path); err != nil {
			return fmt.Errorf("autoloadFiles[%d]: %w", index, err)
		}
	}

	requiredSplits := map[string]bool{}
	for _, split := range s.RetrievalPolicy.RequiredSplits {
		requiredSplits[split] = true
	}
	queries := map[string]bool{}
	splitCounts := map[string]int{}
	for index, query := range corpus.Retrieval {
		if strings.TrimSpace(query.ID) == "" || queries[query.ID] || strings.TrimSpace(query.Query) == "" || len(query.Relevant) == 0 {
			return fmt.Errorf("retrieval[%d] has invalid or duplicate id, query, or judgments", index)
		}
		if !requiredSplits[query.Split] {
			return fmt.Errorf("retrieval[%d] has unsupported split %q", index, query.Split)
		}
		if query.Split != RetrievalSplitTrain && len(query.HardNegatives) == 0 {
			return fmt.Errorf("retrieval[%d] %s query requires a hard negative", index, query.Split)
		}
		queries[query.ID] = true
		splitCounts[query.Split]++
		relevantPaths := map[string]bool{}
		for relevantIndex, judgment := range query.Relevant {
			if err := validateRetrievalChunk(root, judgment); err != nil {
				return fmt.Errorf("retrieval[%d].relevant[%d]: %w", index, relevantIndex, err)
			}
			if relevantPaths[judgment.Path] {
				return fmt.Errorf("retrieval[%d].relevant[%d] repeats path %q for a distinct-path adapter", index, relevantIndex, judgment.Path)
			}
			relevantPaths[judgment.Path] = true
		}
		negativePaths := map[string]bool{}
		for negativeIndex, judgment := range query.HardNegatives {
			if err := validateRetrievalChunk(root, judgment); err != nil {
				return fmt.Errorf("retrieval[%d].hardNegatives[%d]: %w", index, negativeIndex, err)
			}
			if negativePaths[judgment.Path] {
				return fmt.Errorf("retrieval[%d].hardNegatives[%d] repeats path %q for a distinct-path adapter", index, negativeIndex, judgment.Path)
			}
			negativePaths[judgment.Path] = true
			for relevantIndex, relevant := range query.Relevant {
				if retrievalChunksOverlap(judgment, relevant) {
					return fmt.Errorf("retrieval[%d].hardNegatives[%d] overlaps relevant[%d]", index, negativeIndex, relevantIndex)
				}
			}
		}
	}
	for _, split := range s.RetrievalPolicy.RequiredSplits {
		if splitCounts[split] == 0 {
			return fmt.Errorf("retrieval requires at least one %s query", split)
		}
	}
	return nil
}

func (s Suite) CorpusRoot(corpus Corpus) (string, error) {
	if s.suitePath == "" {
		return "", errors.New("suite path is unavailable; load the suite from a file")
	}
	if err := validateRelativePath(corpus.Root); err != nil {
		return "", fmt.Errorf("invalid corpus root: %w", err)
	}
	base := filepath.Dir(s.suitePath)
	root := filepath.Clean(filepath.Join(base, filepath.FromSlash(corpus.Root)))
	if root != base && !strings.HasPrefix(root, base+string(filepath.Separator)) {
		return "", fmt.Errorf("corpus root escapes suite directory: %q", corpus.Root)
	}
	return root, nil
}

func (s Suite) FindCorpus(id string) (Corpus, bool) {
	for _, corpus := range s.Corpora {
		if corpus.ID == id {
			return corpus, true
		}
	}
	return Corpus{}, false
}

func SortedCorpusIDs(s Suite) []string {
	ids := make([]string, 0, len(s.Corpora))
	for _, corpus := range s.Corpora {
		ids = append(ids, corpus.ID)
	}
	sort.Strings(ids)
	return ids
}

func IsPHPSourcePath(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	for _, suffix := range []string{".php", ".module", ".install", ".theme", ".inc", ".profile", ".engine"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func validateFixtureFile(root, path string) (int, error) {
	if err := validateCorpusPath(root, path); err != nil {
		return 0, err
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return 0, err
	}
	if len(b) == 0 {
		return 0, fmt.Errorf("fixture file %q is empty", path)
	}
	lines := strings.Count(string(b), "\n")
	if b[len(b)-1] != '\n' {
		lines++
	}
	return lines, nil
}

func validateRetrievalChunk(root string, chunk RetrievalChunkExpectation) error {
	lineCount, err := validateFixtureFile(root, chunk.Path)
	if err != nil {
		return err
	}
	if chunk.StartLine <= 0 || chunk.EndLine < chunk.StartLine || chunk.EndLine > lineCount {
		return fmt.Errorf("invalid line range %d-%d for %q with %d lines", chunk.StartLine, chunk.EndLine, chunk.Path, lineCount)
	}
	return nil
}

func retrievalChunksOverlap(left, right RetrievalChunkExpectation) bool {
	return left.Path == right.Path && left.StartLine <= right.EndLine && left.EndLine >= right.StartLine
}

func validateRelationPaths(root, from, to string) error {
	if from == to {
		return errors.New("relationship cannot point to itself")
	}
	if err := validateCorpusPath(root, from); err != nil {
		return fmt.Errorf("invalid from path: %w", err)
	}
	if err := validateCorpusPath(root, to); err != nil {
		return fmt.Errorf("invalid to path: %w", err)
	}
	return nil
}

func validateCorpusPath(root, path string) error {
	if err := validateRelativePath(path); err != nil {
		return err
	}
	abs := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(abs)
	if err != nil {
		return fmt.Errorf("fixture path %q does not exist", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("fixture path %q is not a regular file", path)
	}
	return nil
}

func validateRelativePath(path string) error {
	clean := filepath.ToSlash(filepath.Clean(path))
	if path == "" || clean == "." || clean != path || filepath.IsAbs(path) || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("path %q must be normalized and relative", path)
	}
	return nil
}

func hasDuplicateStrings(values []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}
