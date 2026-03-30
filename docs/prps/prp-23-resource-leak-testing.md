# PRP-23: Resource Leak Testing Framework

## Goal

Add automated detection of goroutine leaks, connection leaks, and resource cleanup failures to the test suite, preventing regressions like the WebSocket/SSH session leak.

## Background

A WebSocket disconnect was leaking SSH sessions and goroutines because `sshSession.Wait()` blocked the handler forever after the browser tab closed. This was only caught through manual observation of memory growth. The test suite had no way to detect this class of bug — unit tests for the terminal bridge only tested message parsing, not connection lifecycle.

## Requirements

### 1. Goroutine Leak Detection via `goleak`

Add `go.uber.org/goleak` to the project.

Add `TestMain` with `goleak.VerifyTestMain(m)` to these packages:
- `internal/terminal` — most critical, handles WebSocket + SSH goroutines
- `internal/sshpool` — SSH connection pool with background keepalive
- `internal/session` — background polling goroutines
- `internal/auth` — rate limiter cleanup goroutine
- `internal/server` — HTTP server with middleware goroutines

Known goroutines that need filtering (they're intentional long-lived goroutines):
- `go.opencensus.io` / prometheus background workers — filter with `goleak.IgnoreTopFunction`
- Signal handler goroutine from `signal.NotifyContext`

Each package's `TestMain`:
```go
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m,
        goleak.IgnoreTopFunction("go.opencensus.io/stats/view.(*worker).start"),
        // add other known long-lived goroutines as needed
    )
}
```

### 2. Terminal Bridge Lifecycle Integration Test

Create `internal/terminal/lifecycle_test.go`:

**Test: WebSocket close triggers SSH cleanup**
1. Start a test SSH server (reuse pattern from `sshpool/tofu_test.go`)
2. Create a `Bridge` with the test pool
3. Use `httptest.Server` + `gorilla/websocket.Dialer` to open a WebSocket
4. Send a few keystrokes, verify echo
5. Close the WebSocket client
6. Assert: SSH session count returns to 0 (via pool or metrics)
7. Assert: no goroutine leak (goleak catches this)

**Test: Idle timeout triggers cleanup**
1. Same setup but with `idleTimeout: 2 * time.Second`
2. Open WebSocket, don't send anything
3. Wait 3 seconds
4. Assert: WebSocket receives idle_timeout message
5. Assert: connection closed, SSH session cleaned up

**Test: Multiple rapid connect/disconnect**
1. Open 5 WebSocket connections
2. Close them all rapidly
3. Assert: all SSH sessions cleaned up, goroutine count returns to baseline

### 3. Prometheus Metric Assertions

Create a test helper `internal/metrics/testutil.go`:
```go
func GetGaugeValue(g prometheus.Gauge) float64 {
    // Use prometheus testutil to extract current value
}
```

Use in terminal lifecycle tests:
```go
assert(metrics.WebSocketConnectionsActive, 0) // before
// open connection
assert(metrics.WebSocketConnectionsActive, 1)
// close connection
assert(metrics.WebSocketConnectionsActive, 0) // must return to 0
```

Same for `SSHConnectionsActive`.

### 4. Race Detector in CI

Update `.github/workflows/ci.yml` and `.gitlab-ci.yml`:
- Test command already uses `-race` flag — verify it's present
- Add a stress test job that runs `go test -race -count=5 ./internal/terminal/ ./internal/sshpool/`

### 5. SSH Pool Lifecycle Test

Create `internal/sshpool/lifecycle_test.go`:

**Test: Remove cleans up connection**
1. Connect to test SSH server via pool
2. Verify connection active (exec `echo ok`)
3. Call `pool.Remove(serverID)`
4. Verify connection count = 0
5. No goroutine leak

**Test: Close cleans up all connections**
1. Connect to 3 test SSH servers
2. Call `pool.Close()`
3. All connections gone, no leaks

### 6. Session Manager Lifecycle Test

**Test: Stop terminates polling goroutine**
1. Start manager with polling
2. Call `Stop()`
3. Assert polling goroutine exits (goleak)

## Implementation Notes

- The test SSH server pattern from `sshpool/tofu_test.go` should be extracted to a shared `internal/testutil/sshserver.go` since both `sshpool` and `terminal` tests need it
- WebSocket test client: use `gorilla/websocket.Dialer` against `httptest.Server`
- For the terminal lifecycle test, the test SSH server needs to run a simple command (e.g., `cat` which echoes stdin to stdout) instead of tmux
- `goleak` options should be collected in a shared `internal/testutil/goleak.go` so all packages use the same ignore list

## Validation

- `go test ./... -race -count=1` passes with goleak enabled
- Terminal lifecycle test: connect + disconnect + assert no leak
- Idle timeout test: idle + timeout + assert cleanup
- Stress test: 5 rapid connect/disconnect cycles + assert clean
- CI runs `-race` on all tests

## Out of Scope

- Memory profiling / heap analysis (pprof)
- Load testing / benchmarks
- File descriptor leak detection (OS-level)
- Network connection leak detection beyond SSH pool
