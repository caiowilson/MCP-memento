package feedback

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	EventVersion  = 1
	ExportVersion = 1
	eventsFile    = "events-v1.jsonl"
)

var ErrDisabled = errors.New("aggregate feedback is disabled")

type ToolCategory string

const (
	CategoryRepository  ToolCategory = "repository"
	CategoryMemory      ToolCategory = "memory"
	CategoryIndex       ToolCategory = "index"
	CategoryWorkspace   ToolCategory = "workspace"
	CategoryUnavailable ToolCategory = "unavailable"
)

type DurationBucket string

const (
	DurationUnder100MS  DurationBucket = "under_100ms"
	DurationUnder1S     DurationBucket = "100ms_to_1s"
	DurationUnder10S    DurationBucket = "1s_to_10s"
	Duration10SOrMore   DurationBucket = "10s_or_more"
	DurationUnavailable DurationBucket = "unavailable"
)

type ResultSizeBucket string

const (
	ResultEmpty       ResultSizeBucket = "empty"
	ResultUnder4KB    ResultSizeBucket = "under_4kb"
	ResultUnder32KB   ResultSizeBucket = "4kb_to_32kb"
	Result32KBOrMore  ResultSizeBucket = "32kb_or_more"
	ResultUnavailable ResultSizeBucket = "unavailable"
)

type FailureClass string

const (
	FailureNone        FailureClass = "none"
	FailureTool        FailureClass = "tool_error"
	FailureCanceled    FailureClass = "canceled"
	FailureTimeout     FailureClass = "timeout"
	FailureUnavailable FailureClass = "unavailable"
)

type Rating string

const (
	RatingHelpful    Rating = "helpful"
	RatingNotHelpful Rating = "not_helpful"
	RatingUnsure     Rating = "unsure"
)

// FeatureFlags contains only coarse boolean configuration state. It must never
// contain model names, paths, repository fingerprints, or user identifiers.
type FeatureFlags struct {
	SemanticRetrieval bool `json:"semanticRetrieval"`
	RedactionEnabled  bool `json:"redactionEnabled"`
}

// Event is the complete on-disk event schema. All fields are closed enums or
// booleans; there is intentionally no extension metadata or free-text field.
type Event struct {
	Version          int              `json:"version"`
	ToolCategory     ToolCategory     `json:"toolCategory"`
	DurationBucket   DurationBucket   `json:"durationBucket"`
	ResultSizeBucket ResultSizeBucket `json:"resultSizeBucket"`
	FailureClass     FailureClass     `json:"failureClass"`
	Features         FeatureFlags     `json:"features"`
	Rating           Rating           `json:"rating,omitempty"`
}

type Config struct {
	Enabled  bool
	Dir      string
	Features FeatureFlags
}

func ConfigFromEnv() Config {
	return Config{
		Enabled: envOptIn("MEMENTO_FEEDBACK_ENABLED"),
		Dir:     strings.TrimSpace(os.Getenv("MEMENTO_FEEDBACK_DIR")),
		Features: FeatureFlags{
			SemanticRetrieval: envOptIn("MEMENTO_SEMANTIC_ENABLED"),
			RedactionEnabled:  envDefaultTrue("MEMENTO_REDACTION_ENABLED"),
		},
	}
}

func envOptIn(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envDefaultTrue(key string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return true
	}
	return value != "0" && value != "false" && value != "no" && value != "off"
}

func ResolvePath(cfg Config) (string, error) {
	dir := strings.TrimSpace(cfg.Dir)
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".memento-mcp", "feedback")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Clean(abs), eventsFile), nil
}

type Recorder interface {
	Record(Event) error
}

type Store struct {
	path     string
	enabled  bool
	features FeatureFlags
	mu       sync.Mutex
}

func NewStore(cfg Config) (*Store, error) {
	path, err := ResolvePath(cfg)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, enabled: cfg.Enabled, features: cfg.Features}, nil
}

func (s *Store) Enabled() bool { return s != nil && s.enabled }
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) Record(event Event) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	event.Version = EventVersion
	event.Features = s.features
	if err := event.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func (e Event) Validate() error {
	if e.Version != EventVersion {
		return fmt.Errorf("unsupported feedback event version %d", e.Version)
	}
	if !validToolCategory(e.ToolCategory) {
		return fmt.Errorf("invalid tool category %q", e.ToolCategory)
	}
	if !validDurationBucket(e.DurationBucket) {
		return fmt.Errorf("invalid duration bucket %q", e.DurationBucket)
	}
	if !validResultSizeBucket(e.ResultSizeBucket) {
		return fmt.Errorf("invalid result-size bucket %q", e.ResultSizeBucket)
	}
	if !validFailureClass(e.FailureClass) {
		return fmt.Errorf("invalid failure class %q", e.FailureClass)
	}
	if e.Rating != "" && e.Rating != RatingHelpful && e.Rating != RatingNotHelpful && e.Rating != RatingUnsure {
		return fmt.Errorf("invalid rating %q", e.Rating)
	}
	return nil
}

func validToolCategory(value ToolCategory) bool {
	switch value {
	case CategoryRepository, CategoryMemory, CategoryIndex, CategoryWorkspace, CategoryUnavailable:
		return true
	default:
		return false
	}
}

func validDurationBucket(value DurationBucket) bool {
	switch value {
	case DurationUnder100MS, DurationUnder1S, DurationUnder10S, Duration10SOrMore, DurationUnavailable:
		return true
	default:
		return false
	}
}

func validResultSizeBucket(value ResultSizeBucket) bool {
	switch value {
	case ResultEmpty, ResultUnder4KB, ResultUnder32KB, Result32KBOrMore, ResultUnavailable:
		return true
	default:
		return false
	}
}

