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
		Listen:   ":0",
		AuthMode: "local",
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
	if w.Body.String() != `{"status":"ok"}`+"\n" {
		t.Errorf("body = %q, want %q", w.Body.String(), `{"status":"ok"}`+"\n")
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
