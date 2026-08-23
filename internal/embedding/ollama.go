package embedding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const embeddingInputVersion = 1

// ErrOllamaModelMissing identifies Ollama's exact API response for a model
// that has not been pulled locally.
var ErrOllamaModelMissing = errors.New("ollama embedding model missing")

type OllamaConfig struct {
	BaseURL string
	Model   string
	Timeout time.Duration
	Client  *http.Client
}

type Ollama struct {
	endpoint    string
	model       string
	client      *http.Client
	fingerprint string
}

func NewOllama(cfg OllamaConfig) (*Ollama, error) {
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, fmt.Errorf("embedding model is required")
	}
	base, err := validateLoopbackURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	client := cfg.Client
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = DefaultTimeout
		}
		client = &http.Client{Timeout: timeout}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("ollama\x00%d\x00%s", embeddingInputVersion, model)))
	return &Ollama{
		endpoint:    strings.TrimRight(base.String(), "/") + "/api/embed",
		model:       model,
		client:      &clientCopy,
		fingerprint: hex.EncodeToString(sum[:16]),
	}, nil
}

func (o *Ollama) Embed(ctx context.Context, task Task, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	prepared := make([]string, len(inputs))
	for index, input := range inputs {
		prepared[index] = prepareInput(o.model, task, input)
	}
	body, err := json.Marshal(map[string]any{
		"model":    o.model,
		"input":    prepared,
		"truncate": true,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embedding request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		body := strings.TrimSpace(string(message))
		if resp.StatusCode == http.StatusNotFound && isOllamaModelMissingResponse(body, o.model) {
			return nil, fmt.Errorf("%w: ollama embedding request returned %s: %s", ErrOllamaModelMissing, resp.Status, body)
		}
		return nil, fmt.Errorf("ollama embedding request returned %s: %s", resp.Status, body)
	}
	var payload struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 256*1024*1024))
	if err := dec.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode ollama embeddings: %w", err)
	}
	if len(payload.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("ollama returned %d embeddings for %d inputs", len(payload.Embeddings), len(inputs))
	}
	if err := normalizeEmbeddings(payload.Embeddings); err != nil {
		return nil, err
	}
	return payload.Embeddings, nil
}

func (o *Ollama) Fingerprint() string { return o.fingerprint }

func (o *Ollama) Name() string { return "ollama/" + o.model }

func isOllamaModelMissingResponse(body, model string) bool {
	var response struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(body), &response) != nil {
		return false
	}
	for _, known := range []string{
		fmt.Sprintf("model '%s' not found", model),
		fmt.Sprintf("model %q not found", model),
		fmt.Sprintf("model %q not found, try pulling it first", model),
	} {
		if response.Error == known {
			return true
		}
	}
	return false
}

func validateLoopbackURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("parse MEMENTO_OLLAMA_URL: %w", err)
	}
	if parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("MEMENTO_OLLAMA_URL must be an unauthenticated loopback http URL")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, fmt.Errorf("MEMENTO_OLLAMA_URL must not include a path")
	}
	host := parsed.Hostname()
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("MEMENTO_OLLAMA_URL must resolve explicitly to localhost or a loopback IP")
		}
	}
	parsed.Path = ""
	return parsed, nil
}

func prepareInput(model string, task Task, input string) string {
	if strings.HasPrefix(strings.ToLower(model), "nomic-embed-text") {
		if task == TaskQuery {
			return "search_query: " + input
		}
		return "search_document: " + input
	}
	return input
}

func normalizeEmbeddings(vectors [][]float32) error {
	dimension := 0
	for index, vector := range vectors {
		if len(vector) == 0 {
			return fmt.Errorf("embedding %d is empty", index)
		}
		if dimension == 0 {
			dimension = len(vector)
		} else if len(vector) != dimension {
			return fmt.Errorf("embedding %d has dimension %d, expected %d", index, len(vector), dimension)
		}
		var normSquared float64
		for _, value := range vector {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return fmt.Errorf("embedding %d contains a non-finite value", index)
			}
			normSquared += float64(value) * float64(value)
		}
		if normSquared == 0 {
			return fmt.Errorf("embedding %d has zero magnitude", index)
		}
		inverseNorm := 1 / math.Sqrt(normSquared)
		for valueIndex := range vector {
			vector[valueIndex] = float32(float64(vector[valueIndex]) * inverseNorm)
		}
	}
	return nil
}
