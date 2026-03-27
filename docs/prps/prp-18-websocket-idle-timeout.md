# PRP-18: WebSocket Idle Timeout

## Goal
Automatically close idle WebSocket terminal connections to free SSH sessions and prevent resource leaks from abandoned browser tabs.

## Background
`internal/terminal/bridge.go` holds one SSH session and one SSH connection per open WebSocket. A browser tab left open indefinitely — or a network partition that leaves the TCP connection half-open — holds that SSH session and the underlying SSH connection in the pool forever. There is no idle detection on the WebSocket bridge today. `config.go` already has an `IdleTimeout int` field loaded from `OVERLAY_IDLE_TIMEOUT` (currently only wired up in config, not used anywhere), which means the groundwork exists but was never connected.

## Requirements

1. Track last-activity time on each WebSocket connection. "Activity" is defined as any binary message flowing in either direction (user keystrokes or terminal output). Text messages of type `resize` do NOT count as activity (to prevent trivial keep-alive via programmatic resize spam).

2. Read `OVERLAY_TERMINAL_IDLE_TIMEOUT` (seconds, default `0` = disabled) from config. The existing `OVERLAY_IDLE_TIMEOUT` field in `Config` should be renamed to `TerminalIdleTimeout` and the env var should be `OVERLAY_TERMINAL_IDLE_TIMEOUT`. Update `config.go` and `configmap.yaml` accordingly.

3. When the timeout is enabled (> 0), start a watcher goroutine inside `HandleTerminal` after the WebSocket is upgraded. The goroutine:
   - Ticks every `max(1s, timeout/10)` to avoid busy-looping
   - Reads the shared `lastActivity` timestamp (protected by `sync/atomic` or a `sync.Mutex`)
   - If `time.Since(lastActivity) >= timeout`, sends the idle-timeout JSON message then closes the WebSocket

4. Before closing, send a text message to the browser:
   ```json
   {"type":"idle_timeout","message":"Session idle for too long, disconnecting"}
   ```
   Then send a WebSocket close frame with code `1000` (normal closure) and reason `"idle timeout"`.

5. Update the browser-side JS (`cmd/server/static/`) to handle the `idle_timeout` message type in the WebSocket `onmessage` handler. Display the message in the existing reconnect overlay (or a visible banner if no reconnect overlay exists yet). The terminal should not auto-reconnect after an idle timeout — show the message and wait for user interaction.

6. Update `NewBridge` to accept an idle timeout duration parameter (or add a setter), so `server.go` can pass `cfg.TerminalIdleTimeout` through.

## Implementation Notes

- **Where to change:** `internal/terminal/bridge.go` is the only file that needs the core logic. `internal/config/config.go` needs the rename. `internal/server/server.go` needs to pass the timeout to `terminal.NewBridge`. `deploy/helm/agentic-hive/templates/configmap.yaml` needs the env var name update.
- **Atomic timestamp:** Use `sync/atomic` with a `int64` Unix-nano value for `lastActivity`. Call `atomic.StoreInt64` from both the stdout-copy goroutine (after a successful `ws.WriteMessage`) and the stdin-read goroutine (after a successful `stdin.Write` for binary messages only). The watcher goroutine calls `atomic.LoadInt64`.
- **Watcher teardown:** The watcher goroutine must exit when the WebSocket closes. Use a `context.Context` derived from the request context, or a `done` channel closed in a `defer` at the top of `HandleTerminal`. The goroutine should select on the done channel and the ticker.
- **Resize messages:** The `resizeMsg` handling block in the `WebSocket -> SSH stdin` goroutine already identifies resize vs. binary. Do not update `lastActivity` in the resize branch.
- **Bridge constructor signature change:** `NewBridge(pool *sshpool.Pool)` becomes `NewBridge(pool *sshpool.Pool, idleTimeout time.Duration) *Bridge`. Store the duration on the `Bridge` struct. A zero value means disabled.
- **Static JS:** The reconnect overlay or disconnect banner is in `cmd/server/static/`. Read the existing JS before editing; follow whatever pattern is already used for the `{"error":"..."}` message display.

## Validation

```bash
# Unit test: idle timeout fires
# Add to internal/terminal/bridge_test.go:
# - Create a fake WebSocket pair
# - Set idleTimeout = 100ms
# - Send no data for 150ms
# - Assert WebSocket receives idle_timeout JSON message then close frame

# Integration smoke test (manual):
OVERLAY_SESSION_SECRET=test OVERLAY_TERMINAL_IDLE_TIMEOUT=10 ./agentic-hive
# Open a terminal in the browser, do not type for 10 seconds
# Expect: reconnect/disconnect overlay appears with idle timeout message

# Verify resize does not reset timer:
# With OVERLAY_TERMINAL_IDLE_TIMEOUT=10, resize the browser window repeatedly
# Expect: connection still closes after 10s of no keystroke/output activity

# Config rename regression:
OVERLAY_TERMINAL_IDLE_TIMEOUT=30 ./agentic-hive &
curl -s http://localhost:8080/healthz
# Expect: {"status":"ok"} — server starts without error

# Helm configmap renders updated env var name:
helm template test ./deploy/helm/agentic-hive \
  --set config.idleTimeout=300 \
  | grep OVERLAY_TERMINAL_IDLE_TIMEOUT
# Expect: OVERLAY_TERMINAL_IDLE_TIMEOUT: "300"
```

## Out of Scope

- Server-side ping/pong WebSocket keep-alive (orthogonal concern)
- Configuring different timeouts per server or per user
- Idle timeout for SSH connections in `sshpool` (pool has its own lifecycle)
- Auto-reconnect with backoff after idle timeout (show message and stop)
