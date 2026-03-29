package keystore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"
)

func setupTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	srv, err := st.CreateServer("test", "test.example.com", 22, "root", "", "local", "")
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	return st.DB(), srv.ID
}

func TestLocalPutAndGet(t *testing.T) {
	db, serverID := setupTestDB(t)
	ks := NewLocal(db, "test-secret-that-is-long-enough!")
	ctx := context.Background()

	key := []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nfake-key-data\n-----END OPENSSH PRIVATE KEY-----")

	if err := ks.Put(ctx, serverID, key); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, err := ks.Get(ctx, serverID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(got) != string(key) {
		t.Errorf("key mismatch: got %q, want %q", string(got), string(key))
	}
}

func TestLocalPutOverwrite(t *testing.T) {
	db, serverID := setupTestDB(t)
	ks := NewLocal(db, "test-secret-that-is-long-enough!")
	ctx := context.Background()

	_ = ks.Put(ctx, serverID, []byte("key-v1"))
	_ = ks.Put(ctx, serverID, []byte("key-v2"))

	got, _ := ks.Get(ctx, serverID)
	if string(got) != "key-v2" {
		t.Errorf("expected key-v2, got %q", string(got))
	}
}

func TestLocalGetNotFound(t *testing.T) {
	db, _ := setupTestDB(t)
	ks := NewLocal(db, "test-secret-that-is-long-enough!")

	_, err := ks.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
}

func TestLocalDelete(t *testing.T) {
	db, serverID := setupTestDB(t)
	ks := NewLocal(db, "test-secret-that-is-long-enough!")
	ctx := context.Background()

	_ = ks.Put(ctx, serverID, []byte("key"))
	if err := ks.Delete(ctx, serverID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err := ks.Get(ctx, serverID)
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestLocalWrongSecret(t *testing.T) {
	db, serverID := setupTestDB(t)
	ctx := context.Background()

	ks1 := NewLocal(db, "secret-one-long-enough-for-test!")
	_ = ks1.Put(ctx, serverID, []byte("sensitive-key"))

	ks2 := NewLocal(db, "secret-two-long-enough-for-test!")
	_, err := ks2.Get(ctx, serverID)
	if err == nil {
		t.Error("expected error with wrong secret")
	}
}
