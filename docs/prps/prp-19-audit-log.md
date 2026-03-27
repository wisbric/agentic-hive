# PRP-19: Audit Log

## Goal
Record all security-relevant actions with actor, target, and IP address to support incident investigation and compliance requirements.

## Background
Currently there is no record of who connected to which server, who created or deleted servers, who uploaded SSH keys, or who logged in. The application handles multi-user access with role-based auth (admin/user) and stores sensitive encrypted SSH credentials. Any compliance framework (SOC 2, ISO 27001, internal policy) will require an audit trail. The store layer (`internal/store/`) already owns the SQLite DB and migration system, making it the natural place to add the `audit_log` table and query methods.

## Requirements

1. Add migration `002_audit_log.sql` in `internal/store/migrations/`:
   ```sql
   CREATE TABLE IF NOT EXISTS audit_log (
       id          TEXT PRIMARY KEY,
       timestamp   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
       user_id     TEXT NOT NULL DEFAULT '',
       username    TEXT NOT NULL DEFAULT '',
       action      TEXT NOT NULL,
       target_type TEXT NOT NULL DEFAULT '',
       target_id   TEXT NOT NULL DEFAULT '',
       details     TEXT NOT NULL DEFAULT '',
       ip_address  TEXT NOT NULL DEFAULT ''
   );
   CREATE INDEX IF NOT EXISTS audit_log_timestamp ON audit_log(timestamp);
   CREATE INDEX IF NOT EXISTS audit_log_user_id   ON audit_log(user_id);
   CREATE INDEX IF NOT EXISTS audit_log_action    ON audit_log(action);
   INSERT OR IGNORE INTO schema_version (version) VALUES (2);
   ```

2. Add `AuditEntry` model to `internal/store/models.go`:
   ```go
   type AuditEntry struct {
       ID         string
       Timestamp  time.Time
       UserID     string
       Username   string
       Action     string
       TargetType string
       TargetID   string
       Details    string
       IPAddress  string
   }
   ```

3. Add store methods to a new file `internal/store/audit.go`:
   - `LogAudit(entry AuditEntry) error` — inserts a new entry; generates UUID for `ID` if empty
   - `ListAuditLog(filter AuditFilter) ([]AuditEntry, error)` where `AuditFilter` has fields: `UserID string`, `Action string`, `Since time.Time`, `Until time.Time`, `Limit int` (default 100, max 500), `Offset int`

4. Log the following actions. Use the constants below as the `Action` values:

   | Constant | Value | Handler location |
   |---|---|---|
   | `AuditAuthLogin` | `"auth.login"` | `auth/local.go` HandleLogin (success), `auth/oidc.go` HandleCallback (success) |
   | `AuditAuthLoginFailed` | `"auth.login_failed"` | `auth/local.go` HandleLogin (bcrypt mismatch) |
   | `AuditAuthLogout` | `"auth.logout"` | `auth/auth.go` HandleLogout |
   | `AuditServerCreate` | `"server.create"` | `server/server.go` handleCreateServer |
   | `AuditServerDelete` | `"server.delete"` | `server/server.go` handleDeleteServer |
   | `AuditServerKeyUpload` | `"server.key_upload"` | `server/server.go` handleUploadKey |
   | `AuditSessionCreate` | `"session.create"` | `server/server.go` handleCreateSession |
   | `AuditSessionKill` | `"session.kill"` | `server/server.go` handleKillSession |
   | `AuditTerminalConnect` | `"terminal.connect"` | `terminal/bridge.go` HandleTerminal (after upgrade) |
   | `AuditTerminalDisconnect` | `"terminal.disconnect"` | `terminal/bridge.go` HandleTerminal (on close) |

   Define these constants in `internal/store/audit.go`.

5. Add `GET /api/admin/audit` endpoint (admin only) with query params:
   - `user_id`, `action`, `since` (RFC3339), `until` (RFC3339), `limit` (int), `offset` (int)
   - Returns `{"entries": [...], "total": N}` — `total` is the count matching the filter without pagination

6. Audit log entries must also be emitted via `log.Printf` (or `slog.Info` if PRP-12 is merged) at info level so they appear in stdout logs for aggregation by log shippers.

