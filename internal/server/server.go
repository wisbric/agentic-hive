package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/wisbric/agentic-hive/internal/auth"
	"github.com/wisbric/agentic-hive/internal/backup"
	"github.com/wisbric/agentic-hive/internal/config"
	"github.com/wisbric/agentic-hive/internal/keystore"
	"github.com/wisbric/agentic-hive/internal/metrics"
	"github.com/wisbric/agentic-hive/internal/session"
	"github.com/wisbric/agentic-hive/internal/sshpool"
	"github.com/wisbric/agentic-hive/internal/store"
	"github.com/wisbric/agentic-hive/internal/terminal"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	startTime = time.Now()
)

type Server struct {
	cfg         *config.Config
	store       *store.Store
	mux         *http.ServeMux
	httpServer  *http.Server
	localAuth   *auth.LocalHandler
	rateLimiter *auth.RateLimiter
	oidcHandler *auth.SwappableOIDCHandler
	pool        *sshpool.Pool
	keyStore    *keystore.SwappableKeyStore
	sessions    *session.Manager
	terminal    *terminal.Bridge
	staticFS    fs.FS
}

func New(cfg *config.Config, st *store.Store, pool *sshpool.Pool, ks *keystore.SwappableKeyStore, sm *session.Manager, staticFS fs.FS) *Server {
	s := &Server{
		cfg:         cfg,
		store:       st,
		mux:         http.NewServeMux(),
		localAuth:   auth.NewLocalHandler(st, cfg.SessionSecret),
		rateLimiter: auth.NewRateLimiter(cfg.LoginRateLimit, cfg.LoginRateWindow),
		oidcHandler: auth.NewSwappableOIDCHandler(nil),
		pool:        pool,
		keyStore:    ks,
		sessions:    sm,
		terminal:    terminal.NewBridge(pool, time.Duration(cfg.TerminalIdleTimeout)*time.Second, st),
		staticFS:    staticFS,
	}
	s.routes()
	s.httpServer = &http.Server{
		Addr:    cfg.Listen,
		Handler: s.Handler(),
	}
	return s
}

// SetOIDCHandler hot-swaps the OIDC handler. Routes are always registered at
// startup; this method only updates the handler the routes dispatch to.
func SetOIDCHandler(s *Server, h *auth.OIDCHandler) {
	s.oidcHandler.Swap(h)
}

// OIDCHandler returns the swappable OIDC handler held by the server.
func (s *Server) OIDCHandler() *auth.SwappableOIDCHandler {
	return s.oidcHandler
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := r.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("response writer does not implement http.Hijacker")
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip metrics collection for /metrics itself to avoid noise.
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		elapsed := time.Since(start)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", elapsed.Milliseconds(),
		)

		// Use matched route pattern (Go 1.22+) to avoid high cardinality.
		pattern := r.Pattern
		if pattern == "" {
			pattern = r.URL.Path
		}
		statusStr := fmt.Sprintf("%d", rec.status)

		if metrics.HTTPRequestsTotal != nil {
			metrics.HTTPRequestsTotal.WithLabelValues(r.Method, pattern, statusStr).Inc()
		}
		if metrics.HTTPRequestDuration != nil {
			metrics.HTTPRequestDuration.WithLabelValues(r.Method, pattern).Observe(elapsed.Seconds())
		}
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; connect-src 'self' wss:; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src https://fonts.gstatic.com")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) Handler() http.Handler {
	return loggingMiddleware(securityHeaders(auth.CSRFProtect(
		"/api/auth/login",
		"/api/auth/setup",
		"/api/auth/oidc/callback",
		"/api/auth/oidc/login",
		"/healthz",
		"/readyz",
		"/metrics",
	)(s.mux)))
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	slog.Info("listening", "addr", s.cfg.Listen)
	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("http shutdown error", "error", err)
		}
		slog.Info("http server stopped")
		return nil
	}
}

func (s *Server) Close() {
	s.rateLimiter.Close()
}

