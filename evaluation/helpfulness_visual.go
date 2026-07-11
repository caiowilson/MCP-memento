package evaluation

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HelpfulnessVisualVersion is the schema version for optional, aggregate-only
// visualization annotations. It never contains repository or user content.
const HelpfulnessVisualVersion = 1

type HelpfulnessVisualSupplement struct {
	Version   int                       `json:"version"`
	Retrieval []RetrievalVisualSnapshot `json:"retrieval,omitempty"`
	Trend     []TrendVisualSnapshot     `json:"trend,omitempty"`
	Feedback  *OptInFeedbackSummary     `json:"feedback,omitempty"`
}

type RetrievalVisualSnapshot struct {
	Mode                     string  `json:"mode"`
	ConfigurationFingerprint string  `json:"configurationFingerprint"`
	Precision                float64 `json:"precision"`
	Recall                   float64 `json:"recall"`
	MRR                      float64 `json:"mrr"`
	NDCG                     float64 `json:"ndcg"`
}

// TrendVisualSnapshot is one committed, aggregate benchmark baseline. The
// fingerprints annotate comparisons when an input or contract changes.
type TrendVisualSnapshot struct {
	Revision             string   `json:"revision"`
	FixtureFingerprint   string   `json:"fixtureFingerprint"`
	ModelFingerprint     string   `json:"modelFingerprint"`
	PromptFingerprint    string   `json:"promptFingerprint"`
	TokenizerFingerprint string   `json:"tokenizerFingerprint"`
	ScoringFingerprint   string   `json:"scoringFingerprint"`
	TaskSuccessRate      *float64 `json:"taskSuccessRate,omitempty"`
	TotalTokenSavings    *float64 `json:"totalTokenSavings,omitempty"`
}

// OptInFeedbackSummary permits only anonymous aggregate counts. Raw feedback
// events, text, identities, and timestamps have no place in this visual layer.
type OptInFeedbackSummary struct {
	Respondents int `json:"respondents"`
	Helpful     int `json:"helpful"`
	Neutral     int `json:"neutral"`
	Unhelpful   int `json:"unhelpful"`
}

func LoadHelpfulnessReportFile(path string) (HelpfulnessReport, error) {
	f, err := os.Open(path)
	if err != nil {
		return HelpfulnessReport{}, err
	}
	defer f.Close()
	var report HelpfulnessReport
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return HelpfulnessReport{}, fmt.Errorf("decode helpfulness report: %w", err)
	}
	if report.Version != HelpfulnessReportVersion {
		return HelpfulnessReport{}, fmt.Errorf("unsupported helpfulness report version %d", report.Version)
	}
	return report, nil
}

func LoadHelpfulnessVisualSupplementFile(path string) (HelpfulnessVisualSupplement, error) {
	f, err := os.Open(path)
	if err != nil {
		return HelpfulnessVisualSupplement{}, err
	}
	defer f.Close()
	var supplement HelpfulnessVisualSupplement
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&supplement); err != nil {
		return HelpfulnessVisualSupplement{}, fmt.Errorf("decode helpfulness visual supplement: %w", err)
	}
	if err := supplement.validate(); err != nil {
		return HelpfulnessVisualSupplement{}, err
	}
	return supplement, nil
}

func (s HelpfulnessVisualSupplement) validate() error {
	if s.Version != HelpfulnessVisualVersion {
		return fmt.Errorf("unsupported helpfulness visual version %d", s.Version)
	}
	for i, retrieval := range s.Retrieval {
		if retrieval.Mode != "lexical" && retrieval.Mode != "semantic" {
			return fmt.Errorf("retrieval[%d] mode must be lexical or semantic", i)
		}
		if strings.TrimSpace(retrieval.ConfigurationFingerprint) == "" {
			return fmt.Errorf("retrieval[%d] configuration fingerprint is required", i)
		}
		for _, value := range []float64{retrieval.Precision, retrieval.Recall, retrieval.MRR, retrieval.NDCG} {
			if value < 0 || value > 1 {
				return fmt.Errorf("retrieval[%d] metric must be between zero and one", i)
			}
		}
	}
	if s.Feedback != nil {
		if s.Feedback.Respondents < 0 || s.Feedback.Helpful < 0 || s.Feedback.Neutral < 0 || s.Feedback.Unhelpful < 0 || s.Feedback.Helpful+s.Feedback.Neutral+s.Feedback.Unhelpful != s.Feedback.Respondents {
			return fmt.Errorf("feedback counts must be non-negative and sum to respondents")
		}
	}
	return nil
}

