package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const GatePolicyVersion = 1
const GateReportVersion = 1

type ComparisonRule struct {
	Enforce     bool    `json:"enforce"`
	MinSamples  int     `json:"minSamples"`
	MaxDecrease float64 `json:"maxDecrease"`
	Rationale   string  `json:"rationale"`
}

type FloorRule struct {
	Enforce    bool    `json:"enforce"`
	MinSamples int     `json:"minSamples"`
	Minimum    float64 `json:"minimum"`
	Rationale  string  `json:"rationale"`
}

type EfficiencyRule struct {
	Enforce                 bool     `json:"enforce"`
	MinSamples              int      `json:"minSamples"`
	MinimumReductionPercent float64  `json:"minimumReductionPercent"`
	Categories              []string `json:"categories"`
	Rationale               string   `json:"rationale"`
}

// GatePolicy records both thresholds and the rationale required to change
// them. Advisory rules report regressions without blocking publication.
type GatePolicy struct {
	Version              int            `json:"version"`
	RetrievalBaseline    ComparisonRule `json:"retrievalBaseline"`
	RetrievalRecallFloor FloorRule      `json:"retrievalRecallFloor"`
	TaskSuccess          ComparisonRule `json:"taskSuccess"`
	Efficiency           EfficiencyRule `json:"efficiency"`
	HelpfulRating        FloorRule      `json:"helpfulRating"`
}

func LoadGatePolicyFile(path string) (GatePolicy, error) {
	f, err := os.Open(path)
	if err != nil {
		return GatePolicy{}, err
	}
	defer f.Close()
	var policy GatePolicy
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return GatePolicy{}, fmt.Errorf("decode evaluation gate policy: %w", err)
	}
	if err := policy.Validate(); err != nil {
		return GatePolicy{}, err
	}
	return policy, nil
}

func (p GatePolicy) Validate() error {
	if p.Version != GatePolicyVersion {
		return fmt.Errorf("unsupported evaluation gate policy version %d", p.Version)
	}
	for name, rule := range map[string]ComparisonRule{"retrievalBaseline": p.RetrievalBaseline, "taskSuccess": p.TaskSuccess} {
		if rule.MinSamples <= 0 || rule.MaxDecrease < 0 || strings.TrimSpace(rule.Rationale) == "" {
			return fmt.Errorf("%s requires positive minSamples, non-negative maxDecrease, and rationale", name)
		}
	}
	for name, rule := range map[string]FloorRule{"retrievalRecallFloor": p.RetrievalRecallFloor, "helpfulRating": p.HelpfulRating} {
		if rule.MinSamples <= 0 || rule.Minimum < 0 || rule.Minimum > 1 || strings.TrimSpace(rule.Rationale) == "" {
			return fmt.Errorf("%s requires positive minSamples, minimum between zero and one, and rationale", name)
		}
	}
	if p.Efficiency.MinSamples <= 0 || p.Efficiency.MinimumReductionPercent < 0 || len(p.Efficiency.Categories) == 0 || strings.TrimSpace(p.Efficiency.Rationale) == "" {
		return fmt.Errorf("efficiency requires positive minSamples, non-negative reduction, categories, and rationale")
	}
	seen := map[string]bool{}
	for _, category := range p.Efficiency.Categories {
		if strings.TrimSpace(category) == "" || seen[category] {
			return fmt.Errorf("efficiency categories must be unique and non-blank")
		}
		seen[category] = true
	}
	return nil
}

type GateStatus string

const (
	GatePassed       GateStatus = "pass"
	GateRegression   GateStatus = "regression"
	GateUnavailable  GateStatus = "unavailable"
	GateIncomparable GateStatus = "incomparable"
)

type GateCheck struct {
	ID        string     `json:"id"`
	Status    GateStatus `json:"status"`
	Enforced  bool       `json:"enforced"`
	Actual    *float64   `json:"actual,omitempty"`
	Baseline  *float64   `json:"baseline,omitempty"`
	Threshold float64    `json:"threshold"`
	Sample    int        `json:"sample"`
	Unit      string     `json:"unit"`
	Detail    string     `json:"detail"`
	Rationale string     `json:"rationale"`
}

type GateReport struct {
	Version             int         `json:"version"`
	CurrentFingerprint  string      `json:"currentFingerprint"`
	BaselineFingerprint string      `json:"baselineFingerprint"`
	Outcome             string      `json:"outcome"`
	Checks              []GateCheck `json:"checks"`
}

