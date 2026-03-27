# PRP-14: Prometheus Metrics

## Goal

Add a `/metrics` endpoint with operational metrics for Prometheus scraping.

## Background

There is currently no observability into the running application. Operators cannot monitor:
- How many SSH connections are active or failing
- Whether the session poller is keeping up
- How many concurrent terminal WebSocket sessions are open
- Auth failure rates (useful for detecting credential stuffing)
- HTTP request latency per endpoint

The application runs in Kubernetes; Prometheus is the standard scrape target. The Helm chart has no `podAnnotations` for scraping yet. Adding `github.com/prometheus/client_golang` is the standard Go approach — it integrates as a single HTTP handler and provides a global registry.

## Requirements

1. Add `github.com/prometheus/client_golang` as a direct dependency in `go.mod` (use `go get github.com/prometheus/client_golang/prometheus/promhttp`).

2. Register the following metrics in a new package `internal/metrics/metrics.go` using `prometheus.MustRegister`:

   | Metric | Type | Labels | Description |
   |--------|------|--------|-------------|
   | `agentic_hive_ssh_connections_active` | Gauge | — | Current cached SSH connections in pool |
   | `agentic_hive_sessions_active` | GaugeVec | `server_id` | Active tmux sessions per server (updated each poll cycle) |
   | `agentic_hive_websocket_connections_active` | Gauge | — | Current terminal WebSocket connections |
   | `agentic_hive_http_requests_total` | CounterVec | `method`, `path`, `status` | Total HTTP requests |
   | `agentic_hive_http_request_duration_seconds` | HistogramVec | `method`, `path` | Request latency (buckets: 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5) |
   | `agentic_hive_auth_failures_total` | CounterVec | `reason` | Login failures (reasons: `invalid_credentials`, `token_expired`, `token_invalid`) |
   | `agentic_hive_ssh_errors_total` | CounterVec | `server_id` | SSH connection/exec errors per server |

3. Expose metrics at `GET /metrics` using `promhttp.Handler()`. This route is public (no auth required) — it is the standard Prometheus convention, and network-level access control is the operator's responsibility.

4. Add metric instrumentation at these callsites:
   - **SSH pool** (`internal/sshpool/pool.go`): increment `ssh_connections_active` on successful connect, decrement on `Remove` and `Close`. Increment `ssh_errors_total` on dial failure or reconnect failure in `Exec`.
   - **Session manager** (`internal/session/manager.go`): after each successful `pollAll` cycle, set `sessions_active` gauge per server ID from the polled session count. Clear the gauge for unreachable servers (set to 0).
   - **Terminal bridge** (`internal/terminal/bridge.go`): increment `websocket_connections_active` when a WebSocket connection is fully established (after `upgrader.Upgrade` succeeds), decrement in `defer` at the top of `HandleTerminal`.
   - **Auth middleware** (`internal/auth/auth.go` in `RequireAuth`): increment `auth_failures_total{reason="token_expired"}` or `reason="token_invalid"` when JWT verification fails. In `internal/auth/local.go` `HandleLogin`: increment `auth_failures_total{reason="invalid_credentials"}` on bcrypt mismatch or user-not-found.
   - **HTTP middleware** (`internal/server/server.go`): the logging middleware from PRP-12 can be extended to also record `http_requests_total` and `http_request_duration_seconds`. If PRP-12 is not yet merged, add a standalone metrics middleware.

5. Add Helm values for Prometheus scraping in `deploy/helm/agentic-hive/values.yaml`:
   ```yaml
   metrics:
     enabled: false
     podAnnotations:
       prometheus.io/scrape: "true"
       prometheus.io/port: "8080"
       prometheus.io/path: "/metrics"
   ```
   In `deploy/helm/agentic-hive/templates/deployment.yaml`, conditionally merge `metrics.podAnnotations` into the pod's `annotations` block when `metrics.enabled` is true.

## Implementation Notes

**Package structure**: Create `internal/metrics/metrics.go` as a singleton that calls `prometheus.MustRegister` at package init or via an `Init()` function called from `main.go`. Export typed accessors (not the raw `prometheus.*` vars) so callsites don't import prometheus directly:

