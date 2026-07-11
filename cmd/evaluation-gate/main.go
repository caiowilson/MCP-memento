package main

import (
	"flag"
	"fmt"
	"os"

	"memento-mcp/evaluation"
)

func main() {
	currentPath := flag.String("current", "", "current helpfulness report JSON")
	baselinePath := flag.String("baseline", "", "committed helpfulness baseline JSON")
	retrievalCurrentPath := flag.String("retrieval-current", "", "current retrieval summary JSON")
	retrievalBaselinePath := flag.String("retrieval-baseline", "", "committed retrieval baseline JSON")
	policyPath := flag.String("policy", "evaluation/fixtures/regression-gates.json", "gate policy JSON")
	supplementPath := flag.String("supplement", "", "optional aggregate-only feedback supplement")
	out := flag.String("out", "", "output directory")
	flag.Parse()
	if *currentPath == "" || *baselinePath == "" || *retrievalCurrentPath == "" || *retrievalBaselinePath == "" || *out == "" {
		infrastructureFatal("-current, -baseline, -retrieval-current, -retrieval-baseline, and -out are required")
	}
	current := mustHelpfulness(*currentPath)
	baseline := mustHelpfulness(*baselinePath)
	retrievalCurrent := mustRetrieval(*retrievalCurrentPath)
	retrievalBaseline := mustRetrieval(*retrievalBaselinePath)
	policy, err := evaluation.LoadGatePolicyFile(*policyPath)
	if err != nil {
		infrastructureFatal(err.Error())
	}
	supplement := evaluation.HelpfulnessVisualSupplement{Version: evaluation.HelpfulnessVisualVersion}
	if *supplementPath != "" {
		supplement, err = evaluation.LoadHelpfulnessVisualSupplementFile(*supplementPath)
		if err != nil {
			infrastructureFatal(err.Error())
		}
	}
	report, err := evaluation.EvaluateGates(evaluation.GateInputs{Current: current, Baseline: baseline, RetrievalCurrent: retrievalCurrent, RetrievalBaseline: retrievalBaseline, Supplement: supplement, Policy: policy})
	if err != nil {
		infrastructureFatal(err.Error())
	}
	jsonPath, markdownPath, err := evaluation.WriteGateReport(*out, report)
	if err != nil {
		infrastructureFatal(err.Error())
	}
	fmt.Print(evaluation.GateMarkdown(report))
	fmt.Printf("\nWrote %s and %s\n", jsonPath, markdownPath)
	if report.HasInfrastructureFailure() {
		os.Exit(2)
	}
	if report.HasMeasuredRegression() {
		os.Exit(1)
	}
}

func mustHelpfulness(path string) evaluation.HelpfulnessReport {
	report, err := evaluation.LoadHelpfulnessReportFile(path)
	if err != nil {
		infrastructureFatal(err.Error())
	}
	return report
}

func mustRetrieval(path string) evaluation.RetrievalSummary {
	report, err := evaluation.LoadRetrievalSummaryFile(path)
	if err != nil {
		infrastructureFatal(err.Error())
	}
	return report
}

func infrastructureFatal(message string) {
	fmt.Fprintln(os.Stderr, "evaluation gate infrastructure failure:", message)
	os.Exit(2)
}
