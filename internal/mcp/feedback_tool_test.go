package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"memento-mcp/internal/feedback"
)

func toolByName(tools []Tool, name string) *Tool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

func TestFeedbackToolIsAbsentWithoutExplicitOptIn(t *testing.T) {
	t.Setenv("MEMENTO_FEEDBACK_ENABLED", "false")
	server, err := NewServer(Config{Root: t.TempDir(), Child: true})
	if err != nil {
		t.Fatal(err)
	}
	if toolByName(server.tools, "feedback_submit") != nil {
		t.Fatal("feedback_submit must not be exposed by default")
	}
}

func TestEnabledFeedbackRecordsOnlyAggregateFields(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("MEMENTO_FEEDBACK_ENABLED", "true")
	t.Setenv("MEMENTO_FEEDBACK_DIR", dataDir)
	t.Setenv("MEMENTO_SEMANTIC_ENABLED", "false")
	secretPath := filepath.Join(root, "private-query-name.go")
	secretContent := "package private\nconst prompt = \"do not persist this source\"\n"
	if err := os.WriteFile(secretPath, []byte(secretContent), 0o600); err != nil {
		t.Fatal(err)
	}

	server, err := NewServer(Config{Root: root, Child: true})
	if err != nil {
		t.Fatal(err)
	}
	if toolByName(server.tools, "feedback_submit") == nil {
		t.Fatal("feedback_submit must be exposed after explicit opt-in")
	}
	if _, err := server.callTool(context.Background(), toolCallParams{
		Name: "repo_read_file", Arguments: json.RawMessage(`{"path":"private-query-name.go"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.callTool(context.Background(), toolCallParams{
		Name: "feedback_submit", Arguments: json.RawMessage(`{"rating":"helpful","toolCategory":"repository"}`),
	}); err != nil {
		t.Fatal(err)
	}

	store, err := feedback.NewStore(feedback.ConfigFromEnv())
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want one operational and one rating event", len(events))
	}
	if events[0].ToolCategory != feedback.CategoryRepository || events[0].Rating != "" {
		t.Fatalf("unexpected operational event: %#v", events[0])
	}
	if events[1].Rating != feedback.RatingHelpful {
		t.Fatalf("unexpected rating event: %#v", events[1])
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"private-query-name.go", "do not persist this source", root, secretPath} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("feedback data leaked %q: %s", leaked, raw)
		}
	}
}

func TestFeedbackSubmitRejectsFreeTextAndUnknownFields(t *testing.T) {
	store, err := feedback.NewStore(feedback.Config{Enabled: true, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	tool := newFeedbackSubmitTool(store)
	_, err = tool.Handler(context.Background(), json.RawMessage(`{"rating":"helpful","comment":"source path was useful"}`))
	if err == nil || !strings.Contains(err.Error(), "unknown feedback field") {
		t.Fatalf("expected unknown-field rejection, got %v", err)
	}
	if _, statErr := os.Stat(store.Path()); !os.IsNotExist(statErr) {
		t.Fatalf("rejected feedback was stored: %v", statErr)
	}
}

type failingFeedbackRecorder struct{}

func (failingFeedbackRecorder) Record(feedback.Event) error { return errors.New("disk unavailable") }

func TestFeedbackFailureDoesNotAffectMCPToolResult(t *testing.T) {
	enabled := true
	server, err := NewServer(Config{
		Root: t.TempDir(), Child: true,
		feedbackRecorder: failingFeedbackRecorder{}, feedbackEnabledOverride: &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := server.callTool(context.Background(), toolCallParams{Name: "repo_list_files", Arguments: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("feedback failure changed MCP error: %v", err)
	}
	if result.IsError {
		t.Fatalf("feedback failure changed MCP result: %#v", result)
	}
}

func TestBrokerFeedbackToolDoesNotAcceptRootOverride(t *testing.T) {
	t.Setenv("MEMENTO_FEEDBACK_ENABLED", "true")
	t.Setenv("MEMENTO_FEEDBACK_DIR", t.TempDir())
	server := newBrokerServerForTest(t, t.TempDir())
	tool := toolByName(server.tools, "feedback_submit")
	if tool == nil {
		t.Fatal("feedback_submit is missing")
	}
	properties, _ := tool.InputSchema["properties"].(map[string]any)
	if _, exists := properties["root"]; exists {
		t.Fatal("broker exposed a root field on privacy-constrained feedback tool")
	}
	_, err := server.callTool(context.Background(), toolCallParams{
		Name: "feedback_submit", Arguments: json.RawMessage(`{"rating":"helpful","root":"/private/repo"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "unknown feedback field") {
		t.Fatalf("expected root rejection, got %v", err)
	}
}
