# Agentic Hive — Architecture

## Overview

Agentic Hive is a lightweight web application that acts as a central hub for managing tmux sessions across multiple remote hosts ("tmux servers"). It provides two access modes:

1. **SSH command** — one-click copy of an `ssh` command the user runs in their local terminal
2. **Browser terminal** — in-app xterm.js console for immediate access without leaving the browser

The app runs as a single container, manages SSH connectivity to registered hosts, and provides session lifecycle management (create, list, attach, destroy).

---

## Core Concepts

```
┌─────────────────────────────────────────────────────────┐
│                    Agentic Hive                         │
│                   (Go + xterm.js)                         │
│                                                           │
│  ┌──────────┐  ┌──────────────┐  ┌────────────────────┐ │
│  │  Auth     │  │  Session     │  │  Browser Terminal  │ │
│  │  (OIDC /  │  │  Manager     │  │  (xterm.js +       │ │
│  │  local)   │  │              │  │   WebSocket)       │ │
│  └──────────┘  └──────┬───────┘  └─────────┬──────────┘ │
│                        │                     │            │
│                 ┌──────┴─────────────────────┴──┐        │
│                 │        SSH Connection Pool     │        │
│                 │   (golang.org/x/crypto/ssh)    │        │
│                 └──────┬────────────┬────────────┘        │
└────────────────────────┼────────────┼─────────────────────┘
                         │            │
              ┌──────────▼──┐  ┌──────▼──────────┐
              │  tmux server │  │  tmux server    │
              │  devbox      │  │  local PC       │
              │  (SSH :22)   │  │  (SSH :22)      │
              └─────────────┘  └─────────────────┘
```

### Tmux Server

A remote host (or localhost) reachable over SSH that runs tmux. Each tmux server is registered in the app with:

- Display name (e.g., "devbox", "local")
- SSH host + port
- SSH user
- SSH key reference (encrypted locally or in external vault)

### Session

A tmux session on a tmux server. Sessions can be:

- **Created** by the app (via `ssh host "tmux new-session -d -s <name> '<command>'"`)
- **Discovered** from existing sessions on the host (via `tmux list-sessions`)
- **Attached** via browser terminal or local SSH command

---

## Architecture Decisions

### AD-1: Go backend with xterm.js frontend

**Decision:** Custom Go backend + xterm.js (not WeTTY, ttyd, or Guacamole).

**Why:**
- WeTTY (Node.js) handles multi-session well but adds ~80 MB runtime overhead and is slow on maintenance
- ttyd is tiny but can't handle independent concurrent sessions natively (shared terminal model)
- Guacamole is massive overkill (Java + Tomcat + database + guacd for just SSH terminals)
- Go backend with `golang.org/x/crypto/ssh` is lightweight (~20 MB binary), gives full control over session management, and matches the existing stack

**Trade-off:** More custom code to maintain, but the SSH+WebSocket bridge is ~200 lines of Go. xterm.js is battle-tested (powers VS Code's terminal).

### AD-2: Pluggable SSH key storage — local (default) or Vault

**Decision:** SSH private keys are stored via a pluggable `KeyStore` interface. Two backends ship out of the box:

1. **Local (default)** — keys encrypted at rest in the SQLite database using AES-256-GCM. The encryption key is derived from the app's `SESSION_SECRET` via Argon2id. Zero external dependencies.
2. **Vault (optional)** — keys stored in HashiCorp Vault / OpenBao (`secret/agentic-hive/ssh-keys/<server-name>`). Rotation and audit come for free.

**Why pluggable:**
- Not everyone runs a Vault instance — the local backend makes Agentic Hive usable standalone
- Users who already have Vault/OpenBao get first-class integration without compromises
- The `KeyStore` interface is small (Get/Put/Delete), so adding future backends (AWS KMS, SOPS, etc.) is trivial

**Local backend security model:**
- Keys are AES-256-GCM encrypted before writing to SQLite — never plaintext on disk
- The encryption key is derived from `SESSION_SECRET` (which must be 32+ random bytes)
- If `SESSION_SECRET` is lost, stored SSH keys become unrecoverable (by design — re-add them)
- The SQLite file should live on an encrypted volume in production for defense-in-depth

**Vault backend security model:**
- Keys never touch the app's filesystem or database
- The app holds a Vault token (or AppRole) in memory, fetches keys on demand
- Rotation and access auditing handled by Vault

**Future option:** SSH certificates signed by a Vault CA (short-lived, no stored keys at all). This is the ideal end state but requires `TrustedUserCAKeys` setup on all tmux servers.

### AD-3: Two access modes — SSH command and browser terminal

**Decision:** Both modes, not just browser.

**Why:**
- SSH command mode is zero-overhead: the user's local terminal, their local SSH agent, no key on the server at all. The app just generates the right `ssh -t user@host "tmux new -A -s <name>"` command.
- Browser terminal is for when you're on a device without SSH (tablet, Chromebook, someone else's machine) or want everything in one window.
- SSH command mode doesn't require the app to hold SSH keys for that user at all (the user's local key handles auth).

