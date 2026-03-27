package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"slices"

	"github.com/coreos/go-oidc/v3/oidc"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/config"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"
	"golang.org/x/oauth2"
)

type OIDCHandler struct {
	store    *store.Store
	secret   string
	cfg      *config.Config
	provider *oidc.Provider
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func NewOIDCHandler(ctx context.Context, st *store.Store, cfg *config.Config) (*OIDCHandler, error) {
	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuerURL)
	if err != nil {
		return nil, err
	}

	oauthCfg := &oauth2.Config{
		ClientID:     cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret,
		RedirectURL:  cfg.OIDCRedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID})

	return &OIDCHandler{
		store:    st,
		secret:   cfg.SessionSecret,
		cfg:      cfg,
		provider: provider,
		oauth:    oauthCfg,
		verifier: verifier,
	}, nil
}

func (h *OIDCHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	state := hex.EncodeToString(b)

	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300,
	})
	http.Redirect(w, r, h.oauth.AuthCodeURL(state), http.StatusFound)
}

func (h *OIDCHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("oidc_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, `{"error":"invalid state"}`, http.StatusBadRequest)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:   "oidc_state",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, `{"error":"missing code"}`, http.StatusBadRequest)
		return
	}

	oauth2Token, err := h.oauth.Exchange(r.Context(), code)
	if err != nil {
		log.Printf("oidc: token exchange failed: %v", err)
		http.Error(w, `{"error":"token exchange failed"}`, http.StatusInternalServerError)
		return
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		http.Error(w, `{"error":"no id_token in response"}`, http.StatusInternalServerError)
		return
	}

	idToken, err := h.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		log.Printf("oidc: token verification failed: %v", err)
		http.Error(w, `{"error":"token verification failed"}`, http.StatusInternalServerError)
		return
	}

	var tokenClaims map[string]any
	if err := idToken.Claims(&tokenClaims); err != nil {
		http.Error(w, `{"error":"failed to parse claims"}`, http.StatusInternalServerError)
		return
	}

	username := claimString(tokenClaims, "preferred_username")
	if username == "" {
		username = claimString(tokenClaims, "email")
	}
	if username == "" {
		username = idToken.Subject
	}

	role := store.RoleUser
	if groups := claimStringSlice(tokenClaims, h.cfg.OIDCRolesClaim); slices.Contains(groups, h.cfg.OIDCAdminGroup) {
		role = store.RoleAdmin
	}

	user, err := h.store.UpsertOIDCUser(idToken.Subject, username, role)
	if err != nil {
		log.Printf("oidc: upsert user failed: %v", err)
		http.Error(w, `{"error":"failed to create user"}`, http.StatusInternalServerError)
		return
	}

	token, err := SignJWT(&Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
	}, h.secret, SessionTTL)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	SetSessionCookie(w, token, http.SameSiteLaxMode)

	http.Redirect(w, r, "/", http.StatusFound)
}

func claimString(claims map[string]any, key string) string {
	v, ok := claims[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func claimStringSlice(claims map[string]any, key string) []string {
	v, ok := claims[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var result []string
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
