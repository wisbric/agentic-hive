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

	// Verify the schema_version table exists and has the latest version
	var version int
	err = s.db.QueryRow("SELECT version FROM schema_version ORDER BY version DESC LIMIT 1").Scan(&version)
	if err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	if version < 2 {
		t.Errorf("schema version = %d, want >= 2", version)
	}

	// Verify core tables exist by querying them
	tables := []string{"users", "servers", "ssh_keys", "session_templates", "host_keys"}
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

func TestCreateAndGetServer(t *testing.T) {
	s := testStore(t)

	srv, err := s.CreateServer("devbox", "devbox.wisbric.com", 22, "stefan")
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if srv.Name != "devbox" {
		t.Errorf("Name = %q, want %q", srv.Name, "devbox")
	}
	if srv.Status != "unknown" {
		t.Errorf("Status = %q, want %q", srv.Status, "unknown")
	}

	got, err := s.GetServer(srv.ID)
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}
	if got.Host != "devbox.wisbric.com" {
		t.Errorf("Host = %q, want %q", got.Host, "devbox.wisbric.com")
	}
}

func TestListServers(t *testing.T) {
	s := testStore(t)

	_, _ = s.CreateServer("a", "a.example.com", 22, "root")
	_, _ = s.CreateServer("b", "b.example.com", 2222, "deploy")

	servers, err := s.ListServers()
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}
	if len(servers) != 2 {
		t.Errorf("len = %d, want 2", len(servers))
	}
}

func TestDeleteServer(t *testing.T) {
	s := testStore(t)

	srv, _ := s.CreateServer("del", "del.example.com", 22, "root")
	if err := s.DeleteServer(srv.ID); err != nil {
		t.Fatalf("DeleteServer failed: %v", err)
	}

	servers, _ := s.ListServers()
	if len(servers) != 0 {
		t.Errorf("len = %d, want 0 after delete", len(servers))
	}
}

func TestUpdateServerStatus(t *testing.T) {
	s := testStore(t)

	srv, _ := s.CreateServer("s1", "s1.example.com", 22, "root")
	if err := s.UpdateServerStatus(srv.ID, "reachable"); err != nil {
		t.Fatalf("UpdateServerStatus failed: %v", err)
	}

	got, _ := s.GetServer(srv.ID)
	if got.Status != "reachable" {
		t.Errorf("Status = %q, want %q", got.Status, "reachable")
	}
}

func TestUpsertOIDCUser(t *testing.T) {
	s := testStore(t)

	user, err := s.UpsertOIDCUser("oidc-sub-123", "stefan", "user")
	if err != nil {
		t.Fatalf("first UpsertOIDCUser failed: %v", err)
	}
	if user.Username != "stefan" {
		t.Errorf("Username = %q, want %q", user.Username, "stefan")
	}
	if user.Role != "user" {
		t.Errorf("Role = %q, want %q", user.Role, "user")
	}

	user2, err := s.UpsertOIDCUser("oidc-sub-123", "stefan-updated", "admin")
	if err != nil {
		t.Fatalf("second UpsertOIDCUser failed: %v", err)
	}
	if user2.ID != user.ID {
		t.Errorf("ID changed: %q -> %q", user.ID, user2.ID)
	}
	if user2.Username != "stefan-updated" {
		t.Errorf("Username = %q, want %q", user2.Username, "stefan-updated")
	}
	if user2.Role != "admin" {
		t.Errorf("Role = %q, want %q", user2.Role, "admin")
	}
}

func TestSeedAndListTemplates(t *testing.T) {
	s := testStore(t)

	if err := s.SeedTemplates(); err != nil {
		t.Fatalf("SeedTemplates failed: %v", err)
	}

	templates, err := s.ListTemplates("")
	if err != nil {
		t.Fatalf("ListTemplates failed: %v", err)
	}
	if len(templates) < 3 {
		t.Errorf("expected at least 3 default templates, got %d", len(templates))
	}

	// Seed again should be idempotent
	if err := s.SeedTemplates(); err != nil {
		t.Fatalf("second SeedTemplates failed: %v", err)
	}

	templates2, _ := s.ListTemplates("")
	if len(templates2) != len(templates) {
		t.Errorf("template count changed after re-seed: %d -> %d", len(templates), len(templates2))
	}
}
