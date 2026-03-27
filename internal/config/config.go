package config

import (
	"os"
	"strconv"
)

type Config struct {
	Listen          string
	DBPath          string
	AuthMode        string // "local" or "oidc"
	SessionSecret   string
	PollInterval    int // seconds
	IdleTimeout     int // seconds, 0 = disabled
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
		PollInterval:    envIntOr("OVERLAY_POLL_INTERVAL", 30),
		IdleTimeout:     envIntOr("OVERLAY_IDLE_TIMEOUT", 0),
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
		VaultSecretPath: envOr("OVERLAY_VAULT_SECRET_PATH", "secret/agentic-hive/ssh-keys"),

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
