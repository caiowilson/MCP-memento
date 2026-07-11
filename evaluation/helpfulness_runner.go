package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// HelpfulnessReportVersion is the schema version for local paired reports.
const HelpfulnessReportVersion = 1

type RunCondition string

const (
	BaselineCondition RunCondition = "baseline"
	MementoCondition  RunCondition = "memento"
)

type RunOutcome string

const (
	OutcomeSuccess RunOutcome = "success"
	OutcomeFailure RunOutcome = "failure"
	OutcomeInvalid RunOutcome = "invalid"
)

// TokenUsage is intentionally nullable. Missing usage is unavailable, not an
// estimate derived from bytes, a prompt, or a different tokenizer.
type TokenUsage struct {
	Input  *int64 `json:"input,omitempty"`
	Output *int64 `json:"output,omitempty"`
	Total  *int64 `json:"total,omitempty"`
}

type ToolContextMetrics struct {
	ToolCalls    int64 `json:"toolCalls"`
	ContextReads int64 `json:"contextReads"`
	ContextBytes int64 `json:"contextBytes"`
}

type RunMetrics struct {
	ElapsedTimeMs int64              `json:"elapsedTimeMs"`
	Turns         int64              `json:"turns"`
	Tokens        TokenUsage         `json:"tokens"`
	ToolContext   ToolContextMetrics `json:"toolContext"`
}

// RunObservation is the minimum local record an agent adapter supplies. It
// deliberately has no prompt, response, source, path, query, note, or memory
// field, keeping reports safe to retain or share by default.
type RunObservation struct {
	TaskID                   string       `json:"taskID"`
	Condition                RunCondition `json:"condition"`
	Outcome                  RunOutcome   `json:"outcome"`
	MatchFingerprint         string       `json:"matchFingerprint"`
	ConfigurationFingerprint string       `json:"configurationFingerprint"`
	ModelFingerprint         string       `json:"modelFingerprint,omitempty"`
	Metrics                  RunMetrics   `json:"metrics"`
}

// ExecutionRequest is passed to an adapter but is never serialized into the
// report. Adapters can use the task prompt and memory seeds locally.
type ExecutionRequest struct {
	Task      HelpfulnessTask
	Condition RunCondition
	Root      string
}

// ExecutionResult keeps the local workspace private to the runner. A real
// adapter should provide an isolated but revision-matched workspace per
// condition; Workspace is used for local validation and is not reported.
type ExecutionResult struct {
	Observation RunObservation
	Workspace   string
}

// TaskExecutor is the integration point for a local agent client. The runner
// always invokes it once for baseline and once for Memento for every task.
type TaskExecutor interface {
	Execute(context.Context, ExecutionRequest) (ExecutionResult, error)
}

// RecordedExecutor is a deterministic adapter for local fixtures, CI, and
// clients that capture execution telemetry separately from the report.
type RecordedExecutor struct {
	Observations []RunObservation
}

func (e RecordedExecutor) Execute(_ context.Context, request ExecutionRequest) (ExecutionResult, error) {
	for _, observation := range e.Observations {
		if observation.TaskID == request.Task.ID && observation.Condition == request.Condition {
			return ExecutionResult{Observation: observation, Workspace: request.Root}, nil
		}
	}
	return ExecutionResult{}, fmt.Errorf("no recorded %s run for task %q", request.Condition, request.Task.ID)
}

type ValidatorStatus string

const (
	ValidatorPassed      ValidatorStatus = "passed"
	ValidatorFailed      ValidatorStatus = "failed"
	ValidatorUnavailable ValidatorStatus = "unavailable"
)

type ValidatorResult struct {
	Condition   RunCondition    `json:"condition"`
	Kind        string          `json:"kind"`
	Fingerprint string          `json:"fingerprint"`
	Status      ValidatorStatus `json:"status"`
	ExitCode    *int            `json:"exitCode,omitempty"`
}

// LocalValidator performs only local evidence and command validation. It
// drops command output and never makes a network request.
type LocalValidator struct{}

