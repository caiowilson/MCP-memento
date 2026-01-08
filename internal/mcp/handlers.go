package handlers

import (
	"encoding/json"
	"log"
	db "memento-mcp/internal/indexing"
	"net/http"
)

type Handler struct {
	mux *http.ServeMux
	db  *db.DB
}

func New(database *db.DB) *Handler {
	return &Handler{
		mux: http.NewServeMux(),
		db:  database,
	}
}

func (h *Handler) SetupRoutes() {
	h.mux.HandleFunc("/health", h.HealthCheck)

	// Add more routes here (E.g.)
	// h.mux.HandleFunc("/hello", h.Hello)
}

func (h *Handler) StartServer() error {
	err := http.ListenAndServe(":8080", h.mux)
	if err != nil {
		return err
	}
	return nil
}

func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {

	log.Println("Health check passed")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode("OK")
}
