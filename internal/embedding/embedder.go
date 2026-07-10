package embedding

import "context"

type Task string

const (
	TaskDocument Task = "document"
	TaskQuery    Task = "query"
)

type Embedder interface {
	Embed(ctx context.Context, task Task, inputs []string) ([][]float32, error)
	Fingerprint() string
	Name() string
}

type RuntimeConfig struct {
	Enabled        bool
	Embedder       Embedder
	SemanticWeight float64
	BatchSize      int
}
