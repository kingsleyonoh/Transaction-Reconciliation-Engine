package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/adapter"
)

// Deps holds the dependencies required by the router and its handlers.
type Deps struct {
	DB       *sqlx.DB
	Redis    *redis.Client
	APIKey   string
	Logger   *slog.Logger
	Ingester TransactionIngester
	Adapters map[string]adapter.SourceAdapter
	Runner   ReconcileRunner
	RunRepo  RunFinder
	DiscRepo DiscrepancyLister
	DiscUpd  DiscrepancyUpdater
	ReportGen ReportGenerator
}

// NewRouter creates a fully configured Chi router with middleware stack
// and route registration.
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
	r.Get("/api/v1/health", HealthHandler(deps.DB, deps.Redis))

	// --- Authenticated routes ---
	r.Group(func(r chi.Router) {
		r.Use(APIKeyAuth(deps.APIKey))

		// Transaction ingestion
		ingestH := notImplemented
		batchH := notImplemented
		if deps.Ingester != nil {
			ingestH = IngestHandler(deps.Ingester)
			batchH = BatchIngestHandler(deps.Ingester)
		}
		r.With(RateLimiter(200)).Post("/api/v1/transactions/ingest", ingestH)
		r.With(RateLimiter(20)).Post("/api/v1/transactions/ingest/batch", batchH)

		// File upload
		uploadH := notImplemented
		if deps.Ingester != nil && deps.Adapters != nil {
			uploadH = UploadHandler(deps.Ingester, deps.Adapters)
		}
		r.With(RateLimiter(10)).Post("/api/v1/files/upload", uploadH)

		// Reconciliation
		reconcileH := notImplemented
		if deps.Runner != nil {
			reconcileH = ReconcileHandler(deps.Runner)
		}
		r.With(RateLimiter(5)).Post("/api/v1/reconcile", reconcileH)

		getRunH := notImplemented
		listRunsH := notImplemented
		if deps.RunRepo != nil {
			getRunH = GetRunHandler(deps.RunRepo)
			listRunsH = ListRunsHandler(deps.RunRepo)
		}
		r.With(RateLimiter(100)).Get("/api/v1/reconcile/{runID}", getRunH)
		r.With(RateLimiter(100)).Get("/api/v1/reconcile/history", listRunsH)

		// Discrepancies
		listDiscH := notImplemented
		if deps.DiscRepo != nil {
			listDiscH = ListDiscrepanciesHandler(deps.DiscRepo)
		}
		r.With(RateLimiter(100)).Get("/api/v1/discrepancies", listDiscH)

		updateDiscH := notImplemented
		if deps.DiscUpd != nil {
			updateDiscH = UpdateDiscrepancyHandler(deps.DiscUpd)
		}
		r.With(RateLimiter(50)).Patch("/api/v1/discrepancies/{id}", updateDiscH)

		// Reports
		reportGen := deps.ReportGen
		if reportGen == nil {
			reportGen = NewStaticReportGenerator()
		}
		r.With(RateLimiter(20)).Get("/api/v1/reports/settlement", SettlementReportHandler(reportGen))
		r.With(RateLimiter(20)).Get("/api/v1/reports/discrepancies", DiscrepancyReportHandler(reportGen))
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
