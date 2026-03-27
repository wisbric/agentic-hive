# PRP-17: SQLite Backup Strategy

## Goal
Add automated SQLite backup capability to prevent total data loss when the PVC is lost.

## Background
All application state — users, servers, encrypted SSH keys, session templates — lives in a single SQLite file (`/data/overlay.db`) on a PVC. If that PVC is deleted or corrupted, everything is gone with no recovery path. There is no backup mechanism of any kind today. The `VACUUM INTO` SQL command provides a consistent, hot-copy of a live SQLite database without requiring downtime or locking out writers.

## Requirements

1. Add a `backup` subcommand to the binary: `agentic-hive backup --output /path/to/backup.db`
   - Opens the source DB read-only (or uses the existing `store.Open` path)
   - Executes `VACUUM INTO '/path/to/backup.db'` against the live database
   - Prints the output path and file size on success
   - Exits non-zero on failure

2. Add flags for S3 upload to the backup subcommand:
   - `--s3-endpoint` (e.g. `https://s3.amazonaws.com`)
   - `--s3-bucket`
   - `--s3-key-id`
   - `--s3-secret`
   - `--s3-prefix` (optional path prefix, defaults to `backups/`)
   - When S3 flags are provided, the local `--output` file is written first, then uploaded via HTTP PUT using AWS Signature V4, then the local file is removed
   - Use only stdlib + `net/http`; do not add an AWS SDK dependency

3. Add an HTTP endpoint `POST /api/admin/backup` (admin role required):
   - Runs `VACUUM INTO` to a temp file under `/tmp`
   - Streams the file back as `application/octet-stream` with `Content-Disposition: attachment; filename="overlay-backup-<timestamp>.db"`
   - Deletes the temp file after streaming
   - Returns `403` for non-admin callers (check `claims.Role == store.RoleAdmin`)

4. Add `templates/cronjob-backup.yaml` to the Helm chart:
   - Runs `agentic-hive backup --output /backup/overlay-$(date +%Y%m%d-%H%M%S).db` on a configurable schedule
   - Mounts the same data PVC (read-only) and a separate backup PVC or S3 secret
   - Respects `backup.enabled` gate

5. Add Helm values under `backup:`:
   ```yaml
   backup:
     enabled: false
     schedule: "0 2 * * *"  # daily at 2am
     retention: 7            # keep N most recent backup files (PVC mode only)
     storage:
       type: pvc             # or s3
       pvc:
         size: 1Gi
         storageClass: ""
       s3:
         endpoint: ""
         bucket: ""
         prefix: "backups/"
         secretName: ""      # k8s secret with keys S3_KEY_ID and S3_SECRET
   ```

6. When `storage.type: pvc`, the CronJob also runs a cleanup step after backup: `ls -t /backup/*.db | tail -n +$((retention+1)) | xargs rm -f`

## Implementation Notes

- **Subcommand routing:** `cmd/server/main.go` currently calls `config.Load()` then `store.Open()` unconditionally. Add `os.Args[1] == "backup"` detection before the store open, parse the backup flags with `flag.NewFlagSet`, and exit early. Keep the backup logic in a new `internal/backup/backup.go` package.
- **`VACUUM INTO` syntax:** `db.Exec(fmt.Sprintf("VACUUM INTO %q", outputPath))` — the path must be an absolute path to a file that does not already exist. Delete it first if it does.
- **Admin check on the HTTP endpoint:** `auth.RequireAuth` already sets `Claims` in context via `auth.GetUser(r)`. Add a `requireAdmin` middleware helper in `internal/server/server.go` (alongside `requireAuth`) that returns `403` when `claims.Role != store.RoleAdmin`.
- **Helm CronJob:** follow the same label/selector pattern as `deployment.yaml`. The CronJob container uses the same image. Mount the data PVC as `readOnly: true` at `/data` and the backup PVC at `/backup`. For S3 mode, inject `S3_KEY_ID`/`S3_SECRET` from the referenced k8s Secret as env vars.
- **S3 upload without SDK:** use a pre-signed URL approach or manual AWS4-HMAC-SHA256 signing. Implement a minimal `internal/backup/s3.go` helper. This is optional complexity — implement only if the CronJob uses S3 mode.
- **No new go.mod dependencies** unless unavoidable; `VACUUM INTO` and stdlib HTTP are sufficient.

## Validation

```bash
# Build
go build ./cmd/server/...

# Smoke test local backup
OVERLAY_SESSION_SECRET=test ./agentic-hive backup --output /tmp/test-backup.db
# Expect: prints path and size, exits 0
sqlite3 /tmp/test-backup.db ".tables"
# Expect: users servers ssh_keys session_templates schema_version

# API endpoint (requires running server + admin session cookie)
curl -s -X POST http://localhost:8080/api/admin/backup \
  -b "session=<admin_jwt>" \
  -o /tmp/api-backup.db -D -
# Expect: 200, Content-Disposition header, valid SQLite file

# Helm template renders CronJob when enabled
helm template test ./deploy/helm/agentic-hive \
  --set backup.enabled=true \
  --set backup.storage.type=pvc \
  | grep -A5 "kind: CronJob"
# Expect: CronJob block present

# Helm template renders no CronJob when disabled (default)
helm template test ./deploy/helm/agentic-hive \
  | grep "kind: CronJob" | wc -l
# Expect: 0
```

## Out of Scope

- Streaming upload to S3 in the HTTP endpoint (write local temp file first, then upload is fine)
- Backup encryption at rest (SSH keys are already encrypted in the DB; full DB encryption is a separate concern)
- Restore command (restoring is done by replacing the PVC file manually)
- Multiple backup destinations simultaneously
- Backup integrity verification (checksums, test-restore)
