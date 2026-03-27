package auth

import (
	"net/http"
	"sync"
)

// SwappableOIDCHandler wraps an OIDCHandler that can be hot-swapped.
// If no handler is configured, it returns 404.
type SwappableOIDCHandler struct {
	mu      sync.RWMutex
	handler *OIDCHandler
}

func NewSwappableOIDCHandler(h *OIDCHandler) *SwappableOIDCHandler {
	return &SwappableOIDCHandler{handler: h}
}

func (s *SwappableOIDCHandler) Swap(h *OIDCHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
}

func (s *SwappableOIDCHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	h := s.handler
	s.mu.RUnlock()
	if h == nil {
		http.Error(w, `{"error":"OIDC not configured"}`, http.StatusNotFound)
		return
	}
	h.HandleLogin(w, r)
}

func (s *SwappableOIDCHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	h := s.handler
	s.mu.RUnlock()
	if h == nil {
		http.Error(w, `{"error":"OIDC not configured"}`, http.StatusNotFound)
		return
	}
	h.HandleCallback(w, r)
}

func (s *SwappableOIDCHandler) IsConfigured() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.handler != nil
}
