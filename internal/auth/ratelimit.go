package auth

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// attemptTracker holds timestamps of failed login attempts for a single IP.
type attemptTracker struct {
	attempts []time.Time
}

// RateLimiter is a per-IP failed-attempt tracker.
// Create with NewRateLimiter; close with Close to stop the cleanup goroutine.
type RateLimiter struct {
	mu          sync.Mutex
	trackers    map[string]*attemptTracker
	maxAttempts int
	window      time.Duration
	done        chan struct{}
}

// NewRateLimiter creates a limiter: maxAttempts failures within windowSecs seconds triggers 429.
func NewRateLimiter(maxAttempts, windowSecs int) *RateLimiter {
	rl := &RateLimiter{
		trackers:    make(map[string]*attemptTracker),
		maxAttempts: maxAttempts,
		window:      time.Duration(windowSecs) * time.Second,
		done:        make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// cleanupLoop periodically removes expired entries from the tracker map.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.window)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for ip, tracker := range rl.trackers {
				tracker.attempts = filterExpired(tracker.attempts, now, rl.window)
				if len(tracker.attempts) == 0 {
					delete(rl.trackers, ip)
				}
			}
			rl.mu.Unlock()
		case <-rl.done:
			return
		}
	}
}

// Close stops the background cleanup goroutine.
func (rl *RateLimiter) Close() {
	close(rl.done)
}

// Middleware returns an http.Handler middleware.
// It tracks failed attempts by IP. If the wrapped handler writes a 401 status,
// the attempt is counted. A successful response (2xx/3xx) resets the counter for that IP.
// If the limit is exceeded, the middleware short-circuits with 429 and a
// Retry-After header (seconds until the oldest attempt expires).
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)

		rl.mu.Lock()
		tracker := rl.trackers[ip]
		if tracker == nil {
			tracker = &attemptTracker{}
			rl.trackers[ip] = tracker
		}
		// Filter out expired attempts before checking
		tracker.attempts = filterExpired(tracker.attempts, time.Now(), rl.window)

		if len(tracker.attempts) >= rl.maxAttempts {
			// Compute Retry-After: seconds until oldest attempt expires
			retryAfter := int(math.Ceil(tracker.attempts[0].Add(rl.window).Sub(time.Now()).Seconds()))
			if retryAfter < 1 {
				retryAfter = 1
			}
			rl.mu.Unlock()
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			http.Error(w, `{"error":"too many requests"}`, http.StatusTooManyRequests)
			return
		}
		rl.mu.Unlock()

		// Wrap the ResponseWriter to capture the status code
		rec := &responseRecorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r)

		rl.mu.Lock()
		defer rl.mu.Unlock()
		// Re-fetch tracker in case it was cleaned up
		tracker = rl.trackers[ip]
		if tracker == nil {
			tracker = &attemptTracker{}
			rl.trackers[ip] = tracker
		}

		if rec.code == http.StatusUnauthorized {
			tracker.attempts = append(tracker.attempts, time.Now())
		} else if rec.code < 400 {
			// Successful response — reset the counter
			tracker.attempts = nil
			if len(tracker.attempts) == 0 {
				delete(rl.trackers, ip)
			}
		}
		// Other 4xx (e.g. 400, 403) are not counted
	})
}

// responseRecorder captures the HTTP status code written by a handler.
type responseRecorder struct {
	http.ResponseWriter
	code int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

// filterExpired removes timestamps older than window from the slice.
func filterExpired(attempts []time.Time, now time.Time, window time.Duration) []time.Time {
	cutoff := now.Add(-window)
	i := 0
	for i < len(attempts) && attempts[i].Before(cutoff) {
		i++
	}
	return attempts[i:]
}

// extractIP returns the client IP from the request's RemoteAddr.
func extractIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// Fallback: use RemoteAddr as-is if it has no port
		return r.RemoteAddr
	}
	return host
}
