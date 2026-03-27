# Claude Overlay — Design Spec

## Summary

Claude Overlay is a lightweight web application for managing tmux sessions across multiple remote hosts. It provides a dashboard to register SSH-accessible "tmux servers", create/list/kill tmux sessions, and access them via either a copied SSH command (for local terminal use) or an in-browser xterm.js terminal.

Single Go binary. Vanilla JS frontend. Helm chart for deployment.

---

## Project Structure

```
claude-overlay/
├── cmd/server/
│   └── main.go              — entry point, config loading, wire everything
├── internal/
│   ├── config/
│   │   └── config.go        — struct + env/flag parsing
│   ├── store/
│   │   ├── store.go         — SQLite setup, migrations
│   │   └── models.go        — Server, User, SessionTemplate structs
│   ├── auth/
│   │   ├── auth.go          — middleware, JWT, session management
│   │   ├── local.go         — username/password provider
│   │   └── oidc.go          — OIDC provider
│   ├── keystore/
│   │   ├── keystore.go      — KeyStore interface
│   │   ├── local.go         — AES-256-GCM encrypted in SQLite
│   │   └── vault.go         — HashiCorp Vault/OpenBao backend
│   ├── sshpool/
│   │   └── pool.go          — persistent SSH connections, reconnect
│   ├── session/
│   │   └── manager.go       — tmux CRUD, polling, lifecycle
│   └── terminal/
│       └── bridge.go        — WebSocket <> SSH <> tmux PTY
├── static/
│   ├── index.html           — dashboard (server list, sessions)
│   ├── terminal.html        — full-screen xterm.js terminal
│   ├── css/
│   │   └── style.css
│   └── js/
│       ├── app.js           — dashboard logic
│       ├── terminal.js      — xterm.js setup + WebSocket
│       └── vendor/          — xterm.js + addons (committed, no CDN)
├── deploy/
│   └── helm/
│       └── claude-overlay/  — Helm chart
├── Dockerfile
├── go.mod
└── go.sum
```

Config loading: env vars > flags > defaults. No config file (12-factor; Helm sets env vars).

SQLite migrations: embedded SQL files, run on startup. Version table tracks applied migrations.

---

## Authentication

### Local Auth (default)

- First startup with no users: app enters "setup mode" — first request to `/` serves a one-time admin registration form
- After admin user is created, setup mode is permanently disabled
- Admin creates additional users via `POST /api/users`
- Passwords stored as bcrypt hashes in SQLite
- Login via `POST /api/auth/login` returns JWT in httpOnly secure cookie

### OIDC (optional, `OVERLAY_AUTH_MODE=oidc`)

- Standard authorization code flow
- `GET /api/auth/oidc/login` redirects to provider
- `GET /api/auth/oidc/callback` exchanges code, creates/updates user, sets JWT cookie
- JIT user provisioning on first login
- Role mapping: configurable OIDC claim (default `groups`) — admin if claim contains configured admin group
- When OIDC enabled, the local login form is hidden from the UI. Setting `OVERLAY_EMERGENCY_LOCAL_AUTH=true` exposes a `/api/auth/login` endpoint (no UI link — admin must know to POST directly or navigate to `/login?local=true`). This prevents lock-out if the OIDC provider is down.

### JWT

```json
{
  "sub": "user-id",
  "name": "stefan",
  "role": "admin",
  "exp": 1234567890
}
```

Signed with `SESSION_SECRET`. Stored in httpOnly secure cookie. CSRF protection on state-changing endpoints.

### Auth Middleware

Every route except `/api/auth/*` and static assets checks the JWT cookie. Invalid/expired returns 401. WebSocket upgrade at `/ws/terminal/*` also checks the cookie before upgrading.

---

## KeyStore

### Interface

```go
type KeyStore interface {
    Get(ctx context.Context, serverID string) ([]byte, error)  // PEM-encoded private key
    Put(ctx context.Context, serverID string, key []byte) error
    Delete(ctx context.Context, serverID string) error
}
```

### Local Backend (default)

- User pastes or uploads SSH private key via UI on server registration
- Key encrypted with AES-256-GCM before writing to SQLite `ssh_keys` table
- Encryption key derived from `SESSION_SECRET` via Argon2id (salt stored alongside ciphertext)
- On `Get()`: decrypt in memory, return bytes, never touch disk

### Vault Backend (optional)

- `Put()` writes to `secret/claude-overlay/ssh-keys/<serverID>`
- `Get()` reads from there
- Vault client authenticated via token or AppRole at startup
- Key material never passes through SQLite

---

## SSH Connection Pool

```go
type SSHPool struct {
    connections map[string]*ssh.Client  // keyed by server ID
    mu          sync.RWMutex
}
```

- Lazy initialization: connection on first use per server
- Keepalive every 30s to detect dead connections
- On failure: one automatic reconnect attempt, then surface error
- Key fetched from KeyStore on each new connection (not cached, allows rotation)
- Thread-safe via `sync.RWMutex`
- `pool.Exec(serverID, cmd)` runs a command, returns stdout/stderr
- `pool.Session(serverID)` returns raw `ssh.Session` for interactive use (terminal bridge)

