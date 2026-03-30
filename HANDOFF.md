# Handoff: Agentic Hive — v0.1.0 Published, Ready for Launch

**Date:** 2026-03-30
**Branch:** main
**Last Commit:** `a67aa31` docs: add launch posts for HN, Reddit, and Twitter

## Goal

Build and publish Agentic Hive — a web app for managing tmux sessions across remote SSH hosts, supporting Claude Code, Codex, and shell sessions with browser terminal access. The project went from empty directory to open-source v0.1.0 release in a multi-day session.

## Completed

- [x] Core application: Go backend, vanilla JS frontend, SQLite persistence (PRPs 1-7)
- [x] Production hardening: auth, CSRF, rate limiting, TOFU, audit log, metrics, graceful shutdown (PRPs 8-20)
- [x] Admin UI: OIDC/Keycloak and Vault/OpenBao config with hot-reload (PRP-21)
- [x] Vault key references: users point to existing keys, live read per connection (PRP-22)
- [x] Per-user server isolation with ownership
- [x] Gradient-futuristic NightOwl UI with dark/light theme
- [x] Deployed to rke2-projects at https://hive.dev-ai.wisbric.com
- [x] OIDC configured (Keycloak `projects` realm), Vault configured (OpenBao)
- [x] Go module renamed to `github.com/wisbric/agentic-hive`
- [x] GitHub mirror: https://github.com/wisbric/agentic-hive (public, Apache 2.0)
- [x] GitHub Actions CI: test + build + push to ghcr.io + Helm chart on tags
- [x] v0.1.0 tag + GitHub release created
- [x] Code review: all HIGH/MEDIUM findings fixed (XSS, path traversal, error leak, binary removed)
- [x] API tests: 22/22 pass against live instance
- [x] SSH session leak fix: explicit cleanup on WebSocket disconnect
- [x] Resource leak checklist added to skeptical-evaluator skill (global)
- [x] Launch posts drafted (HN, Reddit x3, Twitter)
- [x] Config/skills synced to devbox.wisbric.com
- [x] Docker cleanup: freed 278G from `/var/lib/docker/containerd`

## In Progress / Next Steps

- [ ] **Publish launch posts** — drafts in `docs/launch/`. Post HN first (weekday morning US Pacific), then Reddit, then Twitter.
- [ ] **PRP-23: Resource leak testing** — `goleak`, WebSocket lifecycle integration tests, metric assertions. PRP written, not yet implemented.
- [ ] **Terminal screenshot for README** — Playwright can't capture (needs SSH connection); take manually and add to `docs/screenshots/`
- [ ] **Move inline onclick to addEventListener** — allows removing `'unsafe-inline'` from CSP `script-src`
- [ ] **Vault path allowlist** — currently any path readable by the global token is accessible; consider per-user path prefixes
- [ ] **GitHub container registry permissions** — verify ghcr.io package is set to public

## Key Decisions

- **Go + vanilla JS (no framework)**: keeps the binary self-contained, no build step, ~25MB total. The UI is simple enough that React/Vue adds more complexity than value.
- **SQLite (not Postgres)**: zero external dependencies, single-replica simplicity. Trade-off: no HA. Mitigated by backup CronJob.
- **Vault key references (live read)**: keys stay where users put them, never duplicated. Rotation is instant. Trade-off: every SSH connection reads from Vault (acceptable latency).
- **Per-user isolation via owner_id**: simpler than full RBAC. Admins see all, users see own. Server CRUD open to all users (not admin-only) since ownership enforces isolation.
- **CSRF skips unauthenticated requests**: auth middleware handles rejection with 401, not CSRF with 403. Correct middleware ordering.
- **No auto-refresh**: removed 30s polling that overwrote UI state. Manual refresh button + live query after create/kill.
- **`unsafe-inline` in CSP**: required for inline onclick handlers. Known trade-off, documented for future fix.

## Dead Ends (Don't Repeat These)

- **WeTTY/ttyd for browser terminal**: researched, rejected. WeTTY is Node.js (heavy), ttyd can't do independent concurrent sessions. Custom Go + xterm.js is lighter and gives full control.
- **`responseRecorder` without `Hijacker`**: wrapping ResponseWriter for logging broke WebSocket upgrade. Must implement `http.Hijacker` on the wrapper.
- **Vault settings key `vault.addr` vs `vault.address`**: the admin UI saved as `vault.address` but `ApplyDBSettings` mapped `vault.addr`. Caused Vault to not initialize from DB settings.
- **CSRF before auth middleware**: unauthenticated POSTs got 403 (CSRF) instead of 401 (auth). Fixed by skipping CSRF when no session cookie.
- **Helm configmap sets env vars to empty strings**: `OVERLAY_KEYSTORE_BACKEND=local` from configmap blocked the vault auto-detect (`os.Getenv != ""`). Auto-detect now overrides regardless.
- **`sshSession.Wait()` blocking forever**: when browser tab closes, the WebSocket goroutine exits but `Wait()` blocks the handler. SSH session + goroutines leak. Fixed with explicit SIGHUP + Close on WebSocket disconnect.

## Files Changed

97 commits total. Key areas:
- `cmd/server/` — entry point, embedded static files (HTML/CSS/JS)
- `internal/` — 10 packages: auth, backup, config, keystore, metrics, server, session, sshpool, store, terminal
- `deploy/helm/agentic-hive/` — Helm chart with full values.yaml
- `.github/workflows/ci.yml` — GitHub Actions CI
- `.gitlab-ci.yml` — GitLab CI
- `docs/prps/` — 16 PRPs (design decision history)
- `docs/launch/` — drafted launch posts
- `docs/screenshots/` — login, dashboard, sessions, about

## Current State

- **Tests:** all pass (8 packages with tests, 2 without)
- **Build:** clean (`go build ./cmd/server`)
- **Lint:** `go vet ./...` clean, no TODOs in non-test code
- **Live instance:** healthy at https://hive.dev-ai.wisbric.com (rke2-projects, revision ~20+)
- **GitHub:** https://github.com/wisbric/agentic-hive — v0.1.0 release, CI passing
- **GitLab:** gitlab.com/adfinisde/agentic-workspace/agentic-hive — CI passing
- **Devbox configs:** synced (skills, settings, plugins, memory)
- **Docker:** cleaned 278G stale containerd data on local machine

## Context for Next Session

The project is feature-complete and published as v0.1.0. The immediate action is posting the launch announcements (drafts ready in `docs/launch/`). The main technical debt is PRP-23 (resource leak testing with goleak) and migrating inline onclick handlers to addEventListener for stricter CSP. The skeptical-evaluator skill now globally checks for resource leaks in all code reviews.

**Recommended first action:** Post the Show HN from `docs/launch/show-hn.md` — weekday morning US Pacific time for best visibility.
