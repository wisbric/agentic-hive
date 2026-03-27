package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSignAndVerifyJWT(t *testing.T) {
	secret := "test-secret-that-is-long-enough-32chars!"

	claims := &Claims{
		UserID:   "user123",
		Username: "stefan",
		Role:     "admin",
	}

	token, err := SignJWT(claims, secret, 1*time.Hour)
	if err != nil {
		t.Fatalf("SignJWT failed: %v", err)
	}
	if token == "" {
		t.Fatal("token should not be empty")
	}

	got, err := VerifyJWT(token, secret)
	if err != nil {
		t.Fatalf("VerifyJWT failed: %v", err)
	}
	if got.UserID != "user123" {
		t.Errorf("UserID = %q, want %q", got.UserID, "user123")
	}
	if got.Username != "stefan" {
		t.Errorf("Username = %q, want %q", got.Username, "stefan")
	}
	if got.Role != "admin" {
		t.Errorf("Role = %q, want %q", got.Role, "admin")
	}
}

func TestVerifyJWTExpired(t *testing.T) {
	secret := "test-secret-that-is-long-enough-32chars!"

	claims := &Claims{
		UserID:   "user123",
		Username: "stefan",
		Role:     "admin",
	}

	token, err := SignJWT(claims, secret, -1*time.Hour)
	if err != nil {
		t.Fatalf("SignJWT failed: %v", err)
	}

	_, err = VerifyJWT(token, secret)
	if err == nil {
		t.Error("expected error for expired token, got nil")
	}
}

func TestVerifyJWTWrongSecret(t *testing.T) {
	token, _ := SignJWT(&Claims{UserID: "u1", Username: "u", Role: "user"}, "secret-one-is-long-enough-32chars!", 1*time.Hour)

	_, err := VerifyJWT(token, "secret-two-is-long-enough-32chars!")
	if err == nil {
		t.Error("expected error for wrong secret, got nil")
	}
}

func TestRequireAuthNoToken(t *testing.T) {
	secret := "test-secret-that-is-long-enough-32chars!"

	handler := RequireAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuthValidToken(t *testing.T) {
	secret := "test-secret-that-is-long-enough-32chars!"

	token, _ := SignJWT(&Claims{UserID: "u1", Username: "stefan", Role: "admin"}, secret, 1*time.Hour)

	var gotUser *Claims
	handler := RequireAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = GetUser(r)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if gotUser == nil {
		t.Fatal("expected user in context, got nil")
	}
	if gotUser.Username != "stefan" {
		t.Errorf("Username = %q, want %q", gotUser.Username, "stefan")
	}
}

func TestRequireAuthExpiredToken(t *testing.T) {
	secret := "test-secret-that-is-long-enough-32chars!"

	token, _ := SignJWT(&Claims{UserID: "u1", Username: "stefan", Role: "admin"}, secret, -1*time.Hour)

	handler := RequireAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