### Server Registration Flow

1. Admin POSTs to `/api/servers` with `{name, host, port, user}`
2. Admin uploads SSH private key via `PUT /api/servers/:id/key`
3. Key goes through `KeyStore.Put()`
4. Pool tests connection: `ssh host "echo ok"` — if fails, server saved but marked `status: unreachable`

---

## Session Manager

### tmux Operations (via `pool.Exec()`)

| Operation | Command |
|-----------|---------|
| List | `tmux list-sessions -F '#{session_name}:#{session_created}:#{session_windows}:#{session_attached}:#{session_activity}'` |
| Create | `tmux new-session -d -s <name> -c <workdir> '<command>'` |
| Kill | `tmux kill-session -t <name>` |
| Exists | `tmux has-session -t <name>` |

Session naming convention: `{user}-{label}-{shortid}` (e.g., `stefan-claude-a1b2c3`).

### Polling

- Background goroutine per registered server
- Every 30s (configurable via `OVERLAY_POLL_INTERVAL`): runs List, updates in-memory state
- Dashboard reads from in-memory state (no SSH call per page load)
- If SSH fails during poll, server marked `status: unreachable`, retry next interval

### Session Creation Templates

- Stored in SQLite, seeded with defaults on first run:
  - Claude Code: `claude` in `~/`
  - Claude Code (resume): `claude --resume` in `~/`
  - Shell: `bash` in `~/`
  - Custom: user-provided
- Each template: `{name, command, workdir, server_id (nullable = global)}`
- UI presents template picker; user can override before creating

### Idle Cleanup (optional, off by default)

- Enabled when `OVERLAY_IDLE_TIMEOUT > 0`
- Poller checks `session_activity` timestamps
- Sessions idle longer than threshold are killed
- Sessions with `session_attached > 0` are never auto-killed

### API Response Shape

```json
{
  "name": "stefan-claude-a1b2c3",
  "created": 1711526400,
  "windows": 1,
  "attached": 0,
  "lastActivity": 1711530000,
  "idle": "1h3m",
  "sshCommand": "ssh -t stefan@devbox.wisbric.com \"tmux new -A -s stefan-claude-a1b2c3\""
}
```

The `sshCommand` field is copied to clipboard in "Attach (SSH)" mode.

---

## Terminal Bridge

### WebSocket Endpoint

`GET /ws/terminal/:serverID/:sessionName`

### Connection Lifecycle

1. Browser opens WebSocket. Auth middleware checks JWT cookie before upgrade.
2. Go server gets SSH session from pool: `pool.Session(serverID)`
3. Requests PTY (`ssh.RequestPty`, term `xterm-256color`, initial size from query params `?cols=80&rows=24`)
4. Runs `tmux new -A -s <sessionName>` (create-or-attach)
5. Two goroutines:
   - Read loop: WebSocket -> SSH stdin (user keystrokes)
   - Write loop: SSH stdout -> WebSocket (terminal output)
6. Resize handler: browser sends JSON `{"type":"resize","cols":120,"rows":40}` -> `ssh.WindowChange()`

### Message Protocol

- **Browser -> server:** binary frames = raw input; text frames = JSON control messages (`resize`)
- **Server -> browser:** binary frames = raw terminal output

### Disconnect Handling

- Browser closes tab -> WebSocket closes -> SSH session closes -> tmux detaches (session stays alive)
- SSH drops -> WebSocket close frame -> xterm.js shows "Disconnected. Reconnect?" overlay
- Reconnect: fresh SSH session attaches to same tmux session, no state lost

### Concurrency

Each browser tab is fully independent: WebSocket + SSH session + goroutine pair. No shared state between terminal connections.

---

## Web UI

### Tech

Vanilla HTML/CSS/JS. No build step. Static files served via `go:embed`. xterm.js + addons vendored into `static/js/vendor/`.

### Dashboard (`index.html` + `app.js`)

Single page, three states:

1. **Setup mode** — first-run admin registration form (shown once, ever)
2. **Login** — username/password form (+ "Login with SSO" button if OIDC enabled)
3. **Dashboard** — main view after auth

Dashboard layout:
- Top bar: app name, logged-in user, logout
- Main area: tmux server cards
- Each server card:
  - Name, host, status dot (green/red)
  - Expandable session list from `/api/servers/:id/sessions`
  - Session row: name, idle time, attached count, "SSH" button (copy), "Terminal" button (new tab)
  - "New Session" button -> inline form with template picker + overrides
- Admin section: "Add Server" button -> form (name, host, port, user, SSH key upload)

Data flow:
- On load: `GET /api/servers` -> render cards
- On card expand: `GET /api/servers/:id/sessions` -> render sessions
- Auto-refresh: poll every 30s to update status and sessions
- Actions (create/kill session, add server): POST/DELETE returning updated state

### Terminal Page (`terminal.html` + `terminal.js`)

