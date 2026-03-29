# PRP-22: Vault Key References (Live Read)

## Goal

Allow users to reference SSH keys stored at arbitrary Vault paths instead of pasting them, using the global Vault connection for live reads on every SSH connection.

## Background

Currently SSH keys are either encrypted locally in SQLite or stored at a system-managed Vault path (`{base-path}/{server-id}`). In a multi-user scenario, users may already have SSH keys stored in their own Vault namespaces (e.g., `teams/devops/ssh/devbox` or `users/stefan/keys/prod`). They shouldn't need to extract and paste these keys — they should just point to where they already live.

The admin configures the global Vault connection (address + token). Users reference paths readable by that token. The key is read live on every SSH connection — never copied or duplicated. This means key rotation in Vault takes effect immediately.

## Requirements

### 1. Database Migration

Create `internal/store/migrations/006_vault_key_ref.sql`:
```sql
ALTER TABLE servers ADD COLUMN key_source TEXT NOT NULL DEFAULT 'local';
ALTER TABLE servers ADD COLUMN vault_key_path TEXT NOT NULL DEFAULT '';

INSERT OR IGNORE INTO schema_version (version) VALUES (6);
```

- `key_source`: `"local"` (key managed by the system keystore — paste or system Vault path) or `"vault_ref"` (user-specified Vault path, read live)
- `vault_key_path`: the user-provided path within the `secret/` KVv2 mount (e.g., `teams/devops/ssh/devbox`). Only used when `key_source = "vault_ref"`.

### 2. Store Model Update

Add fields to `Server` struct in `internal/store/models.go`:
```go
KeySource    string // "local" or "vault_ref"
VaultKeyPath string // only when KeySource == "vault_ref"
```

Update `CreateServer` to accept `keySource` and `vaultKeyPath` parameters.

Update all server queries (GetServer, ListServers) to include the new columns.

### 3. SSH Pool Key Resolution

Update `internal/sshpool/pool.go` `connect()` method:

Currently it calls `p.keystore.Get(ctx, serverID)` for every connection. Change to:

```go
func (p *Pool) getKey(ctx context.Context, srv *store.Server) ([]byte, error) {
    if srv.KeySource == "vault_ref" && srv.VaultKeyPath != "" {
        // Read directly from the user-specified Vault path
        return p.keystore.GetFromPath(ctx, srv.VaultKeyPath)
    }
    // Default: read from system keystore (local or system vault path)
    return p.keystore.Get(ctx, srv.ID)
}
```

This requires the pool to have access to the server record (not just the ID). The pool already calls `p.store.GetServer()` in `connect()`, so the server struct is available.

### 4. KeyStore Interface Extension

Add a new method to the `KeyStore` interface in `internal/keystore/keystore.go`:
```go
type KeyStore interface {
    Get(ctx context.Context, serverID string) ([]byte, error)
    GetFromPath(ctx context.Context, vaultPath string) ([]byte, error)  // new
    Put(ctx context.Context, serverID string, key []byte) error
    Delete(ctx context.Context, serverID string) error
}
```

Implementations:
- **LocalKeyStore**: `GetFromPath` returns an error — local store doesn't support arbitrary paths
- **VaultKeyStore**: `GetFromPath` reads from `client.KVv2("secret").Get(ctx, vaultPath)` and extracts the `private_key` field
- **SwappableKeyStore**: delegates to inner

### 5. Server Handler Changes

Update `handleCreateServer` in `internal/server/server.go`:
- Accept `keySource` and `vaultKeyPath` in the request body
- If `keySource == "vault_ref"`: validate that Vault is configured, store the path reference, skip key upload step
- If `keySource == "local"` (default): existing behavior (upload key separately)

Update `handleUploadKey`:
- Only works for servers with `key_source = "local"`. Return 400 for vault-ref servers.

### 6. Frontend Changes

Update the "Add Server" form in `cmd/server/static/js/app.js`:

**"Paste Key" tab (key_source = local):**
- Same as today — textarea for pasting SSH key
- On submit: create server, then upload key via PUT

**"From Vault" tab (key_source = vault_ref):**
- Single input field: "Vault path"
- Placeholder: `e.g. teams/devops/ssh/devbox`
- Help text: "Path within the KVv2 `secret/` mount. Must contain a `private_key` field."
- On submit: create server with `keySource: "vault_ref"` and `vaultKeyPath` — no separate key upload needed

The server card should indicate key source:
- Local key: show a lock icon or "local key"
- Vault ref: show a vault icon or "vault: {path}" in the server details

### 7. Validation on Add

When adding a server with `key_source = "vault_ref"`:
- The backend should attempt to read from the Vault path immediately to verify access
- If the path doesn't exist or the token can't read it, return an error
- If successful, proceed with server creation
- The test-connection (`echo ok`) should also work since the key is readable

### 8. Remove Redundant "From Vault" Concept

The admin settings Vault "Secret Path" field (`agentic-hive/ssh-keys`) is still used for system-managed keys (paste flow). When a user pastes a key and Vault is the active backend, it goes to `{base-path}/{server-id}`. This is the system-managed path.

The user-specified Vault path (this PRP) is completely separate — it's a reference to an existing key the user already has in Vault.

Both can coexist:
- Server A: `key_source=local`, key at `agentic-hive/ssh-keys/{id}` (system-managed)
- Server B: `key_source=vault_ref`, key at `teams/stefan/ssh/devbox` (user-managed)

## Validation

- Build passes: `go build ./cmd/server`
- All tests pass: `go test ./... -count=1`
- Adding a server with "Paste Key" still works as before
- Adding a server with "From Vault" reads from the specified path
- SSH connection works for vault-ref servers
- Key rotation in Vault takes effect on next SSH connection (no app restart)
- Creating a vault-ref server when Vault is not configured returns an error
- Creating a vault-ref server with a non-existent path returns an error
- Server card shows key source indicator

## Out of Scope

- Per-user Vault tokens (users share the global admin-configured token)
- Key migration between local and vault-ref
- Custom KVv2 mount names (always `secret/`)
- Custom data field names (always `private_key`)
- Vault namespaces (enterprise feature)
