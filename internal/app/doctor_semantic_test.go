package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDoctorSemanticOffReportsSkip(t *testing.T) {
	t.Setenv("MEMENTO_SEMANTIC_ENABLED", "false")
	var out bytes.Buffer
	failures := doctorSemantic(context.Background(), &out)
	if failures != 0 {
		t.Fatalf("failures = %d, want 0", failures)
	}
	if !strings.Contains(out.String(), "semantic: disabled") {
		t.Fatalf("output = %q, want a disabled line", out.String())
	}
}

func TestDoctorSemanticAutoUnreachableWarnsWithoutFailing(t *testing.T) {
	t.Setenv("MEMENTO_SEMANTIC_ENABLED", "auto")
	// Port 1 is reserved and refuses connections.
	t.Setenv("MEMENTO_OLLAMA_URL", "http://127.0.0.1:1")
	var out bytes.Buffer
	failures := doctorSemantic(context.Background(), &out)
	if failures != 0 {
		t.Fatalf("auto mode failures = %d, want 0", failures)
	}
	text := out.String()
	if !strings.Contains(text, "[WARN]") {
		t.Fatalf("output = %q, want a WARN line", text)
	}
	if !strings.Contains(text, "ollama pull") {
		t.Fatalf("output = %q, want remediation guidance", text)
	}
}

func TestDoctorSemanticRequiredUnreachableFails(t *testing.T) {
	t.Setenv("MEMENTO_SEMANTIC_ENABLED", "true")
	t.Setenv("MEMENTO_OLLAMA_URL", "http://127.0.0.1:1")
	var out bytes.Buffer
	failures := doctorSemantic(context.Background(), &out)
	if failures != 1 {
		t.Fatalf("required mode failures = %d, want 1", failures)
	}
	if !strings.Contains(out.String(), "[FAIL]") {
		t.Fatalf("output = %q, want a FAIL line", out.String())
	}
}
