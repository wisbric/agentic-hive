package terminal

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/wisbric/agentic-hive/internal/keystore"
	"github.com/wisbric/agentic-hive/internal/metrics"
	"github.com/wisbric/agentic-hive/internal/sshpool"
	"github.com/wisbric/agentic-hive/internal/store"
	"github.com/wisbric/agentic-hive/internal/testutil"
)

// setupTestBridge wires up a full Bridge backed by a real test SSH server,
// store, keystore, and pool. It returns the httptest server URL, the server ID
// registered in the store, the pool, and the store (all cleaned up via
// t.Cleanup).
func setupTestBridge(t *testing.T) (serverURL string, serverID string, pool *sshpool.Pool, st *store.Store) {
	t.Helper()

	// 1. Start test SSH server.
	signer := testutil.GenerateSigner(t)
	addr := testutil.StartSSHServer(t, signer)
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host port %q: %v", addr, err)
	}
	port, _ := strconv.Atoi(portStr)

	// 2. Open store.
	st, err = store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	// 3. Create keystore.
	ks := keystore.NewLocal(st.DB(), "test-secret-long-enough-for-aes!")

	// 4. Create server in store.
	srv, err := st.CreateServer("test-server", host, port, "testuser", "", "local", "")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	serverID = srv.ID

	// 5. Store client key.
	if err := ks.Put(t.Context(), srv.ID, testutil.NewClientKeyPEM(t)); err != nil {
		t.Fatalf("put key: %v", err)
	}

	// 6. Create pool.
	pool = sshpool.New(st, ks)
	t.Cleanup(func() { pool.Close() })

	// 7. Initialize test-local metrics (avoid MustRegister panics on re-register).
	metrics.WebSocketConnectionsActive = prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_ws_active"})
	metrics.SSHConnectionsActive = prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_ssh_active"})

	// 8. Create bridge (no idle timeout, no store for ownership check).
	bridge := NewBridge(pool, 0, nil)

	// 9. Create httptest server with a mux routing the pattern HandleTerminal expects.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/{server}/{session}", bridge.HandleTerminal)
	ts := httptest.NewServer(mux)
	t.Cleanup(func() { ts.Close() })

	return ts.URL, serverID, pool, st
}

// wsURL converts an httptest server URL to a WebSocket URL for a given server
// and session name.
func wsURL(httpURL, serverID, session string) string {
	return fmt.Sprintf("ws%s/ws/%s/%s", strings.TrimPrefix(httpURL, "http"), serverID, session)
}

// dialWS opens a WebSocket to the test bridge for the given server and session.
func dialWS(t *testing.T, httpURL, serverID, session string) *websocket.Conn {
	t.Helper()
	u := wsURL(httpURL, serverID, session)
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("websocket dial %s: %v", u, err)
	}
	return conn
}

func TestWebSocketCloseTriggerSSHCleanup(t *testing.T) {
	tsURL, serverID, _, _ := setupTestBridge(t)

	// Dial WebSocket.
	ws := dialWS(t, tsURL, serverID, "test-session")

	// Send a few binary messages (keystrokes).
	for i := 0; i < 3; i++ {
		if err := ws.WriteMessage(websocket.BinaryMessage, []byte("x")); err != nil {
			t.Fatalf("write binary: %v", err)
		}
	}

	// Read back some data — the test SSH server's exec handler sends exit-status
	// and closes, so the bridge's stdout goroutine will see EOF quickly. We may
	// or may not get data back, but we try.
	ws.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, _ = ws.ReadMessage() // best-effort read

	// Close the WebSocket client.
	ws.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"))
	ws.Close()

	// Wait briefly for cleanup goroutines.
	time.Sleep(500 * time.Millisecond)

	// Assert: WebSocket connections metric is 0.
	if v := testutil.GaugeValue(metrics.WebSocketConnectionsActive); v != 0 {
		t.Errorf("WebSocketConnectionsActive = %v, want 0", v)
	}
}

func TestRapidConnectDisconnect(t *testing.T) {
	tsURL, serverID, _, _ := setupTestBridge(t)

	// Open 5 WebSocket connections sequentially, then close them.
	for i := 0; i < 5; i++ {
		session := fmt.Sprintf("rapid-%d", i)
		ws := dialWS(t, tsURL, serverID, session)

		// Small read attempt — don't block if nothing comes back.
		ws.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, _, _ = ws.ReadMessage()

		ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"))
		ws.Close()
	}

	// Wait briefly for cleanup.
	time.Sleep(500 * time.Millisecond)

	// Assert: WebSocket metric is 0.
	if v := testutil.GaugeValue(metrics.WebSocketConnectionsActive); v != 0 {
		t.Errorf("WebSocketConnectionsActive = %v, want 0", v)
	}
}
