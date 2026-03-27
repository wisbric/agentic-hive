# PRP-10: Login Rate Limiting

## Goal

Protect `/api/auth/login` and `/api/auth/setup` from brute-force attacks with an in-memory, per-IP rate limiter that returns `429 Too Many Requests` after 5 failed attempts within 15 minutes.

## Background

`internal/auth/local.go` implements `HandleLogin` and `HandleSetup` with no brute-force protection. An attacker can make unlimited password guesses. The project has no external cache or queue infrastructure, so the solution must be purely in-memory using a `sync.Mutex`-protected map. Limits should be configurable via environment variables, and the implementation should be a reusable middleware so it can be applied to specific routes.

## Requirements

1. Add config fields to `internal/config/config.go` in the `Config` struct:
   ```go
   LoginRateLimit  int // max failed attempts per window (default 5)
   LoginRateWindow int // window size in seconds (default 900)
   ```
   Load them in `Load()`:
   ```go
   LoginRateLimit:  envIntOr("OVERLAY_LOGIN_RATE_LIMIT", 5),
   LoginRateWindow: envIntOr("OVERLAY_LOGIN_RATE_WINDOW", 900),
   ```

2. Create `internal/auth/ratelimit.go` with the following public API:
   ```go
   package auth

   // RateLimiter is a per-IP failed-attempt tracker.
   // Create with NewRateLimiter; close with Close to stop the cleanup goroutine.
   type RateLimiter struct { ... }

   // NewRateLimiter creates a limiter: maxAttempts failures within windowSecs seconds triggers 429.
   func NewRateLimiter(maxAttempts, windowSecs int) *RateLimiter

   // Middleware returns an http.Handler middleware.
   // It tracks failed attempts by IP. If the wrapped handler writes a 4xx status
   // (other than 429 itself), the attempt is counted. A successful response (2xx/3xx)
   // resets the counter for that IP.
   // If the limit is exceeded, the middleware short-circuits with 429 and a
   // Retry-After header (seconds until the oldest attempt expires).
   func (rl *RateLimiter) Middleware(next http.Handler) http.Handler

   // Close stops the background cleanup goroutine.
   func (rl *RateLimiter) Close()
   ```

3. Internal implementation details for `ratelimit.go`:
   - `attemptTracker` struct: `attempts []time.Time` (timestamps of failed attempts within window), protected by the parent `RateLimiter.mu sync.Mutex`.
   - `RateLimiter` fields: `mu sync.Mutex`, `trackers map[string]*attemptTracker`, `maxAttempts int`, `window time.Duration`, `done chan struct{}`.
   - IP extraction: use `r.RemoteAddr`, strip the port with `net.SplitHostAddr` (or `strings.Split(addr, ":")[0]` as fallback). Prefer the `X-Forwarded-For` header only when explicitly needed — for now use `r.RemoteAddr` only to avoid header spoofing.
   - Counting: the middleware wraps the `http.ResponseWriter` with a `responseRecorder` (status-capturing wrapper) to inspect the status code AFTER the inner handler returns. If `status >= 400 && status != 429`, increment. If `status < 400`, reset.
   - Window expiry: before checking the count, filter out `attemptTracker.attempts` entries older than `window`.
   - Cleanup goroutine (started in `NewRateLimiter`): every `window` duration, remove entries from `trackers` where all attempts are expired. Use `select { case <-ticker.C: ... case <-rl.done: return }`.
   - `Retry-After` value: seconds until the earliest attempt in the window expires (`attempts[0].Add(window).Sub(time.Now()).Seconds()`, rounded up).

4. Wire the rate limiter in `internal/server/server.go`:
   - Add a `rateLimiter *auth.RateLimiter` field to the `Server` struct.
   - Create it in `New()`: `rateLimiter: auth.NewRateLimiter(cfg.LoginRateLimit, cfg.LoginRateWindow)`.
   - In `routes()`, wrap login and setup handlers:
     ```go
     s.mux.Handle("POST /api/auth/login", s.rateLimiter.Middleware(http.HandlerFunc(s.localAuth.HandleLogin)))
     s.mux.Handle("POST /api/auth/setup", s.rateLimiter.Middleware(http.HandlerFunc(s.localAuth.HandleSetup)))
     ```
     Remove the existing `HandleFunc` registrations for these two routes.
   - Add a `Close()` method to `Server` (or extend an existing one) that calls `s.rateLimiter.Close()`. If `Server` has no `Close()` yet, add it:
     ```go
     func (s *Server) Close() { s.rateLimiter.Close() }
     ```
   - Update `cmd/server/main.go` to call `srv.Close()` before exit (use `defer`).

5. `HandleSetup` in `local.go` returns `403 Forbidden` when setup is already completed — this must NOT count as a failed login attempt. Only 401 responses should increment the counter. Adjust the middleware: count only when `status == http.StatusUnauthorized` (401), not all `4xx`.

## Implementation Notes

- The `responseRecorder` can be minimal: embed `http.ResponseWriter`, add `code int` field, override `WriteHeader` to capture the code and call through. Set `code = http.StatusOK` as default so handlers that never call `WriteHeader` (implicit 200) are treated as success.
- Thread safety: all reads and writes to `trackers` and each tracker's `attempts` slice must hold `rl.mu`. A single top-level mutex is sufficient since login is not a hot path.
- Do not import any external rate-limiting library. The standard library (`sync`, `time`, `net`) is sufficient.
- The `Server` struct in `server.go` currently does not have a `Close()` method. Adding one is safe; `main.go` uses `srv.ListenAndServe()` which blocks, so `defer srv.Close()` before `srv.ListenAndServe()` is the correct call site.
- `cmd/server/main.go` uses `server.New(...)` — check its signature in `server.go`: `func New(cfg, st, pool, ks, sm, staticFS)`. The `cfg *config.Config` is already passed, so the new fields will be read automatically from the already-loaded config.

## Validation

```bash
# Build
cd /home/stefans/git/agentic-workspace/projects/claude-overlay
go build ./...
go vet ./...

# Unit tests — create internal/auth/ratelimit_test.go:
go test ./internal/auth/... -v -run TestRateLimit

# Expected test cases:
# - TestRateLimitUnder: 4 failed attempts → all return 401, no 429
# - TestRateLimitExact: 5th failed attempt → 401; 6th → 429 with Retry-After header
# - TestRateLimitReset: 3 failures then success → counter reset; next 5 failures needed
# - TestRateLimitWindow: inject attempts with timestamps older than window → they are discarded, no 429
# - TestRateLimitRetryAfterHeader: 429 response has Retry-After header with positive integer value
# - TestRateLimitSetup403NotCounted: 403 from HandleSetup (already set up) does NOT count as failure

# Integration test in internal/server/server_test.go:
go test ./internal/server/... -v -run TestLoginRateLimit

# Full suite
go test ./... -count=1
```

Acceptance criteria:
- 5 POST requests to `/api/auth/login` with wrong password all return 401.
- The 6th identical request returns 429 with a `Retry-After: N` header where N > 0.
- A successful login after 3 failures resets the counter; a fresh sequence of 5 failures is needed before the next 429.
- `OVERLAY_LOGIN_RATE_LIMIT=3` environment variable causes the 4th failed attempt to return 429.

## Out of Scope

- Persistent rate limiting across restarts (in-memory only).
- Rate limiting on endpoints other than `/api/auth/login` and `/api/auth/setup`.
- IP allowlisting or trusted proxy header (`X-Forwarded-For`) support.
- Distributed rate limiting (single-instance only).
- Account lockout (the rate limiter blocks by IP, not by username).
- Exponential backoff or CAPTCHA challenges.
