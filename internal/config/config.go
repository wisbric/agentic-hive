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

	// Emergency local auth when OIDC is primary
	EmergencyLocalAuth bool
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

		OIDCIssuerURL:    envOr("OVERLAY_OIDC_ISSUER_URL", ""),
		OIDCClientID:     envOr("OVERLAY_OIDC_CLIENT_ID", ""),
		OIDCClientSecret: envOr("OVERLAY_OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:  envOr("OVERLAY_OIDC_REDIRECT_URL", ""),
		OIDCRolesClaim:   envOr("OVERLAY_OIDC_ROLES_CLAIM", "groups"),
		OIDCAdminGroup:   envOr("OVERLAY_OIDC_ADMIN_GROUP", "overlay-admin"),

		VaultAddr:       envOr("OVERLAY_VAULT_ADDR", ""),
		VaultToken:      envOr("OVERLAY_VAULT_TOKEN", ""),
		VaultSecretPath: envOr("OVERLAY_VAULT_SECRET_PATH", "secret/claude-overlay/ssh-keys"),

		EmergencyLocalAuth: envOr("OVERLAY_EMERGENCY_LOCAL_AUTH", "") == "true",
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
