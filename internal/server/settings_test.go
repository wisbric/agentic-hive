package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"
)

// makeAdminRequest constructs an HTTP request with admin session + CSRF tokens.
func makeAdminRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	var reqBody *strings.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	} else {
		reqBody = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeSessionCookie(t, store.RoleAdmin))
	csrfToken := "settings-test-csrf-token"
	req.AddCookie(&http.Cookie{Name: "csrf", Value: csrfToken})
	req.Header.Set("X-CSRF-Token", csrfToken)
	return req
}

// TestGetSettings verifies the GET /api/admin/settings endpoint returns resolved settings.
func TestGetSettings(t *testing.T) {
	srv := testServer(t)
	req := makeAdminRequest(t, http.MethodGet, "/api/admin/settings", "")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Top-level keys must be present.
	for _, key := range []string{"oidc", "vault", "general"} {
		if _, ok := body[key]; !ok {
			t.Errorf("response missing key %q", key)
		}
	}

	// General section must contain poll_interval with a source.
	general, ok := body["general"].(map[string]any)
	if !ok {
		t.Fatalf("general is not a map: %T", body["general"])
	}
	pollInterval, ok := general["poll_interval"].(map[string]any)
	if !ok {
		t.Fatalf("general.poll_interval is not a map: %T", general["poll_interval"])
	}
	if pollInterval["source"] == "" {
		t.Error("general.poll_interval.source must not be empty")
	}
}

// TestGetSettingsRequiresAdmin verifies that non-admin users cannot read settings.
func TestGetSettingsRequiresAdmin(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil)
	req.AddCookie(makeSessionCookie(t, store.RoleUser))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for non-admin", w.Code)
	}
}

// TestUpdateSettings saves a setting and verifies the response contains the updated value.
func TestUpdateSettings(t *testing.T) {
	srv := testServer(t)

	body := `{"general.log_level": "debug"}`
	req := makeAdminRequest(t, http.MethodPut, "/api/admin/settings", body)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Check it was persisted.
	val, err := srv.store.GetSetting("general.log_level")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if val != "debug" {
		t.Errorf("stored value = %q, want %q", val, "debug")
	}

	// Response should include resolved settings.
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	general, ok := resp["general"].(map[string]any)
	if !ok {
		t.Fatalf("general is not a map: %T", resp["general"])
	}
	logLevel, ok := general["log_level"].(map[string]any)
	if !ok {
		t.Fatalf("general.log_level is not a map: %T", general["log_level"])
	}
	if logLevel["value"] != "debug" {
		t.Errorf("general.log_level.value = %q, want %q", logLevel["value"], "debug")
	}
	if logLevel["source"] != "db" {
		t.Errorf("general.log_level.source = %q, want %q", logLevel["source"], "db")
	}
}

// TestUpdateSettingsEmptyBody rejects an empty or missing body.
func TestUpdateSettingsEmptyBody(t *testing.T) {
	srv := testServer(t)

	req := makeAdminRequest(t, http.MethodPut, "/api/admin/settings", `{}`)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for empty map", w.Code)
	}
}

// TestUpdateSettingsPollInterval verifies that updating general.poll_interval
// calls UpdateInterval on the session manager.
func TestUpdateSettingsPollInterval(t *testing.T) {
	srv := testServer(t)

	// Capture the interval signal by draining the channel after the call.
	// We use a fresh goroutine to receive the value sent by UpdateInterval.
	intervalReceived := make(chan time.Duration, 1)
	go func() {
		// UpdateInterval sends on intervalCh; drain it here.
		select {
		case d := <-srv.sessions.IntervalCh():
			intervalReceived <- d
		case <-time.After(2 * time.Second):
			// nothing received
			close(intervalReceived)
		}
	}()

	body := `{"general.poll_interval": "120"}`
	req := makeAdminRequest(t, http.MethodPut, "/api/admin/settings", body)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	select {
	case d := <-intervalReceived:
		if d != 120*time.Second {
			t.Errorf("interval = %v, want 120s", d)
		}
	case <-time.After(2 * time.Second):
		t.Error("UpdateInterval was not called within 2 seconds")
	}
}

// TestTestOIDCInvalidIssuer returns a non-200 status when the issuer URL is unreachable.
func TestTestOIDCInvalidIssuer(t *testing.T) {
	srv := testServer(t)

	body := `{"issuer_url": "https://invalid.example.test/oidc"}`
	req := makeAdminRequest(t, http.MethodPost, "/api/admin/settings/test-oidc", body)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	// Must return 502 (bad gateway) — OIDC discovery failed.
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 for unreachable OIDC issuer", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["ok"] != false {
		t.Errorf("ok = %v, want false", resp["ok"])
	}
	if resp["error"] == "" {
		t.Error("error field must not be empty")
	}
}

// TestTestOIDCMissingIssuer returns 400 when no issuer_url is provided and config is empty.
func TestTestOIDCMissingIssuer(t *testing.T) {
	srv := testServer(t)

	body := `{}`
	req := makeAdminRequest(t, http.MethodPost, "/api/admin/settings/test-oidc", body)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when issuer_url is missing", w.Code)
	}
}

// TestTestVaultInvalid returns 502 when the Vault address is unreachable.
func TestTestVaultInvalid(t *testing.T) {
	srv := testServer(t)

	body := `{"address": "http://127.0.0.1:19999", "token": "fake-token"}`
	req := makeAdminRequest(t, http.MethodPost, "/api/admin/settings/test-vault", body)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 for unreachable Vault", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["ok"] != false {
		t.Errorf("ok = %v, want false", resp["ok"])
	}
	if resp["error"] == "" {
		t.Error("error field must not be empty")
	}
}

// TestTestVaultMissingAddress returns 400 when no address is provided.
func TestTestVaultMissingAddress(t *testing.T) {
	srv := testServer(t)

	body := `{}`
	req := makeAdminRequest(t, http.MethodPost, "/api/admin/settings/test-vault", body)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when address is missing", w.Code)
	}
}
