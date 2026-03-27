package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/hashicorp/vault/api"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/auth"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/config"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/keystore"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"
)

// handleGetSettings returns all resolved settings with source attribution.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	dbSettings, err := s.store.GetAllSettings()
	if err != nil {
		jsonError(w, "failed to read settings", http.StatusInternalServerError)
		return
	}
	resolved := config.ResolveSettings(s.cfg, dbSettings)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resolved)
}

// handleUpdateSettings saves one or more settings and hot-reloads affected components.
func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var updates map[string]string
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if len(updates) == 0 {
		jsonError(w, "no settings provided", http.StatusBadRequest)
		return
	}

	// Persist each setting.
	for key, value := range updates {
		if err := s.store.SetSetting(key, value); err != nil {
			jsonError(w, fmt.Sprintf("failed to save setting %q: %v", key, err), http.StatusInternalServerError)
			return
		}
	}

	// Apply saved settings to the live config.
	allSettings, err := s.store.GetAllSettings()
	if err != nil {
		jsonError(w, "failed to re-read settings", http.StatusInternalServerError)
		return
	}
	s.cfg.ApplyDBSettings(allSettings)

	// Detect which subsystems are affected and hot-reload them.
	oidcChanged := false
	vaultChanged := false
	pollIntervalChanged := false
	for key := range updates {
		switch {
		case strings.HasPrefix(key, "oidc."):
			oidcChanged = true
		case strings.HasPrefix(key, "vault."):
			vaultChanged = true
		case key == "general.poll_interval":
			pollIntervalChanged = true
		}
	}

	if oidcChanged {
		if err := s.reinitOIDC(r.Context()); err != nil {
			slog.Warn("OIDC re-init failed after settings update, keeping old handler", "error", err)
			jsonError(w, fmt.Sprintf("settings saved but OIDC re-init failed: %v", err), http.StatusBadGateway)
			return
		}
	}

	if vaultChanged {
		if err := s.reinitVault(r.Context()); err != nil {
			slog.Warn("Vault re-init failed after settings update, keeping old handler", "error", err)
			jsonError(w, fmt.Sprintf("settings saved but Vault re-init failed: %v", err), http.StatusBadGateway)
			return
		}
	}

	if pollIntervalChanged {
		interval := time.Duration(s.cfg.PollInterval) * time.Second
		if interval > 0 {
			s.sessions.UpdateInterval(interval)
		}
	}

	// Audit log.
	user := auth.GetUser(r)
	keys := make([]string, 0, len(updates))
	for k := range updates {
		keys = append(keys, k)
	}
	logAudit(s, r, user, store.AuditSettingsUpdate, "settings", "", fmt.Sprintf(`{"keys":%q}`, keys))

	// Return new resolved settings.
	resolved := config.ResolveSettings(s.cfg, allSettings)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resolved)
}

// testOIDCRequest is the request body for POST /api/admin/settings/test-oidc.
type testOIDCRequest struct {
	IssuerURL    string `json:"issuer_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURL  string `json:"redirect_url"`
}

// handleTestOIDC verifies OIDC discovery without saving anything.
func (s *Server) handleTestOIDC(w http.ResponseWriter, r *http.Request) {
	var req testOIDCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Fall back to current config when fields are not supplied.
	issuerURL := req.IssuerURL
	if issuerURL == "" {
		issuerURL = s.cfg.OIDCIssuerURL
	}
	if issuerURL == "" {
		jsonError(w, "issuer_url is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	// Return discovered endpoints.
	var claims struct {
		Issuer                string   `json:"issuer"`
		AuthorizationEndpoint string   `json:"authorization_endpoint"`
		TokenEndpoint         string   `json:"token_endpoint"`
		UserinfoEndpoint      string   `json:"userinfo_endpoint"`
		JWKSURI               string   `json:"jwks_uri"`
		ScopesSupported       []string `json:"scopes_supported"`
	}
	_ = provider.Claims(&claims)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"ok":        true,
		"issuer":    claims.Issuer,
		"endpoints": claims,
	})
}

// testVaultRequest is the request body for POST /api/admin/settings/test-vault.
type testVaultRequest struct {
	Address    string `json:"address"`
	Token      string `json:"token"`
	SecretPath string `json:"secret_path"`
}

// handleTestVault verifies Vault connectivity without saving anything.
func (s *Server) handleTestVault(w http.ResponseWriter, r *http.Request) {
	var req testVaultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Fall back to current config when fields are not supplied.
	address := req.Address
	if address == "" {
		address = s.cfg.VaultAddr
	}
	token := req.Token
	if token == "" {
		token = s.cfg.VaultToken
	}
	if address == "" {
		jsonError(w, "address is required", http.StatusBadRequest)
		return
	}

	vaultCfg := api.DefaultConfig()
	vaultCfg.Address = address
	// Honour request context for the health check.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	vaultCfg.HttpClient.Timeout = 10 * time.Second

	client, err := api.NewClient(vaultCfg)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}
	if token != "" {
		client.SetToken(token)
	}

	health, err := client.Sys().HealthWithContext(ctx)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"initialized": health.Initialized,
		"sealed":      health.Sealed,
		"standby":     health.Standby,
		"version":     health.Version,
	})
}

// reinitOIDC re-creates the OIDC handler from the current effective config and swaps it in.
// If OIDC is not configured (missing issuer URL or client ID), the handler is disabled.
func (s *Server) reinitOIDC(ctx context.Context) error {
	dbSettings, _ := s.store.GetAllSettings()
	cfg := *s.cfg
	cfg.ApplyDBSettings(dbSettings)

	if cfg.OIDCIssuerURL == "" || cfg.OIDCClientID == "" {
		s.oidcHandler.Swap(nil) // disable OIDC
		return nil
	}

	handler, err := auth.NewOIDCHandler(ctx, s.store, &cfg)
	if err != nil {
		return err
	}
	s.oidcHandler.Swap(handler)
	return nil
}

// reinitVault re-creates the Vault key store from the current effective config and swaps it in.
// Falls back to local keystore when Vault is not configured.
func (s *Server) reinitVault(_ context.Context) error {
	dbSettings, _ := s.store.GetAllSettings()
	cfg := *s.cfg
	cfg.ApplyDBSettings(dbSettings)

	if cfg.VaultAddr == "" || cfg.VaultToken == "" {
		encSecret := cfg.EncryptionSecret
		if encSecret == "" {
			encSecret = cfg.SessionSecret
		}
		ks := keystore.NewLocal(s.store.DB(), encSecret)
		s.keyStore.Swap(ks)
		return nil
	}

	ks, err := keystore.NewVault(cfg.VaultAddr, cfg.VaultToken, cfg.VaultSecretPath)
	if err != nil {
		return err
	}
	s.keyStore.Swap(ks)
	return nil
}
