package indexing

import (
	"context"
	"strings"
	"testing"

	"memento-mcp/internal/embedding"
)

func TestSemanticStatusAutoUnavailableIsNotAnError(t *testing.T) {
	root, store := backfillFixture(t)
	inner := &flakyEmbedder{release: make(chan struct{})} // always fails
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

func TestSemanticStatusRequiredUnavailableIsAnError(t *testing.T) {
	root, store := backfillFixture(t)
	inner := &flakyEmbedder{release: make(chan struct{})}
	runtime := embedding.NewRuntime(inner, embedding.ModeRequired)

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
	if status.Error == "" {
		t.Fatal("required mode must report an error when the runtime is unavailable")
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