type visualTaskRow struct {
	ID, Category                                                                string
	Baseline, Memento                                                           RunObservation
	InputBase, InputMemento, OutputBase, OutputMemento, TotalBase, TotalMemento *int64
}

type visualSummary struct {
	Rows                              []visualTaskRow
	BaselineSuccess, BaselineAttempts int
	MementoSuccess, MementoAttempts   int
	InvalidPairs                      int
	TimeoutPairs                      int
	InputTokenPairs                   int
	OutputTokenPairs                  int
	TotalTokenPairs                   int
	UnavailableTotalTokenPairs        int
	InputBaseline, InputMemento       int64
	OutputBaseline, OutputMemento     int64
	TotalBaseline, TotalMemento       int64
}

func buildVisualSummary(report HelpfulnessReport) visualSummary {
	summary := visualSummary{}
	for _, result := range report.Results {
		row := visualTaskRow{ID: result.TaskID, Category: result.Category, Baseline: result.Baseline, Memento: result.Memento, InputBase: result.Baseline.Metrics.Tokens.Input, InputMemento: result.Memento.Metrics.Tokens.Input, OutputBase: result.Baseline.Metrics.Tokens.Output, OutputMemento: result.Memento.Metrics.Tokens.Output, TotalBase: result.Baseline.Metrics.Tokens.Total, TotalMemento: result.Memento.Metrics.Tokens.Total}
		if row.Category == "" {
			row.Category = "unclassified"
		}
		summary.Rows = append(summary.Rows, row)
		if result.Baseline.Outcome == OutcomeInvalid || result.Memento.Outcome == OutcomeInvalid || result.Baseline.Outcome == OutcomeTimeout || result.Memento.Outcome == OutcomeTimeout {
			if result.Baseline.Outcome == OutcomeTimeout || result.Memento.Outcome == OutcomeTimeout {
				summary.TimeoutPairs++
			} else {
				summary.InvalidPairs++
			}
			continue
		}
		summary.BaselineAttempts++
		summary.MementoAttempts++
		if result.Baseline.Outcome == OutcomeSuccess {
			summary.BaselineSuccess++
		}
		if result.Memento.Outcome == OutcomeSuccess {
			summary.MementoSuccess++
		}
		if row.InputBase != nil && row.InputMemento != nil {
			summary.InputTokenPairs++
			summary.InputBaseline += *row.InputBase
			summary.InputMemento += *row.InputMemento
		}
		if row.OutputBase != nil && row.OutputMemento != nil {
			summary.OutputTokenPairs++
			summary.OutputBaseline += *row.OutputBase
			summary.OutputMemento += *row.OutputMemento
		}
		if row.TotalBase == nil || row.TotalMemento == nil {
			summary.UnavailableTotalTokenPairs++
		} else {
			summary.TotalTokenPairs++
			summary.TotalBaseline += *row.TotalBase
			summary.TotalMemento += *row.TotalMemento
		}
	}
	sort.SliceStable(summary.Rows, func(i, j int) bool {
		if summary.Rows[i].Category == summary.Rows[j].Category {
			return summary.Rows[i].ID < summary.Rows[j].ID
		}
		return summary.Rows[i].Category < summary.Rows[j].Category
	})
	return summary
}

func tokenSaving(baseline, memento int64) (int64, *float64) {
	saved := baseline - memento
	if baseline == 0 {
		return saved, nil
	}
	percent := float64(saved) * 100 / float64(baseline)
	return saved, &percent
}

type proportionInterval struct {
	Rate, Low, High float64
	N               int
}

