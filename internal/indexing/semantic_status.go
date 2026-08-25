package indexing

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"memento-mcp/internal/embedding"
)

// StoredVectorsPending reads the persisted index manifest without constructing
// an Indexer or mutating sidecars. Doctor uses this read-only seam to report
// warmup state for the workspace whose index the server uses.
func StoredVectorsPending(rootAbs string) (int, error) {
	rootAbs, err := filepath.Abs(rootAbs)
	if err != nil {
		return 0, err
	}
	dir, err := repoIndexDir(rootAbs)
	if err != nil {
		return 0, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var stored manifest
	if err := json.Unmarshal(b, &stored); err != nil {
		return 0, fmt.Errorf("decode semantic index manifest: %w", err)
	}
	if stored.Version != 1 {
		return 0, fmt.Errorf("unsupported semantic index manifest version %d", stored.Version)
	}
	return vectorsPendingFromManifest(stored), nil
}

func vectorsPendingFromManifest(stored manifest) int {
	pending := 0
	for _, entry := range stored.Files {
		if entry.Chunks > 0 && entry.Vectors != entry.Chunks {
			pending++
		}
	}
	return pending
}

// SemanticStatus reports how retrieval is currently operating. Every field is
// derived on read from the manifest and the runtime, so it cannot drift from
// what search actually does.
type SemanticStatus struct {
	Mode           string `json:"mode"`
	State          string `json:"state"`
	Provider       string `json:"provider,omitempty"`
	Model          string `json:"model,omitempty"`
	Available      bool   `json:"available"`
	Reason         string `json:"reason,omitempty"`
	VectorsPending int    `json:"vectorsPending"`
	Hint           string `json:"hint,omitempty"`
}

// semanticReporter is the part of embedding.Runtime the indexer needs. Taking
// an interface keeps indexing free of a hard dependency on the concrete type.
type semanticReporter interface {
	Mode() embedding.Mode
	Availability() embedding.Availability
	Name() string
}

// semanticStatus builds the status block, or nil when semantic retrieval is
// not configured at all.
func (i *Indexer) semanticStatus() *SemanticStatus {
	reporter, ok := i.embedder.(semanticReporter)
	if !ok || !reporter.Mode().Enabled() {
		return nil
	}
	availability := reporter.Availability()
	provider, model := splitEmbedderName(reporter.Name())

	status := &SemanticStatus{
		Mode:           string(reporter.Mode()),
		State:          "lexical",
		Provider:       provider,
		Model:          model,
		Available:      availability.Available,
		Reason:         availability.Reason,
		VectorsPending: i.pendingVectorCount(),
	}
	if availability.Available {
		status.State = "hybrid"
		return status
	}
	if status.Reason == "" {
		status.Reason = "embedding runtime has not been reached yet"
	}
	status.Hint = fmt.Sprintf(
		"Semantic retrieval is off (%s). Start a local Ollama and run 'ollama pull %s' to enable it.",
		status.Reason, model,
	)
	return status
}

// splitEmbedderName turns "ollama/nomic-embed-text:v1.5" into its provider and
// model halves.
func splitEmbedderName(name string) (provider, model string) {
	provider, model, found := strings.Cut(name, "/")
	if !found {
		return "", name
	}
	return provider, model
}
