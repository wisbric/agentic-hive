# PRP-8: SSH Host Key Verification (TOFU)

## Goal

Replace `ssh.InsecureIgnoreHostKey()` in the SSH pool with Trust-On-First-Use (TOFU) host key verification backed by SQLite.

## Background

`internal/sshpool/pool.go` line 51 calls `ssh.InsecureIgnoreHostKey()`, which means any host claiming to be a registered server will be accepted. This allows a MITM attacker to intercept SSH sessions. The fix is TOFU: store the host's public key on first connection, reject mismatches on every subsequent connection, and expose an admin-only API endpoint to reset the stored key for legitimate rotations.

## Requirements

1. Add SQLite migration `internal/store/migrations/002_host_keys.sql` with table:
   ```sql
   CREATE TABLE IF NOT EXISTS host_keys (
       server_id  TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
       host_key   BLOB NOT NULL,
       fingerprint TEXT NOT NULL,
       first_seen DATETIME DEFAULT CURRENT_TIMESTAMP
   );
   INSERT OR IGNORE INTO schema_version (version) VALUES (2);
   ```

2. Add status constant to `internal/store/models.go`:
   ```go
   StatusKeyMismatch = "key_mismatch"
   ```
   Place it alongside the existing `StatusUnknown`, `StatusReachable`, `StatusUnreachable` constants.

3. Add three store methods to a new file `internal/store/host_keys.go`:
   - `StoreHostKey(serverID string, hostKey []byte, fingerprint string) error` — upserts the record (INSERT OR REPLACE).
   - `GetHostKey(serverID string) (hostKey []byte, fingerprint string, err error)` — returns `sql.ErrNoRows` (wrapped as `"no host key"`) when absent.
   - `DeleteHostKey(serverID string) error` — deletes the row; does NOT error if absent.

4. Update `internal/sshpool/pool.go`:
   - Change `Pool` struct to also hold `store *store.Store` (it already does — no struct change needed, the field is `p.store`).
   - Replace `ssh.InsecureIgnoreHostKey()` with a custom `ssh.HostKeyCallback` that:
     a. If no key is stored for the server, stores the presented key (via `p.store.StoreHostKey`) and returns `nil` (allow).
     b. If a key is stored and it matches `bytes.Equal(stored, presented.Marshal())`, returns `nil`.
     c. If a key is stored and it does NOT match, calls `p.store.UpdateServerStatus(serverID, store.StatusKeyMismatch)` and returns `fmt.Errorf("host key mismatch for server %s: stored fingerprint %s, got %s", serverID, storedFP, ssh.FingerprintSHA256(presented))`.
   - The callback must be built inside `connect()` after `srv` is fetched, capturing `serverID` and `srv` in closure.
   - Use `ssh.FingerprintSHA256(key)` for the fingerprint string when storing.
   - Import `"crypto/rand"` is not needed; `"golang.org/x/crypto/ssh"` is already imported.

5. Add API endpoint in `internal/server/server.go`:
   - Handler: `s.handleAcceptKey(w http.ResponseWriter, r *http.Request)`
   - Route (admin only, see PRP-9): `POST /api/servers/{id}/accept-key`
   - Logic: call `s.store.DeleteHostKey(id)`, then `s.pool.Remove(id)` (force reconnect on next use), then respond `{"status":"ok"}`.
   - For now (before PRP-9 is implemented), protect it with the standard `am` middleware. Add a `// TODO(PRP-9): restrict to admin` comment.
   - Register the route in `routes()` alongside the other server routes.

6. Pass `store` into `Pool` — it already holds `store *store.Store` (set in `New`). No constructor change needed.

## Implementation Notes

- The `HostKeyCallback` type is `func(hostname string, remote net.Addr, key ssh.PublicKey) error`. The `hostname` parameter is the `host:port` string; use the captured `serverID` (not `hostname`) as the DB key.
- `ssh.PublicKey.Marshal()` returns the wire-format bytes. Store these bytes, not the string. On comparison use `bytes.Equal`.
- The callback is called from within `ssh.Dial`, which is called inside `connect()`. The `serverID` is already in scope.
- The reconnect path in `Exec()` calls `p.connect(ctx, serverID)` directly; it will also go through the new callback — no special handling needed.
- Migration file naming: the existing migration is `001_initial.sql`. Name the new one `002_host_keys.sql`. The `migrate()` function in `store.go` already handles sequential application by parsing the integer prefix.
- Error response from `handleAcceptKey` on store failure: `jsonError(w, "failed to clear host key", http.StatusInternalServerError)`.

## Validation

```bash
# Build passes
cd /home/stefans/git/agentic-workspace/projects/claude-overlay
go build ./...

# Unit tests pass (includes new tests)
go test ./internal/store/... -v -run TestHostKey
go test ./internal/sshpool/... -v

# Expected test cases to write in internal/store/host_keys_test.go:
# - TestStoreAndGetHostKey: store a key, get it back, fingerprint matches
# - TestGetHostKeyMissing: get on empty DB returns "no host key" error
# - TestDeleteHostKey: store then delete, subsequent get returns missing error

# Expected test cases to write in internal/sshpool/pool_test.go (extend existing file):
# - TestTOFUFirstConnect: mock SSH server, first connect stores key in DB
# - TestTOFUSameKey: mock SSH server, second connect with same key succeeds
# - TestTOFUKeyMismatch: stored key != presented key → error, server status = key_mismatch

# Full test suite
go test ./... -count=1
```

Key acceptance criteria:
- `go vet ./...` produces no warnings.
- A server that has never connected has no row in `host_keys`.
- A server that connected once has a row; its `fingerprint` matches `ssh.FingerprintSHA256` of the server's actual host key.
- Changing the server's host key (simulate by storing a different key in the DB) causes the next `pool.connect` to return an error containing `"host key mismatch"`.
- Calling `POST /api/servers/{id}/accept-key` removes the row; the next connection stores the new key.
- Server `status` column becomes `"key_mismatch"` after a mismatch, visible in `GET /api/servers` response.

## Out of Scope

- Persisting host keys to Vault or any external store (SQLite only).
- UI changes to surface `key_mismatch` status differently (CSS already handles unknown status dots; adding `key_mismatch` to the CSS is acceptable but not required).
- Key rotation notifications or audit log.
- Trust-on-every-use (TOEU) or certificate-based host authentication.
- The `accept-key` endpoint does NOT re-connect automatically; the caller must trigger a new operation that causes reconnection.
