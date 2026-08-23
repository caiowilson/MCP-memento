package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type ollamaRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f ollamaRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestOllamaEmbedBatchesAndNormalizes(t *testing.T) {
	var got struct {
		Model    string   `json:"model"`
		Input    []string `json:"input"`
		Truncate bool     `json:"truncate"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{3, 4}, {0, 2}}})
	}))
	defer server.Close()

	client, err := NewOllama(OllamaConfig{BaseURL: server.URL, Model: "nomic-embed-text:v1.5"})
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := client.Embed(context.Background(), TaskDocument, []string{"alpha", "beta"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "nomic-embed-text:v1.5" || !got.Truncate {
		t.Fatalf("unexpected request payload: %#v", got)
	}
	if len(got.Input) != 2 || got.Input[0] != "search_document: alpha" || got.Input[1] != "search_document: beta" {
		t.Fatalf("expected document prefixes, got %#v", got.Input)
	}
	if math.Abs(float64(vectors[0][0]-0.6)) > 1e-6 || math.Abs(float64(vectors[0][1]-0.8)) > 1e-6 {
		t.Fatalf("expected normalized vector, got %#v", vectors[0])
	}
}

func TestOllamaUsesQueryPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if len(request.Input) != 1 || request.Input[0] != "search_query: login flow" {
			t.Errorf("unexpected query input: %#v", request.Input)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1, 0}}})
	}))
	defer server.Close()
	client, err := NewOllama(OllamaConfig{BaseURL: server.URL, Model: "nomic-embed-text:v1.5"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Embed(context.Background(), TaskQuery, []string{"login flow"}); err != nil {
		t.Fatal(err)
	}
}

func TestOllamaRejectsNonLoopbackURL(t *testing.T) {
	for _, raw := range []string{"https://example.com", "http://192.0.2.10:11434", "http://127.0.0.1:11434/custom"} {
		if _, err := NewOllama(OllamaConfig{BaseURL: raw, Model: "model"}); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestOllamaReportsServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer server.Close()
	client, err := NewOllama(OllamaConfig{BaseURL: server.URL, Model: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Embed(context.Background(), TaskQuery, []string{"query"})
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("expected server error, got %v", err)
	}
}

func TestOllamaClassifiesExactModelMissingResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "model 'missing' not found"})
	}))
	defer server.Close()
	client, err := NewOllama(OllamaConfig{BaseURL: server.URL, Model: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Embed(context.Background(), TaskQuery, []string{"query"})
	if !errors.Is(err, ErrOllamaModelMissing) {
		t.Fatalf("error = %v, want ErrOllamaModelMissing", err)
	}
}

func TestOllamaTypesPreResponseTransportErrors(t *testing.T) {
	client, err := NewOllama(OllamaConfig{
		BaseURL: "http://127.0.0.1:11434",
		Model:   "test-model",
		Client: &http.Client{Transport: ollamaRoundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial failed")
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Embed(context.Background(), TaskQuery, []string{"query"})
	var transportErr *PreResponseTransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("error = %v, want PreResponseTransportError", err)
	}
}

func TestOllamaDoesNotFollowRedirects(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1, 0}}})
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	client, err := NewOllama(OllamaConfig{BaseURL: redirect.URL, Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Embed(context.Background(), TaskQuery, []string{"query"}); err == nil {
		t.Fatal("expected redirect response to be rejected")
	}
	if calls := targetCalls.Load(); calls != 0 {
		t.Fatalf("expected redirect target not to receive source content, got %d calls", calls)
	}
}

func TestFromEnvDisabledDoesNotValidateOllamaSettings(t *testing.T) {
	t.Setenv("MEMENTO_SEMANTIC_ENABLED", "false")
	t.Setenv("MEMENTO_OLLAMA_URL", "https://example.com")
	config, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.Mode != ModeOff || config.Embedder != nil {
		t.Fatalf("expected semantic mode disabled, got %#v", config)
	}
}

func TestFromEnvEnabledConfiguration(t *testing.T) {
	t.Setenv("MEMENTO_SEMANTIC_ENABLED", "true")
	t.Setenv("MEMENTO_OLLAMA_URL", "http://localhost:11434")
	t.Setenv("MEMENTO_EMBEDDING_MODEL", "all-minilm")
	t.Setenv("MEMENTO_HYBRID_SEMANTIC_WEIGHT", "0.75")
	t.Setenv("MEMENTO_EMBEDDING_BATCH_SIZE", "8")
	config, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.Mode != ModeRequired || config.Embedder == nil || config.Embedder.Name() != "ollama/all-minilm" || config.SemanticWeight != 0.75 || config.BatchSize != 8 {
		t.Fatalf("unexpected semantic config: %#v", config)
	}
}

func TestFromEnvWrapsEmbedderInRuntime(t *testing.T) {
	t.Setenv("MEMENTO_SEMANTIC_ENABLED", "auto")
	t.Setenv("MEMENTO_OLLAMA_URL", "http://127.0.0.1:11434")
	config, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.Mode != ModeAuto {
		t.Fatalf("mode = %q, want auto", config.Mode)
	}
	runtime, ok := config.Embedder.(*Runtime)
	if !ok {
		t.Fatalf("embedder type = %T, want *Runtime", config.Embedder)
	}
	if runtime.Mode() != ModeAuto {
		t.Fatalf("runtime mode = %q, want auto", runtime.Mode())
	}
	if runtime.Availability().Available {
		t.Fatal("availability must start false until something is attempted")
	}
}

func TestFromEnvRejectsNonFiniteSemanticWeight(t *testing.T) {
	t.Setenv("MEMENTO_SEMANTIC_ENABLED", "true")
	t.Setenv("MEMENTO_HYBRID_SEMANTIC_WEIGHT", "NaN")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected non-finite semantic weight to be rejected")
	}
}

func TestNormalizeEmbeddingsHandlesTinyFiniteVectors(t *testing.T) {
	vectors := [][]float32{{1e-40, 1e-40}}
	if err := normalizeEmbeddings(vectors); err != nil {
		t.Fatal(err)
	}
	for _, value := range vectors[0] {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			t.Fatalf("expected finite normalized vector, got %#v", vectors[0])
		}
	}
}
