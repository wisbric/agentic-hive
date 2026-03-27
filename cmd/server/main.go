package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
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

func main() {
	cfg := config.Load()

	if cfg.SessionSecret == "" {
		log.Fatalf("OVERLAY_SESSION_SECRET must be set (generate with: openssl rand -hex 32)")
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer st.Close()

	if err := st.SeedTemplates(); err != nil {
		log.Fatalf("failed to seed templates: %v", err)
	}

	// Initialize KeyStore
	var ks keystore.KeyStore
	switch cfg.KeyStoreBackend {
	case "vault":
		ks, err = keystore.NewVault(cfg.VaultAddr, cfg.VaultToken, cfg.VaultSecretPath)
		if err != nil {
			log.Fatalf("failed to initialize vault keystore: %v", err)
		}
		log.Printf("using vault keystore (%s)", cfg.VaultAddr)
	default:
		ks = keystore.NewLocal(st.DB(), cfg.SessionSecret)
		log.Printf("using local keystore")
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
		log.Fatalf("failed to sub static fs: %v", err)
	}

	srv := server.New(cfg, st, pool, ks, sm, staticFS)
	defer srv.Close()

	if cfg.AuthMode == "oidc" && cfg.OIDCIssuerURL != "" {
		oidcHandler, err := auth.NewOIDCHandler(context.Background(), st, cfg)
		if err != nil {
			log.Fatalf("failed to initialize OIDC: %v", err)
		}
		server.SetOIDCHandler(srv, oidcHandler)
		log.Printf("OIDC authentication enabled (issuer=%s)", cfg.OIDCIssuerURL)
	}

	log.Printf("agentic-hive starting (auth=%s, keystore=%s)", cfg.AuthMode, cfg.KeyStoreBackend)

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
