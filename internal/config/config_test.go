package config

import (
	"os"
	"testing"
)


func TestLoadConfigDefaults(t *testing.T) {
	os.Unsetenv("OVERLAY_LISTEN")
	os.Unsetenv("OVERLAY_DB_PATH")
	os.Unsetenv("OVERLAY_AUTH_MODE")
	os.Unsetenv("OVERLAY_POLL_INTERVAL")
	os.Unsetenv("OVERLAY_TERMINAL_IDLE_TIMEOUT")
	os.Unsetenv("OVERLAY_SESSION_SECRET")
	os.Unsetenv("OVERLAY_KEYSTORE_BACKEND")

	cfg := Load()

	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":8080")
	}
	if cfg.DBPath != "/data/overlay.db" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "/data/overlay.db")
	}
	if cfg.AuthMode != "local" {
		t.Errorf("AuthMode = %q, want %q", cfg.AuthMode, "local")
	}
	if cfg.PollInterval != 30 {
		t.Errorf("PollInterval = %d, want %d", cfg.PollInterval, 30)
	}
	if cfg.TerminalIdleTimeout != 0 {
		t.Errorf("TerminalIdleTimeout = %d, want %d", cfg.TerminalIdleTimeout, 0)
	}
	if cfg.KeyStoreBackend != "local" {
		t.Errorf("KeyStoreBackend = %q, want %q", cfg.KeyStoreBackend, "local")
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("OVERLAY_LISTEN", ":9090")
	t.Setenv("OVERLAY_DB_PATH", "/tmp/test.db")
	t.Setenv("OVERLAY_AUTH_MODE", "oidc")
	t.Setenv("OVERLAY_POLL_INTERVAL", "60")
	t.Setenv("OVERLAY_TERMINAL_IDLE_TIMEOUT", "1800")
	t.Setenv("OVERLAY_SESSION_SECRET", "abc123")
	t.Setenv("OVERLAY_KEYSTORE_BACKEND", "vault")

	cfg := Load()

	if cfg.Listen != ":9090" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":9090")
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "/tmp/test.db")
	}
	if cfg.AuthMode != "oidc" {
		t.Errorf("AuthMode = %q, want %q", cfg.AuthMode, "oidc")
	}
	if cfg.PollInterval != 60 {
		t.Errorf("PollInterval = %d, want %d", cfg.PollInterval, 60)
	}
	if cfg.TerminalIdleTimeout != 1800 {
		t.Errorf("TerminalIdleTimeout = %d, want %d", cfg.TerminalIdleTimeout, 1800)
	}
	if cfg.SessionSecret != "abc123" {
		t.Errorf("SessionSecret = %q, want %q", cfg.SessionSecret, "abc123")
	}
	if cfg.KeyStoreBackend != "vault" {
		t.Errorf("KeyStoreBackend = %q, want %q", cfg.KeyStoreBackend, "vault")
	}
}

func clearOIDCEnv(t *testing.T) {
	t.Helper()
	for _, env := range []string{
		"OVERLAY_OIDC_ISSUER_URL", "OVERLAY_OIDC_CLIENT_ID", "OVERLAY_OIDC_CLIENT_SECRET",
		"OVERLAY_OIDC_REDIRECT_URL", "OVERLAY_OIDC_ROLES_CLAIM", "OVERLAY_OIDC_ADMIN_GROUP",
		"OVERLAY_VAULT_ADDR", "OVERLAY_VAULT_TOKEN", "OVERLAY_VAULT_SECRET_PATH",
		"OVERLAY_AUTH_MODE", "OVERLAY_KEYSTORE_BACKEND", "OVERLAY_POLL_INTERVAL",
		"OVERLAY_TERMINAL_IDLE_TIMEOUT", "OVERLAY_LOG_LEVEL", "OVERLAY_EMERGENCY_LOCAL_AUTH",
	} {
		t.Setenv(env, "") // use t.Setenv so the test restores env on cleanup
		os.Unsetenv(env)
	}
}

