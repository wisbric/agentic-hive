package store

import (
	"fmt"
	"math/rand"
	"time"
)

// Audit action constants.
const (
	AuditAuthLogin        = "auth.login"
	AuditAuthLoginFailed  = "auth.login_failed"
	AuditAuthLogout       = "auth.logout"
	AuditServerCreate     = "server.create"
	AuditServerDelete     = "server.delete"
	AuditServerKeyUpload  = "server.key_upload"
	AuditSessionCreate    = "session.create"
	AuditSessionKill      = "session.kill"
	AuditTerminalConnect  = "terminal.connect"
	AuditTerminalDisconnect = "terminal.disconnect"
)

// AuditFilter controls which audit log entries are returned.
type AuditFilter struct {
	UserID string
	Action string
	Since  time.Time
	Until  time.Time
	Limit  int
	Offset int
}

// LogAudit inserts an audit entry. If entry.ID is empty a unique ID is generated.
func (s *Store) LogAudit(entry AuditEntry) error {
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("%x-%x", time.Now().UnixNano(), rand.Int63()) //nolint:gosec
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	_, err := s.db.Exec(
		`INSERT INTO audit_log (id, timestamp, user_id, username, action, target_type, target_id, details, ip_address)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID,
		entry.Timestamp,
		entry.UserID,
		entry.Username,
		entry.Action,
		entry.TargetType,
		entry.TargetID,
		entry.Details,
		entry.IPAddress,
	)
	return err
}

// ListAuditLog returns audit entries matching the filter.
// Limit defaults to 100 and is capped at 500.
func (s *Store) ListAuditLog(filter AuditFilter) ([]AuditEntry, error) {
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}

	query := `SELECT id, timestamp, user_id, username, action, target_type, target_id, details, ip_address
	          FROM audit_log WHERE 1=1`
	args := []any{}

	if filter.UserID != "" {
		query += " AND user_id = ?"
		args = append(args, filter.UserID)
	}
	if filter.Action != "" {
		query += " AND action = ?"
		args = append(args, filter.Action)
	}
	if !filter.Since.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.Since)
	}
	if !filter.Until.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, filter.Until)
	}

	query += " ORDER BY timestamp DESC LIMIT ? OFFSET ?"
	args = append(args, filter.Limit, filter.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.UserID, &e.Username, &e.Action, &e.TargetType, &e.TargetID, &e.Details, &e.IPAddress); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// CountAuditLog returns the total number of entries matching the filter (ignoring Limit/Offset).
func (s *Store) CountAuditLog(filter AuditFilter) (int, error) {
	query := `SELECT COUNT(*) FROM audit_log WHERE 1=1`
	args := []any{}

	if filter.UserID != "" {
		query += " AND user_id = ?"
		args = append(args, filter.UserID)
	}
	if filter.Action != "" {
		query += " AND action = ?"
		args = append(args, filter.Action)
	}
	if !filter.Since.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.Since)
	}
	if !filter.Until.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, filter.Until)
	}

	var count int
	err := s.db.QueryRow(query, args...).Scan(&count)
	return count, err
}