func wilson(successes, total int) *proportionInterval {
	if total == 0 {
		return nil
	}
	p := float64(successes) / float64(total)
	z2 := 1.96 * 1.96
	denominator := 1 + z2/float64(total)
	center := (p + z2/(2*float64(total))) / denominator
	half := 1.96 * math.Sqrt((p*(1-p)+z2/(4*float64(total)))/float64(total)) / denominator
	return &proportionInterval{Rate: p, Low: center - half, High: center + half, N: total}
}

// HelpfulnessVisualMarkdown is the accessible table alternative to the HTML
// artifact. Values are calculated only from the versioned report and optional
// aggregate supplement; it does not contact an analytics service.
func HelpfulnessVisualMarkdown(report HelpfulnessReport, supplement HelpfulnessVisualSupplement) string {
	summary := buildVisualSummary(report)
	var b strings.Builder
	fmt.Fprintf(&b, "# Paired helpfulness visual report\n\nReport v%d · fixture `%s`\n\n", report.Version, report.FixtureFingerprint)
	baselineCI, mementoCI := wilson(summary.BaselineSuccess, summary.BaselineAttempts), wilson(summary.MementoSuccess, summary.MementoAttempts)
	b.WriteString("## Decisions\n\n| Decision | Result | Evidence |\n| --- | --- | --- |\n")
	fmt.Fprintf(&b, "| Task-success regression | %s | baseline %s; Memento %s; %d invalid pairs; %d timeout pairs |\n", taskSuccessDecision(baselineCI, mementoCI), formatCI(baselineCI), formatCI(mementoCI), summary.InvalidPairs, summary.TimeoutPairs)
	_, totalPercent := tokenSaving(summary.TotalBaseline, summary.TotalMemento)
	fmt.Fprintf(&b, "| Efficiency/token-cost regression | %s | %s; %s; %d unavailable total-token pairs |\n", efficiencyDecision(totalPercent), tokenAggregate(summary), pairedRunLabel(summary.TotalTokenPairs), summary.UnavailableTotalTokenPairs)
	fmt.Fprintf(&b, "| Retrieval-quality regression | %s | %s |\n", retrievalDecision(supplement.Retrieval), retrievalEvidence(supplement.Retrieval))
	b.WriteString("\n## Token usage\n\n| Task (category) | Input baseline → Memento | Output baseline → Memento | Total baseline → Memento | Total saved |\n| --- | ---: | ---: | ---: | ---: |\n")
	for _, row := range summary.Rows {
		fmt.Fprintf(&b, "| %s (%s) | %s | %s | %s | %s |\n", row.ID, row.Category, tokenPair(row.InputBase, row.InputMemento), tokenPair(row.OutputBase, row.OutputMemento), tokenPair(row.TotalBase, row.TotalMemento), tokenSaved(row.TotalBase, row.TotalMemento))
	}
	fmt.Fprintf(&b, "| Aggregate (input %d / output %d / total %d paired runs) | %d → %d | %d → %d | %d → %d | %s |\n", summary.InputTokenPairs, summary.OutputTokenPairs, summary.TotalTokenPairs, summary.InputBaseline, summary.InputMemento, summary.OutputBaseline, summary.OutputMemento, summary.TotalBaseline, summary.TotalMemento, tokenSaved(&summary.TotalBaseline, &summary.TotalMemento))
	fmt.Fprintf(&b, "\n%d task pairs have unavailable total-token usage and are excluded from total-token aggregates.\n", summary.UnavailableTotalTokenPairs)
	b.WriteString("\n## Usage provenance\n\n| Task | Condition | Usage source | Model fingerprint | Tokenizer fingerprint | Configuration fingerprint |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, row := range summary.Rows {
		for _, run := range []struct {
			condition   string
			observation RunObservation
		}{{"baseline", row.Baseline}, {"Memento", row.Memento}} {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", row.ID, run.condition, unavailableString(run.observation.Metrics.Tokens.UsageSource), unavailableString(run.observation.ModelFingerprint), unavailableString(run.observation.Metrics.Tokens.TokenizerFingerprint), unavailableString(run.observation.ConfigurationFingerprint))
		}
	}
	b.WriteString("\n## Task success\n\n| Condition | Success rate (95% Wilson interval) | Sample |\n| --- | ---: | ---: |\n")
	fmt.Fprintf(&b, "| Baseline | %s | %d |\n| Memento | %s | %d |\n", formatCI(baselineCI), summary.BaselineAttempts, formatCI(mementoCI), summary.MementoAttempts)
	b.WriteString("\n## Retrieval small multiples\n\n| Configuration | Mode | Precision@k | Recall@k | MRR | nDCG |\n| --- | --- | ---: | ---: | ---: | ---: |\n")
	if len(supplement.Retrieval) == 0 {
		b.WriteString("| unavailable | unavailable | unavailable | unavailable | unavailable | unavailable |\n")
	}
	for _, item := range supplement.Retrieval {
		fmt.Fprintf(&b, "| %s | %s | %.3f | %.3f | %.3f | %.3f |\n", item.ConfigurationFingerprint, item.Mode, item.Precision, item.Recall, item.MRR, item.NDCG)
	}
	b.WriteString("\n## Benchmark trend\n\n| Revision | Task success | Total tokens saved | Fixture / model / prompt / tokenizer / scoring | Change annotation |\n| --- | ---: | ---: | --- | --- |\n")
	if len(supplement.Trend) == 0 {
		b.WriteString("| unavailable | unavailable | unavailable | unavailable | unavailable |\n")
	}
	for i, point := range supplement.Trend {
		fmt.Fprintf(&b, "| %s | %s | %s | %s / %s / %s / %s / %s | %s |\n", point.Revision, percentPointer(point.TaskSuccessRate), numberPointer(point.TotalTokenSavings), point.FixtureFingerprint, point.ModelFingerprint, point.PromptFingerprint, point.TokenizerFingerprint, point.ScoringFingerprint, trendAnnotation(supplement.Trend, i))
	}
	b.WriteString("\n## Opt-in aggregate feedback\n\n| Respondents | Helpful | Neutral | Unhelpful |\n| ---: | ---: | ---: | ---: |\n")
	if supplement.Feedback == nil {
		b.WriteString("| unavailable | unavailable | unavailable | unavailable |\n")
	} else {
		fmt.Fprintf(&b, "| %d | %d | %d | %d |\n", supplement.Feedback.Respondents, supplement.Feedback.Helpful, supplement.Feedback.Neutral, supplement.Feedback.Unhelpful)
	}
	return b.String()
}