func (s *Server) routes() {
	am := auth.RequireAuth(s.cfg.SessionSecret)
	adminM := auth.RequireAdmin(s.cfg.SessionSecret)

	// Observability (public — network-level access control is operator responsibility)
	s.mux.Handle("GET /metrics", promhttp.Handler())

	// Public
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)
	s.mux.HandleFunc("GET /api/about", s.handleAbout)

	// Current user (auth required)
	s.mux.Handle("GET /api/me", am(http.HandlerFunc(s.handleMe)))

	// Auth (no auth required)
	s.mux.Handle("POST /api/auth/login", s.rateLimiter.Middleware(http.HandlerFunc(s.localAuth.HandleLogin)))
	s.mux.Handle("POST /api/auth/setup", s.rateLimiter.Middleware(http.HandlerFunc(s.localAuth.HandleSetup)))
	s.mux.HandleFunc("GET /api/auth/setup/status", s.handleSetupStatus)
	s.mux.Handle("POST /api/auth/logout", am(auth.Logout(s.store)))

	// OIDC routes — always registered; return 404 when not configured
	s.mux.HandleFunc("GET /api/auth/oidc/login", s.oidcHandler.HandleLogin)
	s.mux.HandleFunc("GET /api/auth/oidc/callback", s.oidcHandler.HandleCallback)

	// Servers — any user can create/manage their own, delete checks ownership (admin can delete any)
	s.mux.Handle("GET /api/servers", am(http.HandlerFunc(s.handleListServers)))
	s.mux.Handle("POST /api/servers", am(http.HandlerFunc(s.handleCreateServer)))
	s.mux.Handle("DELETE /api/servers/{id}", am(http.HandlerFunc(s.handleDeleteServer)))
	s.mux.Handle("PUT /api/servers/{id}/key", am(http.HandlerFunc(s.handleUploadKey)))
	s.mux.Handle("POST /api/servers/{id}/accept-key", am(http.HandlerFunc(s.handleAcceptKey)))

	// Sessions
	s.mux.Handle("GET /api/servers/{id}/sessions", am(http.HandlerFunc(s.handleListSessions)))
	s.mux.Handle("POST /api/servers/{id}/sessions", am(http.HandlerFunc(s.handleCreateSession)))
	s.mux.Handle("DELETE /api/servers/{id}/sessions/{name}", am(http.HandlerFunc(s.handleKillSession)))

	// Templates
	s.mux.Handle("GET /api/templates", am(http.HandlerFunc(s.handleListTemplates)))

	// Admin
	s.mux.Handle("POST /api/admin/backup", adminM(http.HandlerFunc(s.handleBackup)))
	s.mux.Handle("GET /api/admin/audit", adminM(http.HandlerFunc(s.handleListAuditLog)))
	s.mux.Handle("GET /api/users", adminM(http.HandlerFunc(s.handleListUsers)))
	s.mux.Handle("DELETE /api/users/{id}", adminM(http.HandlerFunc(s.handleDeleteUser)))
	s.mux.Handle("GET /api/admin/config", adminM(http.HandlerFunc(s.handleGetConfig)))

	// Settings API
	s.mux.Handle("GET /api/admin/settings", adminM(http.HandlerFunc(s.handleGetSettings)))
	s.mux.Handle("PUT /api/admin/settings", adminM(http.HandlerFunc(s.handleUpdateSettings)))
	s.mux.Handle("POST /api/admin/settings/test-oidc", adminM(http.HandlerFunc(s.handleTestOIDC)))
	s.mux.Handle("POST /api/admin/settings/test-vault", adminM(http.HandlerFunc(s.handleTestVault)))

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

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"username":        user.Username,
		"role":            user.Role,
		"vault_available": s.keyStore.Backend() == "vault",
	})
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(startTime).Round(time.Second).String()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"version": Version,
		"commit":  Commit,
		"uptime":  uptime,
	})
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
		servers, err := s.store.ListServers("")
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
	json.NewEncoder(w).Encode(map[string]any{
		"needed":         s.localAuth.SetupNeeded(),
		"oidc_available": s.oidcHandler.IsConfigured(),
	})
}

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	ownerID := ""
	if user != nil && user.Role != store.RoleAdmin {
		ownerID = user.UserID
	}
	servers, err := s.store.ListServers(ownerID)
	if err != nil {
		jsonError(w, "failed to list servers", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(servers)
}

type createServerRequest struct {
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	SSHUser      string `json:"sshUser"`
	KeySource    string `json:"keySource"`    // "local" or "vault_ref"
	VaultKeyPath string `json:"vaultKeyPath"` // only when keySource == "vault_ref"
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
	if req.KeySource == "" {
		req.KeySource = "local"
	}

	// Validate vault_ref: Vault must be configured and path must be readable.
	if req.KeySource == "vault_ref" {
		if req.VaultKeyPath == "" {
			jsonError(w, "vaultKeyPath is required when keySource is vault_ref", http.StatusBadRequest)
			return
		}
		// Sanitize path: reject traversal attempts and absolute paths
		if strings.Contains(req.VaultKeyPath, "..") || strings.HasPrefix(req.VaultKeyPath, "/") {
			jsonError(w, "invalid vault path", http.StatusBadRequest)
			return
		}
		if s.keyStore.Backend() != "vault" {
			jsonError(w, "vault is not configured; cannot use vault_ref key source", http.StatusBadRequest)
			return
		}
		if _, err := s.keyStore.GetFromPath(r.Context(), req.VaultKeyPath); err != nil {
			slog.Warn("vault path validation failed", "path", req.VaultKeyPath, "error", err)
			jsonError(w, "vault path not readable or does not contain a private_key field", http.StatusBadRequest)
			return
		}
	}

	user := auth.GetUser(r)
	ownerID := ""
	if user != nil {
		ownerID = user.UserID
	}

	srv, err := s.store.CreateServer(req.Name, req.Host, req.Port, req.SSHUser, ownerID, req.KeySource, req.VaultKeyPath)
	if err != nil {
		jsonError(w, "failed to create server", http.StatusInternalServerError)
		return
	}

	logAudit(s, r, user, store.AuditServerCreate, "server", srv.ID,
		fmt.Sprintf(`{"server_id":%q,"name":%q,"key_source":%q}`, srv.ID, srv.Name, srv.KeySource))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(srv)
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user := auth.GetUser(r)

	ownerID := ""
	if user != nil && user.Role != store.RoleAdmin {
		ownerID = user.UserID
	}
	if _, err := s.store.GetServer(id, ownerID); err != nil {
		jsonError(w, "server not found", http.StatusNotFound)
		return
	}

	if err := s.keyStore.Delete(r.Context(), id); err != nil {
		slog.Warn("key delete failed", "server_id", id, "error", err)
	}
	s.pool.Remove(id)

	if err := s.store.DeleteServer(id); err != nil {
		jsonError(w, "failed to delete server", http.StatusInternalServerError)
		return
	}

	logAudit(s, r, user, store.AuditServerDelete, "server", id,
		fmt.Sprintf(`{"server_id":%q}`, id))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleUploadKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user := auth.GetUser(r)

	ownerID := ""
	if user != nil && user.Role != store.RoleAdmin {
		ownerID = user.UserID
	}
	existingSrv, err := s.store.GetServer(id, ownerID)
	if err != nil {
		jsonError(w, "server not found", http.StatusNotFound)
		return
	}
	if existingSrv.KeySource == "vault_ref" {
		jsonError(w, "cannot upload key to a vault_ref server; key is managed via Vault path", http.StatusBadRequest)
		return
	}

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
		logAudit(s, r, user, store.AuditServerKeyUpload, "server", id,
			fmt.Sprintf(`{"server_id":%q,"reachable":false}`, id))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "key_saved", "reachable": false, "error": err.Error()})
		return
	}

	_ = s.store.UpdateServerStatus(id, store.StatusReachable)
	logAudit(s, r, user, store.AuditServerKeyUpload, "server", id,
		fmt.Sprintf(`{"server_id":%q,"reachable":true}`, id))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "reachable": true})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user := auth.GetUser(r)

	ownerID := ""
	if user != nil && user.Role != store.RoleAdmin {
		ownerID = user.UserID
	}

	var sessions []session.Session
	if r.URL.Query().Get("live") == "true" {
		// Live query: bypass cache, fetch directly from server via SSH
		srv, err := s.store.GetServer(id, ownerID)
		if err != nil {
			jsonError(w, "server not found", http.StatusNotFound)
			return
		}
		live, err := s.sessions.ListSessions(r.Context(), srv)
		if err != nil {
			// Fall back to cache on error
			sessions = s.sessions.GetSessions(id)
		} else {
			sessions = live
		}
	} else {
		// Non-live: verify ownership before returning cached sessions
		if _, err := s.store.GetServer(id, ownerID); err != nil {
			jsonError(w, "server not found", http.StatusNotFound)
			return
		}
		sessions = s.sessions.GetSessions(id)
	}

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

	ownerID := ""
	if user != nil && user.Role != store.RoleAdmin {
		ownerID = user.UserID
	}
	if _, err := s.store.GetServer(id, ownerID); err != nil {
		jsonError(w, "server not found", http.StatusNotFound)
		return
	}

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

	logAudit(s, r, user, store.AuditSessionCreate, "session", name,
		fmt.Sprintf(`{"server_id":%q,"session":%q}`, id, name))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"name": name})
}

