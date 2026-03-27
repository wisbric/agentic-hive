package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"
)

func TestAuditLogEndpointAdminOnly(t *testing.T) {
	srv := testServer(t)
	userCookie := makeSessionCookie(t, store.RoleUser)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit", nil)
	req.AddCookie(userCookie)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("GET /api/admin/audit with user role: status = %d, want 403", w.Code)
	}
}

func TestAuditLogEndpointRequiresAuth(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/admin/audit without auth: status = %d, want 401", w.Code)
	}
}

func TestAuditLogEndpointAdminEmpty(t *testing.T) {
	srv := testServer(t)
	adminCookie := makeSessionCookie(t, store.RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit", nil)
	req.AddCookie(adminCookie)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/audit with admin: status = %d, want 200", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	entries, ok := body["entries"]
	if !ok {
		t.Fatal("response missing 'entries' key")
	}
	// entries should be an array (empty or not)
	if _, ok := entries.([]any); !ok {
		t.Errorf("entries is not an array: %T", entries)
	}

	total, ok := body["total"]
	if !ok {
		t.Fatal("response missing 'total' key")
	}
	if _, ok := total.(float64); !ok {
		t.Errorf("total is not a number: %T", total)
	}
}

func TestAuditLogEndpointReturnsEntries(t *testing.T) {
	srv := testServer(t)
	adminCookie := makeSessionCookie(t, store.RoleAdmin)

	// Seed some audit entries directly via the store
	_ = srv.store.LogAudit(store.AuditEntry{
		Action:    store.AuditAuthLogin,
		UserID:    "u1",
		Username:  "alice",
		IPAddress: "127.0.0.1",
	})
	_ = srv.store.LogAudit(store.AuditEntry{
		Action:    store.AuditServerCreate,
		UserID:    "u1",
		Username:  "alice",
		IPAddress: "127.0.0.1",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit", nil)
	req.AddCookie(adminCookie)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	entries := body["entries"].([]any)
	if len(entries) != 2 {
		t.Errorf("entries len = %d, want 2", len(entries))
	}

	total := int(body["total"].(float64))
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
}

func TestAuditLogEndpointFilterByAction(t *testing.T) {
	srv := testServer(t)
	adminCookie := makeSessionCookie(t, store.RoleAdmin)

	_ = srv.store.LogAudit(store.AuditEntry{Action: store.AuditAuthLogin, UserID: "u1"})
	_ = srv.store.LogAudit(store.AuditEntry{Action: store.AuditServerCreate, UserID: "u1"})
	_ = srv.store.LogAudit(store.AuditEntry{Action: store.AuditAuthLogin, UserID: "u2"})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit?action=auth.login", nil)
	req.AddCookie(adminCookie)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	entries := body["entries"].([]any)
	if len(entries) != 2 {
		t.Errorf("filtered entries len = %d, want 2", len(entries))
	}
	total := int(body["total"].(float64))
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
}

func TestAuditLogEndpointInvalidLimit(t *testing.T) {
	srv := testServer(t)
	adminCookie := makeSessionCookie(t, store.RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit?limit=bad", nil)
	req.AddCookie(adminCookie)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid limit: status = %d, want 400", w.Code)
	}
}

func TestAuditLogEndpointInvalidSince(t *testing.T) {
	srv := testServer(t)
	adminCookie := makeSessionCookie(t, store.RoleAdmin)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit?since=not-a-date", nil)
	req.AddCookie(adminCookie)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid since: status = %d, want 400", w.Code)
	}
}

func TestAuditLogPagination(t *testing.T) {
	srv := testServer(t)
	adminCookie := makeSessionCookie(t, store.RoleAdmin)

	for i := 0; i < 5; i++ {
		_ = srv.store.LogAudit(store.AuditEntry{Action: store.AuditAuthLogin})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit?limit=2&offset=0", nil)
	req.AddCookie(adminCookie)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	entries := body["entries"].([]any)
	if len(entries) != 2 {
		t.Errorf("page1 len = %d, want 2", len(entries))
	}
	// total should reflect all 5, not just the page
	total := int(body["total"].(float64))
	if total != 5 {
		t.Errorf("total = %d, want 5 (all entries, not just page)", total)
	}
}

func TestClientIPHelper(t *testing.T) {
	tests := []struct {
		name     string
		xff      string
		xri      string
		remote   string
		expected string
	}{
		{"xff single", "1.2.3.4", "", "10.0.0.1:9999", "1.2.3.4"},
		{"xff multi", "1.2.3.4, 5.6.7.8", "", "10.0.0.1:9999", "1.2.3.4"},
		{"x-real-ip", "", "9.9.9.9", "10.0.0.1:9999", "9.9.9.9"},
		{"remote addr", "", "", "192.168.1.1:12345", "192.168.1.1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remote
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xri != "" {
				req.Header.Set("X-Real-IP", tc.xri)
			}
			got := clientIP(req)
			if got != tc.expected {
				t.Errorf("clientIP = %q, want %q", got, tc.expected)
			}
		})
	}
}
