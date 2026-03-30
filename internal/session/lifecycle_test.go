package session

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wisbric/agentic-hive/internal/keystore"
	"github.com/wisbric/agentic-hive/internal/sshpool"
	"github.com/wisbric/agentic-hive/internal/store"
)

func TestStopTerminatesPollingGoroutine(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	ks := keystore.NewLocal(st.DB(), "test-secret-long-enough-for-aes!")
	pool := sshpool.New(st, ks)

	m := NewManager(st, pool)
	m.StartPolling(t.Context(), 50*time.Millisecond)

	// Let the goroutine start and run at least one poll cycle.
	time.Sleep(100 * time.Millisecond)

	m.Stop()

	// Give the goroutine time to exit.
	time.Sleep(100 * time.Millisecond)

	// goleak in TestMain will catch any leaked goroutine — if the test
	// passes with goleak, Stop works correctly.
}
