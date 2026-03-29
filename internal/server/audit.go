package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/wisbric/agentic-hive/internal/auth"
	"github.com/wisbric/agentic-hive/internal/store"
)

// logAudit writes an audit entry to the store and emits an slog message.
// user may be nil (e.g. for failed logins where the session is not set).
func logAudit(s *Server, r *http.Request, user *auth.Claims, action, targetType, targetID, details string) {
	entry := store.AuditEntry{
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Details:    details,
		IPAddress:  clientIP(r),
	}
	if user != nil {
		entry.UserID = user.UserID
		entry.Username = user.Username
	}
	if err := s.store.LogAudit(entry); err != nil {
		slog.Error("audit log write failed", "action", action, "error", err)
	}
	slog.Info("audit",
		"action", action,
		"user_id", entry.UserID,
		"username", entry.Username,
		"target_type", targetType,
		"target_id", targetID,
		"ip", entry.IPAddress,
	)
}

func (s *Server) handleListAuditLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := store.AuditFilter{}

	if v := q.Get("user_id"); v != "" {
		filter.UserID = v
	}
	if v := q.Get("action"); v != "" {
		filter.Action = v
	}
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			jsonError(w, "invalid since: use RFC3339", http.StatusBadRequest)
			return
		}
		filter.Since = t
	}
	if v := q.Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			jsonError(w, "invalid until: use RFC3339", http.StatusBadRequest)
			return
		}
		filter.Until = t
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			jsonError(w, "invalid limit", http.StatusBadRequest)
			return
		}
		filter.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			jsonError(w, "invalid offset", http.StatusBadRequest)
			return
		}
		filter.Offset = n
	}

	total, err := s.store.CountAuditLog(filter)
	if err != nil {
		jsonError(w, "failed to count audit log", http.StatusInternalServerError)
		return
	}

	entries, err := s.store.ListAuditLog(filter)
	if err != nil {
		jsonError(w, "failed to list audit log", http.StatusInternalServerError)
		return
	}

	// Return an empty array rather than null when there are no entries.
	if entries == nil {
		entries = []store.AuditEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"entries": entries,
		"total":   total,
	})
}
