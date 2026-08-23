package embedding

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type scriptedEmbedder struct {
	err    error
	calls  int
	inputs [][]string
}

func (e *scriptedEmbedder) Embed(_ context.Context, _ Task, inputs []string) ([][]float32, error) {
	e.calls++
	e.inputs = append(e.inputs, append([]string(nil), inputs...))
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

type blockingEmbedder struct {
	mu          sync.Mutex
	calls       int
	blockOnCall int
	err         error
	started     chan struct{}
	release     chan struct{}
}

func (e *blockingEmbedder) Embed(_ context.Context, _ Task, _ []string) ([][]float32, error) {
	e.mu.Lock()
	e.calls++
	call := e.calls
	e.mu.Unlock()
	if call == e.blockOnCall {
		e.started <- struct{}{}
		<-e.release
	}
	return nil, e.err
}

func (*blockingEmbedder) Fingerprint() string { return "blocking-v1" }
func (*blockingEmbedder) Name() string        { return "ollama/test-model" }

func (e *blockingEmbedder) Calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

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
		{ErrOllamaModelMissing, "is not available"},
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
	now := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
	rt.now = func() time.Time { return now }
	if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); err == nil {
		t.Fatal("expected failure")
	}

	inner.err = nil
	now = now.Add(time.Minute)

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
	if len(inner.inputs) != 1 || len(inner.inputs[0]) != 1 || inner.inputs[0][0] != probeSentinel {
		t.Fatalf("probe inputs = %#v, want sentinel %q", inner.inputs, probeSentinel)
	}
	if availability.CheckedAt.IsZero() {
		t.Fatal("probe must stamp CheckedAt")
	}
}

func TestRuntimeProbeRespectsBackoff(t *testing.T) {
	inner := &scriptedEmbedder{err: errors.New("connection refused")}
	rt := NewRuntime(inner, ModeAuto)
	rt.backoff = time.Minute
	if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); err == nil {
		t.Fatal("expected failure")
	}

	rt.Probe(context.Background())

	if inner.calls != 1 {
		t.Fatalf("probe made %d calls during backoff, want 1 total", inner.calls)
	}
}

func TestRuntimeAdmitsOnlyOneConcurrentRetry(t *testing.T) {
	inner := &blockingEmbedder{
		blockOnCall: 2,
		err:         errors.New("connection refused"),
		started:     make(chan struct{}, 1),
		release:     make(chan struct{}),
	}
	rt := NewRuntime(inner, ModeAuto)
	rt.backoff = time.Minute
	now := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
	rt.now = func() time.Time { return now }
	if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); err == nil {
		t.Fatal("expected initial failure")
	}
	now = now.Add(time.Minute)

	first := make(chan error, 1)
	go func() {
		_, err := rt.Embed(context.Background(), TaskDocument, []string{"x"})
		first <- err
	}()
	<-inner.started

	second := make(chan error, 1)
	go func() {
		_, err := rt.Embed(context.Background(), TaskDocument, []string{"x"})
		second <- err
	}()
	select {
	case err := <-second:
		if !errors.Is(err, ErrRuntimeUnavailable) {
			t.Fatalf("concurrent retry error = %v, want ErrRuntimeUnavailable", err)
		}
	case <-time.After(time.Second):
		close(inner.release)
		<-first
		<-second
		t.Fatal("concurrent retry was admitted")
	}
	close(inner.release)
	<-first
	if inner.Calls() != 2 {
		t.Fatalf("wrapped embedder called %d times, want initial failure plus one retry", inner.Calls())
	}
}

func TestRuntimeCancelledContextNeverCallsEmbedder(t *testing.T) {
	inner := &scriptedEmbedder{}
	rt := NewRuntime(inner, ModeAuto)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := rt.Embed(ctx, TaskDocument, []string{"x"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled embed error = %v, want context.Canceled", err)
	}
	if inner.calls != 0 {
		t.Fatalf("cancelled embed called the provider %d times", inner.calls)
	}
	if availability := rt.Availability(); availability.Available || !availability.CheckedAt.IsZero() {
		t.Fatalf("availability = %+v, want untouched state", availability)
	}
}

func TestRuntimeNameDelegatesToWrappedEmbedder(t *testing.T) {
	inner := &scriptedEmbedder{}
	rt := NewRuntime(inner, ModeAuto)
	if rt.Name() != inner.Name() {
		t.Fatalf("runtime name = %q, want %q", rt.Name(), inner.Name())
	}
}