func (LocalValidator) Validate(ctx context.Context, root string, task HelpfulnessTask, condition RunCondition, check ValidationCheck) ValidatorResult {
	result := ValidatorResult{Condition: condition, Kind: check.Kind, Fingerprint: fingerprint(check)}
	switch check.Kind {
	case "evidence":
		for _, evidence := range task.Expected.Evidence {
			info, err := os.Stat(filepath.Join(root, filepath.FromSlash(evidence.Path)))
			if err != nil || info.IsDir() {
				result.Status = ValidatorFailed
				return result
			}
		}
		result.Status = ValidatorPassed
	case "command":
		cmd := exec.CommandContext(ctx, "sh", "-c", check.Command)
		cmd.Dir = root
		cmd.Stdout = nil
		cmd.Stderr = nil
		err := cmd.Run()
		code := 0
		if err != nil {
			code = 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				code = exitErr.ExitCode()
			}
			result.Status = ValidatorFailed
		} else {
			result.Status = ValidatorPassed
		}
		result.ExitCode = &code
	case "review":
		// A review has no trustworthy automatic verdict. It is explicitly
		// exported through BlindedReviewItem instead of being guessed here.
		result.Status = ValidatorUnavailable
	default:
		result.Status = ValidatorFailed
	}
	return result
}

// BlindedReviewItem is a local handoff shape for a later reviewer. It omits
// task IDs, conditions, prompts, paths, and memory. Response is supplied by a
// caller only when it has applied its own redaction policy; this runner never
// uploads or transports the item.
type BlindedReviewItem struct {
	Version  int    `json:"version"`
	ReviewID string `json:"reviewID"`
	Rubric   string `json:"rubric"`
	Response string `json:"response"`
}

func NewBlindedReviewItem(task HelpfulnessTask, check ValidationCheck, response string) BlindedReviewItem {
	return BlindedReviewItem{
		Version:  HelpfulnessReportVersion,
		ReviewID: fingerprint(struct{ Task, Validator string }{task.ID, fingerprint(check)}),
		Rubric:   check.Rule,
		Response: response,
	}
}

type PairedRunConfig struct {
	Root      string
	TaskIDs   []string
	Executor  TaskExecutor
	Validator LocalValidator
}

type MetricDelta struct {
	Pairs       int      `json:"pairs"`
	Delta       *float64 `json:"delta,omitempty"`
	CI95Low     *float64 `json:"ci95Low,omitempty"`
	CI95High    *float64 `json:"ci95High,omitempty"`
	Unavailable bool     `json:"unavailable"`
}

type TokenDeltas struct {
	Input  MetricDelta `json:"input"`
	Output MetricDelta `json:"output"`
	Total  MetricDelta `json:"total"`
}

type PairedDeltas struct {
	Success      MetricDelta `json:"success"`
	ElapsedTime  MetricDelta `json:"elapsedTimeMs"`
	Turns        MetricDelta `json:"turns"`
	Tokens       TokenDeltas `json:"tokens"`
	ToolCalls    MetricDelta `json:"toolCalls"`
	ContextReads MetricDelta `json:"contextReads"`
	ContextBytes MetricDelta `json:"contextBytes"`
}

type PairedTaskResult struct {
	TaskID           string            `json:"taskID"`
	TaskFingerprint  string            `json:"taskFingerprint"`
	MatchFingerprint string            `json:"matchFingerprint"`
	Baseline         RunObservation    `json:"baseline"`
	Memento          RunObservation    `json:"memento"`
	Validators       []ValidatorResult `json:"validators"`
	Deltas           PairedDeltas      `json:"deltas"`
}

type HelpfulnessSummary struct {
	SelectedTasks int          `json:"selectedTasks"`
	ValidPairs    int          `json:"validPairs"`
	InvalidPairs  int          `json:"invalidPairs"`
	Deltas        PairedDeltas `json:"deltas"`
}

// HelpfulnessReport contains only identifiers, fingerprints, telemetry, and
// validator statuses. It is safe-by-default for local report files.
type HelpfulnessReport struct {
	Version            int                `json:"version"`
	FixtureFingerprint string             `json:"fixtureFingerprint"`
	Results            []PairedTaskResult `json:"results"`
	Summary            HelpfulnessSummary `json:"summary"`
}