func taskSuccessDecision(baseline, memento *proportionInterval) string {
	if baseline == nil || memento == nil {
		return "unavailable"
	}
	return decision(baseline.Rate, memento.Rate, false)
}
func decision(baseline, memento float64, lowerIsBetter bool) string {
	if lowerIsBetter {
		if memento > baseline {
			return "regression"
		}
		return "no regression"
	}
	if memento < baseline {
		return "regression"
	}
	return "no regression"
}
func efficiencyDecision(percent *float64) string {
	if percent == nil {
		return "unavailable"
	}
	if *percent < 0 {
		return "regression"
	}
	return "no regression"
}
func formatCI(value *proportionInterval) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.1f%% (%.1f%%–%.1f%%)", value.Rate*100, value.Low*100, value.High*100)
}
func tokenPair(baseline, memento *int64) string {
	if baseline == nil || memento == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%d → %d", *baseline, *memento)
}
func tokenSaved(baseline, memento *int64) string {
	if baseline == nil || memento == nil {
		return "unavailable"
	}
	saved, percent := tokenSaving(*baseline, *memento)
	return fmt.Sprintf("%d (%s)", saved, percentValue(percent))
}
func percentValue(value *float64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.1f%%", *value)
}
func percentPointer(value *float64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.1f%%", *value*100)
}
func unavailableString(value string) string {
	if value == "" {
		return "unavailable"
	}
	return value
}
func numberPointer(value *float64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.0f", *value)
}
func tokenAggregate(summary visualSummary) string {
	if summary.TotalTokenPairs == 0 {
		return "unavailable"
	}
	return fmt.Sprintf("%d → %d total tokens", summary.TotalBaseline, summary.TotalMemento)
}
func pairedRunLabel(count int) string {
	if count == 1 {
		return "1 paired run"
	}
	return fmt.Sprintf("%d paired runs", count)
}
func retrievalDecision(items []RetrievalVisualSnapshot) string {
	if len(items) < 2 {
		return "unavailable"
	}
	lexical, semantic := retrievalMode(items, "lexical"), retrievalMode(items, "semantic")
	if lexical == nil || semantic == nil {
		return "unavailable"
	}
	if semantic.Precision < lexical.Precision || semantic.Recall < lexical.Recall || semantic.MRR < lexical.MRR || semantic.NDCG < lexical.NDCG {
		return "regression"
	}
	return "no regression"
}
func retrievalEvidence(items []RetrievalVisualSnapshot) string {
	if len(items) == 0 {
		return "no lexical/semantic comparison supplied"
	}
	return fmt.Sprintf("%d configuration snapshots", len(items))
}
func retrievalMode(items []RetrievalVisualSnapshot, mode string) *RetrievalVisualSnapshot {
	for i := range items {
		if items[i].Mode == mode {
			return &items[i]
		}
	}
	return nil
}

