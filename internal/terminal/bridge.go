package terminal

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/gorilla/websocket"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/sshpool"
	"golang.org/x/crypto/ssh"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type resizeMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

type Bridge struct {
	pool *sshpool.Pool
}

func NewBridge(pool *sshpool.Pool) *Bridge {
	return &Bridge{pool: pool}
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

	// Upgrade to WebSocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade failed: %v", err)
		return
	}
	defer ws.Close()

	// Get SSH session
	_, sshSession, err := b.pool.Session(r.Context(), serverID)
	if err != nil {
		log.Printf("ssh session failed for %s: %v", serverID, err)
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
		log.Printf("request pty failed: %v", err)
		ws.WriteMessage(websocket.TextMessage, []byte(`{"error":"pty request failed"}`))
		return
	}

	// Get stdin/stdout pipes
	stdin, err := sshSession.StdinPipe()
	if err != nil {
		log.Printf("stdin pipe failed: %v", err)
		return
	}

	stdout, err := sshSession.StdoutPipe()
	if err != nil {
		log.Printf("stdout pipe failed: %v", err)
		return
	}

	// Start tmux attach command
	cmd := "tmux new -A -s " + sessionName
	if err := sshSession.Start(cmd); err != nil {
		log.Printf("start command failed: %v", err)
		ws.WriteMessage(websocket.TextMessage, []byte(`{"error":"tmux attach failed"}`))
		return
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
				// Raw terminal input
				if _, err := stdin.Write(msg); err != nil {
					return
				}
			case websocket.TextMessage:
				// Control message (resize)
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