func validFailureClass(value FailureClass) bool {
	switch value {
	case FailureNone, FailureTool, FailureCanceled, FailureTimeout, FailureUnavailable:
		return true
	default:
		return false
	}
}

func (s *Store) ReadEvents() ([]Event, error) {
	if s == nil {
		return nil, errors.New("feedback store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	events := []Event{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	for line := 1; scanner.Scan(); line++ {
		var event Event
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&event); err != nil {
			return nil, fmt.Errorf("decode feedback event line %d: %w", line, err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			if err == nil {
				err = errors.New("multiple JSON values")
			}
			return nil, fmt.Errorf("decode feedback event line %d trailing data: %w", line, err)
		}
		if err := event.Validate(); err != nil {
			return nil, fmt.Errorf("validate feedback event line %d: %w", line, err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *Store) Delete() error {
	if s == nil {
		return errors.New("feedback store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

type FeedbackSummary struct {
	Respondents        int      `json:"respondents"`
	Helpful            int      `json:"helpful"`
	Neutral            int      `json:"neutral"`
	Unhelpful          int      `json:"unhelpful"`
	HelpfulSessionRate *float64 `json:"helpfulSessionRate,omitempty"`
}

type OperationCount struct {
	ToolCategory     ToolCategory     `json:"toolCategory"`
	DurationBucket   DurationBucket   `json:"durationBucket"`
	ResultSizeBucket ResultSizeBucket `json:"resultSizeBucket"`
	FailureClass     FailureClass     `json:"failureClass"`
	Features         FeatureFlags     `json:"features"`
	Count            int              `json:"count"`
}

type ExportReport struct {
	Version    int              `json:"version"`
	Operations []OperationCount `json:"operations"`
	Feedback   FeedbackSummary  `json:"feedback"`
}

func BuildExport(events []Event) ExportReport {
	type operationKey struct {
		category ToolCategory
		duration DurationBucket
		size     ResultSizeBucket
		failure  FailureClass
		semantic bool
		redact   bool
	}
	counts := map[operationKey]int{}
	summary := FeedbackSummary{}
	for _, event := range events {
		if event.Rating == "" {
			key := operationKey{event.ToolCategory, event.DurationBucket, event.ResultSizeBucket, event.FailureClass, event.Features.SemanticRetrieval, event.Features.RedactionEnabled}
			counts[key]++
		}
		switch event.Rating {
		case RatingHelpful:
			summary.Helpful++
		case RatingNotHelpful:
			summary.Unhelpful++
		case RatingUnsure:
			summary.Neutral++
		}
	}
	summary.Respondents = summary.Helpful + summary.Neutral + summary.Unhelpful
	if summary.Respondents > 0 {
		rate := float64(summary.Helpful) / float64(summary.Respondents)
		summary.HelpfulSessionRate = &rate
	}

	operations := make([]OperationCount, 0, len(counts))
	for key, count := range counts {
		operations = append(operations, OperationCount{
			ToolCategory: key.category, DurationBucket: key.duration, ResultSizeBucket: key.size,
			FailureClass: key.failure, Features: FeatureFlags{SemanticRetrieval: key.semantic, RedactionEnabled: key.redact}, Count: count,
		})
	}
	sort.Slice(operations, func(i, j int) bool {
		left, _ := json.Marshal(operations[i])
		right, _ := json.Marshal(operations[j])
		return string(left) < string(right)
	})
	return ExportReport{Version: ExportVersion, Operations: operations, Feedback: summary}
}

func (s *Store) WriteExport(w io.Writer) error {
	events, err := s.ReadEvents()
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(BuildExport(events))
}

func (s *Store) WriteEvaluationSupplement(w io.Writer) error {
	events, err := s.ReadEvents()
	if err != nil {
		return err
	}
	summary := BuildExport(events).Feedback
	payload := struct {
		Version  int `json:"version"`
		Feedback struct {
			Respondents int `json:"respondents"`
			Helpful     int `json:"helpful"`
			Neutral     int `json:"neutral"`
			Unhelpful   int `json:"unhelpful"`
		} `json:"feedback"`
	}{Version: 1}
	payload.Feedback.Respondents = summary.Respondents
	payload.Feedback.Helpful = summary.Helpful
	payload.Feedback.Neutral = summary.Neutral
	payload.Feedback.Unhelpful = summary.Unhelpful
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func BucketDuration(duration time.Duration) DurationBucket {
	switch {
	case duration < 100*time.Millisecond:
		return DurationUnder100MS
	case duration < time.Second:
		return DurationUnder1S
	case duration < 10*time.Second:
		return DurationUnder10S
	default:
		return Duration10SOrMore
	}
}

func BucketResultSize(size int) ResultSizeBucket {
	switch {
	case size <= 0:
		return ResultEmpty
	case size < 4*1024:
		return ResultUnder4KB
	case size < 32*1024:
		return ResultUnder32KB
	default:
		return Result32KBOrMore
	}
}

func ClassifyFailure(err error) FailureClass {
	switch {
	case err == nil:
		return FailureNone
	case errors.Is(err, context.DeadlineExceeded):
		return FailureTimeout
	case errors.Is(err, context.Canceled):
		return FailureCanceled
	default:
		return FailureTool
	}
}

func CategoryForTool(name string) ToolCategory {
	switch {
	case name == "repo_switch_workspace":
		return CategoryWorkspace
	case strings.HasPrefix(name, "repo_index_") || name == "repo_reindex" || name == "repo_clear_index":
		return CategoryIndex
	case strings.HasPrefix(name, "repo_"):
		return CategoryRepository
	case strings.HasPrefix(name, "memory_"):
		return CategoryMemory
	default:
		return CategoryUnavailable
	}
}
