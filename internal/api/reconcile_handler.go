package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// ---------------------------------------------------------------------------
// Handler-level interfaces
// ---------------------------------------------------------------------------

// ReconcileRunner triggers reconciliation runs.
type ReconcileRunner interface {
	Reconcile(ctx context.Context, req domain.ReconcileRequest) (domain.ReconcileResult, error)
}

// RunFinder retrieves reconciliation run data.
type RunFinder interface {
	FindByID(ctx context.Context, id string) (*domain.ReconciliationRun, error)
	List(ctx context.Context, limit, offset int) ([]domain.ReconciliationRun, error)
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// ReconcileHandler returns an http.HandlerFunc that triggers a reconciliation run.
func ReconcileHandler(runner ReconcileRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req domain.ReconcileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			RespondError(w, http.StatusBadRequest, NewValidationError("invalid JSON body", nil))
			return
		}

		if req.DateFrom.IsZero() || req.DateTo.IsZero() {
			RespondError(w, http.StatusBadRequest, NewValidationError("date_from and date_to are required", nil))
			return
		}

		result, err := runner.Reconcile(r.Context(), req)
		if err != nil {
			// Check for lock contention
			if errors.Is(err, context.DeadlineExceeded) {
				RespondError(w, http.StatusConflict, NewReconciliationLockedError("reconciliation already in progress"))
				return
			}
			RespondError(w, http.StatusInternalServerError, NewInternalError(err.Error()))
			return
		}

		RespondJSON(w, http.StatusCreated, result)
	}
}

// GetRunHandler returns an http.HandlerFunc that retrieves a reconciliation run by ID.
func GetRunHandler(finder RunFinder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := chi.URLParam(r, "runID")
		if runID == "" {
			RespondError(w, http.StatusBadRequest, NewValidationError("run ID is required", nil))
			return
		}

		run, err := finder.FindByID(r.Context(), runID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				RespondError(w, http.StatusNotFound, NewNotFoundError("reconciliation run not found"))
				return
			}
			RespondError(w, http.StatusInternalServerError, NewInternalError(err.Error()))
			return
		}

		RespondJSON(w, http.StatusOK, run)
	}
}

// ListRunsHandler returns an http.HandlerFunc that lists reconciliation runs with pagination.
func ListRunsHandler(finder RunFinder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, perPage := parsePagination(r)
		offset := (page - 1) * perPage

		runs, err := finder.List(r.Context(), perPage, offset)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, NewInternalError(err.Error()))
			return
		}

		RespondJSON(w, http.StatusOK, map[string]interface{}{
			"data":     runs,
			"page":     page,
			"per_page": perPage,
		})
	}
}

// parsePagination extracts page and per_page from query params with defaults.
func parsePagination(r *http.Request) (int, int) {
	page := 1
	perPage := 25

	if v := r.URL.Query().Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			page = p
		}
	}
	if v := r.URL.Query().Get("per_page"); v != "" {
		if pp, err := strconv.Atoi(v); err == nil && pp > 0 {
			perPage = pp
		}
	}

	if perPage > 100 {
		perPage = 100
	}
	return page, perPage
}
