package server

import (
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"

	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/auth"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/config"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/keystore"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/session"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/sshpool"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/store"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/terminal"
)

type Server struct {
	cfg       *config.Config
	store     *store.Store
	mux       *http.ServeMux
	localAuth *auth.LocalHandler
	pool      *sshpool.Pool
	keyStore  keystore.KeyStore
	sessions  *session.Manager
	terminal  *terminal.Bridge
	staticFS  fs.FS
}

func New(cfg *config.Config, st *store.Store, pool *sshpool.Pool, ks keystore.KeyStore, sm *session.Manager, staticFS fs.FS) *Server {
	s := &Server{
		cfg:       cfg,
		store:     st,
		mux:       http.NewServeMux(),
		localAuth: auth.NewLocalHandler(st, cfg.SessionSecret),
		pool:      pool,
		keyStore:  ks,
		sessions:  sm,
		terminal:  terminal.NewBridge(pool),
		staticFS:  staticFS,
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
	am := auth.RequireAuth(s.cfg.SessionSecret)

	// Public
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)

	// Auth (no auth required)
	s.mux.HandleFunc("POST /api/auth/login", s.localAuth.HandleLogin)
	s.mux.HandleFunc("POST /api/auth/setup", s.localAuth.HandleSetup)
	s.mux.HandleFunc("GET /api/auth/setup/status", s.handleSetupStatus)
	s.mux.HandleFunc("POST /api/auth/logout", auth.HandleLogout)

	// Servers
	s.mux.Handle("GET /api/servers", am(http.HandlerFunc(s.handleListServers)))
	s.mux.Handle("POST /api/servers", am(http.HandlerFunc(s.handleCreateServer)))
	s.mux.Handle("DELETE /api/servers/{id}", am(http.HandlerFunc(s.handleDeleteServer)))
	s.mux.Handle("PUT /api/servers/{id}/key", am(http.HandlerFunc(s.handleUploadKey)))

	// Sessions
	s.mux.Handle("GET /api/servers/{id}/sessions", am(http.HandlerFunc(s.handleListSessions)))
	s.mux.Handle("POST /api/servers/{id}/sessions", am(http.HandlerFunc(s.handleCreateSession)))
	s.mux.Handle("DELETE /api/servers/{id}/sessions/{name}", am(http.HandlerFunc(s.handleKillSession)))

	// Templates
	s.mux.Handle("GET /api/templates", am(http.HandlerFunc(s.handleListTemplates)))

	// Terminal WebSocket
	s.mux.Handle("GET /ws/terminal/{server}/{session}", am(http.HandlerFunc(s.terminal.HandleTerminal)))

	// Static files
	if s.staticFS != nil {
		s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(s.staticFS))))
		s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			data, err := fs.ReadFile(s.staticFS, "index.html")
			if err != nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
		})
	}
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
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

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	servers, err := s.store.ListServers()
	if err != nil {
		http.Error(w, `{"error":"failed to list servers"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(servers)
}

type createServerRequest struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	SSHUser string `json:"sshUser"`
}

func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	var req createServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	if req.Port == 0 {
		req.Port = 22
	}

	srv, err := s.store.CreateServer(req.Name, req.Host, req.Port, req.SSHUser)
	if err != nil {
		http.Error(w, `{"error":"failed to create server"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(srv)
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.keyStore.Delete(r.Context(), id); err != nil {
		log.Printf("delete key for server %s: %v", id, err)
	}
	s.pool.Remove(id)

	if err := s.store.DeleteServer(id); err != nil {
		http.Error(w, `{"error":"failed to delete server"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleUploadKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	keyData, err := io.ReadAll(io.LimitReader(r.Body, 32*1024))
	if err != nil {
		http.Error(w, `{"error":"failed to read key"}`, http.StatusBadRequest)
		return
	}

	if err := s.keyStore.Put(r.Context(), id, keyData); err != nil {
		http.Error(w, `{"error":"failed to store key"}`, http.StatusInternalServerError)
		return
	}

	// Test connection
	_, _, err = s.pool.Exec(r.Context(), id, "echo ok")
	if err != nil {
		_ = s.store.UpdateServerStatus(id, "unreachable")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "key_saved", "reachable": false, "error": err.Error()})
		return
	}

	_ = s.store.UpdateServerStatus(id, "reachable")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "reachable": true})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sessions := s.sessions.GetSessions(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

type createSessionRequest struct {
	Label   string `json:"label"`
	Command string `json:"command"`
	Workdir string `json:"workdir"`
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user := auth.GetUser(r)

	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	username := "user"
	if user != nil {
		username = user.Username
	}

	name, err := s.sessions.CreateSession(r.Context(), id, username, req.Label, req.Command, req.Workdir)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"name": name})
}

func (s *Server) handleKillSession(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("id")
	sessionName := r.PathValue("name")

	if err := s.sessions.KillSession(r.Context(), serverID, sessionName); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	serverID := r.URL.Query().Get("server_id")
	templates, err := s.store.ListTemplates(serverID)
	if err != nil {
		http.Error(w, `{"error":"failed to list templates"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(templates)
}
