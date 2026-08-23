package embedding

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type scriptedEmbedder struct {
	err   error
	calls int
}

func (e *scriptedEmbedder) Embed(_ context.Context, _ Task, inputs []string) ([][]float32, error) {
	e.calls++
	if e.err != nil {
		return nil, e.err
	}
	out := make([][]float32, len(inputs))
	for index := range out {
		out[index] = []float32{1}
	}
	return out, nil
}

func (*scriptedEmbedder) Fingerprint() string { return "scripted-v1" }
func (*scriptedEmbedder) Name() string        { return "ollama/test-model" }

func TestRuntimeFingerprintIgnoresAvailability(t *testing.T) {
	inner := &scriptedEmbedder{err: errors.New("connection refused")}
	rt := NewRuntime(inner, ModeAuto)
	before := rt.Fingerprint()
	if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); err == nil {
		t.Fatal("expected embed to fail")
	}
	if rt.Availability().Available {
		t.Fatal("runtime should be unavailable after a failure")
	}
	if rt.Fingerprint() != before {
		t.Fatal("fingerprint changed with availability")
	}
	if rt.Fingerprint() != inner.Fingerprint() {
		t.Fatal("fingerprint must delegate to the wrapped embedder")
	}
}

func TestRuntimeClassifiesFailureReasons(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errors.New("ollama embedding request: dial tcp 127.0.0.1:11434: connect: connection refused"), "no embedding runtime detected"},
		{errors.New(`ollama embedding request returned 404 Not Found: {"error":"model not found"}`), "is not available"},
		{context.DeadlineExceeded, "did not respond"},
	}
	for _, tc := range cases {
		rt := NewRuntime(&scriptedEmbedder{err: tc.err}, ModeAuto)
		if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); err == nil {
			t.Fatalf("expected failure for %v", tc.err)
		}
		reason := rt.Availability().Reason
		if !strings.Contains(reason, tc.want) {
			t.Fatalf("reason %q does not contain %q", reason, tc.want)
		}
	}
}

func TestRuntimeBackoffSuppressesRepeatedCalls(t *testing.T) {
	inner := &scriptedEmbedder{err: errors.New("connection refused")}
	rt := NewRuntime(inner, ModeAuto)
	rt.backoff = time.Minute

	for attempt := 0; attempt < 3; attempt++ {
		if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); err == nil {
			t.Fatal("expected failure")
		}
	}
	if inner.calls != 1 {
		t.Fatalf("wrapped embedder called %d times, want 1 while in backoff", inner.calls)
	}
	if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("suppressed call error = %v, want ErrRuntimeUnavailable", err)
	}
}

func TestRuntimeRecoversAfterBackoffWindow(t *testing.T) {
	inner := &scriptedEmbedder{err: errors.New("connection refused")}
	rt := NewRuntime(inner, ModeAuto)
	rt.backoff = time.Minute
	if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); err == nil {
		t.Fatal("expected failure")
	}

	inner.err = nil
	rt.expireBackoffForTest()

	if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); err != nil {
		t.Fatalf("expected recovery, got %v", err)
	}
	availability := rt.Availability()
	if !availability.Available || availability.Reason != "" {
		t.Fatalf("availability = %+v, want available with no reason", availability)
	}
}

func TestRuntimeOffModeNeverCalls(t *testing.T) {
	inner := &scriptedEmbedder{}
	rt := NewRuntime(inner, ModeOff)
	if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("off mode error = %v, want ErrRuntimeUnavailable", err)
	}
	if inner.calls != 0 {
		t.Fatalf("off mode called the embedder %d times", inner.calls)
	}
}

func TestRuntimeProbeUsesSentinel(t *testing.T) {
	inner := &scriptedEmbedder{}
	rt := NewRuntime(inner, ModeAuto)
	availability := rt.Probe(context.Background())
	if !availability.Available {
		t.Fatalf("probe availability = %+v, want available", availability)
	}
	if inner.calls != 1 {
		t.Fatalf("probe made %d calls, want 1", inner.calls)
	}
	if availability.CheckedAt.IsZero() {
		t.Fatal("probe must stamp CheckedAt")
	}
}
