package config

import (
	"os"
	"strconv"
)

// SettingSource indicates where a config value comes from.
type SettingSource string

const (
	SourceEnv     SettingSource = "env"
	SourceDB      SettingSource = "db"
	SourceDefault SettingSource = "default"
)

// SettingValue holds a config value with its source.
type SettingValue struct {
	Value  string        `json:"value"`
	Source SettingSource `json:"source"`
	IsSet  bool          `json:"is_set,omitempty"` // for secrets: whether non-empty
}

// ResolvedSettings holds all configurable settings with their sources.
type ResolvedSettings struct {
	OIDC    map[string]SettingValue `json:"oidc"`
	Vault   map[string]SettingValue `json:"vault"`
	General map[string]SettingValue `json:"general"`
}

// settingDef maps a logical key to its env var name, default value, and whether it is a secret.
type settingDef struct {
	envVar       string
	defaultValue string
	isSecret     bool
}

// oidcSettings lists all OIDC-related settings.
var oidcSettings = map[string]settingDef{
	"issuer_url":    {envVar: "OVERLAY_OIDC_ISSUER_URL", defaultValue: ""},
	"client_id":     {envVar: "OVERLAY_OIDC_CLIENT_ID", defaultValue: ""},
	"client_secret": {envVar: "OVERLAY_OIDC_CLIENT_SECRET", defaultValue: "", isSecret: true},
	"redirect_url":  {envVar: "OVERLAY_OIDC_REDIRECT_URL", defaultValue: ""},
	"roles_claim":   {envVar: "OVERLAY_OIDC_ROLES_CLAIM", defaultValue: "groups"},
	"admin_group":   {envVar: "OVERLAY_OIDC_ADMIN_GROUP", defaultValue: "overlay-admin"},
}

// vaultSettings lists all Vault-related settings.
var vaultSettings = map[string]settingDef{
	"address":     {envVar: "OVERLAY_VAULT_ADDR", defaultValue: ""},
	"token":       {envVar: "OVERLAY_VAULT_TOKEN", defaultValue: "", isSecret: true},
	"secret_path": {envVar: "OVERLAY_VAULT_SECRET_PATH", defaultValue: "agentic-hive/ssh-keys"},
}

// generalSettings lists general/top-level settings.
var generalSettings = map[string]settingDef{
	"auth_mode":             {envVar: "OVERLAY_AUTH_MODE", defaultValue: "local"},
	"keystore_backend":      {envVar: "OVERLAY_KEYSTORE_BACKEND", defaultValue: "local"},
	"poll_interval":         {envVar: "OVERLAY_POLL_INTERVAL", defaultValue: "30"},
	"terminal_idle_timeout": {envVar: "OVERLAY_TERMINAL_IDLE_TIMEOUT", defaultValue: "0"},
	"log_level":             {envVar: "OVERLAY_LOG_LEVEL", defaultValue: "info"},
	"emergency_local_auth":  {envVar: "OVERLAY_EMERGENCY_LOCAL_AUTH", defaultValue: "false"},
}

// resolveGroup resolves a group of settings, returning a SettingValue per key.
func resolveGroup(defs map[string]settingDef, dbSettings map[string]string, prefix string) map[string]SettingValue {
	out := make(map[string]SettingValue, len(defs))
	for key, def := range defs {
		dbKey := prefix + key
		envVal := os.Getenv(def.envVar)
		dbVal := dbSettings[dbKey]

		var sv SettingValue
		switch {
		case envVal != "":
			sv = SettingValue{Source: SourceEnv}
			if def.isSecret {
				sv.IsSet = true
			} else {
				sv.Value = envVal
			}
		case dbVal != "":
			sv = SettingValue{Source: SourceDB}
			if def.isSecret {
				sv.IsSet = true
			} else {
				sv.Value = dbVal
			}
		default:
			sv = SettingValue{Source: SourceDefault}
			if def.isSecret {
				sv.IsSet = def.defaultValue != ""
			} else {
				sv.Value = def.defaultValue
			}
		}
		out[key] = sv
	}
	return out
}

// ResolveSettings resolves all settings from environment variables and DB, tagged with their source.
// envCfg is used only to check whether values are set (compared against env vars directly).
func ResolveSettings(envCfg *Config, dbSettings map[string]string) *ResolvedSettings {
	return &ResolvedSettings{
		OIDC:    resolveGroup(oidcSettings, dbSettings, "oidc."),
		Vault:   resolveGroup(vaultSettings, dbSettings, "vault."),
		General: resolveGroup(generalSettings, dbSettings, "general."),
	}
}

