# PRP-11: CSRF Protection

## Goal

Add double-submit cookie CSRF protection to all state-changing API endpoints and the terminal WebSocket, and update the frontend JavaScript to include the CSRF token on every API call.

## Background

The application uses `HttpOnly` session cookies for authentication. The session cookie has `SameSite=Strict` for local login and `SameSite=Lax` for OIDC (`internal/auth/auth.go` `SetSessionCookie`, `internal/auth/oidc.go`). `SameSite=Lax` does not block cross-site POST requests in all browsers, and `SameSite=Strict` cannot be relied upon as the only CSRF defence. A CSRF attack against an admin user could create or delete servers. The double-submit cookie pattern requires no server-side state: on login, set a non-HttpOnly `csrf` cookie; on every state-changing request, the JS reads the cookie and sends it as `X-CSRF-Token`; the middleware compares them.

## Requirements

1. Add `SetCSRFCookie` and `ReadCSRFCookie` helpers to `internal/auth/auth.go`:
   ```go
   // SetCSRFCookie generates a 32-byte random hex token, sets a non-HttpOnly
   // "csrf" cookie (Secure, SameSite=Strict, same MaxAge as session), and
   // returns the token string.
   func SetCSRFCookie(w http.ResponseWriter) (string, error)

   // ReadCSRFCookie returns the value of the "csrf" cookie, or "" if absent.
   func ReadCSRFCookie(r *http.Request) string
   ```
   - `SetCSRFCookie` uses `crypto/rand` to generate 32 bytes, hex-encodes them, sets the cookie with `HttpOnly: false` (intentional — JS must read it), `Secure: true`, `SameSite: http.SameSiteStrictMode`, `MaxAge: sessionCookieMaxAge`.
   - Cookie name: `"csrf"`.

2. Add CSRF middleware to `internal/auth/auth.go`:
   ```go
   // CSRFProtect returns middleware that validates the double-submit cookie pattern
   // for methods POST, PUT, DELETE, PATCH.
   // It compares r.Header.Get("X-CSRF-Token") against the "csrf" cookie value.
   // Mismatch or missing token returns 403.
   // Safe methods (GET, HEAD, OPTIONS) and the exempt paths pass through unchanged.
   func CSRFProtect(exempt ...string) func(http.Handler) http.Handler
   ```
   - Exempt paths are exact prefix matches. Pass these from `routes()`:
     `/api/auth/login`, `/api/auth/setup`, `/api/auth/oidc/callback`, `/healthz`, `/readyz`.
   - Comparison: constant-time using `subtle.ConstantTimeCompare` (import `"crypto/subtle"`).
   - If the `csrf` cookie is absent or the `X-CSRF-Token` header is absent or they do not match, respond `{"error":"csrf token mismatch"}` with status 403.
   - Safe methods (`GET`, `HEAD`, `OPTIONS`) always pass through (no check needed).

3. Call `SetCSRFCookie` in every place that establishes a session:
   - `internal/auth/local.go` `HandleLogin`: after `SetSessionCookie(...)`, call `SetCSRFCookie(w)`. Discard the returned token (the JS will read the cookie). On error generating the token, return 500.
   - `internal/auth/local.go` `HandleSetup`: same as above, after `SetSessionCookie(...)`.
   - `internal/auth/oidc.go` `HandleCallback`: after `SetSessionCookie(w, token, http.SameSiteLaxMode)`, call `SetCSRFCookie(w)`.

4. Register the CSRF middleware globally in `internal/server/server.go`:
   - Wrap the entire mux with `CSRFProtect(...)` in `Handler()`:
     ```go
     func (s *Server) Handler() http.Handler {
         return auth.CSRFProtect(
             "/api/auth/login",
             "/api/auth/setup",
             "/api/auth/oidc/callback",
             "/healthz",
             "/readyz",
         )(s.mux)
     }
     ```
   - Update `ListenAndServe()` to use `s.Handler()` instead of `s.mux`:
     ```go
     func (s *Server) ListenAndServe() error {
         log.Printf("listening on %s", s.cfg.Listen)
         return http.ListenAndServe(s.cfg.Listen, s.Handler())
     }
     ```
   - `cmd/server/main.go` calls `srv.ListenAndServe()`, which will pick up the change automatically. No changes needed in `main.go`.

