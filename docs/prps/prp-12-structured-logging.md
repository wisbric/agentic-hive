# PRP-12: Structured Logging (slog)

## Goal

Replace all `log.Printf` calls with Go stdlib `log/slog` for JSON-structured logging.

## Background

The codebase uses `log.Printf` and `log.Fatalf` throughout — no structured fields, no log levels, no machine-readable output. Go 1.21+ provides `log/slog` in stdlib with zero additional dependencies. The current logs are not parseable by Loki, ELK, or any structured log aggregator. Key callsites today:

- `cmd/server/main.go`: 5x `log.Printf` / `log.Fatalf` for startup lifecycle
- `internal/server/server.go`: 1x `log.Printf` for server listen, 1x for key delete errors
- `internal/auth/oidc.go`: 4x `log.Printf` for token exchange/verification/upsert failures
- `internal/session/manager.go`: 2x `log.Printf` for poll failures
- `internal/terminal/bridge.go`: 7x `log.Printf` for WebSocket/SSH/PTY errors

No HTTP request logging exists at all — there is no middleware layer instrumenting method, path, status, or duration.

## Requirements

1. Initialize a JSON slog handler in `main.go` before any other initialization:
   ```go
   slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})))
   ```

2. Add config field `LogLevel string` to `internal/config/config.go`, populated from `OVERLAY_LOG_LEVEL` env var (values: `debug`, `info`, `warn`, `error`; default `info`). Parse to `slog.Level` in `main.go` before initializing the handler.

3. Replace ALL `log.Printf` and `log.Fatalf` calls across all packages:
   - `log.Fatalf(msg, args...)` → `slog.Error(msg, key, val, ...); os.Exit(1)` — only in `main.go`
   - `log.Printf("thing failed: %v", err)` → `slog.Error("thing failed", "error", err)` or `slog.Warn(...)` depending on severity
   - Startup info logs (keystore type, auth mode, listen address) → `slog.Info(...)` with structured fields
   - Error conditions in handlers → `slog.Error(...)` with `"server_id"`, `"error"` fields as appropriate
   - Poll failures in session manager → `slog.Warn(...)` (transient, not fatal)

4. Add HTTP request logging middleware in `internal/server/server.go`. Wrap `s.mux` in a `loggingMiddleware` that captures:
   - method, path, status code, duration (use `http.ResponseWriter` wrapper to capture status)
   - Log at `slog.Info` level with fields: `"method"`, `"path"`, `"status"`, `"duration_ms"`
   - Apply to all routes by wrapping at `ListenAndServe` / `Handler()` level, not per-route

5. `slog.Fatalf` does not exist. In `main.go`, the pattern for fatal startup errors is:
   ```go
   slog.Error("failed to open database", "error", err)
   os.Exit(1)
   ```

## Implementation Notes

**Exact callsite inventory:**

`cmd/server/main.go`:
- `log.Fatalf("OVERLAY_SESSION_SECRET must be set...")` → `slog.Error` + `os.Exit(1)`
- `log.Fatalf("failed to open database: %v", err)` → `slog.Error("failed to open database", "error", err)` + `os.Exit(1)`
- `log.Fatalf("failed to seed templates: %v", err)` → same pattern
- `log.Fatalf("failed to initialize vault keystore: %v", err)` → same pattern
- `log.Printf("using vault keystore (%s)", cfg.VaultAddr)` → `slog.Info("keystore initialized", "backend", "vault", "addr", cfg.VaultAddr)`
- `log.Printf("using local keystore")` → `slog.Info("keystore initialized", "backend", "local")`
- `log.Fatalf("failed to sub static fs: %v", err)` → `slog.Error` + `os.Exit(1)`
- `log.Printf("OIDC authentication enabled...")` → `slog.Info("oidc enabled", "issuer", cfg.OIDCIssuerURL)`
- `log.Printf("agentic-hive starting...")` → `slog.Info("server starting", "auth", cfg.AuthMode, "keystore", cfg.KeyStoreBackend)`
- `log.Fatalf("server error: %v", err)` → `slog.Error("server stopped", "error", err)` + `os.Exit(1)`

