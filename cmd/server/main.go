package main

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"time"

	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/auth"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/config"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/keystore"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/server"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/session"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/sshpool"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"
)

//go:embed all:static
var embeddedStatic embed.FS

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func main() {
	cfg := config.Load()

	logLevel := parseLogLevel(cfg.LogLevel)
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})))

	if cfg.SessionSecret == "" {
		slog.Error("OVERLAY_SESSION_SECRET must be set (generate with: openssl rand -hex 32)")
		os.Exit(1)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	if err := st.SeedTemplates(); err != nil {
		slog.Error("failed to seed templates", "error", err)
		os.Exit(1)
	}

	// Initialize KeyStore
	var ks keystore.KeyStore
	switch cfg.KeyStoreBackend {
	case "vault":
		ks, err = keystore.NewVault(cfg.VaultAddr, cfg.VaultToken, cfg.VaultSecretPath)
		if err != nil {
			slog.Error("failed to initialize vault keystore", "error", err)
			os.Exit(1)
		}
		slog.Info("keystore initialized", "backend", "vault", "addr", cfg.VaultAddr)
	default:
		encSecret := cfg.EncryptionSecret
		if encSecret == "" {
			slog.Warn("OVERLAY_ENCRYPTION_SECRET not set, falling back to SESSION_SECRET for key encryption — set a separate secret in production")
			encSecret = cfg.SessionSecret
		}
		ks = keystore.NewLocal(st.DB(), encSecret)
		slog.Info("keystore initialized", "backend", "local")
	}

	// Initialize SSH Pool and Session Manager
	pool := sshpool.New(st, ks)
	defer pool.Close()

	sm := session.NewManager(st, pool)
	if cfg.PollInterval > 0 {
		sm.StartPolling(context.Background(), time.Duration(cfg.PollInterval)*time.Second)
		defer sm.Stop()
	}

	staticFS, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		slog.Error("failed to sub static fs", "error", err)
		os.Exit(1)
	}

	srv := server.New(cfg, st, pool, ks, sm, staticFS)
	defer srv.Close()

	if cfg.AuthMode == "oidc" && cfg.OIDCIssuerURL != "" {
		oidcHandler, err := auth.NewOIDCHandler(context.Background(), st, cfg)
		if err != nil {
			slog.Error("failed to initialize OIDC", "error", err)
			os.Exit(1)
		}
		server.SetOIDCHandler(srv, oidcHandler)
		slog.Info("oidc enabled", "issuer", cfg.OIDCIssuerURL)
	}

	slog.Info("server starting", "auth", cfg.AuthMode, "keystore", cfg.KeyStoreBackend)

	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
