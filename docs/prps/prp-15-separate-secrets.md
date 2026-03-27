# PRP-15: Separate JWT and Encryption Secrets

## Goal

Use separate secrets for JWT signing and SSH key encryption to limit blast radius of a compromise.

## Background

`SESSION_SECRET` is currently used for two unrelated cryptographic operations:

1. **JWT HMAC-SHA256 signing** — in `internal/auth/auth.go` (`SignJWT` / `VerifyJWT`), `internal/auth/local.go`, `internal/auth/oidc.go`
2. **Argon2id key derivation for AES-256-GCM** — in `internal/keystore/local.go` (`deriveKey`), used to encrypt SSH private keys at rest in SQLite

This violates the principle of key separation. If the JWT secret is extracted (e.g., via an HMAC timing attack or a future SSRF that leaks env vars), an attacker gains the ability to decrypt all stored SSH private keys — they do not need to forge tokens at all to pivot from web compromise to infrastructure access.

The fix is a single new env var (`OVERLAY_ENCRYPTION_SECRET`) used exclusively by `LocalKeyStore`. Backward compatibility is maintained by falling back to `SESSION_SECRET` when `OVERLAY_ENCRYPTION_SECRET` is not set, so existing deployments continue to work without a migration.

The vault keystore (`internal/keystore/vault.go`) is unaffected — it delegates encryption to Vault and never receives a passphrase.

## Requirements

1. Add `EncryptionSecret string` to `internal/config/config.go`, populated from `OVERLAY_ENCRYPTION_SECRET` env var (default `""`).

2. In `cmd/server/main.go`, after loading config:
   - If `cfg.EncryptionSecret == ""`, log a startup warning: `"OVERLAY_ENCRYPTION_SECRET not set, falling back to SESSION_SECRET for key encryption — set a separate secret in production"`
   - Pass `cfg.EncryptionSecret` (or `cfg.SessionSecret` as fallback) as the encryption secret to `keystore.NewLocal`

3. Update `keystore.NewLocal()` signature to accept the encryption secret as a distinct parameter (it already receives `secret string` — rename the parameter to `encryptionSecret` for clarity, no functional change when the same value is passed):
   ```go
   func NewLocal(db *sql.DB, encryptionSecret string) *LocalKeyStore
   ```
   The struct field `secret` in `LocalKeyStore` should be renamed to `encryptionSecret` for clarity.

4. Update the call in `cmd/server/main.go`:
   ```go
   encSecret := cfg.EncryptionSecret
   if encSecret == "" {
       slog.Warn("OVERLAY_ENCRYPTION_SECRET not set, falling back to SESSION_SECRET for key encryption")
       encSecret = cfg.SessionSecret
   }
   ks = keystore.NewLocal(st.DB(), encSecret)
   ```

5. Update Helm chart:
   - In `deploy/helm/agentic-hive/values.yaml`: add `config.encryptionSecret: ""` under the `config:` block
   - In `deploy/helm/agentic-hive/templates/secret.yaml`: add `ENCRYPTION_SECRET` with the same lookup-or-generate pattern as `SESSION_SECRET`:
     ```yaml
     {{- $encryptionSecret := "" -}}
     {{- if .Values.config.encryptionSecret }}
       {{- $encryptionSecret = .Values.config.encryptionSecret }}
     {{- else if and $existingSecret $existingSecret.data (index $existingSecret.data "ENCRYPTION_SECRET") }}
       {{- $encryptionSecret = (index $existingSecret.data "ENCRYPTION_SECRET" | b64dec) }}
     {{- else }}
       {{- $encryptionSecret = (randAlphaNum 64) }}
     {{- end }}
     ```
     Add `ENCRYPTION_SECRET: {{ $encryptionSecret | quote }}` to `stringData`.
   - In `deploy/helm/agentic-hive/templates/deployment.yaml` (or `configmap.yaml`, wherever `OVERLAY_SESSION_SECRET` is referenced as an env var), add the corresponding env var:
     ```yaml
     - name: OVERLAY_ENCRYPTION_SECRET
       valueFrom:
         secretKeyRef:
           name: {{ include "agentic-hive.fullname" . }}
           key: ENCRYPTION_SECRET
     ```

