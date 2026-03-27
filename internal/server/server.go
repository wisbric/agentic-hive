package server

import (
	"encoding/json"
	"log"
	"net/http"

	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/auth"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/config"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/store"
)

type Server struct {
	cfg       *config.Config
	store     *store.Store
	mux       *http.ServeMux
	localAuth *auth.LocalHandler
}

func New(cfg *config.Config, st *store.Store) *Server {
	s := &Server{
		cfg:       cfg,
		store:     st,
		mux:       http.NewServeMux(),
		localAuth: auth.NewLocalHandler(st, cfg.SessionSecret),
	}
	s.routes()
	return s
}

func SetOIDCHandler(s *Server, h *auth.OIDCHandler) {
	s.mux.HandleFunc("GET /api/auth/oidc/login", h.HandleLogin)
	s.mux.HandleFunc("GET /api/auth/oidc/callback", h.HandleCallback)
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) ListenAndServe() error {
	log.Printf("listening on %s", s.cfg.Listen)
	return http.ListenAndServe(s.cfg.Listen, s.mux)
}

func (s *Server) routes() {
	authMiddleware := auth.RequireAuth(s.cfg.SessionSecret)

	// Public routes
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)

	// Auth routes (no auth required)
	s.mux.HandleFunc("POST /api/auth/login", s.localAuth.HandleLogin)
	s.mux.HandleFunc("POST /api/auth/setup", s.localAuth.HandleSetup)
	s.mux.HandleFunc("GET /api/auth/setup/status", s.handleSetupStatus)
	s.mux.HandleFunc("POST /api/auth/logout", auth.HandleLogout)

	// Protected routes
	s.mux.Handle("GET /api/servers", authMiddleware(http.HandlerFunc(s.handleListServers)))
	s.mux.Handle("POST /api/servers", authMiddleware(http.HandlerFunc(s.handleCreateServer)))
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"needed": s.localAuth.SetupNeeded()})
}

// Placeholder — will be implemented in PRP-4
func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode([]any{})
}

// Placeholder — will be implemented in PRP-4
func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	http.Error(w, `{"error":"not implemented"}`, http.StatusNotImplemented)
}
