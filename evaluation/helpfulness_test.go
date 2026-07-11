package evaluation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadHelpfulnessFixtureFile(t *testing.T) {
	fixtures, err := LoadHelpfulnessFixtureFile("fixtures/helpfulness.json")
	if err != nil {
		t.Fatalf("load helpfulness fixtures: %v", err)
	}
	if got := len(fixtures.Tasks); got != 20 {
		t.Fatalf("task count = %d, want 20", got)
	}
	counts := map[string]int{}
	for _, task := range fixtures.Tasks {
		counts[task.Category]++
		for _, evidence := range task.Expected.Evidence {
			if _, err := os.Stat(filepath.Join("..", filepath.FromSlash(evidence.Path))); err != nil {
				t.Fatalf("task %q evidence path %q: %v", task.ID, evidence.Path, err)
			}
		}
	}
	for _, category := range []string{"discovery", "impact-analysis", "implementation", "onboarding"} {
		if counts[category] == 0 {
			t.Fatalf("missing %s task", category)
		}
	}
	if got := counts["memory-recovery"]; got != 5 {
		t.Fatalf("memory-recovery tasks = %d, want 5", got)
	}
	if !contains(fixtures.Scorecard.Agent, "inputTokens") || !contains(fixtures.Scorecard.Agent, "outputTokens") || !contains(fixtures.Scorecard.Agent, "totalTokens") {
		t.Fatalf("scorecard must track input, output, and total tokens: %#v", fixtures.Scorecard.Agent)
	}
}

func TestHelpfulnessFixtureValidation(t *testing.T) {
	valid := validHelpfulnessFixture()
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid fixture rejected: %v", err)
	}

	invalid := validHelpfulnessFixture()
	invalid.Tasks[0].Start.MemorySeeds = nil
	err := invalid.Validate()
	if err == nil || !strings.Contains(err.Error(), "requires a fresh session and memory seeds") {
		t.Fatalf("missing memory seeds error = %v", err)
	}

	invalid = validHelpfulnessFixture()
	invalid.Tasks[0].Expected.Evidence[0].Path = "../secret"
	err = invalid.Validate()
	if err == nil || !strings.Contains(err.Error(), "invalid repo-relative path") {
		t.Fatalf("unsafe evidence path error = %v", err)
	}

	invalid = validHelpfulnessFixture()
	invalid.Tasks[0].Validators = []ValidationCheck{{Kind: "command"}}
	err = invalid.Validate()
	if err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("missing command error = %v", err)
	}
}

func validHelpfulnessFixture() HelpfulnessFixtureSet {
	return HelpfulnessFixtureSet{
		Version: 1,
		Name:    "valid",
		Experiment: ExperimentContract{
			Baseline:  "without memento",
			Treatment: "with memento",
			Controls:  []string{"same prompt"},
		},
		Scorecard: ScorecardContract{
			Retrieval: []string{"recallAtK"},
			Agent:     []string{"totalTokens"},
			Memory:    []string{"correctRecoveryRate"},
			User:      []string{"helpfulSessionRate"},
		},
		Tasks: []HelpfulnessTask{{
			ID:       "memory",
			Category: "memory-recovery",
			Title:    "Recover a decision",
			Prompt:   "Recover it",
			Start: TaskStart{
				Repository: "repo",
				Revision:   "main",
				Session:    "fresh",
				MemorySeeds: []MemorySeed{{
					Key:  "decision",
					Text: "Keep the invariant.",
				}},
			},
			AllowedTools: []string{"memory_search"},
			Expected: ExpectedOutcome{Evidence: []EvidenceRequirement{{
				Path:        "internal/example.go",
				Description: "implementation anchor",
			}}},
			Validators: []ValidationCheck{{Kind: "evidence", Rule: "mentions the invariant"}},
		}},
	}
}

func TestLoadHelpfulnessFixturesRejectsUnknownFields(t *testing.T) {
	_, err := LoadHelpfulnessFixtures(strings.NewReader(`{
  "version": 1,
  "name": "bad",
  "experiment": {"baseline": "a", "treatment": "b", "controls": ["same"]},
  "scorecard": {"retrieval": ["r"], "agent": ["a"], "memory": ["m"], "user": ["u"]},
  "tasks": [],
  "unexpected": true
}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error = %v", err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