type GateInputs struct {
	Current           HelpfulnessReport
	Baseline          HelpfulnessReport
	RetrievalCurrent  RetrievalSummary
	RetrievalBaseline RetrievalSummary
	Supplement        HelpfulnessVisualSupplement
	Policy            GatePolicy
}

func EvaluateGates(inputs GateInputs) (GateReport, error) {
	if err := inputs.Policy.Validate(); err != nil {
		return GateReport{}, err
	}
	if inputs.Current.Version != HelpfulnessReportVersion || inputs.Baseline.Version != HelpfulnessReportVersion {
		return GateReport{}, fmt.Errorf("current and baseline helpfulness reports must use version %d", HelpfulnessReportVersion)
	}
	if err := inputs.RetrievalCurrent.Validate(); err != nil {
		return GateReport{}, fmt.Errorf("current retrieval summary: %w", err)
	}
	if err := inputs.RetrievalBaseline.Validate(); err != nil {
		return GateReport{}, fmt.Errorf("baseline retrieval summary: %w", err)
	}
	if err := inputs.Supplement.validate(); err != nil {
		return GateReport{}, fmt.Errorf("visual supplement: %w", err)
	}
	report := GateReport{
		Version: GateReportVersion,
		CurrentFingerprint: fingerprint(struct {
			Helpfulness string `json:"helpfulness"`
			Retrieval   string `json:"retrieval"`
		}{fingerprint(inputs.Current), fingerprint(inputs.RetrievalCurrent)}),
		BaselineFingerprint: fingerprint(struct {
			Helpfulness string `json:"helpfulness"`
			Retrieval   string `json:"retrieval"`
		}{fingerprint(inputs.Baseline), fingerprint(inputs.RetrievalBaseline)}),
	}
	report.Checks = append(report.Checks,
		evaluateRetrievalBaseline(inputs.RetrievalCurrent, inputs.RetrievalBaseline, inputs.Policy.RetrievalBaseline),
		evaluateRetrievalFloor(inputs.RetrievalCurrent, inputs.Policy.RetrievalRecallFloor),
		evaluateTaskSuccess(inputs.Current, inputs.Baseline, inputs.Policy.TaskSuccess),
		evaluateEfficiency(inputs.Current, inputs.Policy.Efficiency),
		evaluateHelpfulRating(inputs.Supplement.Feedback, inputs.Policy.HelpfulRating),
	)
	report.Outcome = "pass"
	if report.HasInfrastructureFailure() {
		report.Outcome = "infrastructure-failure"
	} else if report.HasMeasuredRegression() {
		report.Outcome = "product-regression"
	} else if report.HasAdvisoryRegression() {
		report.Outcome = "advisory-regression"
	}
	return report, nil
}

func evaluateRetrievalBaseline(current, baseline RetrievalSummary, rule ComparisonRule) GateCheck {
	check := GateCheck{ID: "retrieval-baseline", Enforced: rule.Enforce, Threshold: rule.MaxDecrease, Unit: "recall-rate decrease", Rationale: rule.Rationale, Sample: current.QueryCount}
	check.Actual, check.Baseline = floatPointer(current.Metrics.Recall), floatPointer(baseline.Metrics.Recall)
	if current.Mode != baseline.Mode || current.ConfigurationFingerprint != baseline.ConfigurationFingerprint || current.FixtureFingerprint != baseline.FixtureFingerprint || current.K != baseline.K {
		check.Status = GateIncomparable
		check.Detail = "retrieval fixture or configuration changed; update the committed baseline with rationale"
		return check
	}
	if current.QueryCount < rule.MinSamples {
		check.Status = GateUnavailable
		check.Detail = fmt.Sprintf("need at least %d retrieval queries", rule.MinSamples)
		return check
	}
	check.Status = GatePassed
	check.Detail = "current recall does not decrease beyond the committed baseline allowance"
	if baseline.Metrics.Recall-current.Metrics.Recall > rule.MaxDecrease {
		check.Status = GateRegression
		check.Detail = "current recall decreased beyond the committed baseline allowance"
	}
	return check
}

func evaluateRetrievalFloor(current RetrievalSummary, rule FloorRule) GateCheck {
	check := GateCheck{ID: "retrieval-recall-floor", Enforced: rule.Enforce, Threshold: rule.Minimum, Unit: "recall@k", Rationale: rule.Rationale, Sample: current.QueryCount, Actual: floatPointer(current.Metrics.Recall)}
	if current.QueryCount < rule.MinSamples {
		check.Status = GateUnavailable
		check.Detail = fmt.Sprintf("need at least %d retrieval queries", rule.MinSamples)
		return check
	}
	check.Status = GatePassed
	check.Detail = "retrieval recall meets the target floor"
	if current.Metrics.Recall < rule.Minimum {
		check.Status = GateRegression
		check.Detail = "retrieval recall is below the target floor"
	}
	return check
}

