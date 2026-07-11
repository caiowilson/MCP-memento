package feedback

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"memento-mcp/evaluation"
)

func testConfig(t *testing.T, enabled bool) Config {
	t.Helper()
	return Config{
		Enabled: enabled,
		Dir:     t.TempDir(),
		Features: FeatureFlags{
			SemanticRetrieval: true,
			RedactionEnabled:  true,
		},
	}
}

func validEvent() Event {
	return Event{
		Version:          EventVersion,
		ToolCategory:     CategoryRepository,
		DurationBucket:   DurationUnder100MS,
		ResultSizeBucket: ResultUnder4KB,
		FailureClass:     FailureNone,
	}
}

func TestConfigFromEnvIsOptOutByDefault(t *testing.T) {
	t.Setenv("MEMENTO_FEEDBACK_ENABLED", "")
	if ConfigFromEnv().Enabled {
		t.Fatal("feedback must be disabled by default")
	}
	t.Setenv("MEMENTO_FEEDBACK_ENABLED", "true")
	if !ConfigFromEnv().Enabled {
		t.Fatal("explicit true must enable feedback")
	}
	t.Setenv("MEMENTO_FEEDBACK_ENABLED", "unexpected")
	if ConfigFromEnv().Enabled {
		t.Fatal("unrecognized values must not opt in")
	}
}

func TestDisabledStoreDoesNotCreateData(t *testing.T) {
	store, err := NewStore(testConfig(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(validEvent()); err != ErrDisabled {
		t.Fatalf("record error = %v, want ErrDisabled", err)
	}
	if _, err := os.Stat(store.Path()); !os.IsNotExist(err) {
		t.Fatalf("disabled feedback created data: %v", err)
	}
}

func TestEventSchemaHasOnlyApprovedFields(t *testing.T) {
	typ := reflect.TypeOf(Event{})
	want := map[string]bool{
		"version": true, "toolCategory": true, "durationBucket": true,
		"resultSizeBucket": true, "failureClass": true, "features": true, "rating": true,
	}
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if !want[name] {
			t.Fatalf("event schema contains unapproved field %q", name)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("event schema is missing fields: %#v", want)
	}

	data, err := json.Marshal(validEvent())
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"path", "query", "code", "note", "arguments", "rawResult", "identifier", "session", "repositoryID", "timestamp"} {
		if _, exists := object[forbidden]; exists {
			t.Fatalf("event serialization contains forbidden field %q: %s", forbidden, data)
		}
	}
}

func TestPublishedJSONSchemaMatchesEventFields(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "schemas", "feedback-event-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		AdditionalProperties bool                      `json:"additionalProperties"`
		Properties           map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.AdditionalProperties {
		t.Fatal("published feedback schema must reject additional properties")
	}
	typ := reflect.TypeOf(Event{})
	if len(schema.Properties) != typ.NumField() {
		t.Fatalf("schema properties=%d event fields=%d", len(schema.Properties), typ.NumField())
	}
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if _, ok := schema.Properties[name]; !ok {
			t.Fatalf("published schema is missing event field %q", name)
		}
	}
	features, ok := schema.Properties["features"]
	if !ok || features["additionalProperties"] != false {
		t.Fatal("published feature schema must reject additional properties")
	}
}

func TestStoreRecordReadExportAndDelete(t *testing.T) {
	store, err := NewStore(testConfig(t, true))
	if err != nil {
		t.Fatal(err)
	}
	operational := validEvent()
	if err := store.Record(operational); err != nil {
		t.Fatal(err)
	}
	rating := Event{
		ToolCategory: CategoryRepository, DurationBucket: DurationUnavailable,
		ResultSizeBucket: ResultUnavailable, FailureClass: FailureUnavailable, Rating: RatingHelpful,
	}
	if err := store.Record(rating); err != nil {
		t.Fatal(err)
	}

	events, err := store.ReadEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if !events[0].Features.SemanticRetrieval || !events[0].Features.RedactionEnabled {
		t.Fatalf("store did not apply feature flags: %#v", events[0].Features)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("event file mode = %o, want 600", info.Mode().Perm())
	}

	var out bytes.Buffer
	if err := store.WriteExport(&out); err != nil {
		t.Fatal(err)
	}
	var report ExportReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Feedback.Respondents != 1 || report.Feedback.Helpful != 1 || report.Feedback.HelpfulSessionRate == nil || *report.Feedback.HelpfulSessionRate != 1 {
		t.Fatalf("unexpected feedback summary: %#v", report.Feedback)
	}
	if len(report.Operations) != 1 || report.Operations[0].Count != 1 {
		t.Fatalf("unexpected operation counts: %#v", report.Operations)
	}

	if err := store.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Path()); !os.IsNotExist(err) {
		t.Fatalf("feedback data remains after delete: %v", err)
	}
	if err := store.Delete(); err != nil {
		t.Fatalf("repeated delete must be safe: %v", err)
	}
}

func TestReadRejectsUnknownAndInvalidFields(t *testing.T) {
	cfg := testConfig(t, true)
	store, err := NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(store.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	bad := `{"version":1,"toolCategory":"repository","durationBucket":"under_100ms","resultSizeBucket":"under_4kb","failureClass":"none","features":{"semanticRetrieval":false,"redactionEnabled":true},"path":"secret.go"}`
	if err := os.WriteFile(store.Path(), []byte(bad+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadEvents(); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown-field rejection, got %v", err)
	}
}

func TestReadRejectsTrailingJSON(t *testing.T) {
	store, err := NewStore(testConfig(t, true))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(store.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	good, err := json.Marshal(validEvent())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), append(good, []byte(` {"query":"hidden"}`+"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadEvents(); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("expected trailing-data rejection, got %v", err)
	}
}

func TestEvaluationSupplementMatchesVisualContract(t *testing.T) {
	store, err := NewStore(testConfig(t, true))
	if err != nil {
		t.Fatal(err)
	}
	for _, rating := range []Rating{RatingHelpful, RatingUnsure, RatingNotHelpful} {
		if err := store.Record(Event{ToolCategory: CategoryUnavailable, DurationBucket: DurationUnavailable, ResultSizeBucket: ResultUnavailable, FailureClass: FailureUnavailable, Rating: rating}); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := store.WriteEvaluationSupplement(&out); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Version  int `json:"version"`
		Feedback struct {
			Respondents int `json:"respondents"`
			Helpful     int `json:"helpful"`
			Neutral     int `json:"neutral"`
			Unhelpful   int `json:"unhelpful"`
		} `json:"feedback"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Version != 1 || decoded.Feedback.Respondents != 3 || decoded.Feedback.Helpful != 1 || decoded.Feedback.Neutral != 1 || decoded.Feedback.Unhelpful != 1 {
		t.Fatalf("unexpected evaluation supplement: %#v", decoded)
	}
	path := filepath.Join(t.TempDir(), "supplement.json")
	if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	supplement, err := evaluation.LoadHelpfulnessVisualSupplementFile(path)
	if err != nil {
		t.Fatalf("evaluation contract rejected feedback export: %v", err)
	}
	if supplement.Feedback == nil || supplement.Feedback.Respondents != 3 {
		t.Fatalf("unexpected loaded supplement: %#v", supplement)
	}
}
