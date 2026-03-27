package auth

import (
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