// RunHelpfulness executes selected fixtures in deterministic fixture order.
// A configuration mismatch creates explicit invalid observations rather than
// letting an unmatched pair influence aggregate deltas.
func RunHelpfulness(ctx context.Context, fixtures HelpfulnessFixtureSet, cfg PairedRunConfig) (HelpfulnessReport, error) {
	if cfg.Executor == nil {
		return HelpfulnessReport{}, fmt.Errorf("helpfulness runner executor is required")
	}
	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return HelpfulnessReport{}, err
	}
	selected, err := selectTasks(fixtures.Tasks, cfg.TaskIDs)
	if err != nil {
		return HelpfulnessReport{}, err
	}
	report := HelpfulnessReport{Version: HelpfulnessReportVersion, FixtureFingerprint: fingerprint(fixtures)}
	for _, task := range selected {
		baselineResult, err := cfg.Executor.Execute(ctx, ExecutionRequest{Task: task, Condition: BaselineCondition, Root: root})
		if err != nil {
			return HelpfulnessReport{}, fmt.Errorf("baseline task %q: %w", task.ID, err)
		}
		mementoResult, err := cfg.Executor.Execute(ctx, ExecutionRequest{Task: task, Condition: MementoCondition, Root: root})
		if err != nil {
			return HelpfulnessReport{}, fmt.Errorf("memento task %q: %w", task.ID, err)
		}
		baseline, memento := baselineResult.Observation, mementoResult.Observation
		if err := validateObservation(task.ID, BaselineCondition, baseline); err != nil {
			return HelpfulnessReport{}, err
		}
		if err := validateObservation(task.ID, MementoCondition, memento); err != nil {
			return HelpfulnessReport{}, err
		}
		if baseline.MatchFingerprint != memento.MatchFingerprint {
			baseline.Outcome = OutcomeInvalid
			memento.Outcome = OutcomeInvalid
		}
		result := PairedTaskResult{
			TaskID: task.ID, TaskFingerprint: fingerprint(task), MatchFingerprint: baseline.MatchFingerprint,
			Baseline: baseline, Memento: memento, Deltas: taskDeltas(baseline, memento),
		}
		for _, run := range []struct {
			condition RunCondition
			workspace string
		}{{BaselineCondition, baselineResult.Workspace}, {MementoCondition, mementoResult.Workspace}} {
			workspace := run.workspace
			if workspace == "" {
				workspace = root
			}
			for _, check := range task.Validators {
				result.Validators = append(result.Validators, cfg.Validator.Validate(ctx, workspace, task, run.condition, check))
			}
		}
		report.Results = append(report.Results, result)
	}
	report.Summary = summarize(report.Results)
	return report, nil
}

func selectTasks(tasks []HelpfulnessTask, ids []string) ([]HelpfulnessTask, error) {
	if len(ids) == 0 {
		return tasks, nil
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			wanted[id] = true
		}
	}
	selected := make([]HelpfulnessTask, 0, len(wanted))
	for _, task := range tasks {
		if wanted[task.ID] {
			selected = append(selected, task)
			delete(wanted, task.ID)
		}
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for id := range wanted {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("selected helpfulness tasks not found: %s", strings.Join(missing, ", "))
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no helpfulness tasks selected")
	}
	return selected, nil
}

func validateObservation(taskID string, condition RunCondition, got RunObservation) error {
	if got.TaskID != taskID || got.Condition != condition {
		return fmt.Errorf("%s task %q observation does not match requested task and condition", condition, taskID)
	}
	if got.Outcome != OutcomeSuccess && got.Outcome != OutcomeFailure && got.Outcome != OutcomeInvalid {
		return fmt.Errorf("%s task %q has unsupported outcome %q", condition, taskID, got.Outcome)
	}
	if strings.TrimSpace(got.MatchFingerprint) == "" || strings.TrimSpace(got.ConfigurationFingerprint) == "" {
		return fmt.Errorf("%s task %q requires match and configuration fingerprints", condition, taskID)
	}
	if got.Metrics.ElapsedTimeMs < 0 || got.Metrics.Turns < 0 || got.Metrics.ToolContext.ToolCalls < 0 || got.Metrics.ToolContext.ContextReads < 0 || got.Metrics.ToolContext.ContextBytes < 0 {
		return fmt.Errorf("%s task %q has negative telemetry", condition, taskID)
	}
	for _, tokens := range []*int64{got.Metrics.Tokens.Input, got.Metrics.Tokens.Output, got.Metrics.Tokens.Total} {
		if tokens != nil && *tokens < 0 {
			return fmt.Errorf("%s task %q has negative token usage", condition, taskID)
		}
	}
	return nil
}

