package store

import (
	"os"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenAndMigrate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", dbPath, err)
	}
	defer s.Close()

	// Verify the schema_version table exists and has version 1
	var version int
	err = s.db.QueryRow("SELECT version FROM schema_version ORDER BY version DESC LIMIT 1").Scan(&version)
	if err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	if version != 1 {
		t.Errorf("schema version = %d, want 1", version)
	}

	// Verify core tables exist by querying them
	tables := []string{"users", "servers", "ssh_keys", "session_templates"}
	for _, table := range tables {
		_, err := s.db.Exec("SELECT COUNT(*) FROM " + table)
		if err != nil {
			t.Errorf("table %q does not exist: %v", table, err)
		}
	}
}

func TestOpenCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", dbPath, err)
	}
	defer s.Close()

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("directory %q was not created", dir)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}
	s.Close()

	// Open again — migrations should not fail
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open failed: %v", err)
	}
	defer s2.Close()
}

func TestCreateAndGetUser(t *testing.T) {
	s := testStore(t)

	user, err := s.CreateUser("admin", "hashedpw", "admin")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if user.Username != "admin" {
		t.Errorf("Username = %q, want %q", user.Username, "admin")
	}
	if user.Role != "admin" {
		t.Errorf("Role = %q, want %q", user.Role, "admin")
	}
	if user.ID == "" {
		t.Error("ID should not be empty")
	}

	got, err := s.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("GetUserByUsername failed: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("got ID = %q, want %q", got.ID, user.ID)
	}
	if got.PasswordHash != "hashedpw" {
		t.Errorf("PasswordHash = %q, want %q", got.PasswordHash, "hashedpw")
	}
}

func TestCreateUserDuplicate(t *testing.T) {
	s := testStore(t)

	_, err := s.CreateUser("admin", "pw1", "admin")
	if err != nil {
		t.Fatalf("first CreateUser failed: %v", err)
	}

	_, err = s.CreateUser("admin", "pw2", "admin")
	if err == nil {
		t.Error("expected error for duplicate username, got nil")
	}
}

func TestUserCount(t *testing.T) {
	s := testStore(t)

	count, err := s.UserCount()
	if err != nil {
		t.Fatalf("UserCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}

	_, _ = s.CreateUser("u1", "pw", "user")
	_, _ = s.CreateUser("u2", "pw", "admin")

	count, err = s.UserCount()
	if err != nil {
		t.Fatalf("UserCount failed: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}