7. IP address extraction: create a helper `clientIP(r *http.Request) string` in `internal/server/` that returns `r.Header.Get("X-Forwarded-For")` (first value if comma-separated), falling back to `r.Header.Get("X-Real-IP")`, falling back to stripping the port from `r.RemoteAddr`.

## Implementation Notes

- **Store dependency injection:** `auth/local.go` and `auth/oidc.go` currently receive a `*store.Store`. They can call `st.LogAudit(...)` directly. For `HandleLogout` (a package-level function that does not have store access), either pass the store into a closure or promote `HandleLogout` to a method on a new `LogoutHandler` struct. The simplest approach: add a `Logout(store)` function in `auth` that returns an `http.HandlerFunc`, replacing the current `auth.HandleLogout` registration in `server.go`.
- **Terminal bridge:** `Bridge` currently has no reference to the store. Add a `store *store.Store` field to `Bridge` and update `NewBridge` to accept it. Pass the store from `server.go` where `terminal.NewBridge(pool)` is called.
- **UUID generation:** use `fmt.Sprintf("%x-%x", time.Now().UnixNano(), rand.Int63())` as a lightweight ID, or add `github.com/google/uuid` — check if it is already an indirect dependency before adding.
- **`details` field:** use JSON-encoded key-value pairs for structured context (e.g. `{"server_id":"abc","name":"prod-1"}` for `server.create`). Keep it short.
- **Admin middleware:** a `requireAdmin` helper does not exist yet. Add it in `internal/server/server.go`:
  ```go
  func requireAdmin(next http.Handler) http.Handler {
      return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
          user := auth.GetUser(r)
          if user == nil || user.Role != store.RoleAdmin {
              jsonError(w, "forbidden", http.StatusForbidden)
              return
          }
          next.ServeHTTP(w, r)
      })
  }
  ```
  Wrap the audit endpoint: `am(requireAdmin(http.HandlerFunc(s.handleListAuditLog)))`.
- **Migration numbering:** the only existing migration is `001_initial.sql`. The next file must be `002_audit_log.sql`. The migration runner in `store.go` parses the leading integer from the filename.

## Validation

```bash
# Build
go build ./cmd/server/...

# Run migrations and verify table exists
sqlite3 /tmp/audit-test.db ".tables"
# (tables will be empty until server runs)

OVERLAY_SESSION_SECRET=test OVERLAY_DB_PATH=/tmp/audit-test.db ./agentic-hive &
sleep 1

# Login creates an audit entry
curl -s -c /tmp/cookies.txt -X POST http://localhost:8080/api/auth/setup \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Password1!"}'

curl -s -c /tmp/cookies.txt -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Password1!"}'

# Fetch audit log
curl -s -b /tmp/cookies.txt http://localhost:8080/api/admin/audit | jq .
# Expect: entries array contains at least one auth.login entry with username=admin

# Failed login creates auth.login_failed entry
curl -s -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"wrong"}'
curl -s -b /tmp/cookies.txt http://localhost:8080/api/admin/audit?action=auth.login_failed | jq .
# Expect: at least one entry

# Create server creates server.create entry
curl -s -b /tmp/cookies.txt -X POST http://localhost:8080/api/servers \
  -H 'Content-Type: application/json' \
  -d '{"name":"test","host":"10.0.0.1","port":22,"sshUser":"root"}'
curl -s -b /tmp/cookies.txt 'http://localhost:8080/api/admin/audit?action=server.create' | jq .
# Expect: one entry with target_type=server

# Non-admin cannot access audit log (returns 403)
# (requires creating a non-admin user first)

# IP address is captured
curl -s -b /tmp/cookies.txt http://localhost:8080/api/admin/audit | jq '.[0].ip_address'
# Expect: non-empty string (127.0.0.1 or ::1 in local testing)
```

## Out of Scope

- Audit log UI in the frontend (API-only in this PRP)
- Audit log export (CSV, JSON download)
- Alerting on specific audit actions
- Tamper-evidence (signing or append-only enforcement)
- Retention/purge policy for old audit entries
