# Agentic Hive

A lightweight web application for managing tmux sessions across multiple remote hosts. Access your Claude Code, Codex, or shell sessions from anywhere — via browser terminal or SSH command.

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square)
![License](https://img.shields.io/badge/license-proprietary-lightgrey?style=flat-square)

## Features

- **Multi-host tmux management** — register SSH-accessible servers, create/list/kill tmux sessions from a single dashboard
- **Browser terminal** — full xterm.js terminal in-browser via WebSocket, no SSH client needed
- **SSH command copy** — one-click copy of the `ssh -t` command for local terminal use
- **Session templates** — Claude Code, Claude Code (full access), Codex, Shell, or custom commands
- **Local + SSO auth** — built-in username/password with optional OIDC (Keycloak, Authentik, etc.)
- **Encrypted SSH key storage** — AES-256-GCM with Argon2id (local) or HashiCorp Vault / OpenBao
- **Admin UI** — user management, OIDC/Vault configuration, poll interval — all hot-reloadable without restart
- **Production-ready** — structured logging, Prometheus metrics, graceful shutdown, audit log, CSRF protection, rate limiting, SSH host key verification (TOFU)

## Quick Start

### Docker

```bash
docker run -p 8080:8080 \
  -e OVERLAY_SESSION_SECRET=$(openssl rand -hex 32) \
  -v hive-data:/data \
  registry.gitlab.com/adfinisde/agentic-workspace/agentic-hive:latest
```

Open http://localhost:8080 — create your admin account on first visit.

### Helm (Kubernetes)

```bash
helm install agentic-hive \
  oci://registry.gitlab.com/adfinisde/agentic-workspace/agentic-hive/helm/agentic-hive \
  --set config.sessionSecret=$(openssl rand -hex 32) \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=hive.example.com \
  --set ingress.hosts[0].paths[0].path=/ \
  --set ingress.hosts[0].paths[0].pathType=Prefix
```

### From source

```bash
go build -o agentic-hive ./cmd/server
OVERLAY_SESSION_SECRET=$(openssl rand -hex 32) \
OVERLAY_DB_PATH=./hive.db \
./agentic-hive
```

## Usage

1. **Create admin account** — first visit shows a setup form
2. **Add a tmux server** — provide hostname, SSH user, and paste the SSH private key
3. **Create sessions** — pick a template (Claude Code, Codex, Shell, etc.) and a working directory
4. **Connect** — click "Terminal" for in-browser access or "SSH" to copy the command for your local terminal

## Architecture

```
Browser (xterm.js) <--WebSocket--> Agentic Hive (Go) <--SSH--> Remote tmux server
```

Single Go binary, SQLite for state, no external dependencies. Designed for single-replica deployment.

| Component | Package | Purpose |
|-----------|---------|---------|
| Config | `internal/config` | Environment variable loading |
| Store | `internal/store` | SQLite persistence, migrations |
| Auth | `internal/auth` | JWT, local login, OIDC, CSRF, rate limiting |
| KeyStore | `internal/keystore` | SSH key encryption (local AES-256-GCM or Vault) |
| SSH Pool | `internal/sshpool` | Persistent SSH connections with auto-reconnect |
| Session | `internal/session` | tmux CRUD, background polling |
| Terminal | `internal/terminal` | WebSocket-to-SSH PTY bridge |
| Metrics | `internal/metrics` | Prometheus instrumentation |
| Backup | `internal/backup` | SQLite VACUUM INTO backup |
| Server | `internal/server` | HTTP server, API handlers, static file serving |

## Configuration

All configuration is via environment variables. When deployed with Helm, these are set via `values.yaml`.

### Required

| Variable | Description |
|----------|-------------|
| `OVERLAY_SESSION_SECRET` | JWT signing secret (32+ random hex bytes) |

### Optional

| Variable | Default | Description |
|----------|---------|-------------|
| `OVERLAY_LISTEN` | `:8080` | Listen address |
| `OVERLAY_DB_PATH` | `/data/overlay.db` | SQLite database path |
| `OVERLAY_AUTH_MODE` | `local` | `local` or `oidc` |
| `OVERLAY_ENCRYPTION_SECRET` | (falls back to SESSION_SECRET) | Separate secret for SSH key encryption |
| `OVERLAY_POLL_INTERVAL` | `30` | Session polling interval (seconds) |
| `OVERLAY_TERMINAL_IDLE_TIMEOUT` | `0` | WebSocket idle timeout (seconds, 0=disabled) |
| `OVERLAY_LOG_LEVEL` | `info` | Log level (debug/info/warn/error) |
| `OVERLAY_KEYSTORE_BACKEND` | `local` | `local` or `vault` |
| `OVERLAY_LOGIN_RATE_LIMIT` | `5` | Max failed login attempts per window |
| `OVERLAY_LOGIN_RATE_WINDOW` | `900` | Rate limit window (seconds) |
| `OVERLAY_READYZ_REQUIRE_SERVER` | `false` | Readiness probe requires reachable server |

### OIDC (when `OVERLAY_AUTH_MODE=oidc`)

| Variable | Description |
|----------|-------------|
| `OVERLAY_OIDC_ISSUER_URL` | OIDC provider URL |
| `OVERLAY_OIDC_CLIENT_ID` | OAuth2 client ID |
| `OVERLAY_OIDC_CLIENT_SECRET` | OAuth2 client secret |
| `OVERLAY_OIDC_REDIRECT_URL` | Callback URL |
| `OVERLAY_OIDC_ROLES_CLAIM` | JWT claim for roles (default: `groups`) |
| `OVERLAY_OIDC_ADMIN_GROUP` | Group name for admin role (default: `overlay-admin`) |

### Vault (when `OVERLAY_KEYSTORE_BACKEND=vault`)

| Variable | Description |
|----------|-------------|
| `OVERLAY_VAULT_ADDR` | Vault/OpenBao address |
| `OVERLAY_VAULT_TOKEN` | Vault token |
| `OVERLAY_VAULT_SECRET_PATH` | KVv2 secret path prefix |

OIDC, Vault, and poll interval can also be configured via the admin UI (Settings panel) without restart. Environment variables take precedence over UI settings.

## Helm Values

See [`deploy/helm/agentic-hive/values.yaml`](deploy/helm/agentic-hive/values.yaml) for the full reference. Key sections:

- `auth` — authentication mode and OIDC settings
- `keyStore` — SSH key storage backend
- `config` — secrets, timeouts, poll interval
- `ingress` — external access with automatic WebSocket timeout annotations for nginx
- `persistence` — SQLite PVC
- `backup` — scheduled backup CronJob (PVC or S3)
- `metrics` — Prometheus scrape annotations
- `podDisruptionBudget` — drain behavior documentation

## API

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/healthz` | - | Liveness probe |
| `GET` | `/readyz` | - | Readiness probe (checks DB, optionally servers) |
| `GET` | `/metrics` | - | Prometheus metrics |
| `POST` | `/api/auth/login` | - | Local login |
| `POST` | `/api/auth/setup` | - | First-run admin setup |
| `POST` | `/api/auth/logout` | user | Logout |
| `GET` | `/api/auth/oidc/login` | - | OIDC login redirect |
| `GET` | `/api/servers` | user | List servers |
| `POST` | `/api/servers` | admin | Register server |
| `DELETE` | `/api/servers/:id` | admin | Remove server |
| `PUT` | `/api/servers/:id/key` | admin | Upload SSH key |
| `POST` | `/api/servers/:id/accept-key` | admin | Accept new host key |
| `GET` | `/api/servers/:id/sessions` | user | List tmux sessions |
| `POST` | `/api/servers/:id/sessions` | user | Create session |
| `DELETE` | `/api/servers/:id/sessions/:name` | user | Kill session |
| `GET` | `/api/templates` | user | List session templates |
| `GET` | `/ws/terminal/:server/:session` | user | WebSocket terminal |
| `GET` | `/api/users` | admin | List users |
| `DELETE` | `/api/users/:id` | admin | Delete user |
| `GET` | `/api/admin/settings` | admin | Get settings |
| `PUT` | `/api/admin/settings` | admin | Update settings |
| `POST` | `/api/admin/settings/test-oidc` | admin | Test OIDC discovery |
| `POST` | `/api/admin/settings/test-vault` | admin | Test Vault connection |
| `GET` | `/api/admin/audit` | admin | Audit log |
| `POST` | `/api/admin/backup` | admin | Download DB backup |

## Security

- **Authentication**: Local bcrypt + JWT, or OIDC SSO. httpOnly secure cookies.
- **RBAC**: Admin/user roles. Server management restricted to admins.
- **CSRF**: Double-submit cookie pattern on all state-changing endpoints.
- **Rate limiting**: Per-IP brute-force protection on login.
- **SSH keys**: AES-256-GCM encrypted at rest (Argon2id KDF) or stored in Vault.
- **Host keys**: Trust-on-first-use (TOFU) — stored on first connect, rejected on mismatch.
- **Audit log**: All security-relevant actions logged to SQLite + structured logs.

## Development

```bash
# Run tests
go test ./... -v

# Build
go build -o agentic-hive ./cmd/server

# Run locally
OVERLAY_SESSION_SECRET=dev-secret-change-me-in-prod-32chars \
OVERLAY_DB_PATH=./dev.db \
OVERLAY_LOG_LEVEL=debug \
./agentic-hive
```

## License

Proprietary. All rights reserved.
