# Handoff — Agentic Hive v0.1.0

**Date:** 2026-03-30
**Repo:** https://github.com/wisbric/agentic-hive (primary), GitLab mirror at adfinisde/agentic-workspace/agentic-hive
**Live:** https://hive.dev-ai.wisbric.com (rke2-projects cluster, namespace agentic-hive)

## What Was Built

Agentic Hive — a Go web application for managing tmux sessions across remote SSH hosts, built from scratch in a multi-day session.

### Core Features
- Multi-host tmux management with browser terminal (xterm.js + WebSocket) and SSH command copy
- Session templates: Claude Code, Claude Code (full access), Codex, Shell, custom
- Per-user server isolation with ownership

### Auth & Security
- Local auth (bcrypt + JWT) with first-run setup mode
- OIDC/Keycloak SSO (hot-configurable via admin UI)
- CSRF double-submit cookie, per-IP rate limiting, SSH host key TOFU
- Security headers (CSP, X-Frame-Options, HSTS, etc.)
- Audit log for all security-relevant actions

### Key Storage
- Local: AES-256-GCM + Argon2id in SQLite
- Vault/OpenBao: system-managed path or user-specified Vault references (live read, no key duplication)
- Hot-swappable backends via admin UI

### Operations
- Structured logging (slog JSON), Prometheus metrics, graceful shutdown
- SQLite backup (CLI command + admin API + Helm CronJob)
- Deep readiness probe, PodDisruptionBudget
- Helm chart with full values.yaml, Dockerfile, docker-compose.yml

### UI
- Gradient-futuristic NightOwl design with glassmorphism
- Dark/light theme toggle (persisted per user)
- Collapsible settings panels, loading states on all actions
- About modal with version/commit/uptime

## Current Deployment

- **Image:** `ghcr.io/wisbric/agentic-hive:v0.1.0` (also on GitLab registry)
- **Helm revision:** ~20+ on rke2-projects
- **OIDC:** configured via admin UI, Keycloak `projects` realm, `stefan` user in `hive-admin` group
- **Vault:** OpenBao at `openbao.mgmt.dev-ai.wisbric.com`, token scoped to `agentic-hive` policy
- **Devbox server:** registered, key in Vault at `agentic-hive/ssh-keys/{id}`, owned by stefan

## Test Results (2026-03-30)

### API Tests: 22/22 PASS
- Public endpoints, auth enforcement, CSRF ordering, rate limiting, security headers, TLS, path traversal — all pass

### Code Review Findings (addressed)
- Removed committed binary from git
- Fixed XSS in vault path HTML attribute (esc() now encodes quotes)
- Added vault path traversal prevention (`..` and `/` prefix blocked)
- Removed Vault error detail leaking to clients

## Deploy Pattern

```bash
# Push to both remotes
git push && git push github main

# Wait for CI (GitLab ~5min, GitHub Actions ~3min)

# Deploy to cluster
SHA=$(git rev-parse --short=8 HEAD)
helm upgrade agentic-hive deploy/helm/agentic-hive \
  --namespace agentic-hive --kube-context rke2-projects \
  --reuse-values --set image.tag=$SHA
```

## Recommended Next Steps

1. **Take terminal screenshot** — Playwright can't capture the terminal (needs SSH connection); manually screenshot and add to README
2. **Migrate Go module references in go.sum** — `go mod tidy` after next dependency update will clean this up
3. **Move inline onclick handlers to addEventListener** — allows removing `'unsafe-inline'` from CSP script-src
4. **Add Vault path allowlist** — currently any path readable by the global token is accessible; consider per-user path prefixes
5. **GitHub container registry** — CI pushes images on push to main; verify ghcr.io permissions are set to public for the package
