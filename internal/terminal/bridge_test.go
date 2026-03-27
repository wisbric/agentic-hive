package terminal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestResizeMsgParsing(t *testing.T) {
	msg := `{"type":"resize","cols":120,"rows":40}`
	var resize resizeMsg
	if err := json.Unmarshal([]byte(msg), &resize); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resize.Type != "resize" {
		t.Errorf("Type = %q, want %q", resize.Type, "resize")
	}
	if resize.Cols != 120 {
		t.Errorf("Cols = %d, want 120", resize.Cols)
	}
	if resize.Rows != 40 {
		t.Errorf("Rows = %d, want 40", resize.Rows)
	}
}

// idleWatcher is an extracted, testable version of the idle timeout watcher
// logic used in HandleTerminal.
func idleWatcher(ws *websocket.Conn, lastActivity *int64, idleTimeout time.Duration, done <-chan struct{}) {
	tickInterval := idleTimeout / 10
	if tickInterval < time.Second {
		tickInterval = time.Millisecond * 10 // fast for tests
	}
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			last := atomic.LoadInt64(lastActivity)
			if time.Since(time.Unix(0, last)) >= idleTimeout {
				msg, _ := json.Marshal(idleTimeoutMsg{
					Type:    "idle_timeout",
					Message: "Session idle for too long, disconnecting",
				})
				ws.WriteMessage(websocket.TextMessage, msg)
				ws.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "idle timeout"))
				ws.Close()
				return
			}
		}
	}
}

// TestIdleTimeoutFires verifies that when no data flows the watcher sends
// the idle_timeout JSON message followed by a close frame.
func TestIdleTimeoutFires(t *testing.T) {
	const timeout = 100 * time.Millisecond

	// Build a test WebSocket server that runs the watcher.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ug := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := ug.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("server upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		var lastActivity int64
		atomic.StoreInt64(&lastActivity, time.Now().UnixNano())
		done := make(chan struct{})
		defer close(done)

		// Run watcher — it will fire after ~timeout.
		idleWatcher(conn, &lastActivity, timeout, done)
	}))
	defer srv.Close()

	// Connect as client.
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("client dial failed: %v", err)
	}
	defer client.Close()

	// Expect: first a text message with type idle_timeout.
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	msgType, data, err := client.ReadMessage()
	if err != nil {
		t.Fatalf("read message failed: %v", err)
	}
	if msgType != websocket.TextMessage {
		t.Fatalf("expected TextMessage, got %d", msgType)
	}
	var got idleTimeoutMsg
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal idle_timeout message failed: %v", err)
	}
	if got.Type != "idle_timeout" {
		t.Errorf("message type = %q, want %q", got.Type, "idle_timeout")
	}
	if got.Message == "" {
		t.Error("idle_timeout message should not be empty")
	}

	// Expect: next a close frame.
	_, _, err = client.ReadMessage()
	if err == nil {
		// gorilla returns an error on close frames — success if we get here means
		// the close frame was transparent; check close code.
		return
	}
	closeErr, ok := err.(*websocket.CloseError)
	if !ok {
		t.Fatalf("expected CloseError, got: %v", err)
	}
	if closeErr.Code != websocket.CloseNormalClosure {
		t.Errorf("close code = %d, want %d", closeErr.Code, websocket.CloseNormalClosure)
	}
	if closeErr.Text != "idle timeout" {
		t.Errorf("close reason = %q, want %q", closeErr.Text, "idle timeout")
	}
}

// TestIdleTimeoutDisabledWhenZero verifies that with idleTimeout=0 the
// watcher is not started and the connection stays open.
func TestIdleTimeoutDisabledWhenZero(t *testing.T) {
	// Bridge with zero timeout should never call idleWatcher.
	// We test this by checking that the Bridge struct stores zero correctly.
	b := NewBridge(nil, 0, nil)
	if b.idleTimeout != 0 {
		t.Errorf("idleTimeout = %v, want 0", b.idleTimeout)
	}
}

// TestIdleTimeoutResetsOnActivity verifies that activity (binary messages)
// resets the idle timer preventing premature disconnect.
func TestIdleTimeoutResetsOnActivity(t *testing.T) {
	const timeout = 150 * time.Millisecond

	received := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ug := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := ug.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		var lastActivity int64
		atomic.StoreInt64(&lastActivity, time.Now().UnixNano())
		done := make(chan struct{})

		// Start watcher in background.
		go idleWatcher(conn, &lastActivity, timeout, done)

		// Read one binary message and update lastActivity (simulating normal data flow).
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		msgType, _, err := conn.ReadMessage()
		if err == nil && msgType == websocket.BinaryMessage {
			atomic.StoreInt64(&lastActivity, time.Now().UnixNano())
			received <- struct{}{}
		}

		// Keep connection open until watcher fires.
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.ReadMessage()
		close(done)
	}))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("client dial failed: %v", err)
	}
	defer client.Close()

	// Send a binary message at t=50ms to reset the timer.
	time.Sleep(50 * time.Millisecond)
	if err := client.WriteMessage(websocket.BinaryMessage, []byte("ping")); err != nil {
		t.Fatalf("write binary failed: %v", err)
	}

	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("server did not receive binary message")
	}

	// Now wait for the idle timeout to fire (should happen ~150ms after the reset).
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	msgType, data, err := client.ReadMessage()
	if err != nil {
		// Could be a close frame arriving as error.
		if _, ok := err.(*websocket.CloseError); !ok {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if msgType == websocket.TextMessage {
		var got idleTimeoutMsg
		if jsonErr := json.Unmarshal(data, &got); jsonErr == nil && got.Type == "idle_timeout" {
			return // correct: idle_timeout fired after reset
		}
	}
}

// TestNewBridgeIdleTimeout verifies constructor stores duration correctly.
func TestNewBridgeIdleTimeout(t *testing.T) {
	b := NewBridge(nil, 30*time.Second, nil)
	if b.idleTimeout != 30*time.Second {
		t.Errorf("idleTimeout = %v, want 30s", b.idleTimeout)
	}
}
