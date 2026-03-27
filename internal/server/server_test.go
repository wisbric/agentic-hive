package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/auth"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/config"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/keystore"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/session"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/sshpool"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"
)

const testSecret = "test-secret-that-is-long-enough-32chars!"

func testServer(t *testing.T) *Server {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	cfg := &config.Config{
		Listen:        ":0",
		AuthMode:      "local",
		SessionSecret: testSecret,
	}

	ks := keystore.NewLocal(st.DB(), cfg.SessionSecret)
	pool := sshpool.New(st, ks)
	t.Cleanup(func() { pool.Close() })
	sm := session.NewManager(st, pool)

	srv := New(cfg, st, pool, ks, sm, nil)
	return srv
}

func makeSessionCookie(t *testing.T, role string) *http.Cookie {
	t.Helper()
	token, err := auth.SignJWT(&auth.Claims{UserID: "u1", Username: "testuser", Role: role}, testSecret, 1*time.Hour)
	if err != nil {
		t.Fatalf("SignJWT failed: %v", err)
	}
	return &http.Cookie{Name: "session", Value: token}
}

func TestHealthz(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestReadyz(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAPIRoutesRequireAuth(t *testing.T) {
	srv := testServer(t)
	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/servers"},
		{"POST", "/api/servers"},
	}
	for _, route := range routes {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want %d", route.method, route.path, w.Code, http.StatusUnauthorized)
		}
	}
}

func TestHealthzNoAuth(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("healthz should not require auth, got %d", w.Code)
	}
}

func TestSetupEndpoint(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/setup/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAdminRoutesRequireAdminRole(t *testing.T) {
	srv := testServer(t)
	userCookie := makeSessionCookie(t, store.RoleUser)

	adminRoutes := []struct {
		method string
		path   string
	}{
		{"POST", "/api/servers"},
		{"DELETE", "/api/servers/some-id"},
		{"PUT", "/api/servers/some-id/key"},
	}

	for _, route := range adminRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.AddCookie(userCookie)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s with user role: status = %d, want %d", route.method, route.path, w.Code, http.StatusForbidden)
		}
	}
}

func TestAdminRoutesAllowAdminRole(t *testing.T) {
	srv := testServer(t)
	adminCookie := makeSessionCookie(t, store.RoleAdmin)

	// POST /api/servers with admin should reach handler (not be rejected by RBAC)
	// It may fail with 400/500 due to missing body/DB state, but not 401/403
	req := httptest.NewRequest(http.MethodPost, "/api/servers", nil)
	req.AddCookie(adminCookie)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
		t.Errorf("POST /api/servers with admin role: status = %d, must not be 401 or 403", w.Code)
	}
}

func TestUserRouteAccessibleWithUserRole(t *testing.T) {
	srv := testServer(t)
	userCookie := makeSessionCookie(t, store.RoleUser)

	// GET /api/servers should be accessible to any authenticated user
	req := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	req.AddCookie(userCookie)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /api/servers with user role: status = %d, want %d", w.Code, http.StatusOK)
	}
}