func evaluateTaskSuccess(current, baseline HelpfulnessReport, rule ComparisonRule) GateCheck {
	currentRate, currentN := mementoSuccessRate(current)
	baselineRate, _ := mementoSuccessRate(baseline)
	check := GateCheck{ID: "task-success", Enforced: rule.Enforce, Threshold: rule.MaxDecrease, Unit: "success-rate decrease", Rationale: rule.Rationale, Sample: currentN, Actual: currentRate, Baseline: baselineRate}
	if !comparableHelpfulness(current, baseline) {
		check.Status = GateIncomparable
		check.Detail = "helpfulness fixture or matched task configuration changed; update the baseline with rationale"
		return check
	}
	if currentN == 0 || currentRate == nil || baselineRate == nil {
		check.Status = GateIncomparable
		check.Detail = "no valid paired task result is available; inspect the runner or model execution"
		return check
	}
	if currentN < rule.MinSamples {
		check.Status = GateUnavailable
		check.Detail = fmt.Sprintf("need at least %d valid paired tasks", rule.MinSamples)
		return check
	}
	check.Status = GatePassed
	check.Detail = "Memento task-success rate does not decrease beyond the baseline allowance"
	if *baselineRate-*currentRate > rule.MaxDecrease {
		check.Status = GateRegression
		check.Detail = "Memento task-success rate decreased beyond the baseline allowance"
	}
	return check
}

func comparableHelpfulness(current, baseline HelpfulnessReport) bool {
	if current.FixtureFingerprint != baseline.FixtureFingerprint || len(current.Results) != len(baseline.Results) {
		return false
	}
	byID := make(map[string]PairedTaskResult, len(baseline.Results))
	for _, result := range baseline.Results {
		byID[result.TaskID] = result
	}
	for _, result := range current.Results {
		other, ok := byID[result.TaskID]
		if !ok || result.TaskFingerprint != other.TaskFingerprint || result.MatchFingerprint != other.MatchFingerprint ||
			result.Baseline.ConfigurationFingerprint != other.Baseline.ConfigurationFingerprint || result.Memento.ConfigurationFingerprint != other.Memento.ConfigurationFingerprint ||
			result.Baseline.ModelFingerprint != other.Baseline.ModelFingerprint || result.Memento.ModelFingerprint != other.Memento.ModelFingerprint ||
			result.Baseline.Metrics.Tokens.TokenizerFingerprint != other.Baseline.Metrics.Tokens.TokenizerFingerprint || result.Memento.Metrics.Tokens.TokenizerFingerprint != other.Memento.Metrics.Tokens.TokenizerFingerprint {
			return false
		}
	}
	return true
}

func mementoSuccessRate(report HelpfulnessReport) (*float64, int) {
	successes, sample := 0, 0
	for _, result := range report.Results {
		if result.Baseline.Outcome == OutcomeInvalid || result.Memento.Outcome == OutcomeInvalid || result.Baseline.Outcome == OutcomeTimeout || result.Memento.Outcome == OutcomeTimeout {
			continue
		}
		sample++
		if result.Memento.Outcome == OutcomeSuccess {
			successes++
		}
	}
	if sample == 0 {
		return nil, 0
	}
	return floatPointer(float64(successes) / float64(sample)), sample
}

