package store

import (
	"testing"
	"time"
)

func TestLogAndListAuditLog(t *testing.T) {
	s := testStore(t)

	// Insert two entries
	err := s.LogAudit(AuditEntry{
		UserID:     "u1",
		Username:   "alice",
		Action:     AuditAuthLogin,
		TargetType: "",
		IPAddress:  "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("LogAudit failed: %v", err)
	}

	err = s.LogAudit(AuditEntry{
		UserID:     "u2",
		Username:   "bob",
		Action:     AuditServerCreate,
		TargetType: "server",
		TargetID:   "srv1",
		Details:    `{"name":"prod"}`,
		IPAddress:  "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("LogAudit (2) failed: %v", err)
	}

	entries, err := s.ListAuditLog(AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditLog failed: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("len = %d, want 2", len(entries))
	}
}

func TestAuditLogIDGenerated(t *testing.T) {
	s := testStore(t)

	err := s.LogAudit(AuditEntry{Action: AuditAuthLogout})
	if err != nil {
		t.Fatalf("LogAudit failed: %v", err)
	}

	entries, err := s.ListAuditLog(AuditFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListAuditLog failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one entry")
	}
	if entries[0].ID == "" {
		t.Error("ID should be auto-generated and non-empty")
	}
}

func TestAuditLogFilterByAction(t *testing.T) {
	s := testStore(t)

	_ = s.LogAudit(AuditEntry{Action: AuditAuthLogin, UserID: "u1"})
	_ = s.LogAudit(AuditEntry{Action: AuditServerCreate, UserID: "u1"})
	_ = s.LogAudit(AuditEntry{Action: AuditAuthLogin, UserID: "u2"})

	entries, err := s.ListAuditLog(AuditFilter{Action: AuditAuthLogin})
	if err != nil {
		t.Fatalf("ListAuditLog failed: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("filtered by action: len = %d, want 2", len(entries))
	}
	for _, e := range entries {
		if e.Action != AuditAuthLogin {
			t.Errorf("unexpected action %q in filtered result", e.Action)
		}
	}
}

func TestAuditLogFilterByUserID(t *testing.T) {
	s := testStore(t)

	_ = s.LogAudit(AuditEntry{Action: AuditAuthLogin, UserID: "u1", Username: "alice"})
	_ = s.LogAudit(AuditEntry{Action: AuditAuthLogin, UserID: "u2", Username: "bob"})
	_ = s.LogAudit(AuditEntry{Action: AuditAuthLogout, UserID: "u1", Username: "alice"})

	entries, err := s.ListAuditLog(AuditFilter{UserID: "u1"})
	if err != nil {
		t.Fatalf("ListAuditLog failed: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("filtered by user_id: len = %d, want 2", len(entries))
	}
}

func TestAuditLogFilterBySince(t *testing.T) {
	s := testStore(t)

	// Insert entry in the past using explicit timestamp
	past := time.Now().UTC().Add(-2 * time.Hour)
	_ = s.LogAudit(AuditEntry{
		Action:    AuditAuthLogin,
		Timestamp: past,
	})
	// Insert entry "now"
	_ = s.LogAudit(AuditEntry{
		Action: AuditServerCreate,
		// Timestamp zero → defaults to now in LogAudit
	})

	// Both entries should be returned when no filter
	all, err := s.ListAuditLog(AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditLog (all) failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 entries before filtering, got %d", len(all))
	}

	// Filter Since 1 hour ago — should exclude the 2-hour-ago entry
	// We use the timestamp stored in the "past" entry's row as reference
	sinceFilter := past.Add(30 * time.Minute) // 90 min ago, excludes -2h entry
	entries, err := s.ListAuditLog(AuditFilter{Since: sinceFilter})
	if err != nil {
		t.Fatalf("ListAuditLog (since) failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("filtered by since: len = %d, want 1", len(entries))
	}
	if len(entries) > 0 && entries[0].Action != AuditServerCreate {
		t.Errorf("expected server.create entry, got %q", entries[0].Action)
	}
}

func TestAuditLogLimitDefault(t *testing.T) {
	s := testStore(t)

	for i := 0; i < 5; i++ {
		_ = s.LogAudit(AuditEntry{Action: AuditAuthLogin})
	}

	// Default limit is 100 — all 5 entries should be returned
	entries, err := s.ListAuditLog(AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditLog failed: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("len = %d, want 5", len(entries))
	}
}

func TestAuditLogLimitCap(t *testing.T) {
	s := testStore(t)

	for i := 0; i < 3; i++ {
		_ = s.LogAudit(AuditEntry{Action: AuditAuthLogin})
	}

	// Requesting more than 500 is capped at 500 — but we only have 3, so we get 3
	entries, err := s.ListAuditLog(AuditFilter{Limit: 1000})
	if err != nil {
		t.Fatalf("ListAuditLog failed: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("len = %d, want 3", len(entries))
	}
}

func TestAuditLogPagination(t *testing.T) {
	s := testStore(t)

	for i := 0; i < 5; i++ {
		_ = s.LogAudit(AuditEntry{Action: AuditAuthLogin})
	}

	page1, err := s.ListAuditLog(AuditFilter{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("page1 failed: %v", err)
	}
	if len(page1) != 2 {
		t.Errorf("page1 len = %d, want 2", len(page1))
	}

	page2, err := s.ListAuditLog(AuditFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("page2 failed: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("page2 len = %d, want 2", len(page2))
	}

	page3, err := s.ListAuditLog(AuditFilter{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("page3 failed: %v", err)
	}
	if len(page3) != 1 {
		t.Errorf("page3 len = %d, want 1", len(page3))
	}

	// IDs must not overlap between pages
	seen := map[string]bool{}
	for _, e := range append(append(page1, page2...), page3...) {
		if seen[e.ID] {
			t.Errorf("duplicate ID %q across pages", e.ID)
		}
		seen[e.ID] = true
	}
}

func TestCountAuditLog(t *testing.T) {
	s := testStore(t)

	_ = s.LogAudit(AuditEntry{Action: AuditAuthLogin, UserID: "u1"})
	_ = s.LogAudit(AuditEntry{Action: AuditAuthLoginFailed, UserID: "u1"})
	_ = s.LogAudit(AuditEntry{Action: AuditAuthLogin, UserID: "u2"})

	total, err := s.CountAuditLog(AuditFilter{})
	if err != nil {
		t.Fatalf("CountAuditLog failed: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}

	filtered, err := s.CountAuditLog(AuditFilter{Action: AuditAuthLogin})
	if err != nil {
		t.Fatalf("CountAuditLog (filtered) failed: %v", err)
	}
	if filtered != 2 {
		t.Errorf("filtered total = %d, want 2", filtered)
	}
}

func TestAuditLogTableExists(t *testing.T) {
	s := testStore(t)
	_, err := s.db.Exec("SELECT COUNT(*) FROM audit_log")
	if err != nil {
		t.Errorf("audit_log table does not exist: %v", err)
	}
}
