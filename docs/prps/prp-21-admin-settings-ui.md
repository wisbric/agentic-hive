# PRP-21: Admin Settings UI (OIDC, Vault, Poll Interval)

## Goal

Allow administrators to configure OIDC (Keycloak), Vault (OpenBao), and poll interval via the admin UI, with changes taking effect without restart.

## Background

OIDC, Vault, and poll interval are currently configured exclusively via environment variables. This means any change requires a Helm upgrade + pod restart. For operational agility, admins should be able to configure these from the UI. Environment variables remain authoritative — if set, they override DB settings and the UI shows them as read-only.

## Requirements

### 1. Settings Store

- New migration `004_settings.sql`: `settings` table with `key TEXT PK, value TEXT, updated_at DATETIME`
- Store methods: `GetSetting(key)`, `SetSetting(key, value)`, `GetAllSettings()`, `DeleteSetting(key)`
- Settings keys:
  - `oidc.issuer_url`, `oidc.client_id`, `oidc.client_secret`, `oidc.redirect_url`, `oidc.roles_claim`, `oidc.admin_group`
  - `vault.address`, `vault.token`, `vault.secret_path`
  - `poll_interval`

### 2. Config Resolution

- On startup and on settings change: merge env vars + DB settings
- Env vars take precedence — if `OVERLAY_OIDC_ISSUER_URL` is set, the DB value for `oidc.issuer_url` is ignored
- Add `ResolveConfig(envConfig *Config, dbSettings map[string]string) *Config` that produces the effective config
- Each setting in the API response includes `source: "env" | "db" | "default"` so the UI knows what's locked

### 3. Hot-Reload for OIDC

- Wrap the OIDC handler in a `SwappableOIDCHandler` with `sync.RWMutex`
- When OIDC settings change via UI: re-initialize the OIDC provider/verifier, swap the handler
- If OIDC discovery fails, return error to the admin UI but don't break the existing handler
- If OIDC is not currently configured and admin enables it via UI, register the routes dynamically

### 4. Hot-Reload for Vault KeyStore

- Wrap the KeyStore in a `SwappableKeyStore` that delegates to the current backend
- When Vault settings change: create new VaultKeyStore, test connectivity, swap
- If switching from local to vault (or vice versa): warn that existing keys are in the old backend
- Keys are NOT migrated automatically — that's a separate operation

### 5. Hot-Reload for Poll Interval

- Add `UpdateInterval(d time.Duration)` method to session Manager
- Resets the internal ticker to the new interval
- Takes effect immediately, no restart

### 6. API Endpoints

- `GET /api/admin/settings` (admin only) — returns all settings with source and masked secrets
  ```json
  {
    "oidc": {
      "issuer_url": {"value": "https://auth.example.com/realms/hive", "source": "db"},
      "client_id": {"value": "agentic-hive", "source": "db"},
      "client_secret": {"value": "****", "source": "db", "is_set": true},
      "redirect_url": {"value": "https://hive.example.com/api/auth/oidc/callback", "source": "db"},
      "roles_claim": {"value": "groups", "source": "default"},
      "admin_group": {"value": "overlay-admin", "source": "default"}
    },
    "vault": {
      "address": {"value": "", "source": "default"},
      "token": {"value": "", "source": "default", "is_set": false},
      "secret_path": {"value": "secret/claude-overlay/ssh-keys", "source": "default"}
    },
    "general": {
      "poll_interval": {"value": "30", "source": "env"}
    }
  }
  ```

- `PUT /api/admin/settings` (admin only) — update one or more settings
  ```json
  {
    "oidc.issuer_url": "https://auth.example.com/realms/hive",
    "oidc.client_id": "agentic-hive",
    "oidc.client_secret": "my-secret",
    "poll_interval": "60"
  }
  ```
  Response includes which settings were applied vs skipped (env override).

- `POST /api/admin/settings/test-oidc` (admin only) — test OIDC discovery
  - Fetches `issuer_url + /.well-known/openid-configuration`
  - Returns success with provider info or error message

- `POST /api/admin/settings/test-vault` (admin only) — test Vault connectivity
  - Attempts `vault.sys.Health()` call
  - Returns success with Vault status or error message

### 7. Admin UI

Add a "Settings" tab/section to the admin panel with three cards:

**OIDC / Keycloak card:**
- Form fields for all OIDC settings
- Fields sourced from env vars are shown as read-only with "(set via environment)" label
- "Test Connection" button → calls test-oidc endpoint, shows result inline
- "Save" button → calls PUT settings

**Vault / OpenBao card:**
- Form fields for address, token, secret path
- Same env-override read-only behavior
- "Test Connection" button → calls test-vault endpoint
- "Save" button

**General card:**
- Poll interval (number input, seconds)
- "Save" button

All cards show a success/error toast after save.

### 8. Audit Logging

- Log `settings.update` audit entries when settings are changed
- Include which keys were changed (not the values — secrets)

## Implementation Notes

- `SwappableOIDCHandler` pattern:
  ```go
  type SwappableOIDCHandler struct {
      mu      sync.RWMutex
      handler *OIDCHandler // nil if not configured
  }
  func (s *SwappableOIDCHandler) ServeHTTP(w, r) { s.mu.RLock(); ... }
  func (s *SwappableOIDCHandler) Swap(h *OIDCHandler) { s.mu.Lock(); ... }
  ```
- The server routes OIDC to the swappable handler from the start (returns 404 if nil)
- For Vault hot-swap: `SwappableKeyStore` wraps `keystore.KeyStore` interface
- DB settings are read at startup and merged with env config before initializing anything
- The `PUT /api/admin/settings` handler triggers re-initialization of affected components

## Validation

- Build passes: `go build ./cmd/server`
- All tests pass: `go test ./... -count=1`
- Settings saved via UI persist across pod restart
- OIDC test button works against a real Keycloak
- Changing poll interval via UI changes the actual poll frequency (verify via logs)
- Env var overrides show as read-only in UI
- Changing a setting creates an audit log entry

## Out of Scope

- Key migration between local and Vault backends
- Light/dark mode toggle
- Settings import/export
- Multi-admin conflict resolution (last write wins is fine)
