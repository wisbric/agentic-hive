package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/metrics"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/sshpool"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"
)

var safeNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

var unsafeNameRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func sanitizeName(s string) string {
	return unsafeNameRe.ReplaceAllLiteralString(s, "_")
}

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

type Session struct {
	Name         string `json:"name"`
	Created      int64  `json:"created"`
	Windows      int    `json:"windows"`
	Attached     int    `json:"attached"`
	LastActivity int64  `json:"lastActivity"`
	Idle         string `json:"idle"`
	SSHCommand   string `json:"sshCommand"`
}

type Manager struct {
	store      *store.Store
	pool       *sshpool.Pool
	sessions   map[string][]Session // keyed by server ID
	mu         sync.RWMutex
	stopCh     chan struct{}
	intervalCh chan time.Duration
}

func NewManager(st *store.Store, pool *sshpool.Pool) *Manager {
	return &Manager{
		store:      st,
		pool:       pool,
		sessions:   make(map[string][]Session),
		stopCh:     make(chan struct{}),
		intervalCh: make(chan time.Duration, 1),
	}
}

func (m *Manager) GetSessions(serverID string) []Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sessions := m.sessions[serverID]
	if sessions == nil {
		return []Session{}
	}
	return sessions
}

func (m *Manager) ListSessions(ctx context.Context, srv *store.Server) ([]Session, error) {
	stdout, _, err := m.pool.Exec(ctx, srv.ID,
		"tmux list-sessions -F '#{session_name}:#{session_created}:#{session_windows}:#{session_attached}:#{session_activity}' 2>/dev/null || true",
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return parseSessions(stdout, srv), nil
}

func (m *Manager) CreateSession(ctx context.Context, serverID, user, label, command, workdir string) (string, error) {
	srv, err := m.store.GetServer(serverID)
	if err != nil {
		return "", err
	}

	shortID := randomShortID()
	name := fmt.Sprintf("%s-%s-%s", sanitizeName(user), sanitizeName(label), shortID)

	cmd := fmt.Sprintf("tmux new-session -d -s %s", name)
	if workdir != "" {
		cmd += fmt.Sprintf(" -c %s", shellEscape(workdir))
	}
	if command != "" {
		cmd += fmt.Sprintf(" %s", shellEscape(command))
	}

	_, stderr, err := m.pool.Exec(ctx, serverID, cmd)
	if err != nil {
		return "", fmt.Errorf("create session on %s: %s: %w", srv.Host, stderr, err)
	}

	return name, nil
}

func (m *Manager) KillSession(ctx context.Context, serverID, sessionName string) error {
	if !safeNameRe.MatchString(sessionName) {
		return fmt.Errorf("invalid session name: %s", sessionName)
	}
	cmd := fmt.Sprintf("tmux kill-session -t %s", sessionName)
	_, stderr, err := m.pool.Exec(ctx, serverID, cmd)
	if err != nil {
		return fmt.Errorf("kill session: %s: %w", stderr, err)
	}
	return nil
}

func (m *Manager) StartPolling(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Poll immediately on start
		m.pollAll(ctx)

		for {
			select {
			case <-ticker.C:
				m.pollAll(ctx)
			case newInterval := <-m.intervalCh:
				ticker.Reset(newInterval)
			case <-m.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// IntervalCh returns the channel that receives interval updates from UpdateInterval.
// This is exported for testing purposes.
func (m *Manager) IntervalCh() <-chan time.Duration {
	return m.intervalCh
}

// UpdateInterval signals the polling goroutine to switch to a new tick interval.
// The change takes effect at the next tick boundary. If polling has not been
// started, the value is buffered and will be consumed when StartPolling is called.
func (m *Manager) UpdateInterval(d time.Duration) {
	// Drain any pending value so we don't block on a full channel.
	select {
	case <-m.intervalCh:
	default:
	}
	m.intervalCh <- d
}

func (m *Manager) Stop() {
	close(m.stopCh)
}

func (m *Manager) pollAll(ctx context.Context) {
	servers, err := m.store.ListServers()
	if err != nil {
		slog.Warn("session poll: list servers failed", "error", err)
		return
	}

	for _, srv := range servers {
		sessions, err := m.ListSessions(ctx, &srv)
		if err != nil {
			slog.Warn("session poll failed", "server_name", srv.Name, "host", srv.Host, "error", err)
			if srv.Status != store.StatusUnreachable {
				_ = m.store.UpdateServerStatus(srv.ID, store.StatusUnreachable)
			}
			if metrics.SessionsActive != nil {
				metrics.SessionsActive.With(prometheus.Labels{"server_id": srv.ID}).Set(0)
			}
			continue
		}

		if srv.Status != store.StatusReachable {
			_ = m.store.UpdateServerStatus(srv.ID, store.StatusReachable)
		}

		m.mu.Lock()
		m.sessions[srv.ID] = sessions
		m.mu.Unlock()

		if metrics.SessionsActive != nil {
			metrics.SessionsActive.With(prometheus.Labels{"server_id": srv.ID}).Set(float64(len(sessions)))
		}
	}
}

func parseSessions(output string, srv *store.Server) []Session {
	var sessions []Session
	lines := strings.Split(strings.TrimSpace(output), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 5)
		if len(parts) < 5 {
			continue
		}

		created, _ := strconv.ParseInt(parts[1], 10, 64)
		windows, _ := strconv.Atoi(parts[2])
		attached, _ := strconv.Atoi(parts[3])
		activity, _ := strconv.ParseInt(parts[4], 10, 64)

		idle := formatIdle(time.Now().Unix() - activity)

		sshCmd := fmt.Sprintf(`ssh -t %s@%s "tmux new -A -s %s"`, srv.SSHUser, srv.Host, parts[0])

		sessions = append(sessions, Session{
			Name:         parts[0],
			Created:      created,
			Windows:      windows,
			Attached:     attached,
			LastActivity: activity,
			Idle:         idle,
			SSHCommand:   sshCmd,
		})
	}

	return sessions
}

func formatIdle(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	return fmt.Sprintf("%dh%dm", seconds/3600, (seconds%3600)/60)
}

func randomShortID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