func trendAnnotation(points []TrendVisualSnapshot, index int) string {
	if index == 0 {
		return "baseline"
	}
	previous, current := points[index-1], points[index]
	changes := make([]string, 0, 5)
	for _, field := range []struct{ name, before, after string }{{"fixture", previous.FixtureFingerprint, current.FixtureFingerprint}, {"model", previous.ModelFingerprint, current.ModelFingerprint}, {"prompt", previous.PromptFingerprint, current.PromptFingerprint}, {"tokenizer", previous.TokenizerFingerprint, current.TokenizerFingerprint}, {"scoring", previous.ScoringFingerprint, current.ScoringFingerprint}} {
		if field.before != field.after {
			changes = append(changes, field.name+" changed")
		}
	}
	if len(changes) == 0 {
		return "inputs unchanged"
	}
	return strings.Join(changes, ", ")
}

// HelpfulnessVisualHTML renders the same decisions and an accessible table
// alternative, plus two paired dumbbell charts and retrieval/trend small
// multiples. The document is static and self-contained for local or CI use.
func HelpfulnessVisualHTML(report HelpfulnessReport, supplement HelpfulnessVisualSupplement) string {
	summary := buildVisualSummary(report)
	markdown := HelpfulnessVisualMarkdown(report, supplement)
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><title>Paired helpfulness visual report</title><style>body{font-family:system-ui,sans-serif;margin:2rem;max-width:1100px;color:#172033}table{border-collapse:collapse;width:100%;margin:.75rem 0 2rem}th,td{border:1px solid #c8d0dc;padding:.45rem;text-align:left}td:not(:first-child),th:not(:first-child){text-align:right}.decision{font-weight:600}.regression{color:#a61b1b}.ok{color:#176b3a}svg{max-width:100%;height:auto;border:1px solid #c8d0dc;margin:.5rem 0 1.5rem}.axis{stroke:#738093;stroke-width:1}.base{stroke:#738093;stroke-width:3}.memento{stroke:#176b3a;stroke-width:3}.label{font:12px system-ui,sans-serif;fill:#172033}.muted{color:#5e697a}pre{white-space:pre-wrap}</style></head><body><h1>Paired helpfulness visual report</h1>")
	fmt.Fprintf(&b, "<p class=\"muted\">Report v%d · fixture <code>%s</code></p>", report.Version, esc(report.FixtureFingerprint))
	b.WriteString("<h2>Decision summary</h2><table><thead><tr><th>Decision</th><th>Result</th><th>Evidence</th></tr></thead><tbody>")
	baselineCI, mementoCI := wilson(summary.BaselineSuccess, summary.BaselineAttempts), wilson(summary.MementoSuccess, summary.MementoAttempts)
	_, percent := tokenSaving(summary.TotalBaseline, summary.TotalMemento)
	decisionRow(&b, "Task-success regression", taskSuccessDecision(baselineCI, mementoCI), fmt.Sprintf("baseline %s; Memento %s; %d invalid pairs; %d timeout pairs", formatCI(baselineCI), formatCI(mementoCI), summary.InvalidPairs, summary.TimeoutPairs))
	decisionRow(&b, "Efficiency/token-cost regression", efficiencyDecision(percent), fmt.Sprintf("%s; %s; %d unavailable total-token pairs", tokenAggregate(summary), pairedRunLabel(summary.TotalTokenPairs), summary.UnavailableTotalTokenPairs))
	decisionRow(&b, "Retrieval-quality regression", retrievalDecision(supplement.Retrieval), retrievalEvidence(supplement.Retrieval))
	b.WriteString("</tbody></table><h2>Paired total-token use by task</h2>")
	b.WriteString(dumbbellSVG(summary.Rows, "tokens"))
	b.WriteString("<h2>Paired elapsed time by task</h2>")
	b.WriteString(dumbbellSVG(summary.Rows, "elapsed"))
	b.WriteString("<h2>Retrieval small multiples</h2>")
	b.WriteString(retrievalSVG(supplement.Retrieval))
	b.WriteString("<h2>Committed benchmark trend</h2>")
	b.WriteString(trendSVG(supplement.Trend))
	b.WriteString("<h2>Accessible data tables</h2><pre>")
	b.WriteString(esc(markdown))
	b.WriteString("</pre></body></html>")
	return b.String()
}

func decisionRow(b *strings.Builder, label, result, evidence string) {
	class := "ok"
	if result == "regression" {
		class = "regression"
	}
	fmt.Fprintf(b, "<tr><td>%s</td><td class=\"decision %s\">%s</td><td>%s</td></tr>", esc(label), class, esc(result), esc(evidence))
}
func esc(value string) string { return html.EscapeString(value) }

func dumbbellSVG(rows []visualTaskRow, metric string) string {
	type point struct {
		label             string
		baseline, memento float64
		available         bool
	}
	points := make([]point, 0, len(rows))
	max := 1.0
	for _, row := range rows {
		var baseline, memento float64
		available := true
		if metric == "tokens" {
			if row.TotalBase == nil || row.TotalMemento == nil {
				available = false
			} else {
				baseline, memento = float64(*row.TotalBase), float64(*row.TotalMemento)
			}
		} else {
			baseline, memento = float64(row.Baseline.Metrics.ElapsedTimeMs), float64(row.Memento.Metrics.ElapsedTimeMs)
		}
		if baseline > max {
			max = baseline
		}
		if memento > max {
			max = memento
		}
		points = append(points, point{row.Category + ": " + row.ID, baseline, memento, available})
	}
	height := 54 + len(points)*30
	width, left, right := 860, 250, 30
	var b strings.Builder
	fmt.Fprintf(&b, "<svg role=\"img\" aria-label=\"Paired %s dumbbell chart; gray is baseline and green is Memento\" viewBox=\"0 0 %d %d\"><title>Paired %s by task</title><line class=\"axis\" x1=\"%d\" y1=\"25\" x2=\"%d\" y2=\"25\"/><text class=\"label\" x=\"%d\" y=\"16\">0</text><text class=\"label\" x=\"%d\" y=\"16\" text-anchor=\"end\">%.0f</text>", metric, width, height, metric, left, width-right, left, width-right, max)
	for i, point := range points {
		y := 48 + i*30
		fmt.Fprintf(&b, "<text class=\"label\" x=\"%d\" y=\"%d\" text-anchor=\"end\">%s</text>", left-8, y+4, esc(point.label))
		if !point.available {
			fmt.Fprintf(&b, "<text class=\"label\" x=\"%d\" y=\"%d\">unavailable</text>", left+8, y+4)
			continue
		}
		scale := float64(width-left-right) / max
		bx, mx := float64(left)+point.baseline*scale, float64(left)+point.memento*scale
		fmt.Fprintf(&b, "<line class=\"axis\" x1=\"%.1f\" y1=\"%d\" x2=\"%.1f\" y2=\"%d\"/><circle class=\"base\" cx=\"%.1f\" cy=\"%d\" r=\"5\"/><circle class=\"memento\" cx=\"%.1f\" cy=\"%d\" r=\"5\"/><text class=\"label\" x=\"%d\" y=\"%d\">%.0f → %.0f</text>", bx, y, mx, y, bx, y, mx, y, width-right, y+4, point.baseline, point.memento)
	}
	b.WriteString("</svg>")
	return b.String()
}

func retrievalSVG(items []RetrievalVisualSnapshot) string {
	if len(items) == 0 {
		return "<p class=\"muted\">Retrieval data unavailable.</p>"
	}
	metrics := []struct {
		name  string
		value func(RetrievalVisualSnapshot) float64
	}{{"Precision@k", func(v RetrievalVisualSnapshot) float64 { return v.Precision }}, {"Recall@k", func(v RetrievalVisualSnapshot) float64 { return v.Recall }}, {"MRR", func(v RetrievalVisualSnapshot) float64 { return v.MRR }}, {"nDCG", func(v RetrievalVisualSnapshot) float64 { return v.NDCG }}}
	var b strings.Builder
	b.WriteString("<svg role=\"img\" aria-label=\"Retrieval metric small multiples\" viewBox=\"0 0 860 190\"><title>Retrieval metric small multiples</title>")
	for i, metric := range metrics {
		x := 12 + i*210
		fmt.Fprintf(&b, "<text class=\"label\" x=\"%d\" y=\"18\">%s</text>", x, metric.name)
		for j, item := range items {
			y := 45 + j*45
			width := int(metric.value(item) * 160)
			fmt.Fprintf(&b, "<text class=\"label\" x=\"%d\" y=\"%d\">%s</text><rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"16\" fill=\"%s\"/><text class=\"label\" x=\"%d\" y=\"%d\">%.3f</text>", x, y, esc(item.Mode), x, y+5, width, map[bool]string{true: "#176b3a", false: "#738093"}[item.Mode == "semantic"], x+width+4, y+17, metric.value(item))
		}
	}
	b.WriteString("</svg>")
	return b.String()
}

func trendSVG(points []TrendVisualSnapshot) string {
	if len(points) == 0 {
		return "<p class=\"muted\">Committed benchmark trend unavailable.</p>"
	}
	var b strings.Builder
	b.WriteString("<svg role=\"img\" aria-label=\"Committed benchmark task-success trend\" viewBox=\"0 0 860 180\"><title>Committed benchmark task-success trend</title><line class=\"axis\" x1=\"60\" y1=\"145\" x2=\"830\" y2=\"145\"/><line class=\"axis\" x1=\"60\" y1=\"20\" x2=\"60\" y2=\"145\"/>")
	for i, point := range points {
		x := 80 + i*visualMin(160, 700/visualMax(1, len(points)-1))
		if point.TaskSuccessRate == nil {
			continue
		}
		y := 145 - int(*point.TaskSuccessRate*110)
		fmt.Fprintf(&b, "<circle class=\"memento\" cx=\"%d\" cy=\"%d\" r=\"5\"/><text class=\"label\" x=\"%d\" y=\"165\" text-anchor=\"middle\">%s</text><text class=\"label\" x=\"%d\" y=\"%d\">%.0f%%</text>", x, y, x, esc(point.Revision), x+7, y-6, *point.TaskSuccessRate*100)
	}
	b.WriteString("</svg>")
	return b.String()
}

func visualMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func visualMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func WriteHelpfulnessVisual(dir string, report HelpfulnessReport, supplement HelpfulnessVisualSupplement) (htmlPath, markdownPath string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	htmlPath, markdownPath = filepath.Join(dir, "helpfulness-visual.html"), filepath.Join(dir, "helpfulness-visual.md")
	if err := os.WriteFile(htmlPath, []byte(HelpfulnessVisualHTML(report, supplement)), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(markdownPath, []byte(HelpfulnessVisualMarkdown(report, supplement)), 0o644); err != nil {
		return "", "", err
	}
	return htmlPath, markdownPath, nil
}
