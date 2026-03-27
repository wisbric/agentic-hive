package auth

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestSwappableOIDCHandler_NilReturns404(t *testing.T) {
	s := NewSwappableOIDCHandler(nil)

	for _, path := range []string{"login", "callback"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/"+path, nil)

		if path == "login" {
			s.HandleLogin(rec, req)
		} else {
			s.HandleCallback(rec, req)
		}

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", path, rec.Code)
		}
	}
}

func TestSwappableOIDCHandler_IsConfigured(t *testing.T) {
	s := NewSwappableOIDCHandler(nil)
	if s.IsConfigured() {
		t.Error("IsConfigured should be false with nil handler")
	}

	// Swap in a non-nil handler (concrete type doesn't matter for this test).
	s.Swap(&OIDCHandler{})
	if !s.IsConfigured() {
		t.Error("IsConfigured should be true after Swap with non-nil handler")
	}

	// Swap back to nil.
	s.Swap(nil)
	if s.IsConfigured() {
		t.Error("IsConfigured should be false after Swap with nil")
	}
}

func TestSwappableOIDCHandler_ConcurrentAccess(t *testing.T) {
	s := NewSwappableOIDCHandler(nil)

	var wg sync.WaitGroup
	// Concurrent readers: handler is always nil so we only get 404 responses.
	// This exercises the RLock/RUnlock path without risking nil-field panics
	// inside OIDCHandler (which requires a real OIDC provider to be functional).
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/login", nil)
			s.HandleLogin(rec, req)
			_ = s.IsConfigured()
		}()
	}
	// Concurrent writers swapping between nil values to test the Lock path.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Swap(nil)
			s.Swap(nil)
		}()
	}
	wg.Wait()
}
