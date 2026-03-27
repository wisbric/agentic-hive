# PRP-13: Graceful Shutdown

## Goal

Handle SIGTERM/SIGINT gracefully — drain connections, close resources, and exit cleanly.

## Background

Kubernetes sends SIGTERM when terminating a pod and waits `terminationGracePeriodSeconds` (default 30s) before force-killing it. Currently `main.go` calls `http.ListenAndServe` directly — the process receives SIGTERM and dies immediately. This means:

- In-flight HTTP requests are dropped mid-response
- Active WebSocket/terminal sessions are torn down abruptly (the SSH session itself survives because tmux detaches, but the browser gets a hard disconnect)
- The SSH pool is not closed, leaving connections open on remote servers until the TCP timeout fires
- The SQLite WAL may not be checkpointed cleanly

`internal/server/server.go`'s `ListenAndServe` method returns `http.ListenAndServe(s.cfg.Listen, s.mux)` — there is no `http.Server` struct, no way to call `Shutdown`, and no signal handling anywhere in the codebase.

## Requirements

1. In `internal/server/server.go`, replace the bare `http.ListenAndServe` call with an `http.Server` struct. Change `ListenAndServe()` to accept a context and return when that context is cancelled, using `http.Server.Shutdown(ctx)` for the drain:
   ```go
   func (s *Server) ListenAndServe(ctx context.Context) error
   ```

2. In `cmd/server/main.go`, set up signal handling before calling `ListenAndServe`:
   - Create a context cancelled on SIGTERM or SIGINT: `signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)`
   - Pass this context to `srv.ListenAndServe(ctx)`
   - The shutdown sequence after signal must be: stop session poller → close SSH pool → shutdown HTTP server

3. The HTTP server shutdown must use a 30-second timeout context to allow in-flight requests to complete:
   ```go
   shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
   defer cancel()
   httpServer.Shutdown(shutdownCtx)
   ```

4. Active WebSocket connections (terminal bridge) should complete or be closed cleanly. Since tmux detaches from the SSH session when the connection drops, the session survives on the remote — the browser will see a normal WebSocket close. No special handling needed beyond what `Shutdown` provides (it waits for handlers to return).

5. Log the shutdown sequence at info level:
   - On signal received: `"shutdown signal received, draining..."`
   - After HTTP server drains: `"http server stopped"`
   - After pool close: `"shutdown complete"`

## Implementation Notes

**`internal/server/server.go` changes:**

Add an `httpServer *http.Server` field to `Server` struct, populated in `New()`:
```go
s.httpServer = &http.Server{
    Addr:    cfg.Listen,
    Handler: s.mux,
}
```

Change `ListenAndServe` signature:
```go
func (s *Server) ListenAndServe(ctx context.Context) error {
    slog.Info("listening", "addr", s.cfg.Listen)
    errCh := make(chan error, 1)
    go func() {
        if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            errCh <- err
        }
        close(errCh)
    }()
    select {
    case err := <-errCh:
        return err
    case <-ctx.Done():
        slog.Info("shutdown signal received, draining...")
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
            slog.Error("http shutdown error", "error", err)
        }
        slog.Info("http server stopped")
        return nil
    }
}
```

**`cmd/server/main.go` changes:**

Replace the current signal-unaware startup with:
```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
defer stop()

// Pass ctx to StartPolling (already done today)
if cfg.PollInterval > 0 {
    sm.StartPolling(ctx, time.Duration(cfg.PollInterval)*time.Second)
}

if err := srv.ListenAndServe(ctx); err != nil {
    slog.Error("server error", "error", err)
    os.Exit(1)
}

// Shutdown sequence (runs after ListenAndServe returns)
sm.Stop()
pool.Close()
st.Close()
slog.Info("shutdown complete")
```

Remove the `defer pool.Close()`, `defer sm.Stop()`, and `defer st.Close()` calls — replace with explicit post-`ListenAndServe` cleanup so order is deterministic and logged.

**`server_test.go`**: The existing `server_test.go` tests use `s.Handler()` directly (not `ListenAndServe`), so they are unaffected by this signature change. If any test calls `ListenAndServe`, update to pass `context.Background()`.

**Imports to add in `main.go`**: `"os/signal"`, `"syscall"`. These are stdlib, no new dependencies.

**Do not** add a `ReadHeaderTimeout` or `WriteTimeout` to `http.Server` in this PRP — that is a separate hardening concern.

**Helm `terminationGracePeriodSeconds`**: Add `terminationGracePeriodSeconds: 35` to the Deployment pod spec in `deploy/helm/agentic-hive/templates/deployment.yaml` to give the 30s HTTP drain window plus 5s buffer before Kubernetes force-kills. This is a one-line addition to the pod spec.

## Validation

```bash
cd /home/stefans/git/agentic-workspace/projects/claude-overlay

# Build must succeed
go build ./...

# All tests pass
go test ./...

# Runtime: start the server, send SIGTERM, verify clean exit within 5s
OVERLAY_SESSION_SECRET=test123 OVERLAY_DB_PATH=/tmp/test-overlay.db ./agentic-hive &
SERVER_PID=$!
sleep 1
kill -TERM $SERVER_PID
# Must exit with code 0 within 5 seconds
timeout 5 wait $SERVER_PID
echo "Exit code: $?"  # Expected: 0

# Verify shutdown log line appears
OVERLAY_SESSION_SECRET=test123 OVERLAY_DB_PATH=/tmp/test-overlay.db ./agentic-hive 2>&1 &
SERVER_PID=$!
sleep 1
kill -TERM $SERVER_PID
sleep 2
# (check log output captured — should contain "shutdown complete")
```

## Out of Scope

- Draining individual WebSocket connections with a custom close handshake — `http.Server.Shutdown` handles this
- Adding `ReadHeaderTimeout` / `WriteTimeout` to `http.Server` — separate security PRP
- Helm `preStop` lifecycle hook — the 35s `terminationGracePeriodSeconds` is sufficient
- Handling SIGHUP for config reload
