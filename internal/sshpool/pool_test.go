package sshpool

import (
	"path/filepath"
	"testing"

	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/keystore"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"
)

func TestNewPool(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	defer st.Close()

	ks := keystore.NewLocal(st.DB(), "test-secret-long-enough-for-aes!")

	pool := New(st, ks)
	if pool == nil {
		t.Fatal("New returned nil")
	}
	defer pool.Close()

	// Pool should start empty
	pool.mu.RLock()
	count := len(pool.conns)
	pool.mu.RUnlock()

	if count != 0 {
		t.Errorf("expected 0 connections, got %d", count)
	}
}

func TestPoolRemoveNonexistent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	defer st.Close()

	ks := keystore.NewLocal(st.DB(), "test-secret-long-enough-for-aes!")
	pool := New(st, ks)
	defer pool.Close()

	// Should not panic
	pool.Remove("nonexistent")
}
