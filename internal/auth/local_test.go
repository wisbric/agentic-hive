package auth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func testStoreWithUser(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.DefaultCost)
	_, err = st.CreateUser("admin", string(hash), "admin")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	return st
}

func testEmptyStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestLocalLoginSuccess(t *testing.T) {
	st := testStoreWithUser(t)
	secret := "test-secret-that-is-long-enough-32chars!"
	handler := NewLocalHandler(st, secret)

	body := `{"username":"admin","password":"correctpassword"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleLogin(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie, got none")
	}
	if !sessionCookie.HttpOnly {
		t.Error("session cookie should be httpOnly")
	}

	claims, err := VerifyJWT(sessionCookie.Value, secret)
	if err != nil {
		t.Fatalf("token verification failed: %v", err)
	}
	if claims.Username != "admin" {
		t.Errorf("Username = %q, want %q", claims.Username, "admin")
	}
	if claims.Role != "admin" {
		t.Errorf("Role = %q, want %q", claims.Role, "admin")
	}
}

func TestLocalLoginWrongPassword(t *testing.T) {
	st := testStoreWithUser(t)
	secret := "test-secret-that-is-long-enough-32chars!"
	handler := NewLocalHandler(st, secret)

	body := `{"username":"admin","password":"wrongpassword"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleLogin(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestLocalLoginUnknownUser(t *testing.T) {
	st := testStoreWithUser(t)
	secret := "test-secret-that-is-long-enough-32chars!"
	handler := NewLocalHandler(st, secret)

	body := `{"username":"nobody","password":"pass"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleLogin(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestSetupModeNeeded(t *testing.T) {
	st := testEmptyStore(t)
	handler := NewLocalHandler(st, "secret-long-enough-for-testing-32chars!")

	if !handler.SetupNeeded() {
		t.Error("SetupNeeded should be true with no users")
	}
}

func TestSetupModeNotNeeded(t *testing.T) {
	st := testStoreWithUser(t)
	handler := NewLocalHandler(st, "secret-long-enough-for-testing-32chars!")

	if handler.SetupNeeded() {
		t.Error("SetupNeeded should be false with existing user")
	}
}

func TestHandleSetup(t *testing.T) {
	st := testEmptyStore(t)
	secret := "test-secret-that-is-long-enough-32chars!"
	handler := NewLocalHandler(st, secret)

	body := `{"username":"myadmin","password":"mypassword123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/setup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleSetup(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	user, err := st.GetUserByUsername("myadmin")
	if err != nil {
		t.Fatalf("user not created: %v", err)
	}
	if user.Role != "admin" {
		t.Errorf("Role = %q, want %q", user.Role, "admin")
	}

	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie after setup")
	}

	// Setup should not work again
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/setup", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")

	handler.HandleSetup(w2, req2)

	if w2.Code != http.StatusForbidden {
		t.Errorf("second setup: status = %d, want %d", w2.Code, http.StatusForbidden)
	}
}
