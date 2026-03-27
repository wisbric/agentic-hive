# PRP-9: RBAC on Server Management

## Goal

Restrict server creation, deletion, and key upload to admin users by adding a `RequireAdmin` middleware that wraps the existing `RequireAuth`.

## Background

`internal/server/server.go` wraps all API routes with `am := auth.RequireAuth(s.cfg.SessionSecret)`, which verifies the JWT is valid and puts claims in context. However, it never inspects `claims.Role`. The `store` package already defines `RoleAdmin = "admin"` and `RoleUser = "user"` in `internal/store/models.go`, and every JWT includes a `role` claim (set during both local login in `internal/auth/local.go` and OIDC callback in `internal/auth/oidc.go`). Any authenticated user — including those with `role: "user"` — can currently create servers, delete servers, upload SSH keys, and (once PRP-8 is implemented) reset host keys.

## Requirements

1. Add `RequireAdmin` function to `internal/auth/auth.go`:
   ```go
   func RequireAdmin(secret string) func(http.Handler) http.Handler
   ```
   - Internally calls the same JWT-cookie verification as `RequireAuth`.
   - If the cookie is missing or the JWT is invalid, respond `401 Unauthorized` (same as `RequireAuth`).
   - If the JWT is valid but `claims.Role != store.RoleAdmin`, respond `403 Forbidden` with body `{"error":"forbidden"}`.
   - On success, sets the user in request context (same as `RequireAuth` — call `SetUser`).
   - Import `"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"` in `auth.go` to reference `store.RoleAdmin`.

2. In `internal/server/server.go`, add a second middleware variable in `routes()`:
   ```go
   adminM := auth.RequireAdmin(s.cfg.SessionSecret)
   ```

3. Apply `adminM` (not `am`) to the following routes:
   - `POST /api/servers` — create server
   - `DELETE /api/servers/{id}` — delete server
   - `PUT /api/servers/{id}/key` — upload SSH key
   - `POST /api/servers/{id}/accept-key` — reset stored host key (PRP-8 endpoint; apply `adminM` here, removing the `// TODO(PRP-9)` comment added in PRP-8)

4. Keep `am` (authenticated, any role) on:
   - `GET /api/servers` — list servers
   - `GET /api/servers/{id}/sessions` — list sessions
   - `POST /api/servers/{id}/sessions` — create session
   - `DELETE /api/servers/{id}/sessions/{name}` — kill session
   - `GET /api/templates` — list templates
   - `GET /ws/terminal/{server}/{session}` — terminal WebSocket

5. Future endpoint `POST /api/users` (create user) must also use `adminM` when implemented; add a comment in `routes()` noting this.

## Implementation Notes

- `RequireAdmin` should share no duplicated JWT-parsing logic with `RequireAuth`. Preferred approach: make `RequireAdmin` a thin wrapper that reuses `VerifyJWT` directly, mirroring the exact structure of `RequireAuth` but adding the role check before calling `next.ServeHTTP`.
- Do NOT add a `RequireAdmin` field or dependency to the `Server` struct; pass `s.cfg.SessionSecret` the same way `RequireAuth` is called today.
- The import of `store` package in `auth.go` creates a potential import cycle: `auth` would import `store`, and `store` currently does not import `auth`, so this is safe. Verify with `go build ./...`.
- Response body for 403 must be JSON with `Content-Type: application/json` header, consistent with `jsonError` in `server.go`. In `auth.go`, write it directly: `w.Header().Set("Content-Type", "application/json")` then `http.Error(...)` — or write the body and code manually.
- The existing `RequireAuth` function signature and behavior must not change; only add the new `RequireAdmin` function.

## Validation

```bash
# Build passes with no import cycle
cd /home/stefans/git/agentic-workspace/projects/claude-overlay
go build ./...
go vet ./...

# Unit tests — add to internal/auth/auth_test.go:
go test ./internal/auth/... -v -run TestRequireAdmin

# Expected test cases:
# - TestRequireAdminNoToken: no cookie → 401
# - TestRequireAdminUserRole: valid JWT with role="user" → 403
# - TestRequireAdminAdminRole: valid JWT with role="admin" → 200 + user in context
# - TestRequireAdminExpiredToken: expired JWT → 401

# Integration-style tests — add to internal/server/server_test.go:
go test ./internal/server/... -v -run TestRBAC

# Expected test cases (using httptest.NewServer pattern from existing server_test.go):
# - TestCreateServerRequiresAdmin: user-role JWT → POST /api/servers → 403
# - TestCreateServerAdminSucceeds: admin-role JWT → POST /api/servers → 201
# - TestDeleteServerRequiresAdmin: user-role JWT → DELETE /api/servers/{id} → 403
# - TestUploadKeyRequiresAdmin: user-role JWT → PUT /api/servers/{id}/key → 403
# - TestListServersAllowsUser: user-role JWT → GET /api/servers → 200
# - TestListSessionsAllowsUser: user-role JWT → GET /api/servers/{id}/sessions → 200

# Full suite
go test ./... -count=1
```

Acceptance criteria:
- `curl -X POST /api/servers` with a user-role session cookie returns HTTP 403.
- `curl -X POST /api/servers` with an admin-role session cookie returns HTTP 201 (given a valid request body).
- `curl -X GET /api/servers` with a user-role session cookie returns HTTP 200.
- `curl -X GET /api/servers` with no cookie returns HTTP 401.

## Out of Scope

- Per-server access control (e.g., user can only see their own servers).
- Role management API (create/promote/demote users).
- Granular permissions beyond the admin/user binary.
- Changes to the OIDC role-mapping logic in `oidc.go`.
- Frontend UI changes to hide admin-only buttons from non-admin users (optional enhancement, not required here).