6. **Migration path**: Existing SSH keys encrypted with the old `SESSION_SECRET` continue to work because when `OVERLAY_ENCRYPTION_SECRET` is not set, the fallback is `SESSION_SECRET` — which is exactly what was used before. When an operator sets `OVERLAY_ENCRYPTION_SECRET`, new keys uploaded after that point use the new secret. Existing keys (encrypted with `SESSION_SECRET`) will fail to decrypt because `LocalKeyStore.Get` uses whichever secret the store was initialized with. Document this in the Helm values comment:
   ```yaml
   config:
     # encryptionSecret: separate secret for SSH key encryption (recommended in production).
     # If empty, SESSION_SECRET is used as fallback (backward compatible).
     # WARNING: changing this after SSH keys have been uploaded will require re-uploading all SSH keys.
     encryptionSecret: ""
   ```

## Implementation Notes

**Only `internal/keystore/local.go` changes cryptographic behavior** — the rename of the parameter is the only functional change. `LocalKeyStore.Put` and `LocalKeyStore.Get` both call `deriveKey(s.encryptionSecret, salt)` — no logic changes.

**`internal/auth/` packages are not touched** — they already receive `cfg.SessionSecret` directly and that does not change.

**`keystore/vault.go`** does not accept a secret parameter — no changes needed.

**Test impact**: `internal/keystore/local_test.go` calls `NewLocal(db, secret)` — the signature is identical (same parameter count and types), so tests pass without modification. The parameter rename is internal only.

**`server_test.go`**: The test in `internal/server/server_test.go` constructs `server.New(...)` — it does not call `keystore.NewLocal` directly. Unaffected.

**No data migration needed for the default case**: The Helm `secret.yaml` generates a new `ENCRYPTION_SECRET` on first upgrade (since the key did not exist before). Since `OVERLAY_ENCRYPTION_SECRET` is now set, the binary will use it rather than falling back. This means existing SSH keys (encrypted with `SESSION_SECRET`) will fail to decrypt after the first upgrade unless the operator re-uploads them. To avoid breaking existing deployments on upgrade, the Helm template should be documented clearly: operators should re-upload SSH keys after enabling `OVERLAY_ENCRYPTION_SECRET`, or set `config.encryptionSecret` to the same value as their current `config.sessionSecret` initially, then rotate later.

An alternative approach to avoid the upgrade breakage: in the Helm template, set the initial value of `ENCRYPTION_SECRET` to the existing `SESSION_SECRET` value when no `ENCRYPTION_SECRET` was previously stored. This is complex in Helm templating and out of scope — the documentation warning is sufficient.

## Validation

```bash
cd /home/stefans/git/agentic-workspace/projects/claude-overlay

# Build
go build ./...

# All tests pass (including keystore tests)
go test ./...

# Verify warning emitted when ENCRYPTION_SECRET not set
OVERLAY_SESSION_SECRET=test123 OVERLAY_DB_PATH=/tmp/test-enc.db ./agentic-hive 2>&1 | grep -i "encryption_secret" && echo "PASS: warning shown" || echo "FAIL: no warning"

# Verify no warning when both secrets set
OVERLAY_SESSION_SECRET=test123 OVERLAY_ENCRYPTION_SECRET=enc456 OVERLAY_DB_PATH=/tmp/test-enc2.db ./agentic-hive 2>&1 | grep -i "falling back" && echo "FAIL: spurious warning" || echo "PASS: no warning"

# Helm template renders ENCRYPTION_SECRET in secret
cd deploy/helm/agentic-hive
helm template . | grep "ENCRYPTION_SECRET" && echo "PASS: secret present" || echo "FAIL"

# Helm template renders env var in deployment
helm template . | grep "OVERLAY_ENCRYPTION_SECRET" && echo "PASS: env var present" || echo "FAIL"
```

## Out of Scope

- Automatic re-encryption of existing SSH keys when the encryption secret changes
- Key rotation tooling / migration scripts
- Changing the encryption algorithm (AES-256-GCM + Argon2id is appropriate)
- Encrypting the JWT secret at rest
- Separate secrets for OIDC state cookies vs JWT
