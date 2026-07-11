package evaluation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// HelpfulnessFixtureSet is the versioned contract for paired end-to-end
// helpfulness evaluation. It deliberately describes tasks and scoring without
// prescribing a model runner, so clients can supply their own execution layer.
type HelpfulnessFixtureSet struct {
	Version    int                `json:"version"`
	Name       string             `json:"name"`
	Experiment ExperimentContract `json:"experiment"`
	Scorecard  ScorecardContract  `json:"scorecard"`
	Tasks      []HelpfulnessTask  `json:"tasks"`
}

// ExperimentContract records the controls that must stay fixed between a
// baseline and Memento-assisted run.
type ExperimentContract struct {
	Baseline  string   `json:"baseline"`
	Treatment string   `json:"treatment"`
	Controls  []string `json:"controls"`
}

// ScorecardContract names the metrics that a compatible runner must report.
// Token metrics are separate so reports can distinguish input, output, and
// total token savings rather than fabricating a single estimate.
type ScorecardContract struct {
	Retrieval []string `json:"retrieval"`
	Agent     []string `json:"agent"`
	Memory    []string `json:"memory"`
	User      []string `json:"user"`
}

// HelpfulnessTask is one paired scenario. Expected evidence describes what a
// correct answer must ground itself in; validators describe how the later
// runner or reviewer decides whether the result meets that expectation.
type HelpfulnessTask struct {
	ID           string            `json:"id"`
	Category     string            `json:"category"`
	Title        string            `json:"title"`
	Prompt       string            `json:"prompt"`
	Start        TaskStart         `json:"start"`
	AllowedTools []string          `json:"allowedTools"`
	Expected     ExpectedOutcome   `json:"expected"`
	Validators   []ValidationCheck `json:"validators"`
}

// TaskStart makes the checkout and session state explicit. Memory seeds are
// used only for fresh-session memory-recovery scenarios.
type TaskStart struct {
	Repository  string       `json:"repository"`
	Revision    string       `json:"revision"`
	Session     string       `json:"session"`
	MemorySeeds []MemorySeed `json:"memorySeeds,omitempty"`
}

type MemorySeed struct {
	Key  string   `json:"key"`
	Text string   `json:"text"`
	Tags []string `json:"tags,omitempty"`
}

type ExpectedOutcome struct {
	Evidence []EvidenceRequirement `json:"evidence"`
	Patch    []string              `json:"patch,omitempty"`
	Tests    []string              `json:"tests,omitempty"`
}

type EvidenceRequirement struct {
	Path        string `json:"path"`
	Description string `json:"description"`
}

// ValidationCheck supports deterministic commands and evidence checks plus a
// blinded-review rubric for qualitative tasks. The runner issue will provide
// the execution adapters for these kinds.
type ValidationCheck struct {
	Kind    string `json:"kind"`
	Command string `json:"command,omitempty"`
	Rule    string `json:"rule,omitempty"`
}

func LoadHelpfulnessFixtures(r io.Reader) (HelpfulnessFixtureSet, error) {
	var fixtures HelpfulnessFixtureSet
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&fixtures); err != nil {
		return HelpfulnessFixtureSet{}, fmt.Errorf("decode helpfulness fixtures: %w", err)
	}
	if err := fixtures.Validate(); err != nil {
		return HelpfulnessFixtureSet{}, err
	}
	return fixtures, nil
}

func LoadHelpfulnessFixtureFile(path string) (HelpfulnessFixtureSet, error) {
	f, err := os.Open(path)
	if err != nil {
		return HelpfulnessFixtureSet{}, err
	}
	defer f.Close()
	return LoadHelpfulnessFixtures(f)
}

// Validate enforces the portable contract. It intentionally validates only
// syntax and declared invariants; existence of files, commands, and tool names
// belongs to the paired runner because those depend on the checkout/client.
func (f HelpfulnessFixtureSet) Validate() error {
	if f.Version != 1 {
		return fmt.Errorf("unsupported helpfulness fixture version %d", f.Version)
	}
	if strings.TrimSpace(f.Name) == "" {
		return errors.New("helpfulness fixture name is required")
	}
	if err := f.Experiment.validate(); err != nil {
		return err
	}
	if err := f.Scorecard.validate(); err != nil {
		return err
	}
	if len(f.Tasks) == 0 {
		return errors.New("helpfulness fixtures must contain at least one task")
	}
	ids := make(map[string]struct{}, len(f.Tasks))
	for i, task := range f.Tasks {
		if err := task.validate(i, ids); err != nil {
			return err
		}
	}
	return nil
}