### AD-4: Session naming convention

**Decision:** Sessions are named `{user}-{label}-{short_id}`, e.g., `stefan-claude-a1b2c3`.

**Why:**
- Deterministic prefix allows listing all sessions for a user: `tmux list-sessions -F '#{session_name}' | grep '^stefan-'`
- Short ID prevents collisions
- Human-readable label for the dashboard

### AD-5: Lightweight persistence — SQLite

**Decision:** App state (registered servers, session metadata, user preferences) stored in SQLite. Not PostgreSQL.

**Why:**
- Single container deployment, no external database dependency
- Session metadata is small (server list, session names, last-seen timestamps)
- SSH keys are in OpenBao, not here
- If the SQLite file is lost, re-registering servers is trivial

---

## Components

### 1. Web UI (browser)

**Tech:** Vanilla JS + xterm.js + xterm-addon-fit + xterm-addon-webgl

**Pages:**
- **Dashboard** — list of registered tmux servers, each showing their sessions
- **Server detail** — sessions on that server with create/attach/kill actions
- **Terminal** — full-screen xterm.js terminal (opened in new tab per session)

**Interactions:**
- "Attach (SSH)" → copies `ssh -t user@host "tmux new -A -s <name>"` to clipboard
- "Attach (Browser)" → opens `/terminal/<server>/<session>` in new tab → WebSocket → SSH → tmux attach
- "New Session" → POST `/api/sessions` → creates detached tmux session on remote
- "Kill Session" → DELETE `/api/sessions/<id>` → `tmux kill-session -t <name>`

### 2. API Server (Go)

**Routes:**

```
POST   /api/auth/login          — authenticate user (local auth)
GET    /api/auth/oidc/login     — redirect to OIDC provider
GET    /api/auth/oidc/callback  — OIDC callback, creates session
POST   /api/auth/logout         — destroy session
GET    /api/servers              — list registered tmux servers
POST   /api/servers              — register a new tmux server
DELETE /api/servers/:id          — remove a tmux server
GET    /api/servers/:id/sessions — list tmux sessions (live query via SSH)
POST   /api/servers/:id/sessions — create new tmux session
DELETE /api/servers/:id/sessions/:name — kill tmux session
GET    /ws/terminal/:server/:session   — WebSocket: browser terminal
```

**Key packages:**
- `golang.org/x/crypto/ssh` — SSH client connections
- `github.com/gorilla/websocket` — WebSocket for browser terminal
- `github.com/mattn/go-sqlite3` — persistence
- `golang.org/x/crypto/argon2` — key derivation for local key encryption
- `github.com/hashicorp/vault/api` — OpenBao/Vault client (optional, for Vault key backend)

### 3. SSH Connection Pool

Maintains persistent SSH connections to registered tmux servers (avoids re-auth per action).

```go
type SSHPool struct {
    connections map[string]*ssh.Client  // keyed by server ID
    mu          sync.RWMutex
}
```

- Connections are established on first use and kept alive with keepalives
- Reconnects automatically on failure
- SSH keys fetched from configured KeyStore backend on connect, held in memory only

### 4. Terminal Bridge (WebSocket ↔ SSH ↔ tmux)

For browser terminal mode:

```
Browser (xterm.js) ←WebSocket→ Go server ←SSH session→ remote tmux
```