5. WebSocket CSRF — `GET /ws/terminal/{server}/{session}`:
   - WebSocket upgrades use GET, so the method check in the middleware would normally pass them through. However, the WebSocket handshake cannot include custom headers from the browser, so the JS must pass the CSRF token as a query parameter.
   - Extend `CSRFProtect` to also check paths matching `/ws/` prefix: if `strings.HasPrefix(r.URL.Path, "/ws/")`, compare `r.URL.Query().Get("csrf")` against the `csrf` cookie value (same `ConstantTimeCompare`). Return 403 on mismatch.
   - Update `terminal.js` `connect()` function: read the csrf cookie and append `&csrf=<token>` to the WebSocket URL:
     ```js
     const csrfToken = getCookie('csrf');
     const url = `${proto}//${location.host}/ws/terminal/${serverID}/${sessionName}?cols=${cols}&rows=${rows}&csrf=${csrfToken}`;
     ```
     Add a `getCookie(name)` helper function in `terminal.js`.

6. Update `cmd/server/static/js/app.js`:
   - Modify the `api(method, path, body)` function to include `X-CSRF-Token` on non-GET requests:
     ```js
     async function api(method, path, body) {
       const opts = { method, credentials: 'same-origin', headers: {} };
       if (body) {
         opts.headers['Content-Type'] = 'application/json';
         opts.body = JSON.stringify(body);
       }
       if (method !== 'GET' && method !== 'HEAD') {
         opts.headers['X-CSRF-Token'] = getCookie('csrf');
       }
       const res = await fetch(path, opts);
       if (res.status === 401) { showView('login'); throw new Error('unauthorized'); }
       return res;
     }
     ```
   - The `PUT /api/servers/{id}/key` call in the add-server form handler uses `fetch` directly (not via `api()`). Update it to also include the `X-CSRF-Token` header.
   - Add a `getCookie(name)` helper function in `app.js`:
     ```js
     function getCookie(name) {
       const match = document.cookie.match(new RegExp('(?:^|; )' + name + '=([^;]*)'));
       return match ? decodeURIComponent(match[1]) : '';
     }
     ```

## Implementation Notes

- The `csrf` cookie intentionally does NOT have `HttpOnly` — this is the entire point of the double-submit pattern. `Secure: true` is still set to prevent transmission over HTTP.
- `subtle.ConstantTimeCompare` works on `[]byte`; convert both the header value and cookie value with `[]byte(...)`.
- The exemption list in `CSRFProtect` is checked with `strings.HasPrefix(r.URL.Path, exempt[i])` so that `/api/auth/oidc/callback` covers any query string appended by the OIDC provider.
- `logout` (`POST /api/auth/logout`) is NOT exempt — the user is already logged in and the JS has the cookie. This is correct behaviour.
- The existing `upgrader.CheckOrigin` in `terminal.go` already validates same-origin; the CSRF query param adds a second layer specifically guarding against credentialed cross-origin WebSocket attempts.
- Do not change the `oidc_state` cookie in `oidc.go` — it is unrelated to CSRF.
- `SetCSRFCookie` must use `crypto/rand`, which is already indirectly imported in the module (used in `store/users.go`). Add explicit import in `auth.go`.

## Validation

```bash
# Build
cd /home/stefans/git/agentic-workspace/projects/claude-overlay
go build ./...
go vet ./...

# Unit tests — add to internal/auth/auth_test.go or new file internal/auth/csrf_test.go:
go test ./internal/auth/... -v -run TestCSRF

# Expected test cases:
# - TestSetCSRFCookie: sets non-HttpOnly cookie named "csrf" with 64-char hex value
# - TestCSRFProtectGetPassThrough: GET request without header passes through
# - TestCSRFProtectMissingToken: POST without X-CSRF-Token header → 403
# - TestCSRFProtectWrongToken: POST with X-CSRF-Token != cookie value → 403
# - TestCSRFProtectCorrectToken: POST with matching X-CSRF-Token and cookie → passes through
# - TestCSRFProtectExemptPath: POST to /api/auth/login without token → passes through
# - TestCSRFProtectWebSocket: GET /ws/terminal/... without csrf query param → 403
# - TestCSRFProtectWebSocketCorrectParam: GET /ws/terminal/... with correct csrf param → passes through

# Integration test
go test ./internal/server/... -v -run TestCSRF

# Expected:
# - TestCreateServerNoCSRF: admin-role JWT + no X-CSRF-Token → 403
# - TestCreateServerWithCSRF: admin-role JWT + valid X-CSRF-Token → 201
# - TestLoginNoCSRF: POST /api/auth/login without X-CSRF-Token → 200 (exempt)

# Full suite
go test ./... -count=1
```

End-to-end acceptance criteria (manual browser test):
1. Load the app, complete setup/login — `csrf` cookie is set (visible in browser dev tools, not HttpOnly).
2. Add a server from the UI — succeeds (JS sends `X-CSRF-Token`).
3. Open the terminal — WebSocket connects (JS passes `csrf` query param).
4. `curl -X POST http://localhost:8080/api/servers -H 'Content-Type: application/json' -d '{"name":"x","host":"y","port":22,"sshUser":"root"}' --cookie 'session=<valid_admin_jwt>'` → returns 403 (no CSRF token).
5. Same curl with `-H 'X-CSRF-Token: <correct_csrf_value>'` and `--cookie 'session=<jwt>; csrf=<value>'` → returns 201.

## Out of Scope

- Synchronizer token pattern (server-side token storage).
- CSRF protection for non-API routes (static files, HTML pages).
- Double-submit on non-cookie auth flows (Bearer token APIs are inherently CSRF-safe).
- Rotating CSRF tokens per request.
- SameSite cookie upgrades beyond what is already set.
