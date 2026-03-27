# PRP-16: Readiness Probe Depth

## Goal

Make `/readyz` actually check application health, not just return 200.

## Background

`/readyz` in `internal/server/server.go` currently does:
```go
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

This always returns 200 regardless of application state. Kubernetes uses `/readyz` to decide whether to route traffic to the pod. A pod that has failed its SQLite connection (corrupt DB, full disk, permissions error) or has no reachable servers (when a deployment requires at least one) will continue to receive traffic until liveness kills it — which is too slow and results in request failures delivered to end users.

The `Store` already exposes `db *sql.DB` via `DB()` method. A `SELECT 1` query is the standard DB ping that exercises the full connection lifecycle without touching any data.

The `Store.ListServers()` method returns the current list of servers with their `status` field (`store.StatusReachable` = `"reachable"`). Checking for at least one reachable server is valuable for deployments where the overlay must always have connectivity to infrastructure.

## Requirements

1. `/readyz` performs the following checks:
   - **database**: execute `SELECT 1` on the SQLite DB with a 1-second context timeout. Fail if the query returns an error or times out.
   - **servers** (conditional): if `OVERLAY_READYZ_REQUIRE_SERVER=true`, call `store.ListServers()` and check that at least one has `status == "reachable"`. If no servers are registered at all, this check passes (it's not a fault if the server is freshly deployed). Only fail if servers exist but none is reachable.

2. Response body always includes per-check status:
   ```json
   {"status":"ok","checks":{"database":"ok","servers":"ok"}}
   ```
   or on failure:
   ```json
   {"status":"fail","checks":{"database":"ok","servers":"no reachable servers"}}
   ```
   The `"servers"` key is always present in the response (either `"ok"` or an error string or `"disabled"` when the check is not required).

3. HTTP status: `200` if all checks pass, `503` if any check fails.

4. Add `ReadyzRequireServer bool` to `internal/config/config.go`, populated from `OVERLAY_READYZ_REQUIRE_SERVER` env var. Use the existing `envOr` pattern: `ReadyzRequireServer: envOr("OVERLAY_READYZ_REQUIRE_SERVER", "") == "true"`.

5. The `Store` must expose a health-check method. Add `Ping(ctx context.Context) error` to `internal/store/store.go`:
   ```go
   func (s *Store) Ping(ctx context.Context) error {
       _, err := s.db.ExecContext(ctx, "SELECT 1")
       return err
   }
   ```
   This avoids exposing `s.db` further and keeps the health logic in the right layer.

## Implementation Notes

**`internal/server/server.go` — `handleReadyz` replacement:**

```go
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
    checks := map[string]string{}
    overallOK := true

    // Database check
    dbCtx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
    defer cancel()
    if err := s.store.Ping(dbCtx); err != nil {
        checks["database"] = "error: " + err.Error()
        overallOK = false
    } else {
        checks["database"] = "ok"
    }

    // Servers check (conditional)
    if s.cfg.ReadyzRequireServer {
        servers, err := s.store.ListServers()
        if err != nil {
            checks["servers"] = "error: " + err.Error()
            overallOK = false
        } else if len(servers) == 0 {
            checks["servers"] = "ok" // no servers registered yet, not a fault
        } else {
            reachable := 0
            for _, srv := range servers {
                if srv.Status == store.StatusReachable {
                    reachable++
                }
            }
            if reachable == 0 {
                checks["servers"] = "no reachable servers"
                overallOK = false
            } else {
                checks["servers"] = "ok"
            }
        }
    } else {
        checks["servers"] = "disabled"
    }

    w.Header().Set("Content-Type", "application/json")
    status := "ok"
    if !overallOK {
        w.WriteHeader(http.StatusServiceUnavailable)
        status = "fail"
    }
    json.NewEncoder(w).Encode(map[string]any{
        "status": status,
        "checks": checks,
    })
}
```

**Imports to add in `server.go`**: `"context"`, `"time"`. `store` is already imported.

**`store.StatusReachable`**: Already defined in `internal/store/models.go` (the servers file uses `store.StatusReachable` and `store.StatusUnreachable`). Use the constant directly.

**Do not** hold any lock or connection across the readyz handler beyond the 1-second timeout. The SQLite check is fast (<1ms normally).

**Do not** add the readyz endpoint to the logging middleware's skip list — it's fine to log these probes at debug level if PRP-12 is implemented first. If PRP-12 is not yet done, no change needed.

**Helm probe configuration**: The existing Kubernetes readiness probe in `deploy/helm/agentic-hive/templates/deployment.yaml` likely points to `/readyz`. No change needed to the probe itself — it already expects 200. The `initialDelaySeconds` should be low (5s) since SQLite is local. Check the current deployment template and ensure it has:
```yaml
readinessProbe:
  httpGet:
    path: /readyz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
  failureThreshold: 3
```
If the probe is missing, add it. If it exists but points to `/healthz`, change it to `/readyz`.

**Liveness vs readiness**: `/healthz` remains the liveness probe (always returns 200 if the process is running). `/readyz` is the readiness probe. This is the correct Kubernetes pattern.

## Validation

```bash
cd /home/stefans/git/agentic-workspace/projects/claude-overlay

# Build
go build ./...

# All tests pass
go test ./...

# Healthy state: returns 200 with checks
OVERLAY_SESSION_SECRET=test123 OVERLAY_DB_PATH=/tmp/test-readyz.db ./agentic-hive &
SERVER_PID=$!
sleep 1

RESPONSE=$(curl -s http://localhost:8080/readyz)
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/readyz)
echo "HTTP $STATUS: $RESPONSE"
[ "$STATUS" = "200" ] && echo "PASS: 200" || echo "FAIL: expected 200, got $STATUS"
echo "$RESPONSE" | python3 -c "import sys,json; d=json.load(sys.stdin); assert d['status']=='ok'; assert d['checks']['database']=='ok'; print('PASS: response shape correct')"

# servers check disabled by default
echo "$RESPONSE" | python3 -c "import sys,json; d=json.load(sys.stdin); assert d['checks']['servers']=='disabled'; print('PASS: servers disabled')"

kill $SERVER_PID

# With READYZ_REQUIRE_SERVER=true and no servers: should still return 200 (no servers = ok)
OVERLAY_SESSION_SECRET=test123 OVERLAY_DB_PATH=/tmp/test-readyz2.db OVERLAY_READYZ_REQUIRE_SERVER=true ./agentic-hive &
SERVER_PID=$!
sleep 1
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/readyz)
[ "$STATUS" = "200" ] && echo "PASS: no servers registered = ok" || echo "FAIL: got $STATUS"
kill $SERVER_PID

# Helm probe config present
grep -A5 "readinessProbe" deploy/helm/agentic-hive/templates/deployment.yaml | grep "/readyz" && echo "PASS: readyz probe" || echo "FAIL: readyz probe missing"
```

## Out of Scope

- Checking external dependencies (Vault reachability, OIDC provider) in `/readyz` — these are startup-time dependencies only
- A separate `/livez` endpoint — `/healthz` serves this role
- Custom readiness gate implementations for Kubernetes
- Exposing individual check results as Prometheus metrics (that belongs in PRP-14)
- Timeout configurability for the DB ping — 1 second is appropriate and hard-coded
