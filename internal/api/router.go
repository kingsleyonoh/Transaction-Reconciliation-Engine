package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
)

// Deps holds the dependencies required by the router and its handlers.
type Deps struct {
	DB     *sqlx.DB
	Redis  *redis.Client
	APIKey string
	Logger *slog.Logger
}

// NewRouter creates a fully configured Chi router with middleware stack
// and route registration. Endpoint handlers are stubs (501) — they will
// be implemented in subsequent tasks.
func NewRouter(deps Deps) chi.Router {
	r := chi.NewRouter()

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// --- Global middleware (applied to every request) ---
	r.Use(RequestID)
	r.Use(RequestLogger(logger))
	r.Use(middleware.Recoverer)

	// --- Public routes (no auth) ---
	r.Get("/api/v1/health", healthHandler(deps.DB, deps.Redis))

	// --- Authenticated routes ---
	r.Group(func(r chi.Router) {
		r.Use(APIKeyAuth(deps.APIKey))

		// Transaction ingestion
		r.With(RateLimiter(200)).Post("/api/v1/transactions/ingest", notImplemented)
		r.With(RateLimiter(20)).Post("/api/v1/transactions/ingest/batch", notImplemented)

		// File upload
		r.With(RateLimiter(10)).Post("/api/v1/files/upload", notImplemented)

		// Reconciliation
		r.With(RateLimiter(5)).Post("/api/v1/reconcile", notImplemented)
		r.With(RateLimiter(100)).Get("/api/v1/reconcile/{runID}", notImplemented)
		r.With(RateLimiter(100)).Get("/api/v1/reconcile/history", notImplemented)

		// Discrepancies
		r.With(RateLimiter(100)).Get("/api/v1/discrepancies", notImplemented)
		r.With(RateLimiter(50)).Patch("/api/v1/discrepancies/{id}", notImplemented)

		// Reports
		r.With(RateLimiter(20)).Get("/api/v1/reports/settlement", notImplemented)
		r.With(RateLimiter(20)).Get("/api/v1/reports/discrepancies", notImplemented)
	})

	return r
}

// notImplemented is a placeholder handler for routes that haven't been
// implemented yet. Returns 501 with a structured error.
func notImplemented(w http.ResponseWriter, _ *http.Request) {
	RespondError(w, http.StatusNotImplemented, APIError{
		Code:    CodeNotImplemented,
		Message: "endpoint not implemented",
	})
}

// healthStatus is the response body for the health endpoint.
type healthStatus struct {
	Status string `json:"status"`
	DB     string `json:"db"`
	Redis  string `json:"redis"`
}

// healthHandler returns an http.HandlerFunc that checks database and
// Redis connectivity, reporting individual and overall status.
func healthHandler(db *sqlx.DB, rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		status := healthStatus{
			Status: "ok",
			DB:     "ok",
			Redis:  "ok",
		}

		// DB check: SELECT 1 with 2s timeout.
		if db != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := db.PingContext(ctx); err != nil {
				status.DB = "error"
				status.Status = "degraded"
			}
		} else {
			status.DB = "not_configured"
		}

		// Redis check: PING with 1s timeout.
		if rdb != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			if err := rdb.Ping(ctx).Err(); err != nil {
				status.Redis = "error"
				status.Status = "degraded"
			}
		} else {
			status.Redis = "not_configured"
		}

		httpStatus := http.StatusOK
		if status.Status != "ok" {
			httpStatus = http.StatusServiceUnavailable
		}
		RespondJSON(w, httpStatus, status)
	}
}