`internal/server/server.go`:
- `log.Printf("listening on %s", s.cfg.Listen)` → `slog.Info("listening", "addr", s.cfg.Listen)`
- `log.Printf("delete key for server %s: %v", id, err)` → `slog.Warn("key delete failed", "server_id", id, "error", err)`

`internal/auth/oidc.go`:
- Token exchange failure → `slog.Error("oidc token exchange failed", "error", err)`
- Token verification failure → `slog.Error("oidc token verification failed", "error", err)`
- Upsert user failure → `slog.Error("oidc upsert user failed", "error", err)`

`internal/session/manager.go`:
- `log.Printf("session poll: list servers failed: %v", err)` → `slog.Warn("session poll: list servers failed", "error", err)`
- `log.Printf("session poll: %s (%s) failed: %v", ...)` → `slog.Warn("session poll failed", "server_name", srv.Name, "host", srv.Host, "error", err)`

`internal/terminal/bridge.go`:
- WebSocket upgrade failure → `slog.Error("websocket upgrade failed", "error", err)`
- SSH session failure → `slog.Error("ssh session failed", "server_id", serverID, "error", err)`
- PTY request failure → `slog.Error("pty request failed", "error", err)`
- Pipe failures → `slog.Error("stdin/stdout pipe failed", "error", err)`
- Command start failure → `slog.Error("tmux attach failed", "error", err)`

**Log level parsing** — add a helper in `main.go`:
```go
func parseLogLevel(s string) slog.Level {
    switch strings.ToLower(s) {
    case "debug": return slog.LevelDebug
    case "warn":  return slog.LevelWarn
    case "error": return slog.LevelError
    default:      return slog.LevelInfo
    }
}
```

**Logging middleware** — add a `responseWriter` wrapper in `server.go` that captures status code:
```go
type responseRecorder struct {
    http.ResponseWriter
    status int
}
func (r *responseRecorder) WriteHeader(code int) {
    r.status = code
    r.ResponseWriter.WriteHeader(code)
}
```
Wrap the mux in `Handler()` so the test suite can use it directly.

**Import cleanup**: remove `"log"` imports from all modified files and add `"log/slog"`. In `main.go` also add `"os"` and `"strings"` if not already present.

**Do not** add slog to `internal/store/`, `internal/keystore/`, or `internal/auth/` packages — those packages return errors, they don't log. Only log at the call site (in `main.go`, `server.go`, `session/manager.go`, `terminal/bridge.go`, `auth/oidc.go`).

## Validation

```bash
# Build must succeed with no log import remaining (except log/slog)
cd /home/stefans/git/agentic-workspace/projects/claude-overlay
go build ./...

# No bare "log" package imports remain (log/slog is fine)
grep -rn '"log"' --include='*.go' . && echo "FAIL: bare log imports remain" || echo "PASS"

# All tests pass
go test ./...

# Smoke: binary logs JSON on startup
OVERLAY_SESSION_SECRET=test123 OVERLAY_LOG_LEVEL=debug ./agentic-hive 2>&1 | head -5 | python3 -c "import sys,json; [json.loads(l) for l in sys.stdin]" && echo "PASS: JSON output"

# Log level filtering: debug suppressed at info level
OVERLAY_SESSION_SECRET=test123 OVERLAY_LOG_LEVEL=info ./agentic-hive 2>&1 | grep '"level":"DEBUG"' && echo "FAIL: debug logs leaked" || echo "PASS"
```

## Out of Scope

- Switching to a third-party logging library (zerolog, zap) — use stdlib slog only
- Adding log sampling or rate limiting
- Log rotation or file output — stdout only
- Adding slog to test files — test output is fine as-is
- Changing any log message content in `internal/keystore/` or `internal/store/` — those don't log