func TestResolveSettingsDefaults(t *testing.T) {
	clearOIDCEnv(t)

	cfg := Load()
	resolved := ResolveSettings(cfg, nil)

	// OIDC defaults
	if sv := resolved.OIDC["roles_claim"]; sv.Source != SourceDefault || sv.Value != "groups" {
		t.Errorf("OIDC roles_claim: source=%q value=%q, want default/groups", sv.Source, sv.Value)
	}
	if sv := resolved.OIDC["admin_group"]; sv.Source != SourceDefault || sv.Value != "overlay-admin" {
		t.Errorf("OIDC admin_group: source=%q value=%q, want default/overlay-admin", sv.Source, sv.Value)
	}

	// Vault defaults
	if sv := resolved.Vault["secret_path"]; sv.Source != SourceDefault || sv.Value != "agentic-hive/ssh-keys" {
		t.Errorf("Vault secret_path: source=%q value=%q", sv.Source, sv.Value)
	}

	// General defaults
	if sv := resolved.General["auth_mode"]; sv.Source != SourceDefault || sv.Value != "local" {
		t.Errorf("General auth_mode: source=%q value=%q, want default/local", sv.Source, sv.Value)
	}
	if sv := resolved.General["poll_interval"]; sv.Source != SourceDefault || sv.Value != "30" {
		t.Errorf("General poll_interval: source=%q value=%q, want default/30", sv.Source, sv.Value)
	}
}

func TestResolveSettingsEnvOverride(t *testing.T) {
	clearOIDCEnv(t)
	t.Setenv("OVERLAY_OIDC_ISSUER_URL", "https://idp.example.com")
	t.Setenv("OVERLAY_OIDC_CLIENT_SECRET", "super-secret")
	t.Setenv("OVERLAY_AUTH_MODE", "oidc")

	cfg := Load()
	resolved := ResolveSettings(cfg, map[string]string{
		"oidc.issuer_url":  "https://db-idp.example.com", // env takes precedence
		"general.auth_mode": "local",                      // env takes precedence
	})

	// Env overrides DB
	if sv := resolved.OIDC["issuer_url"]; sv.Source != SourceEnv || sv.Value != "https://idp.example.com" {
		t.Errorf("OIDC issuer_url: source=%q value=%q, want env/https://idp.example.com", sv.Source, sv.Value)
	}

	// Secret: env set => IsSet=true, Value must be empty
	if sv := resolved.OIDC["client_secret"]; sv.Source != SourceEnv || !sv.IsSet || sv.Value != "" {
		t.Errorf("OIDC client_secret: source=%q IsSet=%v value=%q, want env/true/empty", sv.Source, sv.IsSet, sv.Value)
	}

	// General auth_mode from env
	if sv := resolved.General["auth_mode"]; sv.Source != SourceEnv || sv.Value != "oidc" {
		t.Errorf("General auth_mode: source=%q value=%q, want env/oidc", sv.Source, sv.Value)
	}
}

func TestResolveSettingsDBValues(t *testing.T) {
	clearOIDCEnv(t)

	cfg := Load()
	dbSettings := map[string]string{
		"oidc.issuer_url":    "https://db-idp.example.com",
		"oidc.client_id":     "my-client",
		"oidc.client_secret": "db-secret",
		"vault.addr":         "https://vault.example.com",
		"vault.token":        "db-vault-token",
		"general.auth_mode":  "oidc",
		"general.log_level":  "debug",
	}
	resolved := ResolveSettings(cfg, dbSettings)

	if sv := resolved.OIDC["issuer_url"]; sv.Source != SourceDB || sv.Value != "https://db-idp.example.com" {
		t.Errorf("OIDC issuer_url: source=%q value=%q, want db/https://db-idp.example.com", sv.Source, sv.Value)
	}
	if sv := resolved.OIDC["client_id"]; sv.Source != SourceDB || sv.Value != "my-client" {
		t.Errorf("OIDC client_id: source=%q value=%q, want db/my-client", sv.Source, sv.Value)
	}
	// Secret from DB: IsSet=true, Value must be empty
	if sv := resolved.OIDC["client_secret"]; sv.Source != SourceDB || !sv.IsSet || sv.Value != "" {
		t.Errorf("OIDC client_secret: source=%q IsSet=%v value=%q, want db/true/empty", sv.Source, sv.IsSet, sv.Value)
	}
	if sv := resolved.Vault["addr"]; sv.Source != SourceDB || sv.Value != "https://vault.example.com" {
		t.Errorf("Vault addr: source=%q value=%q, want db/https://vault.example.com", sv.Source, sv.Value)
	}
	if sv := resolved.General["auth_mode"]; sv.Source != SourceDB || sv.Value != "oidc" {
		t.Errorf("General auth_mode: source=%q value=%q, want db/oidc", sv.Source, sv.Value)
	}
	if sv := resolved.General["log_level"]; sv.Source != SourceDB || sv.Value != "debug" {
		t.Errorf("General log_level: source=%q value=%q, want db/debug", sv.Source, sv.Value)
	}
}

