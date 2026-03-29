package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/auth"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/config"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/keystore"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/session"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/sshpool"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"
	"golang.org/x/crypto/bcrypt"
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
		Listen:          ":0",
		AuthMode:        "local",
		SessionSecret:   testSecret,
		LoginRateLimit:  5,
		LoginRateWindow: 900,
	}

	ks := keystore.NewSwappable(keystore.NewLocal(st.DB(), cfg.SessionSecret))
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

func TestReadyzResponseShape(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("status = %q, want \"ok\"", body["status"])
	}

	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatalf("checks is not a map: %T", body["checks"])
	}

	if checks["database"] != "ok" {
		t.Errorf("checks.database = %q, want \"ok\"", checks["database"])
	}

	if checks["servers"] != "disabled" {
		t.Errorf("checks.servers = %q, want \"disabled\" (ReadyzRequireServer is false)", checks["servers"])
	}
}

func TestReadyzRequireServerNoServers(t *testing.T) {
	// With OVERLAY_READYZ_REQUIRE_SERVER=true and no servers registered,
	// the probe should still return 200 (freshly deployed state is not a fault).
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	cfg := &config.Config{
		Listen:              ":0",
		AuthMode:            "local",
		SessionSecret:       testSecret,
		ReadyzRequireServer: true,
		LoginRateLimit:      5,
		LoginRateWindow:     900,
	}

	ks := keystore.NewSwappable(keystore.NewLocal(st.DB(), cfg.SessionSecret))
	pool := sshpool.New(st, ks)
	t.Cleanup(func() { pool.Close() })
	sm := session.NewManager(st, pool)
	srv := New(cfg, st, pool, ks, sm, nil)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (no servers = ok)", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatalf("checks is not a map: %T", body["checks"])
	}

	if checks["servers"] != "ok" {
		t.Errorf("checks.servers = %q, want \"ok\" (no servers registered)", checks["servers"])
	}
}

func TestReadyzRequireServerUnreachable(t *testing.T) {
	// With OVERLAY_READYZ_REQUIRE_SERVER=true and all servers unreachable,
	// the probe should return 503.
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	// Register a server but set it to unreachable
	srv2, err := st.CreateServer("test", "192.0.2.1", 22, "root", "", "local", "")
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if err := st.UpdateServerStatus(srv2.ID, store.StatusUnreachable); err != nil {
		t.Fatalf("UpdateServerStatus failed: %v", err)
	}

	cfg := &config.Config{
		Listen:              ":0",
		AuthMode:            "local",
		SessionSecret:       testSecret,
		ReadyzRequireServer: true,
		LoginRateLimit:      5,
		LoginRateWindow:     900,
	}

	ks := keystore.NewSwappable(keystore.NewLocal(st.DB(), cfg.SessionSecret))
	pool := sshpool.New(st, ks)
	t.Cleanup(func() { pool.Close() })
	sm := session.NewManager(st, pool)
	s := New(cfg, st, pool, ks, sm, nil)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d (all servers unreachable)", w.Code, http.StatusServiceUnavailable)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["status"] != "fail" {
		t.Errorf("status = %q, want \"fail\"", body["status"])
	}

	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatalf("checks is not a map: %T", body["checks"])
	}

	if checks["servers"] != "no reachable servers" {
		t.Errorf("checks.servers = %q, want \"no reachable servers\"", checks["servers"])
	}
}

func TestReadyzRequireServerReachable(t *testing.T) {
	// With OVERLAY_READYZ_REQUIRE_SERVER=true and at least one reachable server,
	// the probe should return 200.
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	srv2, err := st.CreateServer("test", "192.0.2.1", 22, "root", "", "local", "")
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if err := st.UpdateServerStatus(srv2.ID, store.StatusReachable); err != nil {
		t.Fatalf("UpdateServerStatus failed: %v", err)
	}

	cfg := &config.Config{
		Listen:              ":0",
		AuthMode:            "local",
		SessionSecret:       testSecret,
		ReadyzRequireServer: true,
		LoginRateLimit:      5,
		LoginRateWindow:     900,
	}

	ks := keystore.NewSwappable(keystore.NewLocal(st.DB(), cfg.SessionSecret))
	pool := sshpool.New(st, ks)
	t.Cleanup(func() { pool.Close() })
	sm := session.NewManager(st, pool)
	s := New(cfg, st, pool, ks, sm, nil)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (one reachable server)", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("status = %q, want \"ok\"", body["status"])
	}

	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatalf("checks is not a map: %T", body["checks"])
	}

	if checks["servers"] != "ok" {
		t.Errorf("checks.servers = %q, want \"ok\"", checks["servers"])
	}
}

