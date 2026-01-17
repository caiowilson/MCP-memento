package mcp

import (
	"context"
	"os"
)

// Handler is kept for backward-compatibility with earlier scaffolding.
// Prefer using `Server` directly.
type Handler struct {
	Server *Server
}

func New() (*Handler, error) {
	srv, err := NewServer(Config{})
	if err != nil {
		return nil, err
	}
	return &Handler{Server: srv}, nil
}

func (h *Handler) StartServer(ctx context.Context) error {
	return h.Server.ServeStdio(ctx, os.Stdin, os.Stdout)
}