func TestApplyDBSettings(t *testing.T) {
	clearOIDCEnv(t)

	cfg := Load()
	dbSettings := map[string]string{
		"oidc.issuer_url":       "https://idp.example.com",
		"oidc.client_id":        "client-from-db",
		"oidc.client_secret":    "secret-from-db",
		"vault.addr":            "https://vault.example.com",
		"vault.token":           "vault-token-from-db",
		"vault.secret_path":     "secret/custom/path",
		"general.auth_mode":     "oidc",
		"general.log_level":     "debug",
		"general.poll_interval": "120",
	}

	cfg.ApplyDBSettings(dbSettings)

	if cfg.OIDCIssuerURL != "https://idp.example.com" {
		t.Errorf("OIDCIssuerURL = %q, want %q", cfg.OIDCIssuerURL, "https://idp.example.com")
	}
	if cfg.OIDCClientID != "client-from-db" {
		t.Errorf("OIDCClientID = %q, want %q", cfg.OIDCClientID, "client-from-db")
	}
	if cfg.OIDCClientSecret != "secret-from-db" {
		t.Errorf("OIDCClientSecret = %q, want %q", cfg.OIDCClientSecret, "secret-from-db")
	}
	if cfg.VaultAddr != "https://vault.example.com" {
		t.Errorf("VaultAddr = %q, want %q", cfg.VaultAddr, "https://vault.example.com")
	}
	if cfg.VaultToken != "vault-token-from-db" {
		t.Errorf("VaultToken = %q, want %q", cfg.VaultToken, "vault-token-from-db")
	}
	if cfg.VaultSecretPath != "secret/custom/path" {
		t.Errorf("VaultSecretPath = %q, want %q", cfg.VaultSecretPath, "secret/custom/path")
	}
	if cfg.AuthMode != "oidc" {
		t.Errorf("AuthMode = %q, want %q", cfg.AuthMode, "oidc")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.PollInterval != 120 {
		t.Errorf("PollInterval = %d, want %d", cfg.PollInterval, 120)
	}
}

func TestApplyDBSettingsEnvWins(t *testing.T) {
	clearOIDCEnv(t)
	// Set env vars — these should NOT be overridden by DB
	t.Setenv("OVERLAY_OIDC_ISSUER_URL", "https://env-idp.example.com")
	t.Setenv("OVERLAY_AUTH_MODE", "local")

	cfg := Load()
	dbSettings := map[string]string{
		"oidc.issuer_url":   "https://db-idp.example.com",
		"general.auth_mode": "oidc",
	}

	cfg.ApplyDBSettings(dbSettings)

	if cfg.OIDCIssuerURL != "https://env-idp.example.com" {
		t.Errorf("OIDCIssuerURL = %q, want env value %q", cfg.OIDCIssuerURL, "https://env-idp.example.com")
	}
	if cfg.AuthMode != "local" {
		t.Errorf("AuthMode = %q, want env value %q", cfg.AuthMode, "local")
	}
}
