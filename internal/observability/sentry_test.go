package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitSentry_EmptyDSN_Skips(t *testing.T) {
	err := InitSentry("")
	assert.NoError(t, err, "empty DSN should skip initialization without error")
}

func TestInitSentry_InvalidDSN_Error(t *testing.T) {
	err := InitSentry("not-a-valid-dsn")
	assert.Error(t, err, "invalid DSN should return an error")
}

func TestCaptureError_NilSafe(t *testing.T) {
	// CaptureError must not panic even if Sentry was never initialized
	require.NotPanics(t, func() {
		CaptureError(nil, nil)
	})
	require.NotPanics(t, func() {
		CaptureError(assert.AnError, map[string]string{"key": "val"})
	})
}

func TestRecoveryMiddleware_CatchesPanic(t *testing.T) {
	handler := RecoveryMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test explosion")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() {
		handler.ServeHTTP(rec, req)
	})
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
