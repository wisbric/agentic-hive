package server

import (
	"encoding/json"
	"log"
	"net/http"

	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/config"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/store"
)

type Server struct {
	cfg   *config.Config
	store *store.Store
	mux   *http.ServeMux
}

func New(cfg *config.Config, st *store.Store) *Server {
	s := &Server{
		cfg:   cfg,
		store: st,
		mux:   http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) ListenAndServe() error {
	log.Printf("listening on %s", s.cfg.Listen)
	return http.ListenAndServe(s.cfg.Listen, s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