- Opened via `window.open('/terminal.html?server=X&session=Y')`
- Full-viewport xterm.js + thin top bar with `server > session` label
- Tab title: `session @ server` (distinguishable across 5-6 tabs)
- xterm-addon-fit for auto-resize on window change
- xterm-addon-webgl for GPU-accelerated rendering (canvas fallback)
- On load: open WebSocket, attach to xterm.js, send initial resize
- On window resize: debounce 150ms, send resize control message

---

## Helm Chart

```
deploy/helm/claude-overlay/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── _helpers.tpl    — name, labels, selectorLabels
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── ingress.yaml
│   ├── pvc.yaml
│   ├── configmap.yaml
│   └── secret.yaml
```

### Key Design Choices

- **Single replica only.** SQLite doesn't support concurrent writers. Chart enforces `replicas: 1` with `Recreate` strategy.
- **PVC mount:** `/data/overlay.db`. If `persistence.enabled=false`, uses emptyDir.
- **Secret generation:** if `config.sessionSecret` is empty, chart generates random 32-byte hex via Helm lookup (persists across upgrades). Zero-config `helm install` gives a working instance.
- **WebSocket ingress:** when `ingress.className=nginx`, chart auto-adds `proxy-read-timeout: "3600"` and `proxy-send-timeout: "3600"` annotations.
- **Health probes:** liveness `GET /healthz`, readiness `GET /readyz` (passes if no servers registered, checks reachability otherwise).
- **Dockerfile:** multi-stage, CGO_ENABLED=1 for SQLite, final image `alpine:3.21` with `openssh-client` and `ca-certificates`.

### Reference values.yaml

```yaml
replicaCount: 1

image:
  repository: registry.wisbric.com/claude-overlay
  tag: latest
  pullPolicy: IfNotPresent

auth:
  mode: local
  local:
    adminUsername: admin
  oidc:
    issuerUrl: ""
    clientId: ""
    clientSecret: ""
    redirectUrl: ""
    scopes: ["openid", "profile", "email"]
    rolesClaim: "groups"
    adminGroup: "overlay-admin"

keyStore:
  backend: local
  vault:
    address: ""
    authMethod: token
    token: ""
    approle:
      roleId: ""
      secretId: ""
    secretPath: "secret/claude-overlay/ssh-keys"

config:
  sessionSecret: ""
  idleTimeout: 0
  pollInterval: 30

persistence:
  enabled: true
  size: 1Gi
  storageClass: ""
  accessMode: ReadWriteOnce

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

resources:
  requests:
    cpu: 50m
    memory: 64Mi
  limits:
    cpu: 500m
    memory: 256Mi
```

---

## API Routes

```
POST   /api/auth/login              — local auth login
GET    /api/auth/oidc/login         — redirect to OIDC provider
GET    /api/auth/oidc/callback      — OIDC callback
POST   /api/auth/logout             — destroy session
GET    /api/servers                  — list servers
POST   /api/servers                  — register server (admin)
DELETE /api/servers/:id              — remove server (admin)
PUT    /api/servers/:id/key          — upload SSH key (admin)
GET    /api/servers/:id/sessions     — list tmux sessions
POST   /api/servers/:id/sessions     — create session
DELETE /api/servers/:id/sessions/:name — kill session
GET    /api/users                    — list users (admin)
POST   /api/users                    — create user (admin)
GET    /ws/terminal/:server/:session — WebSocket terminal
GET    /healthz                      — liveness probe
GET    /readyz                       — readiness probe
```

---

## Implementation Order (PRPs)

These are the independent work chunks, in dependency order:

1. **PRP-1: Project scaffold + config + SQLite store** — go.mod, main.go, config, migrations, models. Produces a binary that starts and serves `/healthz`.
2. **PRP-2: Auth (local + OIDC)** — auth middleware, local provider, OIDC provider, JWT, setup mode. Produces login flow.
3. **PRP-3: KeyStore + SSH pool** — KeyStore interface, local backend, Vault backend, SSH pool. Produces `pool.Exec()` working against a real host.
4. **PRP-4: Session manager** — tmux CRUD, polling, templates. Produces API endpoints for session management.
5. **PRP-5: Terminal bridge** — WebSocket handler, SSH PTY, bidirectional stream. Produces working browser terminal.
6. **PRP-6: Web UI** — dashboard, terminal page, xterm.js integration. Produces the full user-facing app.
7. **PRP-7: Helm chart + Dockerfile** — container image, chart templates, values.yaml. Produces deployable artifact.

PRPs 1-5 are strictly sequential (each depends on the prior). PRP-6 (UI) can start after PRP-4 (needs API endpoints to call) and overlap with PRP-5 (terminal page needs the WebSocket endpoint). PRP-7 can start after PRP-1 (Dockerfile) and be completed after PRP-6.

---

## Out of Scope

- Multi-replica / HA (SQLite is the constraint; would require switching to PostgreSQL)
- Session recording / replay
- RBAC beyond admin/user roles
- Mobile-optimized UI
- SSH certificate authority integration
- Additional KeyStore backends beyond local and Vault