Flow:
1. Browser opens WebSocket to `/ws/terminal/<server>/<session>`
2. Go server opens SSH session to the tmux server (from pool)
3. SSH session runs `tmux attach -t <session>` with PTY
4. Bidirectional byte stream: WebSocket ↔ SSH channel
5. Window resize events forwarded from xterm.js → SSH PTY resize

Each browser tab = independent WebSocket = independent SSH session = independent tmux attach. 5-6 concurrent tabs is trivial.

### 5. Session Manager

Background goroutine that periodically polls registered servers:

```
Every 30s:
  for each server:
    ssh host "tmux list-sessions -F '#{session_name}:#{session_activity}:#{session_attached}:#{pane_current_command}'"
    → update dashboard state
    → mark stale sessions (idle > configurable threshold)
```

Optional: idle session cleanup (configurable, off by default — Claude Code sessions can be long-running).

---

## Security Model

### User Authentication

Two authentication modes, selectable via config:

**Local auth (default):**
- Username + bcrypt password stored in SQLite
- First-run setup creates the initial admin user
- Admin can create additional users via the UI or API
- Session token (JWT, signed with `SESSION_SECRET`) with configurable expiry

**OIDC / SSO (optional):**
- Standard OIDC provider support (Keycloak, Authentik, Dex, Google, etc.)
- Configured via `auth.oidc` in values.yaml or env vars
- Auto-creates user on first login (JIT provisioning)
- Maps OIDC groups/claims to roles (admin, user) via configurable claim mapping
- When OIDC is enabled, local auth can be disabled or kept as fallback

**Common to both:**
- All routes except `/api/auth/login` and `/api/auth/oidc/*` require valid session
- JWT stored in httpOnly secure cookie (not localStorage)
- CSRF protection on state-changing endpoints

### Transport Security

- HTTPS required (TLS termination at the container or reverse proxy)
- WebSocket connections use WSS (encrypted)
- SSH connections to tmux servers use SSH protocol encryption

### SSH Key Security

- Keys always encrypted at rest — either AES-256-GCM in SQLite (local) or in Vault
- Decrypted keys held in memory only, never written to disk in plaintext
- Each tmux server can have its own key or share one
- Key upload via the UI (paste or file upload) — encrypted before storage

### Access Control

- Each user can only see/manage servers they have been granted access to
- Server registration requires admin role
- Session operations scoped to the user's allowed servers

---

## Deployment

### Container Image

```dockerfile
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /agentic-hive ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache openssh-client ca-certificates
COPY --from=build /agentic-hive /usr/local/bin/
COPY static/ /app/static/
EXPOSE 8080
ENTRYPOINT ["agentic-hive"]
```

### Helm Chart

Deployed via Helm. Chart lives in `deploy/helm/agentic-hive/`.

```
deploy/helm/agentic-hive/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── ingress.yaml
│   ├── pvc.yaml
│   ├── secret.yaml
│   ├── configmap.yaml
│   └── _helpers.tpl
```

### values.yaml (reference)

```yaml
replicaCount: 1

image:
  repository: registry.wisbric.com/agentic-hive
  tag: latest
  pullPolicy: IfNotPresent

# --- Authentication ---
auth:
  # "local" (default) or "oidc"
  mode: local

  # Local auth: initial admin user (created on first run)
  local:
    adminUsername: admin
    # adminPassword: set via secret or auto-generated on first run

  # OIDC auth (optional, ignored when mode=local)
  oidc:
    issuerUrl: ""           # e.g., https://auth.wisbric.com/realms/overlay
    clientId: ""
    clientSecret: ""        # reference to existing secret recommended
    redirectUrl: ""         # e.g., https://overlay.dev-ai.wisbric.com/api/auth/oidc/callback
    scopes: ["openid", "profile", "email"]
    # Claim mapping for roles
    rolesClaim: "groups"    # OIDC claim containing roles/groups
    adminGroup: "overlay-admin"

# --- SSH Key Storage ---
keyStore:
  # "local" (default) or "vault"
  backend: local

  # Vault backend (optional, ignored when backend=local)
  vault:
    address: ""             # e.g., https://vault.wisbric.com
    authMethod: token       # "token" or "approle"
    token: ""               # reference to existing secret recommended
    approle:
      roleId: ""
      secretId: ""
    secretPath: "secret/agentic-hive/ssh-keys"

# --- App Config ---
config:
  sessionSecret: ""         # 32+ random bytes, auto-generated if empty
  idleTimeout: 0            # seconds, 0 = no auto-cleanup
  pollInterval: 30          # seconds between session list refreshes

# --- Persistence ---
persistence:
  enabled: true
  size: 1Gi
  storageClass: ""          # uses default SC if empty
  accessMode: ReadWriteOnce

# --- Networking ---
service:
  type: ClusterIP
  port: 8080

ingress:
  enabled: true
  className: nginx
  annotations: {}
  hosts:
    - host: overlay.dev-ai.wisbric.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: overlay-tls
      hosts:
        - overlay.dev-ai.wisbric.com

# --- Resources ---
resources:
  requests:
    cpu: 50m
    memory: 64Mi
  limits:
    cpu: 500m
    memory: 256Mi
```