func (e ExperimentContract) validate() error {
	if strings.TrimSpace(e.Baseline) == "" || strings.TrimSpace(e.Treatment) == "" {
		return errors.New("experiment baseline and treatment are required")
	}
	if e.Baseline == e.Treatment {
		return errors.New("experiment baseline and treatment must differ")
	}
	if len(e.Controls) == 0 {
		return errors.New("experiment controls are required")
	}
	return nil
}

func (s ScorecardContract) validate() error {
	for name, metrics := range map[string][]string{
		"retrieval": s.Retrieval,
		"agent":     s.Agent,
		"memory":    s.Memory,
		"user":      s.User,
	} {
		if len(metrics) == 0 {
			return fmt.Errorf("scorecard %s metrics are required", name)
		}
		if err := uniqueNonBlank(metrics, "scorecard "+name+" metric"); err != nil {
			return err
		}
	}
	return nil
}

func (t HelpfulnessTask) validate(i int, ids map[string]struct{}) error {
	prefix := fmt.Sprintf("tasks[%d]", i)
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf("%s.id is required", prefix)
	}
	if _, exists := ids[t.ID]; exists {
		return fmt.Errorf("duplicate helpfulness task id %q", t.ID)
	}
	ids[t.ID] = struct{}{}
	if !isOneOf(t.Category, "discovery", "impact-analysis", "implementation", "onboarding", "memory-recovery") {
		return fmt.Errorf("task %q has unsupported category %q", t.ID, t.Category)
	}
	if strings.TrimSpace(t.Title) == "" || strings.TrimSpace(t.Prompt) == "" {
		return fmt.Errorf("task %q title and prompt are required", t.ID)
	}
	if err := t.Start.validate(t.ID, t.Category); err != nil {
		return err
	}
	if err := uniqueNonBlank(t.AllowedTools, "task "+t.ID+" allowed tool"); err != nil {
		return err
	}
	if len(t.Expected.Evidence) == 0 {
		return fmt.Errorf("task %q must declare expected evidence", t.ID)
	}
	for j, evidence := range t.Expected.Evidence {
		if err := validateRepoPath(evidence.Path); err != nil {
			return fmt.Errorf("task %q evidence[%d]: %w", t.ID, j, err)
		}
		if strings.TrimSpace(evidence.Description) == "" {
			return fmt.Errorf("task %q evidence[%d] description is required", t.ID, j)
		}
	}
	if t.Category == "implementation" && len(t.Expected.Tests) == 0 {
		return fmt.Errorf("implementation task %q must declare expected tests", t.ID)
	}
	if len(t.Validators) == 0 {
		return fmt.Errorf("task %q must declare validators", t.ID)
	}
	for j, check := range t.Validators {
		if !isOneOf(check.Kind, "command", "evidence", "review") {
			return fmt.Errorf("task %q validator[%d] has unsupported kind %q", t.ID, j, check.Kind)
		}
		if check.Kind == "command" && strings.TrimSpace(check.Command) == "" {
			return fmt.Errorf("task %q validator[%d] command is required", t.ID, j)
		}
		if check.Kind != "command" && strings.TrimSpace(check.Rule) == "" {
			return fmt.Errorf("task %q validator[%d] rule is required", t.ID, j)
		}
	}
	return nil
}

func (s TaskStart) validate(taskID, category string) error {
	if strings.TrimSpace(s.Repository) == "" || strings.TrimSpace(s.Revision) == "" {
		return fmt.Errorf("task %q start repository and revision are required", taskID)
	}
	if !isOneOf(s.Session, "fresh", "continued") {
		return fmt.Errorf("task %q start session must be fresh or continued", taskID)
	}
	if category == "memory-recovery" && (s.Session != "fresh" || len(s.MemorySeeds) == 0) {
		return fmt.Errorf("memory-recovery task %q requires a fresh session and memory seeds", taskID)
	}
	keys := make(map[string]struct{}, len(s.MemorySeeds))
	for i, seed := range s.MemorySeeds {
		if strings.TrimSpace(seed.Key) == "" || strings.TrimSpace(seed.Text) == "" {
			return fmt.Errorf("task %q memory seed[%d] key and text are required", taskID, i)
		}
		if _, exists := keys[seed.Key]; exists {
			return fmt.Errorf("task %q has duplicate memory seed key %q", taskID, seed.Key)
		}
		keys[seed.Key] = struct{}{}
	}
	return nil
}

func validateRepoPath(path string) error {
	clean := filepath.ToSlash(filepath.Clean(path))
	if path == "" || clean == "." || clean != path || filepath.IsAbs(path) || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("invalid repo-relative path %q", path)
	}
	return nil
}

func uniqueNonBlank(values []string, label string) error {
	if len(values) == 0 {
		return fmt.Errorf("%ss are required", label)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s must not be blank", label)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("duplicate %s %q", label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func isOneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
