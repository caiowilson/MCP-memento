package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"memento-mcp/evaluation"
)

func main() {
	root := flag.String("root", ".", "repository root")
	reportPath := flag.String("report", "", "helpfulness-report.json (required)")
	supplementPath := flag.String("supplement", "", "optional aggregate-only visualization supplement JSON")
	out := flag.String("out", "", "output directory (required)")
	flag.Parse()
	if *reportPath == "" || *out == "" {
		fatal("-report and -out are required")
	}
	rootAbs, err := filepath.Abs(*root)
	if err != nil {
		fatal(err.Error())
	}
	report, err := evaluation.LoadHelpfulnessReportFile(resolve(rootAbs, *reportPath))
	if err != nil {
		fatal(err.Error())
	}
	supplement := evaluation.HelpfulnessVisualSupplement{Version: evaluation.HelpfulnessVisualVersion}
	if *supplementPath != "" {
		supplement, err = evaluation.LoadHelpfulnessVisualSupplementFile(resolve(rootAbs, *supplementPath))
		if err != nil {
			fatal(err.Error())
		}
	}
	htmlPath, markdownPath, err := evaluation.WriteHelpfulnessVisual(resolve(rootAbs, *out), report, supplement)
	if err != nil {
		fatal(err.Error())
	}
	fmt.Print(evaluation.HelpfulnessVisualMarkdown(report, supplement))
	fmt.Printf("\nWrote %s and %s\n", htmlPath, markdownPath)
}

func resolve(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}
func fatal(message string) {
	fmt.Fprintln(os.Stderr, "helpfulness visualization:", message)
	os.Exit(1)
}