func (s *Server) handleKillSession(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("id")
	sessionName := r.PathValue("name")
	user := auth.GetUser(r)

	ownerID := ""
	if user != nil && user.Role != store.RoleAdmin {
		ownerID = user.UserID
	}
	if _, err := s.store.GetServer(serverID, ownerID); err != nil {
		jsonError(w, "server not found", http.StatusNotFound)
		return
	}

	if err := s.sessions.KillSession(r.Context(), serverID, sessionName); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logAudit(s, r, user, store.AuditSessionKill, "session", sessionName,
		fmt.Sprintf(`{"server_id":%q,"session":%q}`, serverID, sessionName))

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

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers()
	if err != nil {
		jsonError(w, "failed to list users", http.StatusInternalServerError)
		return
	}
	// Strip password hashes before returning
	type safeUser struct {
		ID        string `json:"id"`
		Username  string `json:"username"`
		Role      string `json:"role"`
		CreatedAt string `json:"createdAt"`
	}
	safe := make([]safeUser, 0, len(users))
	for _, u := range users {
		safe = append(safe, safeUser{
			ID:        u.ID,
			Username:  u.Username,
			Role:      u.Role,
			CreatedAt: u.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(safe)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	caller := auth.GetUser(r)
	if caller != nil && caller.UserID == id {
		jsonError(w, "cannot delete yourself", http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteUser(id); err != nil {
		jsonError(w, "failed to delete user", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	authMode := "local"
	if s.oidcHandler.IsConfigured() {
		authMode = "oidc + local fallback"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"authMode":        authMode,
		"keyStoreBackend": s.keyStore.Backend(),
		"pollInterval":    s.cfg.PollInterval,
	})
}

func (s *Server) handleAcceptKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user := auth.GetUser(r)

	ownerID := ""
	if user != nil && user.Role != store.RoleAdmin {
		ownerID = user.UserID
	}
	if _, err := s.store.GetServer(id, ownerID); err != nil {
		jsonError(w, "server not found", http.StatusNotFound)
		return
	}

	if err := s.store.DeleteHostKey(id); err != nil {
		jsonError(w, "failed to clear host key", http.StatusInternalServerError)
		return
	}
	s.pool.Remove(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	timestamp := time.Now().UTC().Format("20060102-150405")
	tmpFile, err := os.CreateTemp("", "overlay-backup-*.db")
	if err != nil {
		jsonError(w, "failed to create temp file", http.StatusInternalServerError)
		return
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	// Remove so VACUUM INTO can create it fresh.
	os.Remove(tmpPath)
	defer os.Remove(tmpPath)

	if err := backup.Run(s.cfg.DBPath, tmpPath); err != nil {
		slog.Error("backup failed", "error", err)
		jsonError(w, "backup failed", http.StatusInternalServerError)
		return
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		slog.Error("failed to open backup file", "error", err)
		jsonError(w, "failed to read backup file", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		jsonError(w, "failed to stat backup file", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("overlay-backup-%s.db", timestamp)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	if _, err := io.Copy(w, f); err != nil {
		slog.Error("failed to stream backup", "error", err)
	}
}
