package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/repository"
)

// ---------------------------------------------------------------------------
// Handler-level interfaces
// ---------------------------------------------------------------------------

// DiscrepancyLister queries discrepancies with filters.
type DiscrepancyLister interface {
	FindByFilters(ctx context.Context, filters repository.DiscrepancyFilters) ([]domain.Discrepancy, error)
}

// DiscrepancyUpdater updates discrepancy status.
type DiscrepancyUpdater interface {
	UpdateStatus(ctx context.Context, id, status, resolvedBy, resolutionNote string) error
}

// ---------------------------------------------------------------------------
// Request type
// ---------------------------------------------------------------------------

type patchDiscrepancyRequest struct {
	Status         string `json:"status"`
	ResolvedBy     string `json:"resolved_by"`
	ResolutionNote string `json:"resolution_note"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// ListDiscrepanciesHandler returns an http.HandlerFunc that lists discrepancies with filters.
func ListDiscrepanciesHandler(lister DiscrepancyLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, perPage := parsePagination(r)
		offset := (page - 1) * perPage

		filters := repository.DiscrepancyFilters{
			Status:   r.URL.Query().Get("status"),
			Severity: r.URL.Query().Get("severity"),
			Limit:    perPage,
			Offset:   offset,
		}

		if v := r.URL.Query().Get("date_from"); v != "" {
			if t, err := time.Parse("2006-01-02", v); err == nil {
				filters.DateFrom = &t
			}
		}
		if v := r.URL.Query().Get("date_to"); v != "" {
			if t, err := time.Parse("2006-01-02", v); err == nil {
				filters.DateTo = &t
			}
		}

		discrepancies, err := lister.FindByFilters(r.Context(), filters)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, NewInternalError(err.Error()))
			return
		}

		RespondJSON(w, http.StatusOK, map[string]interface{}{
			"data":     discrepancies,
			"page":     page,
			"per_page": perPage,
		})
	}
}

// UpdateDiscrepancyHandler returns an http.HandlerFunc that updates a discrepancy's status.
func UpdateDiscrepancyHandler(updater DiscrepancyUpdater) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			RespondError(w, http.StatusBadRequest, NewValidationError("discrepancy id is required", nil))
			return
		}

		var req patchDiscrepancyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			RespondError(w, http.StatusBadRequest, NewValidationError("invalid JSON body", nil))
			return
		}

		if req.Status == "" {
			RespondError(w, http.StatusBadRequest, NewValidationError("status is required", nil))
			return
		}

		// Validate status value
		validStatuses := map[string]bool{
			domain.StatusOpen:          true,
			domain.StatusInvestigating: true,
			domain.StatusResolved:      true,
			domain.StatusIgnored:       true,
		}
		if !validStatuses[req.Status] {
			RespondError(w, http.StatusBadRequest, NewValidationError("invalid status value", nil))
			return
		}

		// Resolved requires a resolution_note
		if req.Status == domain.StatusResolved && req.ResolutionNote == "" {
			RespondError(w, http.StatusBadRequest, NewValidationError("resolution_note is required when resolving", nil))
			return
		}

		err := updater.UpdateStatus(r.Context(), id, req.Status, req.ResolvedBy, req.ResolutionNote)
		if err != nil {
			if err.Error() == "discrepancy "+id+" not found" {
				RespondError(w, http.StatusNotFound, NewNotFoundError("discrepancy not found"))
				return
			}
			RespondError(w, http.StatusInternalServerError, NewInternalError(err.Error()))
			return
		}

		RespondJSON(w, http.StatusOK, map[string]string{
			"id":     id,
			"status": req.Status,
		})
	}
}
