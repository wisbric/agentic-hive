package server

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"time"

	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/auth"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/config"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/keystore"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/session"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/sshpool"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/terminal"
)

type Server struct {
	cfg         *config.Config
	store       *store.Store
	mux         *http.ServeMux
	localAuth   *auth.LocalHandler
	rateLimiter *auth.RateLimiter
	pool        *sshpool.Pool
	keyStore    keystore.KeyStore
	sessions    *session.Manager
	terminal    *terminal.Bridge
	staticFS    fs.FS
}

func New(cfg *config.Config, st *store.Store, pool *sshpool.Pool, ks keystore.KeyStore, sm *session.Manager, staticFS fs.FS) *Server {
	s := &Server{
		cfg:         cfg,
		store:       st,
		mux:         http.NewServeMux(),
		localAuth:   auth.NewLocalHandler(st, cfg.SessionSecret),
		rateLimiter: auth.NewRateLimiter(cfg.LoginRateLimit, cfg.LoginRateWindow),
		pool:        pool,
		keyStore:    ks,
		sessions:    sm,
		terminal:    terminal.NewBridge(pool),
		staticFS:    staticFS,
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

func (s *Server) Close() {
	s.rateLimiter.Close()
}

func (s *Server) routes() {
	am := auth.RequireAuth(s.cfg.SessionSecret)
	adminM := auth.RequireAdmin(s.cfg.SessionSecret)

	// Public
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)

	// Auth (no auth required)
	s.mux.Handle("POST /api/auth/login", s.rateLimiter.Middleware(http.HandlerFunc(s.localAuth.HandleLogin)))
	s.mux.Handle("POST /api/auth/setup", s.rateLimiter.Middleware(http.HandlerFunc(s.localAuth.HandleSetup)))
	s.mux.HandleFunc("GET /api/auth/setup/status", s.handleSetupStatus)
	s.mux.HandleFunc("POST /api/auth/logout", auth.HandleLogout)

	// Servers
	s.mux.Handle("GET /api/servers", am(http.HandlerFunc(s.handleListServers)))
	s.mux.Handle("POST /api/servers", adminM(http.HandlerFunc(s.handleCreateServer)))
	s.mux.Handle("DELETE /api/servers/{id}", adminM(http.HandlerFunc(s.handleDeleteServer)))
	s.mux.Handle("PUT /api/servers/{id}/key", adminM(http.HandlerFunc(s.handleUploadKey)))
	s.mux.Handle("POST /api/servers/{id}/accept-key", adminM(http.HandlerFunc(s.handleAcceptKey))) // TODO(PRP-9): restrict to admin

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
	checks := map[string]string{}
	overallOK := true

	// Database check
	dbCtx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
	defer cancel()
	if err := s.store.Ping(dbCtx); err != nil {
		checks["database"] = "error: " + err.Error()
		overallOK = false
	} else {
		checks["database"] = "ok"
	}

	// Servers check (conditional)
	if s.cfg.ReadyzRequireServer {
		servers, err := s.store.ListServers()
		if err != nil {
			checks["servers"] = "error: " + err.Error()
			overallOK = false
		} else if len(servers) == 0 {
			checks["servers"] = "ok" // no servers registered yet, not a fault
		} else {
			reachable := 0
			for _, srv := range servers {
				if srv.Status == store.StatusReachable {
					reachable++
				}
			}
			if reachable == 0 {
				checks["servers"] = "no reachable servers"
				overallOK = false
			} else {
				checks["servers"] = "ok"
			}
		}
	} else {
		checks["servers"] = "disabled"
	}

	w.Header().Set("Content-Type", "application/json")
	status := "ok"
	if !overallOK {
		w.WriteHeader(http.StatusServiceUnavailable)
		status = "fail"
	}
	json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"checks": checks,
	})
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"needed": s.localAuth.SetupNeeded()})
}

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	servers, err := s.store.ListServers()
	if err != nil {
		jsonError(w, "failed to list servers", http.StatusInternalServerError)
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
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Port == 0 {
		req.Port = 22
	}

	srv, err := s.store.CreateServer(req.Name, req.Host, req.Port, req.SSHUser)
	if err != nil {
		jsonError(w, "failed to create server", http.StatusInternalServerError)
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
		jsonError(w, "failed to delete server", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleUploadKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	keyData, err := io.ReadAll(io.LimitReader(r.Body, 32*1024))
	if err != nil {
		jsonError(w, "failed to read key", http.StatusBadRequest)
		return
	}

	if err := s.keyStore.Put(r.Context(), id, keyData); err != nil {
		jsonError(w, "failed to store key", http.StatusInternalServerError)
		return
	}

	// Test connection
	_, _, err = s.pool.Exec(r.Context(), id, "echo ok")
	if err != nil {
		_ = s.store.UpdateServerStatus(id, store.StatusUnreachable)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "key_saved", "reachable": false, "error": err.Error()})
		return
	}

	_ = s.store.UpdateServerStatus(id, store.StatusReachable)
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
		jsonError(w, "invalid request", http.StatusBadRequest)
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
		jsonError(w, "failed to list templates", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(templates)
}

func (s *Server) handleAcceptKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteHostKey(id); err != nil {
		jsonError(w, "failed to clear host key", http.StatusInternalServerError)
		return
	}
	s.pool.Remove(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