### Minimal quickstart (local auth, local key store)

```bash
helm install agentic-hive deploy/helm/agentic-hive/ \
  --set config.sessionSecret=$(openssl rand -hex 32)
```

### With OIDC + Vault

```bash
helm install agentic-hive deploy/helm/agentic-hive/ \
  --set auth.mode=oidc \
  --set auth.oidc.issuerUrl=https://auth.wisbric.com/realms/overlay \
  --set auth.oidc.clientId=agentic-hive \
  --set auth.oidc.clientSecret=<secret> \
  --set keyStore.backend=vault \
  --set keyStore.vault.address=https://vault.wisbric.com \
  --set keyStore.vault.token=<token>
```

### Runtime Requirements

- Network access to tmux server SSH ports from the pod
- PVC for SQLite DB (or set `persistence.enabled=false` for ephemeral)
- Ingress controller with WebSocket support (nginx-ingress handles this by default)
- If using Vault backend: network access to Vault from the pod

---

## Remote tmux Management — Command Reference

The app executes these over SSH:

| Action | Command |
|--------|---------|
| List sessions | `tmux list-sessions -F '#{session_name}:#{session_created}:#{session_windows}:#{session_attached}'` |
| Create session | `tmux new-session -d -s <name> -c <workdir> '<command>'` |
| Create with env | `tmux new-session -d -s <name> -e 'KEY=val' '<command>'` |
| Kill session | `tmux kill-session -t <name>` |
| Check if alive | `tmux has-session -t <name>` |
| Attach (for terminal bridge) | `tmux attach -t <name>` (with PTY) |
| Get session activity | `tmux list-sessions -F '#{session_name}:#{session_activity}'` |

### Gotchas

- **PTY allocation**: `tmux attach` requires a PTY. The SSH library must request one (`ssh.RequestPty`).
- **Environment**: SSH non-interactive sessions get minimal env. Pass vars explicitly via `-e` or source profile.
- **Quoting**: Commands go through Go → SSH → remote shell. Use single-level quoting carefully.
- **Exit codes**: `tmux new-session -d` returns 1 if name already exists. Handle gracefully.
- **`destroy-unattached`**: Do NOT set this in `~/.tmux.conf` on targets — it kills sessions when the browser disconnects. The app manages lifecycle instead.

---

## Session Creation Templates

The "New Session" dialog supports templates for common use cases:

| Template | Command | Working Dir |
|----------|---------|-------------|
| Claude Code | `claude` | `~/` |
| Claude Code (resume) | `claude --resume` | `~/` |
| Shell | `bash` | `~/` |
| Custom | user-provided | user-provided |

Templates are stored in SQLite and can be customized per server.

---

## Future Considerations

- **SSH certificates via Vault CA** — eliminates stored keys entirely, short-lived certs per connection
- **Additional KeyStore backends** — AWS KMS, GCP KMS, SOPS
- **Multi-user with RBAC** — different users see different servers, shared sessions for pair programming
- **Session recording** — `tmux pipe-pane` to capture session output for audit/replay
- **Notifications** — webhook or push when a long-running Claude Code session completes
- **Mobile-friendly UI** — responsive terminal for tablet access
