package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func newTestRouter() http.Handler {
	return NewRouter(Deps{
		DB:     nil, // nil is handled gracefully by healthHandler
		Redis:  nil,
		APIKey: "test-key-123",
		Logger: zerolog.Nop(), // silent logger for tests
	})
}

func TestRouter_HealthEndpoint_NoAuth(t *testing.T) {
	router := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Health endpoint is public — should respond without X-API-Key.
	assert.Equal(t, http.StatusOK, w.Code)

	var status healthStatus
	err := json.NewDecoder(w.Body).Decode(&status)
	assert.NoError(t, err)
	assert.Equal(t, "ok", status.Status)
	assert.Equal(t, "not_configured", status.DB)
	assert.Equal(t, "not_configured", status.Redis)

	// Must have X-Request-ID from global middleware.
	assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
}

func TestRouter_AuthenticatedRoute_RejectsWithoutKey(t *testing.T) {
	router := newTestRouter()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reconcile", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var envelope struct{ Error APIError }
	err := json.NewDecoder(w.Body).Decode(&envelope)
	assert.NoError(t, err)
	assert.Equal(t, CodeUnauthorized, envelope.Error.Code)
}

func TestRouter_AllEndpointsRegistered(t *testing.T) {
	router := newTestRouter()
	apiKey := "test-key-123"

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/transactions/ingest"},
		{http.MethodPost, "/api/v1/transactions/ingest/batch"},
		{http.MethodPost, "/api/v1/files/upload"},
		{http.MethodPost, "/api/v1/reconcile"},
		{http.MethodGet, "/api/v1/reconcile/some-run-id"},
		{http.MethodGet, "/api/v1/reconcile/history"},
		{http.MethodGet, "/api/v1/discrepancies"},
		{http.MethodPatch, "/api/v1/discrepancies/some-id"},
		{http.MethodGet, "/api/v1/reports/settlement"},
		{http.MethodGet, "/api/v1/reports/discrepancies"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			req.Header.Set("X-API-Key", apiKey)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Should NOT get 404 (route not found) or 405 (method not allowed).
			// Some handlers return 501 (placeholder) when deps are nil,
			// others (like reports which always get a default ReportGenerator)
			// return 400 for missing query params. Either is fine — we just
			// want to prove the route is registered.
			assert.NotEqual(t, http.StatusNotFound, w.Code,
				"route %s %s should be registered (got 404)", ep.method, ep.path)
			assert.NotEqual(t, http.StatusMethodNotAllowed, w.Code,
				"route %s %s should be registered (got 405)", ep.method, ep.path)
		})
	}
}
