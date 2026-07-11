package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"memento-mcp/evaluation"
)

type localRunFile struct {
	Version      int                         `json:"version"`
	Observations []evaluation.RunObservation `json:"observations"`
}

func main() {
	root := flag.String("root", ".", "repository root")
	fixtures := flag.String("fixtures", "evaluation/fixtures/helpfulness.json", "helpfulness fixture file")
	runs := flag.String("runs", "", "local paired run-observation JSON file (required)")
	tasks := flag.String("tasks", "", "comma-separated task IDs (default all fixture tasks)")
	out := flag.String("out", "", "directory for helpfulness-report.json and .md (required)")
	flag.Parse()
	if *runs == "" || *out == "" {
		fatal("-runs and -out are required")
	}
	rootAbs, err := filepath.Abs(*root)
	if err != nil {
		fatal(err.Error())
	}
	fixturePath := resolve(rootAbs, *fixtures)
	runsPath := resolve(rootAbs, *runs)
	fixtureSet, err := evaluation.LoadHelpfulnessFixtureFile(fixturePath)
	if err != nil {
		fatal(err.Error())
	}
	observationFile, err := loadRuns(runsPath)
	if err != nil {
		fatal(err.Error())
	}
	if observationFile.Version != evaluation.HelpfulnessReportVersion {
		fatal(fmt.Sprintf("unsupported run-observation version %d", observationFile.Version))
	}
	report, err := evaluation.RunHelpfulness(context.Background(), fixtureSet, evaluation.PairedRunConfig{
		Root: rootAbs, TaskIDs: splitTasks(*tasks), Executor: evaluation.RecordedExecutor{Observations: observationFile.Observations},
	})
	if err != nil {
		fatal(err.Error())
	}
	jsonPath, markdownPath, err := evaluation.WriteHelpfulnessReport(resolve(rootAbs, *out), report)
	if err != nil {
		fatal(err.Error())
	}
	fmt.Print(evaluation.HelpfulnessMarkdown(report))
	fmt.Printf("\nWrote %s and %s\n", jsonPath, markdownPath)
}

func loadRuns(path string) (localRunFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return localRunFile{}, err
	}
	defer f.Close()
	var runs localRunFile
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&runs); err != nil {
		return localRunFile{}, fmt.Errorf("decode run observations: %w", err)
	}
	return runs, nil
}

func resolve(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}
func splitTasks(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.Split(raw, ",")
}
func fatal(message string) { fmt.Fprintln(os.Stderr, "helpfulness evaluation:", message); os.Exit(1) }
