package auth

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// handlerWithStatus returns a handler that always writes the given HTTP status code.
func handlerWithStatus(code int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
	})
}

// newRequestFromIP creates a GET request with a fake RemoteAddr for IP-based testing.
func newRequestFromIP(ip string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = ip + ":12345"
	return req
}

// TestRateLimitUnder verifies 4 failed attempts all return 401 (no 429 triggered).
func TestRateLimitUnder(t *testing.T) {
	rl := NewRateLimiter(5, 900)
	defer rl.Close()

	handler := rl.Middleware(handlerWithStatus(http.StatusUnauthorized))

	for i := 0; i < 4; i++ {
		req := newRequestFromIP("192.168.1.1")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("attempt %d: got %d, want %d", i+1, w.Code, http.StatusUnauthorized)
		}
	}
}

// TestRateLimitExact verifies the 5th attempt returns 401 and the 6th returns 429 with Retry-After.
func TestRateLimitExact(t *testing.T) {
	rl := NewRateLimiter(5, 900)
	defer rl.Close()

	handler := rl.Middleware(handlerWithStatus(http.StatusUnauthorized))

	for i := 0; i < 5; i++ {
		req := newRequestFromIP("10.0.0.1")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("attempt %d: got %d, want %d", i+1, w.Code, http.StatusUnauthorized)
		}
	}

	// 6th attempt must be 429
	req := newRequestFromIP("10.0.0.1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("6th attempt: got %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	retryAfterStr := w.Header().Get("Retry-After")
	if retryAfterStr == "" {
		t.Error("6th attempt: Retry-After header missing")
	} else {
		n, err := strconv.Atoi(retryAfterStr)
		if err != nil || n <= 0 {
			t.Errorf("6th attempt: Retry-After = %q, want a positive integer", retryAfterStr)
		}
	}
}

// TestRateLimitReset verifies that a successful response resets the counter.
func TestRateLimitReset(t *testing.T) {
	rl := NewRateLimiter(5, 900)
	defer rl.Close()

	failHandler := rl.Middleware(handlerWithStatus(http.StatusUnauthorized))
	okHandler := rl.Middleware(handlerWithStatus(http.StatusOK))

	// 3 failures
	for i := 0; i < 3; i++ {
		req := newRequestFromIP("10.0.0.2")
		w := httptest.NewRecorder()
		failHandler.ServeHTTP(w, req)
	}

	// successful login resets counter
	req := newRequestFromIP("10.0.0.2")
	w := httptest.NewRecorder()
	okHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("success request: got %d, want 200", w.Code)
	}

	// Now need 5 more failures before 429
	for i := 0; i < 5; i++ {
		req := newRequestFromIP("10.0.0.2")
		w := httptest.NewRecorder()
		failHandler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("post-reset attempt %d: got %d, want 401", i+1, w.Code)
		}
	}

	// 6th after reset should be 429
	req = newRequestFromIP("10.0.0.2")
	w = httptest.NewRecorder()
	failHandler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("6th post-reset: got %d, want 429", w.Code)
	}
}

// TestRateLimitWindow verifies that attempts older than the window are discarded.
func TestRateLimitWindow(t *testing.T) {
	rl := NewRateLimiter(5, 900)
	defer rl.Close()

	// Inject 5 attempts with timestamps older than the window directly.
	oldTime := time.Now().Add(-1000 * time.Second)
	rl.mu.Lock()
	rl.trackers["10.0.0.3"] = &attemptTracker{
		attempts: []time.Time{oldTime, oldTime, oldTime, oldTime, oldTime},
	}
	rl.mu.Unlock()

	// The next request should NOT be 429 because all attempts are expired.
	handler := rl.Middleware(handlerWithStatus(http.StatusUnauthorized))
	req := newRequestFromIP("10.0.0.3")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code == http.StatusTooManyRequests {
		t.Errorf("expected expired attempts to be discarded, got 429")
	}
}

// TestRateLimitRetryAfterHeader verifies the Retry-After header is a positive integer.
func TestRateLimitRetryAfterHeader(t *testing.T) {
	rl := NewRateLimiter(2, 900)
	defer rl.Close()

	handler := rl.Middleware(handlerWithStatus(http.StatusUnauthorized))

	for i := 0; i < 2; i++ {
		req := newRequestFromIP("10.0.0.4")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	req := newRequestFromIP("10.0.0.4")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}

	val := w.Header().Get("Retry-After")
	if val == "" {
		t.Fatal("Retry-After header not set")
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		t.Fatalf("Retry-After is not an integer: %q", val)
	}
	if n <= 0 {
		t.Errorf("Retry-After = %d, want > 0", n)
	}
}

// TestRateLimitSetup403NotCounted verifies that a 403 response (setup already done) is not counted.
func TestRateLimitSetup403NotCounted(t *testing.T) {
	rl := NewRateLimiter(5, 900)
	defer rl.Close()

	// Simulate HandleSetup returning 403 when setup is already completed
	handler := rl.Middleware(handlerWithStatus(http.StatusForbidden))

	// Even 10 requests with 403 should not trigger 429
	for i := 0; i < 10; i++ {
		req := newRequestFromIP("10.0.0.5")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Errorf("403 response at attempt %d triggered 429 — should not be counted", i+1)
		}
	}
}