// ApplyDBSettings updates Config fields from DB settings, but only for fields not already
// set by an environment variable.
func (c *Config) ApplyDBSettings(dbSettings map[string]string) {
	applyStr := func(envVar, dbKey string, field *string) {
		if os.Getenv(envVar) == "" {
			if v, ok := dbSettings[dbKey]; ok && v != "" {
				*field = v
			}
		}
	}
	applyInt := func(envVar, dbKey string, field *int) {
		if os.Getenv(envVar) == "" {
			if v, ok := dbSettings[dbKey]; ok && v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					*field = n
				}
			}
		}
	}
	applyBool := func(envVar, dbKey string, field *bool) {
		if os.Getenv(envVar) == "" {
			if v, ok := dbSettings[dbKey]; ok && v != "" {
				*field = v == "true"
			}
		}
	}

	// OIDC
	applyStr("OVERLAY_OIDC_ISSUER_URL", "oidc.issuer_url", &c.OIDCIssuerURL)
	applyStr("OVERLAY_OIDC_CLIENT_ID", "oidc.client_id", &c.OIDCClientID)
	applyStr("OVERLAY_OIDC_CLIENT_SECRET", "oidc.client_secret", &c.OIDCClientSecret)
	applyStr("OVERLAY_OIDC_REDIRECT_URL", "oidc.redirect_url", &c.OIDCRedirectURL)
	applyStr("OVERLAY_OIDC_ROLES_CLAIM", "oidc.roles_claim", &c.OIDCRolesClaim)
	applyStr("OVERLAY_OIDC_ADMIN_GROUP", "oidc.admin_group", &c.OIDCAdminGroup)

	// Vault
	applyStr("OVERLAY_VAULT_ADDR", "vault.address", &c.VaultAddr)
	applyStr("OVERLAY_VAULT_TOKEN", "vault.token", &c.VaultToken)
	applyStr("OVERLAY_VAULT_SECRET_PATH", "vault.secret_path", &c.VaultSecretPath)

	// Auto-detect keystore backend: if vault address + token are configured, switch to vault
	if c.VaultAddr != "" && c.VaultToken != "" && os.Getenv("OVERLAY_KEYSTORE_BACKEND") == "" {
		c.KeyStoreBackend = "vault"
	}

	// General
	applyStr("OVERLAY_AUTH_MODE", "general.auth_mode", &c.AuthMode)
	applyStr("OVERLAY_KEYSTORE_BACKEND", "general.keystore_backend", &c.KeyStoreBackend)
	applyStr("OVERLAY_LOG_LEVEL", "general.log_level", &c.LogLevel)
	applyInt("OVERLAY_POLL_INTERVAL", "general.poll_interval", &c.PollInterval)
	applyInt("OVERLAY_TERMINAL_IDLE_TIMEOUT", "general.terminal_idle_timeout", &c.TerminalIdleTimeout)
	applyBool("OVERLAY_EMERGENCY_LOCAL_AUTH", "general.emergency_local_auth", &c.EmergencyLocalAuth)
}

type Config struct {
	Listen          string
	DBPath          string
	AuthMode        string // "local" or "oidc"
	SessionSecret   string
	PollInterval        int // seconds
	TerminalIdleTimeout int // seconds, 0 = disabled
	KeyStoreBackend string // "local" or "vault"
	LogLevel        string // "debug", "info", "warn", "error" (default "info")

	// OIDC (only when AuthMode == "oidc")
	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	OIDCRolesClaim   string
	OIDCAdminGroup   string

	// Vault (only when KeyStoreBackend == "vault")
	VaultAddr       string
	VaultToken      string
	VaultSecretPath string

	// EncryptionSecret for SSH key encryption (separate from JWT signing secret).
	// If empty, SessionSecret is used as fallback (backward compatible).
	EncryptionSecret string

	// Emergency local auth when OIDC is primary
	EmergencyLocalAuth bool

	// Login rate limiting
	LoginRateLimit  int // max failed attempts per window (default 5)
	LoginRateWindow int // window size in seconds (default 900)

	// Readiness probe
	ReadyzRequireServer bool
}

func Load() *Config {
	return &Config{
		Listen:          envOr("OVERLAY_LISTEN", ":8080"),
		DBPath:          envOr("OVERLAY_DB_PATH", "/data/overlay.db"),
		AuthMode:        envOr("OVERLAY_AUTH_MODE", "local"),
		SessionSecret:   envOr("OVERLAY_SESSION_SECRET", ""),
		PollInterval:        envIntOr("OVERLAY_POLL_INTERVAL", 30),
		TerminalIdleTimeout: envIntOr("OVERLAY_TERMINAL_IDLE_TIMEOUT", 0),
		KeyStoreBackend: envOr("OVERLAY_KEYSTORE_BACKEND", "local"),
		LogLevel:        envOr("OVERLAY_LOG_LEVEL", "info"),

		OIDCIssuerURL:    envOr("OVERLAY_OIDC_ISSUER_URL", ""),
		OIDCClientID:     envOr("OVERLAY_OIDC_CLIENT_ID", ""),
		OIDCClientSecret: envOr("OVERLAY_OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:  envOr("OVERLAY_OIDC_REDIRECT_URL", ""),
		OIDCRolesClaim:   envOr("OVERLAY_OIDC_ROLES_CLAIM", "groups"),
		OIDCAdminGroup:   envOr("OVERLAY_OIDC_ADMIN_GROUP", "overlay-admin"),

		VaultAddr:       envOr("OVERLAY_VAULT_ADDR", ""),
		VaultToken:      envOr("OVERLAY_VAULT_TOKEN", ""),
		VaultSecretPath: envOr("OVERLAY_VAULT_SECRET_PATH", "agentic-hive/ssh-keys"),

		EncryptionSecret: envOr("OVERLAY_ENCRYPTION_SECRET", ""),

		EmergencyLocalAuth: envOr("OVERLAY_EMERGENCY_LOCAL_AUTH", "") == "true",

		ReadyzRequireServer: envOr("OVERLAY_READYZ_REQUIRE_SERVER", "") == "true",

		LoginRateLimit:  envIntOr("OVERLAY_LOGIN_RATE_LIMIT", 5),
		LoginRateWindow: envIntOr("OVERLAY_LOGIN_RATE_WINDOW", 900),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
