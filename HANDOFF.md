# Handoff — Agentic Hive

**Date:** 2026-03-28
**Session:** Initial build from empty directory to production deployment (71 commits)

## What Was Done

Built the entire Agentic Hive application from scratch in a single session:

### Phase 1: Core (PRPs 1-7)
- Go project scaffold, config, SQLite store with embedded migrations
- Authentication: local bcrypt + JWT, OIDC/Keycloak SSO, setup mode
- KeyStore: AES-256-GCM local backend + HashiCorp Vault backend
- SSH connection pool with lazy connect and auto-reconnect
- Session manager: tmux CRUD over SSH, background polling
- Terminal bridge: WebSocket ↔ SSH PTY (xterm.js in browser)
- Web UI: vanilla JS dashboard + terminal page
- Helm chart + Dockerfile, GitLab CI pipeline

### Phase 2: Production Hardening (PRPs 8-20)
- SSH host key verification (TOFU model)
- RBAC (RequireAdmin middleware)
- Login rate limiting (per-IP)
- CSRF protection (double-submit cookie)
- Structured logging (slog JSON)
- Graceful shutdown (SIGTERM handling)
- Prometheus metrics
- Separate JWT/encryption secrets
- Deep readiness probe
- SQLite backup (CLI + API + CronJob)
- WebSocket idle timeout
- Audit log
- Pod disruption budget

### Phase 3: Admin UI + Polish (PRP-21 + iterations)
- Admin settings UI: OIDC/Keycloak and Vault/OpenBao config, hot-reloadable
- Session templates: Claude Code, Claude Code (full access), Codex, Shell
- Working directory picker per session
- User dropdown menu with dark/light theme toggle
- About modal with version/commit/uptime
- Gradient-futuristic NightOwl design with glassmorphism
- Live session refresh after create/kill (bypasses poll cache)
- Loading states and feedback on all actions
- Security pen test + fixes (CSP, CSRF ordering, security headers)

## Current State

- **Deployed:** https://hive.dev-ai.wisbric.com on rke2-projects cluster
- **All tests passing** across 10 packages
- **CI:** GitLab pipeline builds container + Helm chart on push to main
- **One devbox server registered** and functional

## Known Issues / Next Steps

1. **`unsafe-inline` in CSP** — inline `onclick` handlers require it. Long-term: migrate to `addEventListener` in JS to allow strict CSP.
2. **SSH host key verification** on initial server add sometimes fails silently — user may need to click refresh after adding a server.
3. **No per-server access control** — any authenticated user can see all servers. RBAC is admin/user only.
4. **Codex template** — `codex` CLI needs to be installed on the tmux server for the template to work.
5. **OIDC not yet configured** — forms are ready in admin UI, needs Keycloak realm setup.
6. **Vault not yet configured** — using local encrypted storage for now.

## Recommended First Action

Try connecting OIDC to the existing Keycloak instance via the admin Settings panel — the UI is ready, just needs the issuer URL, client ID, and client secret.