func taskDeltas(baseline, memento RunObservation) PairedDeltas {
	if baseline.Outcome == OutcomeInvalid || memento.Outcome == OutcomeInvalid {
		return unavailableDeltas()
	}
	return PairedDeltas{
		Success:      scalarDelta(successValue(baseline.Outcome), successValue(memento.Outcome)),
		ElapsedTime:  scalarDelta(float64(baseline.Metrics.ElapsedTimeMs), float64(memento.Metrics.ElapsedTimeMs)),
		Turns:        scalarDelta(float64(baseline.Metrics.Turns), float64(memento.Metrics.Turns)),
		Tokens:       TokenDeltas{Input: tokenDelta(baseline.Metrics.Tokens.Input, memento.Metrics.Tokens.Input), Output: tokenDelta(baseline.Metrics.Tokens.Output, memento.Metrics.Tokens.Output), Total: tokenDelta(baseline.Metrics.Tokens.Total, memento.Metrics.Tokens.Total)},
		ToolCalls:    scalarDelta(float64(baseline.Metrics.ToolContext.ToolCalls), float64(memento.Metrics.ToolContext.ToolCalls)),
		ContextReads: scalarDelta(float64(baseline.Metrics.ToolContext.ContextReads), float64(memento.Metrics.ToolContext.ContextReads)),
		ContextBytes: scalarDelta(float64(baseline.Metrics.ToolContext.ContextBytes), float64(memento.Metrics.ToolContext.ContextBytes)),
	}
}

func successValue(outcome RunOutcome) float64 {
	if outcome == OutcomeSuccess {
		return 1
	}
	return 0
}
func scalarDelta(baseline, memento float64) MetricDelta {
	delta := memento - baseline
	return MetricDelta{Pairs: 1, Delta: &delta}
}
func tokenDelta(baseline, memento *int64) MetricDelta {
	if baseline == nil || memento == nil {
		return MetricDelta{Unavailable: true}
	}
	return scalarDelta(float64(*baseline), float64(*memento))
}
func unavailableDeltas() PairedDeltas {
	unavailable := MetricDelta{Unavailable: true}
	return PairedDeltas{Success: unavailable, ElapsedTime: unavailable, Turns: unavailable, Tokens: TokenDeltas{Input: unavailable, Output: unavailable, Total: unavailable}, ToolCalls: unavailable, ContextReads: unavailable, ContextBytes: unavailable}
}

func summarize(results []PairedTaskResult) HelpfulnessSummary {
	summary := HelpfulnessSummary{SelectedTasks: len(results)}
	values := map[string][]float64{}
	for _, result := range results {
		if result.Deltas.Success.Unavailable {
			summary.InvalidPairs++
			continue
		}
		summary.ValidPairs++
		collect := func(name string, metric MetricDelta) {
			if metric.Delta != nil {
				values[name] = append(values[name], *metric.Delta)
			}
		}
		collect("success", result.Deltas.Success)
		collect("elapsed", result.Deltas.ElapsedTime)
		collect("turns", result.Deltas.Turns)
		collect("input", result.Deltas.Tokens.Input)
		collect("output", result.Deltas.Tokens.Output)
		collect("total", result.Deltas.Tokens.Total)
		collect("toolCalls", result.Deltas.ToolCalls)
		collect("contextReads", result.Deltas.ContextReads)
		collect("contextBytes", result.Deltas.ContextBytes)
	}
	summary.Deltas = PairedDeltas{
		Success: aggregate(values["success"]), ElapsedTime: aggregate(values["elapsed"]), Turns: aggregate(values["turns"]),
		Tokens:    TokenDeltas{Input: aggregate(values["input"]), Output: aggregate(values["output"]), Total: aggregate(values["total"])},
		ToolCalls: aggregate(values["toolCalls"]), ContextReads: aggregate(values["contextReads"]), ContextBytes: aggregate(values["contextBytes"]),
	}
	return summary
}

