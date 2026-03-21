package observability

import (
	"fmt"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
)

// sentryInitialized tracks whether Sentry was successfully initialized.
var sentryInitialized bool

// InitSentry configures the Sentry SDK. If dsn is empty, initialization is
// skipped silently (useful in dev environments).
func InitSentry(dsn string) error {
	if dsn == "" {
		sentryInitialized = false
		return nil
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		TracesSampleRate: 0.2,
	})
	if err != nil {
		return fmt.Errorf("sentry init: %w", err)
	}
	sentryInitialized = true
	return nil
}

// CaptureError sends an error event to Sentry with optional string tags.
// Safe to call even if Sentry was not initialized.
func CaptureError(err error, tags map[string]string) {
	if err == nil || !sentryInitialized {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		for k, v := range tags {
			scope.SetTag(k, v)
		}
		sentry.CaptureException(err)
	})
}

// Flush drains the Sentry event buffer. Call before application exit.
func Flush() {
	if sentryInitialized {
		sentry.Flush(2 * time.Second)
	}
}

// RecoveryMiddleware catches panics, returns HTTP 500, and reports to Sentry.
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				if sentryInitialized {
					sentry.CurrentHub().Recover(rv)
				}
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
