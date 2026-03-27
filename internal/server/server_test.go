package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/config"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/store"
)

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
		SessionSecret: "test-secret-that-is-long-enough-32chars!",
	}

	srv := New(cfg, st)
	return srv
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