func aggregate(values []float64) MetricDelta {
	if len(values) == 0 {
		return MetricDelta{Unavailable: true}
	}
	mean := 0.0
	for _, value := range values {
		mean += value
	}
	mean /= float64(len(values))
	result := MetricDelta{Pairs: len(values), Delta: &mean}
	if len(values) >= 5 {
		variance := 0.0
		for _, value := range values {
			variance += math.Pow(value-mean, 2)
		}
		standardError := math.Sqrt(variance / float64(len(values)-1) / float64(len(values)))
		low, high := mean-1.96*standardError, mean+1.96*standardError
		result.CI95Low, result.CI95High = &low, &high
	}
	return result
}

func fingerprint(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("fingerprint value: %v", err))
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// WriteHelpfulnessReport writes deterministic, non-sensitive JSON and a
// concise paired Markdown summary to the requested directory.
func WriteHelpfulnessReport(dir string, report HelpfulnessReport) (jsonPath, markdownPath string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	jsonData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", "", err
	}
	jsonPath = filepath.Join(dir, "helpfulness-report.json")
	markdownPath = filepath.Join(dir, "helpfulness-report.md")
	if err := os.WriteFile(jsonPath, append(jsonData, '\n'), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(markdownPath, []byte(HelpfulnessMarkdown(report)), 0o644); err != nil {
		return "", "", err
	}
	return jsonPath, markdownPath, nil
}

func HelpfulnessMarkdown(report HelpfulnessReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Paired helpfulness evaluation\n\n%d selected tasks; %d valid pairs; %d invalid pairs.\n\n", report.Summary.SelectedTasks, report.Summary.ValidPairs, report.Summary.InvalidPairs)
	b.WriteString("| Metric | Paired delta (Memento − baseline) |\n| --- | ---: |\n")
	for _, row := range []struct {
		name   string
		metric MetricDelta
	}{
		{"Task success", report.Summary.Deltas.Success}, {"Elapsed time (ms)", report.Summary.Deltas.ElapsedTime}, {"Turns", report.Summary.Deltas.Turns},
		{"Input tokens", report.Summary.Deltas.Tokens.Input}, {"Output tokens", report.Summary.Deltas.Tokens.Output}, {"Total tokens", report.Summary.Deltas.Tokens.Total},
		{"Tool calls", report.Summary.Deltas.ToolCalls}, {"Context reads", report.Summary.Deltas.ContextReads}, {"Context bytes", report.Summary.Deltas.ContextBytes},
	} {
		fmt.Fprintf(&b, "| %s | %s |\n", row.name, formatMetric(row.metric))
	}
	b.WriteString("\n## Task pairs\n\n| Task | Success | Elapsed (ms) | Turns | Input tokens | Output tokens | Total tokens |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, result := range report.Results {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n", result.TaskID, formatMetric(result.Deltas.Success), formatMetric(result.Deltas.ElapsedTime), formatMetric(result.Deltas.Turns), formatMetric(result.Deltas.Tokens.Input), formatMetric(result.Deltas.Tokens.Output), formatMetric(result.Deltas.Tokens.Total))
	}
	b.WriteString("\nToken fields marked `unavailable` were not supplied by the local client and are excluded from their aggregate.\n")
	return b.String()
}

func formatMetric(metric MetricDelta) string {
	if metric.Unavailable || metric.Delta == nil {
		return "unavailable"
	}
	value := fmt.Sprintf("%+.2f (%d pairs)", *metric.Delta, metric.Pairs)
	if metric.CI95Low != nil && metric.CI95High != nil {
		value += fmt.Sprintf("; 95%% CI %+.2f to %+.2f", *metric.CI95Low, *metric.CI95High)
	}
	return value
}
