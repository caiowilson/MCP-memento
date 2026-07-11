package app

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"memento-mcp/internal/feedback"
)

func setupFeedbackCLIStore(t *testing.T) *feedback.Store {
	t.Helper()
	t.Setenv("MEMENTO_FEEDBACK_ENABLED", "true")
	t.Setenv("MEMENTO_FEEDBACK_DIR", t.TempDir())
	store, err := feedback.NewStore(feedback.ConfigFromEnv())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(feedback.Event{
		ToolCategory: feedback.CategoryRepository, DurationBucket: feedback.DurationUnder100MS,
		ResultSizeBucket: feedback.ResultUnder4KB, FailureClass: feedback.FailureNone,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(feedback.Event{
		ToolCategory: feedback.CategoryRepository, DurationBucket: feedback.DurationUnavailable,
		ResultSizeBucket: feedback.ResultUnavailable, FailureClass: feedback.FailureUnavailable,
		Rating: feedback.RatingHelpful,
	}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestFeedbackCLIStatusAndExport(t *testing.T) {
	store := setupFeedbackCLIStore(t)
	var stdout, stderr bytes.Buffer
	handled, code := handleCLICommand([]string{"feedback", "status"}, &stdout, &stderr)
	if !handled || code != 0 || stderr.Len() != 0 {
		t.Fatalf("status handled=%v code=%d stderr=%q", handled, code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "feedback: enabled") || !strings.Contains(stdout.String(), store.Path()) || !strings.Contains(stdout.String(), "events: 2") || !strings.Contains(stdout.String(), "network: never") {
		t.Fatalf("unexpected status: %q", stdout.String())
	}

	stdout.Reset()
	_, code = handleCLICommand([]string{"feedback", "export"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("export failed: %s", stderr.String())
	}
	var report feedback.ExportReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Feedback.Helpful != 1 || len(report.Operations) != 1 {
		t.Fatalf("unexpected export: %#v", report)
	}

	stdout.Reset()
	_, code = handleCLICommand([]string{"feedback", "export", "--evaluation"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("evaluation export failed: %s", stderr.String())
	}
	if strings.Contains(stdout.String(), "operations") || !strings.Contains(stdout.String(), `"respondents": 1`) {
		t.Fatalf("unexpected evaluation export: %s", stdout.String())
	}
}

func TestFeedbackCLIDeleteRequiresConfirmation(t *testing.T) {
	store := setupFeedbackCLIStore(t)
	var stdout, stderr bytes.Buffer
	_, code := handleCLICommand([]string{"feedback", "delete"}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "requires --confirm") {
		t.Fatalf("delete code=%d stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(store.Path()); err != nil {
		t.Fatalf("unconfirmed delete changed data: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	_, code = handleCLICommand([]string{"feedback", "delete", "--confirm"}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("confirmed delete code=%d stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(store.Path()); !os.IsNotExist(err) {
		t.Fatalf("feedback file remains: %v", err)
	}
}

func TestFeedbackCLIWorksWhileDisabled(t *testing.T) {
	t.Setenv("MEMENTO_FEEDBACK_ENABLED", "false")
	t.Setenv("MEMENTO_FEEDBACK_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	_, code := handleCLICommand([]string{"feedback", "status"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "feedback: disabled") {
		t.Fatalf("disabled status code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
