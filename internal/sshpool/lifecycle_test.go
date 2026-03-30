package sshpool

import (
	"fmt"
	"net"
	"path/filepath"
	"testing"

	"github.com/wisbric/agentic-hive/internal/keystore"
	"github.com/wisbric/agentic-hive/internal/store"
	"github.com/wisbric/agentic-hive/internal/testutil"
)

func TestRemoveCleansUpConnection(t *testing.T) {
	addr := testutil.StartSSHServer(t, testutil.GenerateSigner(t))

	pool, _, serverID := setupPool(t, addr)

	// Trigger a connection.
	_, _, err := pool.Exec(t.Context(), serverID, "echo ok")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	// Verify pool has 1 connection.
	pool.mu.RLock()
	count := len(pool.conns)
	pool.mu.RUnlock()
	if count != 1 {
		t.Fatalf("expected 1 connection, got %d", count)
	}

	// Remove the server.
	pool.Remove(serverID)

	// Verify pool has 0 connections.
	pool.mu.RLock()
	count = len(pool.conns)
	pool.mu.RUnlock()
	if count != 0 {
		t.Errorf("expected 0 connections after Remove, got %d", count)
	}
}

func TestCloseCleansUpAllConnections(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ks := keystore.NewLocal(st.DB(), "test-secret-long-enough-for-aes!")
	pool := New(st, ks)

	// Start 3 SSH servers with different signers.
	serverIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		addr := testutil.StartSSHServer(t, testutil.GenerateSigner(t))

		host, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatalf("SplitHostPort: %v", err)
		}
		var port int
		fmt.Sscanf(portStr, "%d", &port)

		srv, err := st.CreateServer(
			fmt.Sprintf("server-%d", i),
			host, port, "testuser", "", "local", "",
		)
		if err != nil {
			t.Fatalf("CreateServer[%d]: %v", i, err)
		}

		if err := ks.Put(t.Context(), srv.ID, testutil.NewClientKeyPEM(t)); err != nil {
			t.Fatalf("keystore.Put[%d]: %v", i, err)
		}

		serverIDs[i] = srv.ID
	}

	// Connect to all 3 servers.
	for i, id := range serverIDs {
		if _, _, err := pool.Exec(t.Context(), id, "echo ok"); err != nil {
			t.Fatalf("Exec[%d]: %v", i, err)
		}
	}

	// Verify 3 connections.
	pool.mu.RLock()
	count := len(pool.conns)
	pool.mu.RUnlock()
	if count != 3 {
		t.Fatalf("expected 3 connections, got %d", count)
	}

	// Close the pool.
	pool.Close()

	// Verify 0 connections.
	pool.mu.RLock()
	count = len(pool.conns)
	pool.mu.RUnlock()
	if count != 0 {
		t.Errorf("expected 0 connections after Close, got %d", count)
	}
}
