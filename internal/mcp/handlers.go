package mcp

// Handler is a stub for the MCP server functionality.
// For now it does not start an HTTP server. We'll add MCP
// protocol handling later.
type Handler struct{}

func (h *Handler) StartServer() any {
	panic("unimplemented")
}

// New creates a new MCP handler stub.
func New() *Handler {
	return &Handler{}
}

// Setup is a placeholder for MCP setup logic.
func (h *Handler) Setup() error {
	// no-op for now
	return nil
}
