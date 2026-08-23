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

func TestDoctorSemanticCancelledProbeReportsTruthfully(t *testing.T) {
	cases := []struct {
		name     string
		mode     string
		cancel   func() (context.Context, context.CancelFunc)
		label    string
		failures int
		want     string
	}{
		{
			name: "auto deadline",
			mode: "auto",
			cancel: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 0)
			},
			label:    "[WARN]",
			failures: 0,
			want:     "timed out",
		},
		{
			name: "required deadline",
			mode: "true",
			cancel: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 0)
			},
			label:    "[FAIL]",
			failures: 1,
			want:     "timed out",
		},
		{
			name: "auto cancellation",
			mode: "auto",
			cancel: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			label:    "[WARN]",
			failures: 0,
			want:     "canceled",
		},
		{
			name: "required cancellation",
			mode: "true",
			cancel: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			label:    "[FAIL]",
			failures: 1,
			want:     "canceled",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MEMENTO_SEMANTIC_ENABLED", tc.mode)
			ctx, cancel := tc.cancel()
			defer cancel()
			var out bytes.Buffer
			failures := doctorSemantic(ctx, &out)
			if failures != tc.failures {
				t.Fatalf("failures = %d, want %d", failures, tc.failures)
			}
			text := out.String()
			if !strings.Contains(text, tc.label) || !strings.Contains(text, tc.want) {
				t.Fatalf("output = %q, want %q and %q", text, tc.label, tc.want)
			}
			if strings.Contains(text, "ollama pull") {
				t.Fatalf("output = %q, must not suggest installing a model", text)
			}
		})
	}
}
