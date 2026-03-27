package terminal

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/auth"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/metrics"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/sshpool"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"
	"golang.org/x/crypto/ssh"
)

var safeSessionNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // non-browser clients
		}
		// Allow same-origin
		host := r.Host
		return strings.HasSuffix(origin, "://"+host)
	},
}

type resizeMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

type idleTimeoutMsg struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type Bridge struct {
	pool        *sshpool.Pool
	idleTimeout time.Duration
	store       *store.Store
}

func NewBridge(pool *sshpool.Pool, idleTimeout time.Duration, st *store.Store) *Bridge {
	return &Bridge{pool: pool, idleTimeout: idleTimeout, store: st}
}

// clientIP extracts the real client IP from the request.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// Strip port from RemoteAddr
	for i := len(r.RemoteAddr) - 1; i >= 0; i-- {
		if r.RemoteAddr[i] == ':' {
			return r.RemoteAddr[:i]
		}
	}
	return r.RemoteAddr
}

func (b *Bridge) logAudit(r *http.Request, action, targetType, targetID, details string) {
	if b.store == nil {
		return
	}
	user := auth.GetUser(r)
	entry := store.AuditEntry{
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Details:    details,
		IPAddress:  clientIP(r),
	}
	if user != nil {
		entry.UserID = user.UserID
		entry.Username = user.Username
	}
	if err := b.store.LogAudit(entry); err != nil {
		slog.Error("audit log write failed", "action", action, "error", err)
	}
	slog.Info("audit",
		"action", action,
		"user_id", entry.UserID,
		"username", entry.Username,
		"target_type", targetType,
		"target_id", targetID,
		"ip", entry.IPAddress,
	)
}

func (b *Bridge) HandleTerminal(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("server")
	sessionName := r.PathValue("session")

	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}

	// Validate session name before upgrading
	if !safeSessionNameRe.MatchString(sessionName) {
		http.Error(w, `{"error":"invalid session name"}`, http.StatusBadRequest)
		return
	}

	// Upgrade to WebSocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}
	defer ws.Close()
	if metrics.WebSocketConnectionsActive != nil {
		metrics.WebSocketConnectionsActive.Inc()
		defer metrics.WebSocketConnectionsActive.Dec()
	}

	b.logAudit(r, store.AuditTerminalConnect, "session", sessionName,
		fmt.Sprintf(`{"server_id":%q,"session":%q}`, serverID, sessionName))
	defer b.logAudit(r, store.AuditTerminalDisconnect, "session", sessionName,
		fmt.Sprintf(`{"server_id":%q,"session":%q}`, serverID, sessionName))

	// done channel for watcher goroutine teardown
	done := make(chan struct{})
	defer close(done)

	// Get SSH session
	_, sshSession, err := b.pool.Session(r.Context(), serverID)
	if err != nil {
		slog.Error("ssh session failed", "server_id", serverID, "error", err)
		ws.WriteMessage(websocket.TextMessage, []byte(`{"error":"ssh connection failed"}`))
		return
	}
	defer sshSession.Close()

	// Request PTY
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := sshSession.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		slog.Error("pty request failed", "error", err)
		ws.WriteMessage(websocket.TextMessage, []byte(`{"error":"pty request failed"}`))
		return
	}

	// Get stdin/stdout pipes
	stdin, err := sshSession.StdinPipe()
	if err != nil {
		slog.Error("stdin pipe failed", "error", err)
		return
	}

	stdout, err := sshSession.StdoutPipe()
	if err != nil {
		slog.Error("stdout pipe failed", "error", err)
		return
	}

	// Start tmux attach command (sessionName already validated as safe)
	cmd := "tmux new -A -s " + sessionName
	if err := sshSession.Start(cmd); err != nil {
		slog.Error("tmux attach failed", "error", err)
		ws.WriteMessage(websocket.TextMessage, []byte(`{"error":"tmux attach failed"}`))
		return
	}

	// lastActivity tracks the Unix nanoseconds of last binary I/O (atomic).
	var lastActivity int64
	atomic.StoreInt64(&lastActivity, time.Now().UnixNano())

	// Start idle timeout watcher if configured.
	if b.idleTimeout > 0 {
		tickInterval := b.idleTimeout / 10
		if tickInterval < time.Second {
			tickInterval = time.Second
		}
		go func() {
			ticker := time.NewTicker(tickInterval)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					last := atomic.LoadInt64(&lastActivity)
					if time.Since(time.Unix(0, last)) >= b.idleTimeout {
						// Send idle timeout notification before closing.
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
		}()
	}

	var wg sync.WaitGroup

	// SSH stdout -> WebSocket (terminal output)
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				if writeErr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					return
				}
				// Terminal output counts as activity.
				atomic.StoreInt64(&lastActivity, time.Now().UnixNano())
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket -> SSH stdin (user input) + resize handling
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer stdin.Close()
		for {
			msgType, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}

			switch msgType {
			case websocket.BinaryMessage:
				// Raw terminal input — counts as activity.
				if _, err := stdin.Write(msg); err != nil {
					return
				}
				atomic.StoreInt64(&lastActivity, time.Now().UnixNano())
			case websocket.TextMessage:
				// Control message (resize) — does NOT count as activity.
				var resize resizeMsg
				if err := json.Unmarshal(msg, &resize); err != nil {
					continue
				}
				if resize.Type == "resize" && resize.Cols > 0 && resize.Rows > 0 {
					_ = sshSession.WindowChange(resize.Rows, resize.Cols)
				}
			}
		}
	}()

	// Wait for SSH command to finish
	_ = sshSession.Wait()

	// Close WebSocket to signal disconnect to browser
	ws.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "session ended"))
	wg.Wait()
}
