# r/devops Post

**Title:** Open-sourced a lightweight tmux session manager with browser terminal, Vault integration, and Helm chart

**Body:**

Built a tool for managing tmux sessions across multiple servers — primarily for AI coding agent sessions (Claude Code, Codex) but useful for any tmux workflow.

**Architecture:**

```
Browser (xterm.js) ←WebSocket→ Agentic Hive (Go) ←SSH→ Remote tmux servers
```

Single Go binary, SQLite, no external deps.

**Ops features:**

- **SSH key storage:** AES-256-GCM encrypted in SQLite or HashiCorp Vault/OpenBao. Users can reference existing Vault paths (live read, no key duplication, instant rotation).
- **SSH host key TOFU:** first connect stores the host key, subsequent connects verify. Mismatch blocks connection.
- **OIDC SSO:** Keycloak, Authentik, Dex — configurable via admin UI without restart (hot-swappable OIDC handler behind RWMutex).
- **Per-user isolation:** servers have owner_id, users see only their own.
- **Prometheus metrics:** `/metrics` endpoint with connection gauges, request histograms, auth failure counters.
- **Structured logging:** slog JSON output.
- **Graceful shutdown:** SIGTERM handling with 30s drain.
- **Helm chart:** single replica (SQLite constraint), PVC, ingress with WebSocket timeout annotations, backup CronJob, PDB.

**Deployment:**

```bash
helm install agentic-hive oci://ghcr.io/wisbric/agentic-hive/helm/agentic-hive \
  --set config.sessionSecret=$(openssl rand -hex 32) \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=hive.example.com
```

Or `docker compose up` for standalone.

GitHub: https://github.com/wisbric/agentic-hive
License: Apache 2.0

Interested in feedback on the Vault key reference pattern — users point to where their SSH key already lives in Vault instead of pasting it, and the system reads it live on every connection. Seems to work well for multi-user setups.
