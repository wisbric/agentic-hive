package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/wisbric/agentic-hive/internal/auth"
	"github.com/wisbric/agentic-hive/internal/backup"
	"github.com/wisbric/agentic-hive/internal/config"
	"github.com/wisbric/agentic-hive/internal/keystore"
	"github.com/wisbric/agentic-hive/internal/metrics"
	"github.com/wisbric/agentic-hive/internal/server"
	"github.com/wisbric/agentic-hive/internal/session"
	"github.com/wisbric/agentic-hive/internal/sshpool"
	"github.com/wisbric/agentic-hive/internal/store"
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

func runBackup(args []string) {
	fs2 := flag.NewFlagSet("backup", flag.ExitOnError)
	output := fs2.String("output", "", "destination path for the backup file (required)")
	if err := fs2.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "backup: %v\n", err)
		os.Exit(1)
	}
	if *output == "" {
		fmt.Fprintln(os.Stderr, "backup: --output is required")
		fs2.Usage()
		os.Exit(1)
	}

	cfg := config.Load()

	if err := backup.Run(cfg.DBPath, *output); err != nil {
		fmt.Fprintf(os.Stderr, "backup failed: %v\n", err)
		os.Exit(1)
	}

	info, err := os.Stat(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backup: stat output: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("backup written to %s (%d bytes)\n", *output, info.Size())
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		runBackup(os.Args[2:])
		return
	}

	metrics.Init()

	cfg := config.Load()

	logLevel := parseLogLevel(cfg.LogLevel)
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})))

	if cfg.SessionSecret == "" {
		slog.Error("OVERLAY_SESSION_SECRET must be set (generate with: openssl rand -hex 32)")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}

	if err := st.SeedTemplates(); err != nil {
		slog.Error("failed to seed templates", "error", err)
		os.Exit(1)
	}

	// Load DB settings and merge with env config (env vars take precedence)
	dbSettings, err := st.GetAllSettings()
	if err != nil {
		slog.Warn("failed to load DB settings, using env-only config", "error", err)
	} else if len(dbSettings) > 0 {
		cfg.ApplyDBSettings(dbSettings)
		slog.Info("applied DB settings", "count", len(dbSettings))
	}

	// Initialize KeyStore
	var innerKS keystore.KeyStore
	switch cfg.KeyStoreBackend {
	case "vault":
		innerKS, err = keystore.NewVault(cfg.VaultAddr, cfg.VaultToken, cfg.VaultSecretPath)
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
		innerKS = keystore.NewLocal(st.DB(), encSecret)
		slog.Info("keystore initialized", "backend", "local")
	}
	ks := keystore.NewSwappable(innerKS)

	// Initialize SSH Pool and Session Manager
	pool := sshpool.New(st, ks)

	sm := session.NewManager(st, pool)
	if cfg.PollInterval > 0 {
		sm.StartPolling(ctx, time.Duration(cfg.PollInterval)*time.Second)
	}

	staticFS, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		slog.Error("failed to sub static fs", "error", err)
		os.Exit(1)
	}

	srv := server.New(cfg, st, pool, ks, sm, staticFS)

	// Initialize OIDC if configured (via env vars OR DB settings)
	if cfg.OIDCIssuerURL != "" && cfg.OIDCClientID != "" {
		oidcHandler, err := auth.NewOIDCHandler(context.Background(), st, cfg)
		if err != nil {
			// Non-fatal: OIDC can be reconfigured via admin UI
			slog.Warn("OIDC initialization failed (can be reconfigured via admin UI)", "error", err, "issuer", cfg.OIDCIssuerURL)
		} else {
			server.SetOIDCHandler(srv, oidcHandler)
			slog.Info("oidc enabled", "issuer", cfg.OIDCIssuerURL)
		}
	}

	slog.Info("server starting", "auth", cfg.AuthMode, "keystore", cfg.KeyStoreBackend)

	if err := srv.ListenAndServe(ctx); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	// Shutdown sequence: deterministic ordering and logging
	sm.Stop()
	pool.Close()
	st.Close()
	srv.Close()
	slog.Info("shutdown complete")
}
