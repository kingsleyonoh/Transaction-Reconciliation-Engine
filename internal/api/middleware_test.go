package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// --- RequestID middleware tests ---

func TestRequestID_SetsHeaderAndContext(t *testing.T) {
	var gotID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	headerID := w.Header().Get("X-Request-ID")
	assert.NotEmpty(t, headerID, "X-Request-ID header must be set")
	assert.Equal(t, headerID, gotID, "header and context ID must match")
	assert.Len(t, headerID, 36, "must be a UUID")
}

// --- APIKeyAuth middleware tests ---

func TestAPIKeyAuth_ValidKey(t *testing.T) {
	handler := APIKeyAuth("test-secret-key")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-API-Key", "test-secret-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAPIKeyAuth_MissingKey(t *testing.T) {
	handler := APIKeyAuth("test-secret-key")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var envelope struct{ Error APIError }
	err := json.NewDecoder(w.Body).Decode(&envelope)
	assert.NoError(t, err)
	assert.Equal(t, CodeUnauthorized, envelope.Error.Code)
	assert.Contains(t, envelope.Error.Message, "missing")
}

func TestAPIKeyAuth_WrongKey(t *testing.T) {
	handler := APIKeyAuth("test-secret-key")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var envelope struct{ Error APIError }
	err := json.NewDecoder(w.Body).Decode(&envelope)
	assert.NoError(t, err)
	assert.Equal(t, CodeUnauthorized, envelope.Error.Code)
	assert.Contains(t, envelope.Error.Message, "invalid")
}

// --- RateLimiter middleware tests ---

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	var mu sync.Mutex
	buckets := make(map[string]*rateBucket)
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	mw := rateLimiterWithClock(3, &mu, buckets, now)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "request %d should succeed", i+1)
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	var mu sync.Mutex
	buckets := make(map[string]*rateBucket)
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	mw := rateLimiterWithClock(2, &mu, buckets, now)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First 2 should pass.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// Third should be rate limited.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))

	var envelope struct{ Error APIError }
	err := json.NewDecoder(w.Body).Decode(&envelope)
	assert.NoError(t, err)
	assert.Equal(t, CodeRateLimited, envelope.Error.Code)
}

func TestRateLimiter_RefillsAfterMinute(t *testing.T) {
	var mu sync.Mutex
	buckets := make(map[string]*rateBucket)
	currentTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := func() time.Time { return currentTime }

	mw := rateLimiterWithClock(1, &mu, buckets, now)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Use the single token.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Should be rate limited now.
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	// Advance time by 1 minute — bucket should refill.
	currentTime = currentTime.Add(time.Minute)
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// --- RequestLogger middleware tests ---

func TestRequestLogger_LogsRequestFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	handler := RequestLogger(logger)(inner)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reconcile", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var logEntry map[string]interface{}
	err := json.NewDecoder(&buf).Decode(&logEntry)
	assert.NoError(t, err)
	assert.Equal(t, "POST", logEntry["method"])
	assert.Equal(t, "/api/v1/reconcile", logEntry["path"])
	assert.Equal(t, float64(201), logEntry["status"])
	assert.Contains(t, logEntry, "duration_ms")
}