func evaluateEfficiency(current HelpfulnessReport, rule EfficiencyRule) GateCheck {
	categories := make(map[string]bool, len(rule.Categories))
	for _, category := range rule.Categories {
		categories[category] = true
	}
	reductions := make([]float64, 0)
	tokenPairs, elapsedPairs := 0, 0
	for _, result := range current.Results {
		if !categories[result.Category] || result.Baseline.Outcome != OutcomeSuccess || result.Memento.Outcome != OutcomeSuccess {
			continue
		}
		baselineTotal, mementoTotal := result.Baseline.Metrics.Tokens.Total, result.Memento.Metrics.Tokens.Total
		if baselineTotal != nil && mementoTotal != nil && *baselineTotal > 0 {
			reductions = append(reductions, float64(*baselineTotal-*mementoTotal)*100/float64(*baselineTotal))
			tokenPairs++
			continue
		}
		if result.Baseline.Metrics.ElapsedTimeMs > 0 {
			reductions = append(reductions, float64(result.Baseline.Metrics.ElapsedTimeMs-result.Memento.Metrics.ElapsedTimeMs)*100/float64(result.Baseline.Metrics.ElapsedTimeMs))
			elapsedPairs++
		}
	}
	check := GateCheck{ID: "context-efficiency", Enforced: rule.Enforce, Threshold: rule.MinimumReductionPercent, Unit: "median percent reduction", Rationale: rule.Rationale, Sample: len(reductions), Detail: fmt.Sprintf("%s and %s", gatePairLabel(tokenPairs, "token-based pair"), gatePairLabel(elapsedPairs, "elapsed-time fallback pair"))}
	if len(reductions) > 0 {
		sort.Float64s(reductions)
		median := reductions[len(reductions)/2]
		if len(reductions)%2 == 0 {
			median = (reductions[len(reductions)/2-1] + reductions[len(reductions)/2]) / 2
		}
		check.Actual = floatPointer(median)
	}
	if len(reductions) < rule.MinSamples {
		check.Status = GateUnavailable
		check.Detail += fmt.Sprintf("; need at least %d qualifying successful tasks", rule.MinSamples)
		return check
	}
	median := *check.Actual
	check.Status = GatePassed
	if median < rule.MinimumReductionPercent {
		check.Status = GateRegression
		check.Detail += "; median reduction is below the target"
	} else {
		check.Detail += "; median reduction meets the target"
	}
	return check
}

func evaluateHelpfulRating(feedback *OptInFeedbackSummary, rule FloorRule) GateCheck {
	check := GateCheck{ID: "helpful-rating", Enforced: rule.Enforce, Threshold: rule.Minimum, Unit: "helpful-rating rate", Rationale: rule.Rationale}
	if feedback != nil && feedback.Respondents > 0 {
		check.Sample = feedback.Respondents
		check.Actual = floatPointer(float64(feedback.Helpful) / float64(feedback.Respondents))
	}
	if feedback == nil || feedback.Respondents < rule.MinSamples {
		check.Status = GateUnavailable
		check.Detail = fmt.Sprintf("need at least %d explicitly opted-in ratings", rule.MinSamples)
		return check
	}
	rate := *check.Actual
	check.Status = GatePassed
	check.Detail = "aggregate opt-in helpful rating meets the target"
	if rate < rule.Minimum {
		check.Status = GateRegression
		check.Detail = "aggregate opt-in helpful rating is below the target"
	}
	return check
}

func (r GateReport) HasMeasuredRegression() bool {
	for _, check := range r.Checks {
		if check.Enforced && check.Status == GateRegression {
			return true
		}
	}
	return false
}

func (r GateReport) HasInfrastructureFailure() bool {
	for _, check := range r.Checks {
		if check.Status == GateIncomparable || (check.Enforced && check.Status == GateUnavailable) {
			return true
		}
	}
	return false
}

func (r GateReport) HasAdvisoryRegression() bool {
	for _, check := range r.Checks {
		if !check.Enforced && check.Status == GateRegression {
			return true
		}
	}
	return false
}

func GateMarkdown(report GateReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Evaluation regression gates\n\nOutcome: **%s**\n\nCurrent `%s` · baseline `%s`\n\n", report.Outcome, report.CurrentFingerprint, report.BaselineFingerprint)
	b.WriteString("| Gate | Status | Enforced | Actual | Baseline | Threshold | Sample | Detail | Rationale |\n| --- | --- | --- | ---: | ---: | ---: | ---: | --- | --- |\n")
	for _, check := range report.Checks {
		fmt.Fprintf(&b, "| %s | %s | %t | %s | %s | %.3f %s | %d | %s | %s |\n", check.ID, check.Status, check.Enforced, gateNumber(check.Actual), gateNumber(check.Baseline), check.Threshold, check.Unit, check.Sample, check.Detail, check.Rationale)
	}
	b.WriteString("\nExit classification: enforced measured regressions are product failures; enforced unavailable or incomparable checks are infrastructure/configuration failures; advisory regressions do not fail CI.\n")
	return b.String()
}

func gateNumber(value *float64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.3f", *value)
}

func WriteGateReport(dir string, report GateReport) (jsonPath, markdownPath string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	jsonPath, markdownPath = filepath.Join(dir, "gate-report.json"), filepath.Join(dir, "gate-report.md")
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(jsonPath, append(data, '\n'), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(markdownPath, []byte(GateMarkdown(report)), 0o644); err != nil {
		return "", "", err
	}
	return jsonPath, markdownPath, nil
}

func floatPointer(value float64) *float64 { return &value }

func gatePairLabel(count int, singular string) string {
	if count == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %ss", count, singular)
}
