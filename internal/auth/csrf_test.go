package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestSetCSRFCookie(t *testing.T) {
	w := httptest.NewRecorder()
	token, err := SetCSRFCookie(w)
	if err != nil {
		t.Fatalf("SetCSRFCookie returned error: %v", err)
	}

	// Token should be 64 hex chars (32 bytes)
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64", len(token))
	}

	cookies := w.Result().Cookies()
	var csrfCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "csrf" {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatal("csrf cookie not set")
	}
	if csrfCookie.HttpOnly {
		t.Error("csrf cookie must NOT be HttpOnly (JS needs to read it)")
	}
	if csrfCookie.Value != token {
		t.Errorf("cookie value = %q, want %q", csrfCookie.Value, token)
	}
	if csrfCookie.MaxAge != sessionCookieMaxAge {
		t.Errorf("MaxAge = %d, want %d", csrfCookie.MaxAge, sessionCookieMaxAge)
	}
}

func TestCSRFProtectGetPassThrough(t *testing.T) {
	mw := CSRFProtect()
	handler := mw(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET without CSRF token: status = %d, want 200", w.Code)
	}
}

func TestCSRFProtectMissingToken(t *testing.T) {
	mw := CSRFProtect()
	handler := mw(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/servers", nil)
	req.AddCookie(&http.Cookie{Name: "csrf", Value: "abc123"})
	// No X-CSRF-Token header
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("POST without header: status = %d, want 403", w.Code)
	}
}

func TestCSRFProtectWrongToken(t *testing.T) {
	mw := CSRFProtect()
	handler := mw(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/servers", nil)
	req.AddCookie(&http.Cookie{Name: "csrf", Value: "cookie-value"})
	req.Header.Set("X-CSRF-Token", "different-value")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("POST with wrong token: status = %d, want 403", w.Code)
	}
}

func TestCSRFProtectCorrectToken(t *testing.T) {
	mw := CSRFProtect()
	handler := mw(okHandler())

	token := "matching-csrf-token-value"
	req := httptest.NewRequest(http.MethodPost, "/api/servers", nil)
	req.AddCookie(&http.Cookie{Name: "csrf", Value: token})
	req.Header.Set("X-CSRF-Token", token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST with correct token: status = %d, want 200", w.Code)
	}
}

func TestCSRFProtectExemptPath(t *testing.T) {
	mw := CSRFProtect("/api/auth/login", "/api/auth/setup", "/api/auth/oidc/callback", "/healthz", "/readyz")
	handler := mw(okHandler())

	// POST to /api/auth/login without any CSRF token should pass
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST to exempt path: status = %d, want 200", w.Code)
	}
}

func TestCSRFProtectExemptSetup(t *testing.T) {
	mw := CSRFProtect("/api/auth/login", "/api/auth/setup", "/api/auth/oidc/callback", "/healthz", "/readyz")
	handler := mw(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/auth/setup", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST to exempt /api/auth/setup: status = %d, want 200", w.Code)
	}
}

func TestCSRFProtectWebSocket(t *testing.T) {
	mw := CSRFProtect()
	handler := mw(okHandler())

	// GET /ws/terminal/... without csrf query param → 403
	req := httptest.NewRequest(http.MethodGet, "/ws/terminal/server1/session1?cols=80&rows=24", nil)
	req.AddCookie(&http.Cookie{Name: "csrf", Value: "some-token"})
	// no ?csrf= param
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("WebSocket without csrf param: status = %d, want 403", w.Code)
	}
}

func TestCSRFProtectWebSocketCorrectParam(t *testing.T) {
	mw := CSRFProtect()
	handler := mw(okHandler())

	token := "ws-csrf-token-value"
	req := httptest.NewRequest(http.MethodGet, "/ws/terminal/server1/session1?cols=80&rows=24&csrf="+token, nil)
	req.AddCookie(&http.Cookie{Name: "csrf", Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("WebSocket with correct csrf param: status = %d, want 200", w.Code)
	}
}

func TestCSRFProtectWebSocketWrongParam(t *testing.T) {
	mw := CSRFProtect()
	handler := mw(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/ws/terminal/server1/session1?cols=80&rows=24&csrf=wrong-token", nil)
	req.AddCookie(&http.Cookie{Name: "csrf", Value: "correct-token"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("WebSocket with wrong csrf param: status = %d, want 403", w.Code)
	}
}

func TestCSRFProtectDeleteMethod(t *testing.T) {
	mw := CSRFProtect()
	handler := mw(okHandler())

	token := "delete-csrf-token"
	req := httptest.NewRequest(http.MethodDelete, "/api/servers/some-id", nil)
	req.AddCookie(&http.Cookie{Name: "csrf", Value: token})
	req.Header.Set("X-CSRF-Token", token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("DELETE with correct token: status = %d, want 200", w.Code)
	}
}

func TestCSRFProtectPutMethod(t *testing.T) {
	mw := CSRFProtect()
	handler := mw(okHandler())

	// PUT without token → 403
	req := httptest.NewRequest(http.MethodPut, "/api/servers/some-id/key", nil)
	req.AddCookie(&http.Cookie{Name: "csrf", Value: "token"})
	// no header
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("PUT without header: status = %d, want 403", w.Code)
	}
}

func TestCSRFProtectMissingCookie(t *testing.T) {
	mw := CSRFProtect()
	handler := mw(okHandler())

	// POST with header but no cookie → 403
	req := httptest.NewRequest(http.MethodPost, "/api/servers", nil)
	req.Header.Set("X-CSRF-Token", "some-token")
	// no csrf cookie
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("POST with header but no cookie: status = %d, want 403", w.Code)
	}
}