```go
package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
    SSHConnectionsActive      prometheus.Gauge
    SessionsActive            *prometheus.GaugeVec
    WebSocketConnectionsActive prometheus.Gauge
    HTTPRequestsTotal         *prometheus.CounterVec
    HTTPRequestDuration       *prometheus.HistogramVec
    AuthFailuresTotal         *prometheus.CounterVec
    SSHErrorsTotal            *prometheus.CounterVec
)

func Init() {
    SSHConnectionsActive = prometheus.NewGauge(...)
    // ... register all
    prometheus.MustRegister(
        SSHConnectionsActive,
        SessionsActive,
        ...
    )
}
```

Call `metrics.Init()` from `main.go` before wiring anything else.

**SSH pool gauge accuracy**: The pool uses a `map[string]*ssh.Client` protected by `sync.RWMutex`. Increment the gauge in `getOrConnect` only when a new connection is stored (the `p.conns[serverID] = client` line after the double-check lock). Decrement in `Remove` and in `Close` (once per deleted entry). The `Exec` reconnect path also stores a new connection — ensure the gauge goes up there too (after `p.conns[serverID] = client`).

**Sessions gauge**: In `pollAll`, after updating `m.sessions[srv.ID]`, call:
```go
metrics.SessionsActive.With(prometheus.Labels{"server_id": srv.ID}).Set(float64(len(sessions)))
```
For unreachable servers, set to 0 rather than deleting the label set — this avoids gaps in Prometheus graphs.

**HTTP path normalization**: Use the registered pattern (e.g., `/api/servers/{id}`) not the raw URL path, to avoid high cardinality. In the metrics middleware, `r.Pattern` (available in Go 1.22+ with the new `net/http` mux) gives the matched pattern. Since the project uses Go 1.26 (per `go.mod`), `r.Pattern` is available.

**Route for `/metrics`**: Register in `server.go`'s `routes()` method before the auth middleware block:
```go
s.mux.Handle("GET /metrics", promhttp.Handler())
```
Import `"github.com/prometheus/client_golang/prometheus/promhttp"`.

**Avoid double-counting**: The metrics middleware wraps the entire mux. The `/metrics` endpoint itself should not be recorded in `http_requests_total` (it would add noise). Skip recording when `r.URL.Path == "/metrics"`.

**No label cardinality explosion**: `server_id` labels are bounded by the number of registered servers (typically <20). `method`/`path`/`status` are bounded by the route set. Do not use username or any user-supplied string as a label.

## Validation

```bash
cd /home/stefans/git/agentic-workspace/projects/claude-overlay

# Dependency added and builds
go mod tidy
go build ./...

# All tests pass
go test ./...

# Runtime: metrics endpoint returns Prometheus text format
OVERLAY_SESSION_SECRET=test123 OVERLAY_DB_PATH=/tmp/test-overlay.db ./agentic-hive &
SERVER_PID=$!
sleep 1

curl -s http://localhost:8080/metrics | grep "agentic_hive_" | head -20
# Must include all 7 metric families

# Verify no auth required
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/metrics)
[ "$HTTP_STATUS" = "200" ] && echo "PASS: metrics public" || echo "FAIL: got $HTTP_STATUS"

# Verify specific metric names exist
for metric in ssh_connections_active sessions_active websocket_connections_active http_requests_total http_request_duration_seconds auth_failures_total ssh_errors_total; do
    grep -q "agentic_hive_${metric}" <(curl -s http://localhost:8080/metrics) && echo "PASS: $metric" || echo "FAIL: $metric missing"
done

kill $SERVER_PID
```

## Out of Scope

- OpenTelemetry / OTLP export — Prometheus scrape only
- Tracing (spans) — separate concern
- Custom Grafana dashboard provisioning
- Alerting rules
- Metrics authentication (bearer token scrape auth) — operator responsibility via network policy
- Go runtime metrics customization — the default `prometheus.DefaultRegisterer` includes Go runtime metrics automatically
