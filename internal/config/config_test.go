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
