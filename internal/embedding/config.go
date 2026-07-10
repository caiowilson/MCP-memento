package embedding

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultModel          = "nomic-embed-text:v1.5"
	DefaultOllamaURL      = "http://127.0.0.1:11434"
	DefaultSemanticWeight = 0.65
	DefaultBatchSize      = 32
	DefaultTimeout        = 30 * time.Second
)

func FromEnv() (RuntimeConfig, error) {
	enabled, err := envBool("MEMENTO_SEMANTIC_ENABLED", false)
	if err != nil {
		return RuntimeConfig{}, err
	}
	if !enabled {
		return RuntimeConfig{SemanticWeight: DefaultSemanticWeight, BatchSize: DefaultBatchSize}, nil
	}

	weight, err := envFloat("MEMENTO_HYBRID_SEMANTIC_WEIGHT", DefaultSemanticWeight)
	if err != nil {
		return RuntimeConfig{}, err
	}
	if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 || weight > 1 {
		return RuntimeConfig{}, fmt.Errorf("MEMENTO_HYBRID_SEMANTIC_WEIGHT must be greater than 0 and at most 1")
	}
	batchSize, err := envInt("MEMENTO_EMBEDDING_BATCH_SIZE", DefaultBatchSize)
	if err != nil {
		return RuntimeConfig{}, err
	}
	timeoutSeconds, err := envInt("MEMENTO_EMBEDDING_TIMEOUT_SECONDS", int(DefaultTimeout/time.Second))
	if err != nil {
		return RuntimeConfig{}, err
	}
	model := strings.TrimSpace(os.Getenv("MEMENTO_EMBEDDING_MODEL"))
	if model == "" {
		model = DefaultModel
	}
	baseURL := strings.TrimSpace(os.Getenv("MEMENTO_OLLAMA_URL"))
	if baseURL == "" {
		baseURL = DefaultOllamaURL
	}
	embedder, err := NewOllama(OllamaConfig{
		BaseURL: baseURL,
		Model:   model,
		Timeout: time.Duration(timeoutSeconds) * time.Second,
	})
	if err != nil {
		return RuntimeConfig{}, err
	}
	return RuntimeConfig{
		Enabled:        true,
		Embedder:       embedder,
		SemanticWeight: weight,
		BatchSize:      batchSize,
	}, nil
}

func envBool(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", name, err)
	}
	return value, nil
}

func envFloat(name string, fallback float64) (float64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return value, nil
}

func envInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("parse %s: expected a positive integer", name)
	}
	return value, nil
}
