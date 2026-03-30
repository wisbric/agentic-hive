# r/selfhosted Post

**Title:** Agentic Hive — self-hosted web terminal manager for tmux sessions across multiple servers

**Body:**

Just open-sourced a tool I built for managing tmux sessions across remote servers from a web browser.

**The problem:** I have multiple dev servers running long-lived tmux sessions (AI coding agents, builds, monitoring). I was constantly SSH-ing between them and losing track of what's running where.

**The solution:** A single Go binary that:

- Connects to your servers via SSH
- Lists/creates/kills tmux sessions
- Provides a browser-based terminal (xterm.js + WebSocket) or copies the SSH command for your local terminal
- Manages SSH keys encrypted at rest (AES-256-GCM) or in HashiCorp Vault

**Self-hosting details:**

- Single binary, ~25MB, SQLite for state — no Postgres, no Redis, no external deps
- `docker-compose.yml` included — one command to run
- Helm chart for Kubernetes
- OIDC SSO (Keycloak, Authentik, etc.) or local auth
- Per-user isolation — each user sees only their own servers
- Dark/light theme

```yaml
# docker-compose.yml
services:
  agentic-hive:
    image: ghcr.io/wisbric/agentic-hive:latest
    ports: ["8080:8080"]
    environment:
      OVERLAY_SESSION_SECRET: ${OVERLAY_SESSION_SECRET}
    volumes: [hive-data:/data]
volumes:
  hive-data:
```

GitHub: https://github.com/wisbric/agentic-hive
License: Apache 2.0

Built with Go, vanilla JS (no build step), xterm.js. Originally for managing AI coding sessions but works for any tmux workflow.
