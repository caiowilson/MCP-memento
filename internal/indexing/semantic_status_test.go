package indexing

import (
	"context"
	"errors"
	"strings"
	"testing"

	"memento-mcp/internal/embedding"
)

type semanticStatusEmbedder struct {
	err error
}

func (e *semanticStatusEmbedder) Embed(_ context.Context, _ embedding.Task, _ []string) ([][]float32, error) {
	return nil, e.err
}

func (*semanticStatusEmbedder) Fingerprint() string { return "semantic-status-v1" }
func (*semanticStatusEmbedder) Name() string        { return "test/flaky" }

func semanticStatusForRuntimeError(t *testing.T, mode embedding.Mode, err error) Status {
	t.Helper()
	root, store := backfillFixture(t)
	runtime := embedding.NewRuntime(&semanticStatusEmbedder{err: err}, mode)

	idx, newErr := New(Config{RootAbs: root, StoreDir: store, Embedder: runtime})
	if newErr != nil {
		t.Fatal(newErr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idx.Start(ctx)
	if indexErr := idx.IndexAll(ctx); indexErr != nil {
		t.Fatal(indexErr)
	}
	return idx.Status()
}

func TestSemanticStatusAutoUnavailableIsNotAnError(t *testing.T) {
	status := semanticStatusForRuntimeError(t, embedding.ModeAuto, errors.New("connection refused"))
	if status.Error != "" {
		t.Fatalf("auto mode set status.Error = %q, want empty", status.Error)
	}
	if status.Semantic == nil {
		t.Fatal("expected a semantic block")
	}
	if status.Semantic.Mode != "auto" || status.Semantic.State != "lexical" {
		t.Fatalf("semantic = %+v, want mode auto state lexical", status.Semantic)
	}
	if status.Semantic.Available {
		t.Fatal("semantic.available must be false")
	}
	if status.Semantic.Reason == "" {
		t.Fatal("semantic.reason must explain the fallback")
	}
	if status.Semantic.VectorsPending != 1 {
		t.Fatalf("vectorsPending = %d, want 1", status.Semantic.VectorsPending)
	}
	if !strings.Contains(status.Semantic.Hint, "Semantic retrieval") {
		t.Fatalf("hint = %q, want an actionable sentence", status.Semantic.Hint)
	}
}

func TestSemanticStatusAutoModelMissingIsNotAnError(t *testing.T) {
	status := semanticStatusForRuntimeError(t, embedding.ModeAuto, errors.New("404 model not found"))
	if status.Error != "" {
		t.Fatalf("auto mode set status.Error = %q, want empty", status.Error)
	}
	if status.Semantic == nil || status.Semantic.State != "lexical" || status.Semantic.Available {
		t.Fatalf("semantic = %+v, want unavailable lexical fallback", status.Semantic)
	}
}

func TestSemanticStatusAutoProviderFailuresRemainErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "http 5xx", err: errors.New("embedding provider returned HTTP 503")},
		{name: "malformed response", err: errors.New("decode embedding response: invalid character '}'")},
	} {
		t.Run(test.name, func(t *testing.T) {
			status := semanticStatusForRuntimeError(t, embedding.ModeAuto, test.err)
			if status.Error == "" {
				t.Fatalf("auto mode hid provider failure %q", test.err)
			}
		})
	}
}

func TestSemanticStatusRequiredUnavailableIsAnError(t *testing.T) {
	status := semanticStatusForRuntimeError(t, embedding.ModeRequired, errors.New("connection refused"))
	if status.Error == "" {
		t.Fatal("required mode must report an error when the runtime is unavailable")
	}
	if !strings.Contains(status.Error, "Semantic retrieval") || !strings.Contains(status.Error, "ollama pull") {
		t.Fatalf("required error = %q, want actionable remediation", status.Error)
	}
	if status.Semantic.Mode != "required" {
		t.Fatalf("mode = %q, want required", status.Semantic.Mode)
	}
}

func TestSemanticStatusHybridWhenAvailable(t *testing.T) {
	root, store := backfillFixture(t)
	inner := &flakyEmbedder{release: make(chan struct{})}
	close(inner.release)
	runtime := embedding.NewRuntime(inner, embedding.ModeAuto)

	idx, err := New(Config{RootAbs: root, StoreDir: store, Embedder: runtime})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idx.Start(ctx)
	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}

	status := idx.Status()
	if status.Semantic.State != "hybrid" || !status.Semantic.Available {
		t.Fatalf("semantic = %+v, want hybrid and available", status.Semantic)
	}
	if status.Semantic.Hint != "" {
		t.Fatalf("hint = %q, want empty while hybrid", status.Semantic.Hint)
	}
	if status.Semantic.VectorsPending != 0 {
		t.Fatalf("vectorsPending = %d, want 0", status.Semantic.VectorsPending)
	}
}

func TestSemanticStatusAbsentWhenNoEmbedder(t *testing.T) {
	root, store := backfillFixture(t)
	idx, err := New(Config{RootAbs: root, StoreDir: store})
	if err != nil {
		t.Fatal(err)
	}
	if idx.Status().Semantic != nil {
		t.Fatal("no embedder configured must not produce a semantic block")
	}
}
