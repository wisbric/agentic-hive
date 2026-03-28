# Agentic Hive — Architecture

## Overview

Single Go binary serving a web dashboard for managing tmux sessions across remote SSH hosts. Two access modes: in-browser xterm.js terminal (WebSocket) or copied SSH command for local terminal.

```
Browser (xterm.js) <--WSS--> Agentic Hive (Go) <--SSH--> Remote tmux servers
```

## Component Map

```
cmd/server/main.go
  ├── config.Load()              — env vars + DB settings
  ├── store.Open()               — SQLite + migrations
  ├── keystore (local | vault)   — SSH key storage
  ├── sshpool.New()              — persistent SSH connections
  ├── session.NewManager()       — tmux CRUD + background poller
  ├── terminal.NewBridge()       — WebSocket ↔ SSH PTY
  ├── auth (local | OIDC)        — JWT + middleware
  └── server.New()               — HTTP server + static files
```

## Key Architecture Decisions

### Single replica + SQLite
No external database dependency. Trade-off: no HA. The PDB documents this constraint. Backup CronJob mitigates data loss risk.

### Pluggable KeyStore
`KeyStore` interface with two backends. Local: AES-256-GCM with Argon2id KDF, encrypted in SQLite. Vault: KVv2, keys never touch the app's DB. Swappable at runtime via admin UI.

### Swappable OIDC
`SwappableOIDCHandler` wraps the OIDC provider behind a RWMutex. Admin can configure Keycloak via the UI and it takes effect without restart. Returns 404 when not configured.

### SSH Host Key TOFU
First connection stores the host's public key. Subsequent connections verify it matches. Mismatch refuses the connection and marks the server `key_mismatch`. Admin resets via `accept-key` endpoint.

### Terminal Bridge
Each browser tab = independent WebSocket → SSH session → `tmux new -A -s <name>`. Session names validated against `[a-zA-Z0-9_-]+` to prevent command injection. Idle timeout optional.

### Config Resolution
Env vars > DB settings > defaults. The admin UI writes to DB, but env vars always win and show as read-only in the UI. `ResolveSettings()` produces a unified view with source attribution.

### No Auto-Refresh
The 30s background poller updates server status dots. Session lists use `?live=true` for immediate SSH query after create/kill. Manual refresh button for on-demand updates. This avoids the stale-cache-overwriting-UI problem.

## Security Layers

| Layer | Mechanism |
|-------|-----------|
| Auth | JWT in httpOnly secure cookie, local bcrypt or OIDC |
| RBAC | `RequireAdmin` middleware on server/user management |
| CSRF | Double-submit cookie, skipped for unauthenticated requests |
| Rate limiting | Per-IP on login, configurable window |
| SSH keys | AES-256-GCM at rest (separate encryption secret) or Vault |
| Host keys | TOFU verification, mismatch blocks connection |
| Input validation | Session names sanitized, shell-escaped commands |
| Audit | All security-relevant actions logged to SQLite + slog |
| Headers | CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy |

## Data Model

```sql
users          — id, username, password_hash, role, oidc_subject
servers        — id, name, host, port, ssh_user, status
ssh_keys       — server_id (FK), encrypted_key, salt, nonce
host_keys      — server_id (FK), host_key, fingerprint
session_templates — id, name, command, workdir, server_id
settings       — key, value (admin UI config)
audit_log      — id, timestamp, user_id, action, target, details, ip
```

## Deployment

Helm chart at `deploy/helm/agentic-hive/`. Key features:
- Single replica, `Recreate` strategy
- PVC for SQLite, longhorn storage
- Ingress with nginx WebSocket timeout annotations (3600s)
- TLS via cert-manager (letsencrypt-prod)
- Prometheus scrape annotations (optional)
- Backup CronJob (optional, PVC or S3)
- `imagePullSecrets` for GitLab registry
