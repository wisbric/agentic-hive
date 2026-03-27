package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/metrics"
	"gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/store"
)

const SessionTTL = 24 * time.Hour
const sessionCookieMaxAge = 86400 // SessionTTL in seconds

func SetSessionCookie(w http.ResponseWriter, token string, sameSite http.SameSite) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: sameSite,
		MaxAge:   sessionCookieMaxAge,
	})
}

// SetCSRFCookie generates a 32-byte random hex token, sets a non-HttpOnly
// "csrf" cookie (Secure, SameSite=Strict, same MaxAge as session), and
// returns the token string.
func SetCSRFCookie(w http.ResponseWriter) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf",
		Value:    token,
		Path:     "/",
		HttpOnly: false, // intentional — JS must read it
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   sessionCookieMaxAge,
	})
	return token, nil
}

// ReadCSRFCookie returns the value of the "csrf" cookie, or "" if absent.
func ReadCSRFCookie(r *http.Request) string {
	c, err := r.Cookie("csrf")
	if err != nil {
		return ""
	}
	return c.Value
}

// CSRFProtect returns middleware that validates the double-submit cookie pattern
// for methods POST, PUT, DELETE, PATCH.
// It compares r.Header.Get("X-CSRF-Token") against the "csrf" cookie value.
// Mismatch or missing token returns 403.
// Safe methods (GET, HEAD, OPTIONS) and the exempt paths pass through unchanged.
// WebSocket paths (/ws/) are always validated via the "csrf" query parameter.
func CSRFProtect(exempt ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check exempt paths first (prefix match)
			for _, e := range exempt {
				if strings.HasPrefix(r.URL.Path, e) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// WebSocket paths: validate csrf query param regardless of method
			if strings.HasPrefix(r.URL.Path, "/ws/") {
				cookie := ReadCSRFCookie(r)
				param := r.URL.Query().Get("csrf")
				if cookie == "" || param == "" || subtle.ConstantTimeCompare([]byte(param), []byte(cookie)) != 1 {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					json.NewEncoder(w).Encode(map[string]string{"error": "csrf token mismatch"})
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			// Safe methods pass through
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}

			// State-changing methods: validate double-submit cookie
			cookie := ReadCSRFCookie(r)
			header := r.Header.Get("X-CSRF-Token")
			if cookie == "" || header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(cookie)) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "csrf token mismatch"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

type Claims struct {
	UserID   string `json:"sub"`
	Username string `json:"name"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type contextKey string

const userContextKey contextKey = "user"

func SignJWT(c *Claims, secret string, ttl time.Duration) (string, error) {
	c.RegisteredClaims = jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return token.SignedString([]byte(secret))
}

func VerifyJWT(tokenStr, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
}

func SetUser(r *http.Request, c *Claims) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userContextKey, c))
}

func GetUser(r *http.Request) *Claims {
	c, _ := r.Context().Value(userContextKey).(*Claims)
	return c
}

// HandleLogout clears the session cookie.
func HandleLogout(w http.ResponseWriter, r *http.Request) {
	ClearSessionCookie(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// jwtFailureReason classifies a JWT verification error into a metric label value.
func jwtFailureReason(err error) string {
	if errors.Is(err, jwt.ErrTokenExpired) {
		return "token_expired"
	}
	return "token_invalid"
}

// RequireAuth returns middleware that checks for a valid JWT in the "session" cookie.
// On success, it sets the user claims in the request context.
func RequireAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session")
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}

			claims, err := VerifyJWT(cookie.Value, secret)
			if err != nil {
				if metrics.AuthFailuresTotal != nil {
					metrics.AuthFailuresTotal.WithLabelValues(jwtFailureReason(err)).Inc()
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}

			next.ServeHTTP(w, SetUser(r, claims))
		})
	}
}

// RequireAdmin returns middleware that checks for a valid JWT in the "session" cookie
// and that the user has the admin role. Returns 401 if no valid token, 403 if not admin.
func RequireAdmin(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session")
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}

			claims, err := VerifyJWT(cookie.Value, secret)
			if err != nil {
				if metrics.AuthFailuresTotal != nil {
					metrics.AuthFailuresTotal.WithLabelValues(jwtFailureReason(err)).Inc()
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}

			if claims.Role != store.RoleAdmin {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
				return
			}

			next.ServeHTTP(w, SetUser(r, claims))
		})
	}
}
