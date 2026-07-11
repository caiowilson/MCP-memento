package evaluation

import (
	"strings"
	"testing"
)

func TestEvaluateGatesReportsAdvisoryTargets(t *testing.T) {
	helpfulness, supplement := visualFixture()
	helpfulness.Results[0].Baseline.Outcome = OutcomeSuccess
	retrieval := retrievalGateFixture(.50)
	report, err := EvaluateGates(GateInputs{Current: helpfulness, Baseline: helpfulness, RetrievalCurrent: retrieval, RetrievalBaseline: retrieval, Supplement: supplement, Policy: gatePolicyFixture()})
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != "advisory-regression" || report.HasMeasuredRegression() || report.HasInfrastructureFailure() {
		t.Fatalf("gate report = %#v", report)
	}
	if got := checkByID(t, report, "retrieval-baseline").Status; got != GatePassed {
		t.Fatalf("retrieval baseline = %s", got)
	}
	if got := checkByID(t, report, "retrieval-recall-floor").Status; got != GateRegression {
		t.Fatalf("retrieval target = %s", got)
	}
	if got := checkByID(t, report, "context-efficiency"); got.Status != GatePassed || got.Actual == nil || *got.Actual < 26 {
		t.Fatalf("efficiency = %#v", got)
	}
	if !strings.Contains(GateMarkdown(report), "advisory-regression") {
		t.Fatal("markdown omitted outcome")
	}
}

func TestEvaluateGatesSeparatesProductAndInfrastructureFailures(t *testing.T) {
	helpfulness, _ := visualFixture()
	policy := gatePolicyFixture()
	current := retrievalGateFixture(.40)
	baseline := retrievalGateFixture(.50)
	report, err := EvaluateGates(GateInputs{Current: helpfulness, Baseline: helpfulness, RetrievalCurrent: current, RetrievalBaseline: baseline, Supplement: HelpfulnessVisualSupplement{Version: HelpfulnessVisualVersion}, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != "product-regression" || !report.HasMeasuredRegression() || report.HasInfrastructureFailure() {
		t.Fatalf("measured regression report = %#v", report)
	}

	current.ConfigurationFingerprint = "changed"
	report, err = EvaluateGates(GateInputs{Current: helpfulness, Baseline: helpfulness, RetrievalCurrent: current, RetrievalBaseline: baseline, Supplement: HelpfulnessVisualSupplement{Version: HelpfulnessVisualVersion}, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != "infrastructure-failure" || !report.HasInfrastructureFailure() {
		t.Fatalf("incomparable report = %#v", report)
	}
}

func TestEvaluateGatesClassifiesMissingAgentRunsAsInfrastructure(t *testing.T) {
	baseline, _ := visualFixture()
	current := baseline
	current.Results = nil
	retrieval := retrievalGateFixture(.5)
	report, err := EvaluateGates(GateInputs{Current: current, Baseline: baseline, RetrievalCurrent: retrieval, RetrievalBaseline: retrieval, Supplement: HelpfulnessVisualSupplement{Version: HelpfulnessVisualVersion}, Policy: gatePolicyFixture()})
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != "infrastructure-failure" || !report.HasInfrastructureFailure() || checkByID(t, report, "task-success").Status != GateIncomparable {
		t.Fatalf("missing agent runs = %#v", report)
	}
}

func TestEvaluateGatesRequiresMatchedAgentConfiguration(t *testing.T) {
	baseline, _ := visualFixture()
	current := baseline
	current.Results = append([]PairedTaskResult(nil), baseline.Results...)
	current.Results[0].Baseline.ConfigurationFingerprint = "changed"
	retrieval := retrievalGateFixture(.5)
	report, err := EvaluateGates(GateInputs{Current: current, Baseline: baseline, RetrievalCurrent: retrieval, RetrievalBaseline: retrieval, Supplement: HelpfulnessVisualSupplement{Version: HelpfulnessVisualVersion}, Policy: gatePolicyFixture()})
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != "infrastructure-failure" || checkByID(t, report, "task-success").Status != GateIncomparable {
		t.Fatalf("configuration mismatch = %#v", report)
	}
}

func TestGatePolicyRequiresRationale(t *testing.T) {
	policy := gatePolicyFixture()
	policy.TaskSuccess.Rationale = ""
	if err := policy.Validate(); err == nil || !strings.Contains(err.Error(), "rationale") {
		t.Fatalf("missing rationale error = %v", err)
	}
}

func TestEfficiencyGateExcludesFailedPairs(t *testing.T) {
	helpfulness, _ := visualFixture()
	check := evaluateEfficiency(helpfulness, EfficiencyRule{MinSamples: 1, MinimumReductionPercent: 20, Categories: []string{"discovery"}, Rationale: "successful pairs only"})
	if check.Status != GateUnavailable || check.Sample != 0 || check.Actual != nil {
		t.Fatalf("failed baseline pair affected efficiency: %#v", check)
	}
}

func gatePolicyFixture() GatePolicy {
	return GatePolicy{
		Version:              GatePolicyVersion,
		RetrievalBaseline:    ComparisonRule{Enforce: true, MinSamples: 1, MaxDecrease: 0, Rationale: "stable deterministic baseline"},
		RetrievalRecallFloor: FloorRule{MinSamples: 1, Minimum: .95, Rationale: "initial target"},
		TaskSuccess:          ComparisonRule{MinSamples: 2, MaxDecrease: 0, Rationale: "no task regression target"},
		Efficiency:           EfficiencyRule{MinSamples: 1, MinimumReductionPercent: 20, Categories: []string{"discovery"}, Rationale: "context-heavy target"},
		HelpfulRating:        FloorRule{MinSamples: 1, Minimum: .8, Rationale: "opt-in target"},
	}
}

func retrievalGateFixture(recall float64) RetrievalSummary {
	return RetrievalSummary{Version: RetrievalSummaryVersion, Mode: "lexical", ConfigurationFingerprint: "config", FixtureFingerprint: "fixture", QueryCount: 4, K: 5, Metrics: Metrics{Precision: .2, Recall: recall, MRR: .3, NDCG: .4}}
}

func checkByID(t *testing.T, report GateReport, id string) GateCheck {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("missing check %q", id)
	return GateCheck{}
}
