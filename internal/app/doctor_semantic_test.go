package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"memento-mcp/internal/embedding"
	"memento-mcp/internal/indexing"
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

func TestDoctorSemanticSuccessfulProbeWinsOverLateCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out bytes.Buffer
	failures := doctorSemanticProbe(ctx, &out, embedding.ModeRequired, "ollama/test-model", 0, func(context.Context) embedding.Availability {
		cancel()
		return embedding.Availability{Available: true}
	})
	if failures != 0 {
		t.Fatalf("failures = %d, want 0", failures)
	}
	text := out.String()
	if !strings.Contains(text, "[PASS] semantic: ollama/test-model reachable") {
		t.Fatalf("output = %q, want successful probe output", text)
	}
	if strings.Contains(text, "timed out") || strings.Contains(text, "canceled") {
		t.Fatalf("output = %q, must not report late cancellation", text)
	}
}

type doctorPendingEmbedder struct{}

func (*doctorPendingEmbedder) Embed(context.Context, embedding.Task, []string) ([][]float32, error) {
	return nil, errors.New("embedding unavailable")
}

func (*doctorPendingEmbedder) Fingerprint() string { return "doctor-pending-v1" }
func (*doctorPendingEmbedder) Name() string        { return "test/doctor-pending" }

func TestDoctorSemanticReachableWithPendingVectorsWarns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "alpha.go"), []byte("package alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := indexing.New(indexing.Config{RootAbs: root, Embedder: &doctorPendingEmbedder{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idx.Start(ctx)
	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1, 0}}})
	}))
	defer server.Close()
	t.Setenv("MEMENTO_SEMANTIC_ENABLED", "auto")
	t.Setenv("MEMENTO_OLLAMA_URL", server.URL)
	t.Setenv("MEMENTO_EMBEDDING_MODEL", "test-model")

	var out bytes.Buffer
	if failures := doctorSemanticAtRoot(ctx, &out, root); failures != 0 {
		t.Fatalf("failures = %d, output = %q", failures, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "[WARN]") || !strings.Contains(text, "vectorsPending=1") || !strings.Contains(text, "warming") {
		t.Fatalf("output = %q, want reachable warmup diagnostic", text)
	}
	if strings.Contains(text, "[PASS] semantic") {
		t.Fatalf("output = %q, pending vectors must not report fully ready", text)
	}
}
