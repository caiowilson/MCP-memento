package evaluation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHelpfulnessReportsPositiveAndNeutralPairs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "evidence.txt"), []byte("local evidence"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixtures := runnerFixtureSet()
	report, err := RunHelpfulness(context.Background(), fixtures, PairedRunConfig{
		Root: root,
		Executor: RecordedExecutor{Observations: []RunObservation{
			observation("positive", BaselineCondition, OutcomeFailure, "match-positive", 100, 4, tokenUsage(100, 20, 120)),
			observation("positive", MementoCondition, OutcomeSuccess, "match-positive", 60, 2, tokenUsage(70, 10, 80)),
			observation("neutral", BaselineCondition, OutcomeSuccess, "match-neutral", 30, 1, TokenUsage{}),
			observation("neutral", MementoCondition, OutcomeSuccess, "match-neutral", 30, 1, TokenUsage{}),
		}},
	})
	if err != nil {
		t.Fatalf("run helpfulness: %v", err)
	}
	if report.Summary.ValidPairs != 2 || report.Summary.InvalidPairs != 0 {
		t.Fatalf("pair summary = %#v", report.Summary)
	}
	if got := *report.Results[0].Deltas.Success.Delta; got != 1 {
		t.Fatalf("positive success delta = %v, want 1", got)
	}
	if got := *report.Results[0].Deltas.Tokens.Total.Delta; got != -40 {
		t.Fatalf("positive total-token delta = %v, want -40", got)
	}
	if !report.Results[1].Deltas.Tokens.Total.Unavailable {
		t.Fatal("neutral task must preserve unavailable token usage")
	}
	conditions := map[RunCondition]int{}
	for _, result := range report.Results[0].Validators {
		conditions[result.Condition]++
		if result.Kind == "review" && result.Status != ValidatorUnavailable {
			t.Fatalf("review validator = %s, want unavailable", result.Status)
		}
		if result.Kind != "review" && result.Status != ValidatorPassed {
			t.Fatalf("validator %#v", result)
		}
	}
	if conditions[BaselineCondition] != 3 || conditions[MementoCondition] != 3 {
		t.Fatalf("validators were not run for both conditions: %#v", conditions)
	}
	markdown := HelpfulnessMarkdown(report)
	if !strings.Contains(markdown, "-40.00") || !strings.Contains(markdown, "| neutral |") || !strings.Contains(markdown, "unavailable") {
		t.Fatalf("markdown does not show paired token delta and unavailable values:\n%s", markdown)
	}
	jsonPath, _, err := WriteHelpfulnessReport(filepath.Join(root, "out"), report)
	if err != nil {
		t.Fatalf("write report: %v", err)
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "answer prompt") || strings.Contains(string(data), "docs/evidence.txt") {
		t.Fatalf("report leaked fixture text or a path: %s", data)
	}
}

func TestRunHelpfulnessMarksMismatchedPairsInvalid(t *testing.T) {
	fixtures := runnerFixtureSet()
	report, err := RunHelpfulness(context.Background(), fixtures, PairedRunConfig{
		Root: t.TempDir(), TaskIDs: []string{"neutral"},
		Executor: RecordedExecutor{Observations: []RunObservation{
			observation("neutral", BaselineCondition, OutcomeSuccess, "one", 1, 1, TokenUsage{}),
			observation("neutral", MementoCondition, OutcomeSuccess, "two", 1, 1, TokenUsage{}),
		}},
	})
	if err != nil {
		t.Fatalf("mismatched pair should be explicit, not an execution error: %v", err)
	}
	if report.Summary.InvalidPairs != 1 || report.Summary.ValidPairs != 0 || report.Results[0].Baseline.Outcome != OutcomeInvalid || !report.Results[0].Deltas.Success.Unavailable {
		t.Fatalf("mismatched pair was not explicit and excluded: %#v", report)
	}
}

func TestRunHelpfulnessSeparatesTimeoutPairs(t *testing.T) {
	fixtures := runnerFixtureSet()
	report, err := RunHelpfulness(context.Background(), fixtures, PairedRunConfig{
		Root: t.TempDir(), TaskIDs: []string{"neutral"},
		Executor: RecordedExecutor{Observations: []RunObservation{
			observation("neutral", BaselineCondition, OutcomeTimeout, "matched", 1, 1, TokenUsage{}),
			observation("neutral", MementoCondition, OutcomeSuccess, "matched", 1, 1, TokenUsage{}),
		}},
	})
	if err != nil {
		t.Fatalf("run timeout pair: %v", err)
	}
	if report.Summary.TimeoutPairs != 1 || report.Summary.InvalidPairs != 0 || report.Summary.ValidPairs != 0 {
		t.Fatalf("timeout pair summary = %#v", report.Summary)
	}
}

func TestNewBlindedReviewItemOmitsConditionAndPrompt(t *testing.T) {
	task := runnerFixtureSet().Tasks[0]
	item := NewBlindedReviewItem(task, task.Validators[2], "redacted response")
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if item.ReviewID == "" || strings.Contains(string(data), "baseline") || strings.Contains(string(data), task.Prompt) {
		t.Fatalf("blinded review export leaked task identity: %s", data)
	}
}

func runnerFixtureSet() HelpfulnessFixtureSet {
	return HelpfulnessFixtureSet{
		Version: 1, Name: "runner-test",
		Experiment: ExperimentContract{Baseline: "baseline", Treatment: "memento", Controls: []string{"same"}},
		Scorecard:  ScorecardContract{Retrieval: []string{"r"}, Agent: []string{"a"}, Memory: []string{"m"}, User: []string{"u"}},
		Tasks: []HelpfulnessTask{
			{ID: "positive", Category: "discovery", Title: "Positive", Prompt: "answer prompt", Start: TaskStart{Repository: "repo", Revision: "main", Session: "fresh"}, AllowedTools: []string{"repo_search"}, Expected: ExpectedOutcome{Evidence: []EvidenceRequirement{{Path: "docs/evidence.txt", Description: "local proof"}}}, Validators: []ValidationCheck{{Kind: "evidence", Rule: "find evidence"}, {Kind: "command", Command: "test -f docs/evidence.txt"}, {Kind: "review", Rule: "assess answer"}}},
			{ID: "neutral", Category: "discovery", Title: "Neutral", Prompt: "neutral prompt", Start: TaskStart{Repository: "repo", Revision: "main", Session: "fresh"}, AllowedTools: []string{"repo_search"}, Expected: ExpectedOutcome{Evidence: []EvidenceRequirement{{Path: "docs/evidence.txt", Description: "local proof"}}}, Validators: []ValidationCheck{{Kind: "evidence", Rule: "find evidence"}}},
		},
	}
}

func observation(taskID string, condition RunCondition, outcome RunOutcome, match string, elapsed, turns int64, tokens TokenUsage) RunObservation {
	return RunObservation{TaskID: taskID, Condition: condition, Outcome: outcome, MatchFingerprint: match, ConfigurationFingerprint: "config-" + string(condition), Metrics: RunMetrics{ElapsedTimeMs: elapsed, Turns: turns, Tokens: tokens, ToolContext: ToolContextMetrics{ToolCalls: turns, ContextReads: turns, ContextBytes: elapsed}}}
}

func tokenUsage(input, output, total int64) TokenUsage {
	return TokenUsage{Input: &input, Output: &output, Total: &total}
}
