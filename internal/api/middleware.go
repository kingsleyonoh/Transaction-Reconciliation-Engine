package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// contextKey is an unexported type for context keys in this package.
type contextKey string

// ContextKeyRequestID is the context key for the request ID.
const ContextKeyRequestID = contextKey("request_id")

// RequestID injects a UUID into the request context and sets the
// X-Request-ID response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.New().String()
		ctx := context.WithValue(r.Context(), ContextKeyRequestID, id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID extracts the request ID from a context, returning "" if absent.
func GetRequestID(ctx context.Context) string {
	if v, ok := ctx.Value(ContextKeyRequestID).(string); ok {
		return v
	}
	return ""
}

// APIKeyAuth returns middleware that validates the X-API-Key header
// using constant-time comparison against expectedKey.
func APIKeyAuth(expectedKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-API-Key")
			if key == "" {
				RespondError(w, http.StatusUnauthorized, NewUnauthorizedError("missing API key"))
				return
			}
			if subtle.ConstantTimeCompare([]byte(key), []byte(expectedKey)) != 1 {
				RespondError(w, http.StatusUnauthorized, NewUnauthorizedError("invalid API key"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (sw *statusWriter) WriteHeader(code int) {
	if !sw.wrote {
		sw.status = code
		sw.wrote = true
	}
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.wrote {
		sw.status = http.StatusOK
		sw.wrote = true
	}
	return sw.ResponseWriter.Write(b)
}

// RequestLogger logs each request's method, path, status, duration, and
// request ID using log/slog.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			dur := time.Since(start)

			logger.Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", dur.Milliseconds(),
				"request_id", GetRequestID(r.Context()),
			)
		})
	}
}

// rateBucket tracks token-bucket state for a single key.
type rateBucket struct {
	tokens    int
	lastReset time.Time
}

// RateLimiter returns middleware that enforces per-IP rate limiting using
// an in-memory token bucket. maxPerMinute is the number of requests allowed
// per minute per client IP.
func RateLimiter(maxPerMinute int) func(http.Handler) http.Handler {
	var mu sync.Mutex
	buckets := make(map[string]*rateBucket)
	now := func() time.Time { return time.Now() }

	return rateLimiterWithClock(maxPerMinute, &mu, buckets, now)
}

// rateLimiterWithClock is the testable core with injectable clock.
func rateLimiterWithClock(
	maxPerMinute int,
	mu *sync.Mutex,
	buckets map[string]*rateBucket,
	now func() time.Time,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			key := ip

			mu.Lock()
			b, ok := buckets[key]
			t := now()
			if !ok {
				b = &rateBucket{tokens: maxPerMinute, lastReset: t}
				buckets[key] = b
			}

			// Refill tokens if a minute has elapsed.
			elapsed := t.Sub(b.lastReset)
			if elapsed >= time.Minute {
				b.tokens = maxPerMinute
				b.lastReset = t
			}

			if b.tokens <= 0 {
				remaining := time.Minute - elapsed
				if remaining < 0 {
					remaining = 0
				}
				mu.Unlock()
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(remaining.Seconds())+1))
				RespondError(w, http.StatusTooManyRequests, NewRateLimitedError("rate limit exceeded"))
				return
			}

			b.tokens--
			mu.Unlock()
			next.ServeHTTP(w, r)
		})
	}
}
