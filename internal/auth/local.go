package auth

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/metrics"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// clientIPFromRequest extracts the real client IP.
func clientIPFromRequest(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func logAuthAudit(st *store.Store, r *http.Request, action, userID, username string) {
	entry := store.AuditEntry{
		Action:    action,
		UserID:    userID,
		Username:  username,
		IPAddress: clientIPFromRequest(r),
	}
	if err := st.LogAudit(entry); err != nil {
		slog.Error("audit log write failed", "action", action, "error", err)
	}
	slog.Info("audit",
		"action", action,
		"user_id", userID,
		"username", username,
		"ip", entry.IPAddress,
	)
}

type LocalHandler struct {
	store  *store.Store
	secret string
}

func NewLocalHandler(st *store.Store, secret string) *LocalHandler {
	return &LocalHandler{store: st, secret: secret}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *LocalHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	user, err := h.store.GetUserByUsername(req.Username)
	if err != nil {
		if metrics.AuthFailuresTotal != nil {
			metrics.AuthFailuresTotal.WithLabelValues("invalid_credentials").Inc()
		}
		logAuthAudit(h.store, r, store.AuditAuthLoginFailed, "", req.Username)
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		if metrics.AuthFailuresTotal != nil {
			metrics.AuthFailuresTotal.WithLabelValues("invalid_credentials").Inc()
		}
		logAuthAudit(h.store, r, store.AuditAuthLoginFailed, user.ID, user.Username)
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	token, err := SignJWT(&Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
	}, h.secret, SessionTTL)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	SetSessionCookie(w, token, http.SameSiteStrictMode)
	if _, err := SetCSRFCookie(w); err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	logAuthAudit(h.store, r, store.AuditAuthLogin, user.ID, user.Username)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "ok",
		"username": user.Username,
		"role":     user.Role,
	})
}

type setupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *LocalHandler) SetupNeeded() bool {
	count, err := h.store.UserCount()
	if err != nil {
		return false
	}
	return count == 0
}

func (h *LocalHandler) HandleSetup(w http.ResponseWriter, r *http.Request) {
	if !h.SetupNeeded() {
		http.Error(w, `{"error":"setup already completed"}`, http.StatusForbidden)
		return
	}

	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" {
		http.Error(w, `{"error":"username and password required"}`, http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	user, err := h.store.CreateUser(req.Username, string(hash), store.RoleAdmin)
	if err != nil {
		http.Error(w, `{"error":"failed to create user"}`, http.StatusInternalServerError)
		return
	}

	token, err := SignJWT(&Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
	}, h.secret, SessionTTL)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	SetSessionCookie(w, token, http.SameSiteStrictMode)
	if _, err := SetCSRFCookie(w); err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "ok",
		"username": user.Username,
		"role":     user.Role,
	})
}