func TestAPIRoutesRequireAuth(t *testing.T) {
	srv := testServer(t)
	// Use a fixed CSRF token so state-changing routes pass the CSRF check
	// and reach the auth middleware (which should return 401 for no session).
	csrfToken := "test-csrf-token-require-auth"
	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/servers"},
		{"POST", "/api/servers"},
	}
	for _, route := range routes {
		req := httptest.NewRequest(route.method, route.path, nil)
		// Include CSRF token for non-GET methods so CSRF middleware passes through
		if route.method != http.MethodGet {
			req.AddCookie(&http.Cookie{Name: "csrf", Value: csrfToken})
			req.Header.Set("X-CSRF-Token", csrfToken)
		}
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
	csrfToken := "test-csrf-token-admin-routes"

	adminRoutes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/users"},
		{"GET", "/api/admin/settings"},
	}

	for _, route := range adminRoutes {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.AddCookie(userCookie)
		// Include CSRF tokens so the CSRF middleware passes and the RBAC check is exercised
		req.AddCookie(&http.Cookie{Name: "csrf", Value: csrfToken})
		req.Header.Set("X-CSRF-Token", csrfToken)
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
	csrfToken := "test-csrf-token-for-admin-route"

	// POST /api/servers with admin should reach handler (not be rejected by RBAC or CSRF)
	// It may fail with 400/500 due to missing body/DB state, but not 401/403
	req := httptest.NewRequest(http.MethodPost, "/api/servers", nil)
	req.AddCookie(adminCookie)
	req.AddCookie(&http.Cookie{Name: "csrf", Value: csrfToken})
	req.Header.Set("X-CSRF-Token", csrfToken)
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

// --- CSRF Integration Tests ---

func TestCreateServerNoCSRF(t *testing.T) {
	srv := testServer(t)
	adminCookie := makeSessionCookie(t, store.RoleAdmin)

	// Admin JWT but no CSRF token → 403
	req := httptest.NewRequest(http.MethodPost, "/api/servers", strings.NewReader(`{"name":"x","host":"y","port":22,"sshUser":"root"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	// no csrf cookie, no X-CSRF-Token header
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("POST /api/servers without CSRF: status = %d, want 403", w.Code)
	}
}

func TestCreateServerWithCSRF(t *testing.T) {
	srv := testServer(t)
	adminCookie := makeSessionCookie(t, store.RoleAdmin)
	csrfToken := "integration-test-csrf-token-value"

	// Admin JWT + valid CSRF → should reach the handler (201 or error, but not 403)
	req := httptest.NewRequest(http.MethodPost, "/api/servers", strings.NewReader(`{"name":"x","host":"192.0.2.1","port":22,"sshUser":"root"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	req.AddCookie(&http.Cookie{Name: "csrf", Value: csrfToken})
	req.Header.Set("X-CSRF-Token", csrfToken)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code == http.StatusForbidden {
		t.Errorf("POST /api/servers with valid CSRF: got 403, CSRF check must pass")
	}
	if w.Code != http.StatusCreated {
		t.Errorf("POST /api/servers with valid CSRF + admin: status = %d, want 201", w.Code)
	}
}

func TestLoginNoCSRF(t *testing.T) {
	srv := testServer(t)

	// POST /api/auth/login without X-CSRF-Token → 200 (exempt path)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"nobody","password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	// no csrf token
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	// Should not return 403 (CSRF does not apply to login)
	if w.Code == http.StatusForbidden {
		t.Errorf("POST /api/auth/login without CSRF: status = %d, must not be 403 (exempt path)", w.Code)
	}
}

func hashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// TestLoginRateLimit verifies that 5 failed login attempts return 401 and the 6th returns 429.
func TestLoginRateLimit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	cfg := &config.Config{
		Listen:          ":0",
		AuthMode:        "local",
		SessionSecret:   testSecret,
		LoginRateLimit:  5,
		LoginRateWindow: 900,
	}

	ks := keystore.NewSwappable(keystore.NewLocal(st.DB(), cfg.SessionSecret))
	pool := sshpool.New(st, ks)
	t.Cleanup(func() { pool.Close() })
	sm := session.NewManager(st, pool)
	srv := New(cfg, st, pool, ks, sm, nil)
	t.Cleanup(func() { srv.Close() })

	// Seed a user so login attempts with wrong password return 401
	hash, err := hashPassword("correct-password")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if _, err := st.CreateUser("admin", hash, store.RoleAdmin); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	wrongBody := `{"username":"admin","password":"wrong"}`

	// First 5 attempts with wrong password must return 401
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(wrongBody))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "10.10.10.10:9999"
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("attempt %d: got %d, want 401", i+1, w.Code)
		}
	}

	// 6th attempt must be 429 with Retry-After header
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(wrongBody))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.10.10.10:9999"
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("6th attempt: got %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("6th attempt: Retry-After header missing")
	}
}

// TestLoginRateLimitConfigurable verifies OVERLAY_LOGIN_RATE_LIMIT=3 causes 429 on the 4th attempt.
func TestLoginRateLimitConfigurable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	cfg := &config.Config{
		Listen:          ":0",
		AuthMode:        "local",
		SessionSecret:   testSecret,
		LoginRateLimit:  3,
		LoginRateWindow: 900,
	}

	ks := keystore.NewSwappable(keystore.NewLocal(st.DB(), cfg.SessionSecret))
	pool := sshpool.New(st, ks)
	t.Cleanup(func() { pool.Close() })
	sm := session.NewManager(st, pool)
	srv := New(cfg, st, pool, ks, sm, nil)
	t.Cleanup(func() { srv.Close() })

	hash, err := hashPassword("correct-password")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if _, err := st.CreateUser("admin", hash, store.RoleAdmin); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	wrongBody := `{"username":"admin","password":"wrong"}`

	// 3 failures — all should be 401
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(wrongBody))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "10.20.30.40:1234"
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("attempt %d: got %d, want 401", i+1, w.Code)
		}
	}

	// 4th attempt — must be 429
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(wrongBody))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.20.30.40:1234"
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("4th attempt with limit=3: got %d, want 429", w.Code)
	}
}
