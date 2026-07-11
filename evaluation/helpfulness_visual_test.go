package evaluation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpfulnessVisualRendersPairedSavingsAndUnavailableUsage(t *testing.T) {
	report, supplement := visualFixture()
	markdown := HelpfulnessVisualMarkdown(report, supplement)
	for _, want := range []string{
		"1200 → 880", "320 (26.7%)", "1 task pairs have unavailable total-token usage", "usage-client", "tokenizer-example", "Retrieval small multiples", "Benchmark trend",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	html := HelpfulnessVisualHTML(report, supplement)
	if !strings.Contains(html, "Paired total-token use by task") || strings.Count(html, "<svg") < 3 || strings.Contains(html, "secret prompt") {
		t.Fatalf("html is missing accessible static charts or leaked content:\n%s", html)
	}
	dir := t.TempDir()
	htmlPath, markdownPath, err := WriteHelpfulnessVisual(dir, report, supplement)
	if err != nil {
		t.Fatalf("write visual: %v", err)
	}
	if _, err := os.Stat(htmlPath); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "helpfulness-visual.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("visual markdown snapshot changed\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestHelpfulnessVisualSupplementValidation(t *testing.T) {
	invalid := HelpfulnessVisualSupplement{Version: HelpfulnessVisualVersion, Retrieval: []RetrievalVisualSnapshot{{Mode: "hybrid", ConfigurationFingerprint: "cfg"}}}
	if err := invalid.validate(); err == nil || !strings.Contains(err.Error(), "lexical or semantic") {
		t.Fatalf("invalid retrieval error = %v", err)
	}
	invalid = HelpfulnessVisualSupplement{Version: HelpfulnessVisualVersion, Feedback: &OptInFeedbackSummary{Respondents: 2, Helpful: 1}}
	if err := invalid.validate(); err == nil || !strings.Contains(err.Error(), "sum to respondents") {
		t.Fatalf("invalid feedback error = %v", err)
	}
}

func TestHelpfulnessVisualSeparatesTimeoutPairs(t *testing.T) {
	report, _ := visualFixture()
	report.Results = append(report.Results, PairedTaskResult{TaskID: "timed-out", Category: "discovery", Baseline: RunObservation{Outcome: OutcomeTimeout}, Memento: RunObservation{Outcome: OutcomeSuccess}})
	summary := buildVisualSummary(report)
	if summary.TimeoutPairs != 1 || summary.InvalidPairs != 0 || summary.BaselineAttempts != 2 {
		t.Fatalf("timeout pair was folded into outcomes: %#v", summary)
	}
}

func TestHelpfulnessVisualAggregatesTotalTokensIndependently(t *testing.T) {
	report, _ := visualFixture()
	baselineTotal, mementoTotal := int64(50), int64(30)
	report.Results = append(report.Results, PairedTaskResult{TaskID: "total-only", Category: "discovery", Baseline: RunObservation{Outcome: OutcomeSuccess, Metrics: RunMetrics{Tokens: TokenUsage{Total: &baselineTotal}}}, Memento: RunObservation{Outcome: OutcomeSuccess, Metrics: RunMetrics{Tokens: TokenUsage{Total: &mementoTotal}}}})
	summary := buildVisualSummary(report)
	if summary.TotalTokenPairs != 2 || summary.InputTokenPairs != 1 || summary.TotalBaseline != 1250 || summary.TotalMemento != 910 {
		t.Fatalf("token dimensions were not aggregated independently: %#v", summary)
	}
}

func TestTrendAnnotationNamesChangedInputs(t *testing.T) {
	points := []TrendVisualSnapshot{{FixtureFingerprint: "fixture-a", ModelFingerprint: "model", PromptFingerprint: "prompt", TokenizerFingerprint: "tokenizer", ScoringFingerprint: "scoring"}, {FixtureFingerprint: "fixture-b", ModelFingerprint: "model", PromptFingerprint: "prompt", TokenizerFingerprint: "tokenizer", ScoringFingerprint: "scoring"}}
	if got := trendAnnotation(points, 0); got != "baseline" {
		t.Fatalf("first annotation = %q", got)
	}
	if got := trendAnnotation(points, 1); got != "fixture changed" {
		t.Fatalf("changed annotation = %q", got)
	}
}

func visualFixture() (HelpfulnessReport, HelpfulnessVisualSupplement) {
	base := int64(1200)
	memento := int64(880)
	inputBase := int64(1000)
	inputMemento := int64(700)
	outputBase := int64(200)
	outputMemento := int64(180)
	report := HelpfulnessReport{Version: HelpfulnessReportVersion, FixtureFingerprint: "sha256:fixture", Results: []PairedTaskResult{
		{TaskID: "discovery-task", Category: "discovery", Baseline: RunObservation{Outcome: OutcomeFailure, ModelFingerprint: "sha256:model", ConfigurationFingerprint: "sha256:baseline-config", Metrics: RunMetrics{ElapsedTimeMs: 1200, Tokens: TokenUsage{Input: &inputBase, Output: &outputBase, Total: &base, UsageSource: "usage-client", TokenizerFingerprint: "sha256:tokenizer-example"}}}, Memento: RunObservation{Outcome: OutcomeSuccess, ModelFingerprint: "sha256:model", ConfigurationFingerprint: "sha256:memento-config", Metrics: RunMetrics{ElapsedTimeMs: 900, Tokens: TokenUsage{Input: &inputMemento, Output: &outputMemento, Total: &memento, UsageSource: "usage-client", TokenizerFingerprint: "sha256:tokenizer-example"}}}},
		{TaskID: "onboarding-task", Category: "onboarding", Baseline: RunObservation{Outcome: OutcomeSuccess, ConfigurationFingerprint: "sha256:baseline-config", Metrics: RunMetrics{ElapsedTimeMs: 300}}, Memento: RunObservation{Outcome: OutcomeSuccess, ConfigurationFingerprint: "sha256:memento-config", Metrics: RunMetrics{ElapsedTimeMs: 300}}},
	}}
	success := 0.75
	savings := 320.0
	supplement := HelpfulnessVisualSupplement{Version: HelpfulnessVisualVersion, Retrieval: []RetrievalVisualSnapshot{{Mode: "lexical", ConfigurationFingerprint: "sha256:lexical", Precision: .2, Recall: .3, MRR: .3, NDCG: .25}, {Mode: "semantic", ConfigurationFingerprint: "sha256:semantic", Precision: .3, Recall: .4, MRR: .4, NDCG: .35}}, Trend: []TrendVisualSnapshot{{Revision: "abc123", FixtureFingerprint: "sha256:fixture", ModelFingerprint: "sha256:model", PromptFingerprint: "sha256:prompt", TokenizerFingerprint: "sha256:tokenizer-example", ScoringFingerprint: "sha256:score", TaskSuccessRate: &success, TotalTokenSavings: &savings}}, Feedback: &OptInFeedbackSummary{Respondents: 3, Helpful: 2, Neutral: 1}}
	return report, supplement
}
