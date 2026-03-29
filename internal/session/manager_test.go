package session

import (
	"testing"
	"time"

	"github.com/wisbric/agentic-hive/internal/store"
)

func TestParseSessions(t *testing.T) {
	srv := &store.Server{
		SSHUser: "stefan",
		Host:    "devbox.example.com",
	}

	output := "stefan-claude-abc123:1711526400:1:0:1711530000\nstefan-shell-def456:1711526500:2:1:1711530100\n"

	sessions := parseSessions(output, srv)
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	if sessions[0].Name != "stefan-claude-abc123" {
		t.Errorf("Name = %q, want %q", sessions[0].Name, "stefan-claude-abc123")
	}
	if sessions[0].Windows != 1 {
		t.Errorf("Windows = %d, want 1", sessions[0].Windows)
	}
	if sessions[0].Attached != 0 {
		t.Errorf("Attached = %d, want 0", sessions[0].Attached)
	}
	if sessions[1].Attached != 1 {
		t.Errorf("sessions[1].Attached = %d, want 1", sessions[1].Attached)
	}

	expectedCmd := `ssh -t stefan@devbox.example.com "tmux new -A -s stefan-claude-abc123"`
	if sessions[0].SSHCommand != expectedCmd {
		t.Errorf("SSHCommand = %q, want %q", sessions[0].SSHCommand, expectedCmd)
	}
}

func TestParseSessionsEmpty(t *testing.T) {
	srv := &store.Server{SSHUser: "root", Host: "host"}
	sessions := parseSessions("", srv)
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions for empty output, got %d", len(sessions))
	}
}

func TestFormatIdle(t *testing.T) {
	tests := []struct {
		seconds int64
		want    string
	}{
		{30, "30s"},
		{90, "1m"},
		{3700, "1h1m"},
		{7200, "2h0m"},
	}

	for _, tt := range tests {
		got := formatIdle(tt.seconds)
		if got != tt.want {
			t.Errorf("formatIdle(%d) = %q, want %q", tt.seconds, got, tt.want)
		}
	}
}

func TestRandomShortID(t *testing.T) {
	id := randomShortID()
	if len(id) != 6 {
		t.Errorf("len(shortID) = %d, want 6", len(id))
	}

	// Should be different each time
	id2 := randomShortID()
	if id == id2 {
		t.Error("two random IDs should not be identical")
	}
}

func TestManagerUpdateInterval(t *testing.T) {
	m := NewManager(nil, nil)

	// UpdateInterval should not block even before StartPolling is called.
	done := make(chan struct{})
	go func() {
		m.UpdateInterval(5 * time.Second)
		close(done)
	}()

	select {
	case <-done:
		// success: call returned without blocking
	case <-time.After(time.Second):
		t.Fatal("UpdateInterval blocked for more than 1 second")
	}

	// Calling UpdateInterval multiple times should drain the old value and
	// replace it with the new one without blocking.
	m.UpdateInterval(10 * time.Second)
	m.UpdateInterval(20 * time.Second)

	// The channel should hold the last written value.
	select {
	case d := <-m.intervalCh:
		if d != 20*time.Second {
			t.Errorf("intervalCh value = %v, want 20s", d)
		}
	default:
		t.Error("expected a value in intervalCh")
	}
}
