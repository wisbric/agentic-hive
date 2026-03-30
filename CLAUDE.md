# Agentic Hive

## Project

Go web application for managing tmux sessions across remote SSH hosts. Single binary, SQLite persistence, Helm chart for Kubernetes deployment.

**Repo:** `github.com/wisbric/agentic-hive`
**Live:** https://hive.example.com (configure your own deployment)
**Cluster:** your-cluster, namespace `agentic-hive`

## Structure

```
cmd/server/           — entry point + embedded static files
cmd/server/static/    — HTML/CSS/JS frontend (vanilla, no build step)
internal/
  auth/               — JWT, local login, OIDC, CSRF, rate limiting, swappable handlers
  backup/             — SQLite VACUUM INTO backup
  config/             — env var config + DB settings resolution
  keystore/           — SSH key encryption (local AES-256-GCM, Vault, swappable)
  metrics/            — Prometheus instrumentation
  server/             — HTTP server, API handlers, middleware, settings, audit
  session/            — tmux session CRUD, polling
  sshpool/            — persistent SSH connections, TOFU host keys
  store/              — SQLite persistence, migrations, models
  terminal/           — WebSocket-to-SSH PTY bridge
deploy/helm/          — Helm chart
docs/prps/            — historical PRPs (design decisions)
```

## Build & Test

```bash
go test ./... -v          # requires CGO_ENABLED=1 (sqlite3)
go build -o agentic-hive ./cmd/server
```

## Deploy

CI builds on push to main. To deploy manually:
```bash
SHA=$(git rev-parse --short=8 HEAD)
helm upgrade agentic-hive deploy/helm/agentic-hive \
  --namespace agentic-hive --kube-context your-cluster \
  --reuse-values --set image.tag=$SHA
```

## Key Patterns

- **Config resolution:** env vars > DB settings > defaults. See `config.ResolveSettings()`.
- **Hot-reload:** OIDC and Vault handlers are wrapped in `Swappable*` structs with RWMutex. Settings API triggers re-initialization without restart.
- **Auth flow:** JWT in httpOnly cookie (`session`), CSRF double-submit (`csrf` cookie + `X-CSRF-Token` header). CSRF skips if no session cookie (auth middleware handles rejection).
- **SSH keys:** never plaintext on disk. Local backend: AES-256-GCM + Argon2id in SQLite. Vault backend: KVv2 with `{"private_key": "..."}`.
- **Vault key refs:** servers can have `key_source="vault_ref"` with a user-specified Vault path. Key is read live on every connection (no copy). See `sshpool.getKey()`.
- **Host keys:** TOFU model. First connect stores key, subsequent connects verify. Mismatch → `key_mismatch` status, admin must `POST /api/servers/:id/accept-key`.
- **Per-user isolation:** servers have `owner_id`. Users see only their own servers. Admins see all.
- **Session lifecycle:** `?live=true` on session list endpoint bypasses the poll cache for immediate feedback after create/kill. No auto-refresh — manual refresh button.
- **Static files:** embedded via `go:embed` from `cmd/server/static/`. No build step, no framework.

## Conventions

- Structured logging via `log/slog` (JSON output)
- Store constants for roles (`RoleAdmin`/`RoleUser`) and status (`StatusReachable`/`StatusUnreachable`/etc.)
- All state-changing endpoints require CSRF token
- Admin endpoints use `RequireAdmin` middleware
- Audit log entries for all security-relevant actions
- Commits follow conventional commits (`feat:`, `fix:`, `refactor:`, `docs:`, `ci:`, `chore:`)

## Code Review Checklist — Resource Leaks

When reviewing code that creates goroutines, connections, or long-lived resources, check:

1. **Goroutine lifecycle:** every `go func()` must have a clear exit path. If it reads from a channel or connection, what closes it? If it blocks on `Wait()`, can it be unblocked?
2. **WebSocket + SSH pairing:** when a WebSocket closes, the SSH session MUST be explicitly closed (not just stdin). Send SIGHUP + Close(). Don't rely on `defer` after a blocking `Wait()`.
3. **Connection pool cleanup:** `Remove()` and `Close()` must close the underlying connections, not just delete from the map.
4. **Polling goroutines:** `Stop()` must actually terminate the goroutine, not just signal it. Verify with `goleak`.
5. **Defer ordering:** `defer close(done)` must come BEFORE `defer resource.Close()` so the done channel signals watchers before the resource is closed.
6. **Metric symmetry:** every `gauge.Inc()` must have a matching `gauge.Dec()` on all exit paths, including error paths.
7. **Context cancellation:** long-running operations should respect `ctx.Done()` and clean up when the context is cancelled.

## Important Notes

- Go module path: `github.com/wisbric/agentic-hive`
- CGO is required for sqlite3 — the Dockerfile uses `golang:1.26-alpine` with `gcc musl-dev`
- The Vault secret path should NOT include the `secret/` mount prefix (KVv2 client adds it automatically)
- `OVERLAY_SESSION_SECRET` is required — binary refuses to start without it
