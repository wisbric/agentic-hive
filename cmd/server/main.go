package main

import (
	"log"

	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/config"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/server"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/store"
)

func main() {
	cfg := config.Load()

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer st.Close()

	if err := st.SeedTemplates(); err != nil {
		log.Fatalf("failed to seed templates: %v", err)
	}

	log.Printf("claude-overlay starting (auth=%s, keystore=%s)", cfg.AuthMode, cfg.KeyStoreBackend)

	srv := server.New(cfg, st)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
