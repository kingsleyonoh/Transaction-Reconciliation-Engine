package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// ---------------------------------------------------------------------------
// Handler-level interfaces (for testability — handlers don't import engine)
// ---------------------------------------------------------------------------

// TransactionIngester is satisfied by *engine.Ingester.
type TransactionIngester interface {
	Ingest(ctx context.Context, req domain.IngestRequest) (domain.IngestResult, error)
}

// TransactionBatchInserter is satisfied by repository.TransactionRepository.
type TransactionBatchInserter interface {
	InsertBatch(ctx context.Context, txs []domain.Transaction) (int, error)
}

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

const maxBatchSize = 1000

type batchIngestRequest struct {
	Transactions []domain.IngestRequest `json:"transactions"`
}

type batchIngestResponse struct {
	Results []domain.IngestResult `json:"results"`
	Total   int                  `json:"total"`
	Created int                  `json:"created"`
	Skipped int                  `json:"skipped"`
	Errors  int                  `json:"errors"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// IngestHandler returns an http.HandlerFunc that ingests a single transaction.
func IngestHandler(ingester TransactionIngester) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req domain.IngestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			RespondError(w, http.StatusBadRequest, NewValidationError("invalid JSON body", nil))
			return
		}

		result, err := ingester.Ingest(r.Context(), req)
		if err != nil {
			// Validation errors from the engine
			RespondError(w, http.StatusUnprocessableEntity, NewValidationError(err.Error(), nil))
			return
		}

		status := http.StatusCreated
		if result.Status == domain.IngestStatusDuplicate {
			status = http.StatusOK
		}
		RespondJSON(w, status, result)
	}
}

// BatchIngestHandler returns an http.HandlerFunc that ingests up to 1000 transactions.
func BatchIngestHandler(ingester TransactionIngester) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req batchIngestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			RespondError(w, http.StatusBadRequest, NewValidationError("invalid JSON body", nil))
			return
		}

		if len(req.Transactions) == 0 {
			RespondError(w, http.StatusBadRequest, NewValidationError("transactions array is required and must not be empty", nil))
			return
		}
		if len(req.Transactions) > maxBatchSize {
			RespondError(w, http.StatusBadRequest, NewValidationError("batch exceeds maximum of 1000 transactions", nil))
			return
		}

		resp := batchIngestResponse{
			Results: make([]domain.IngestResult, 0, len(req.Transactions)),
			Total:   len(req.Transactions),
		}

		ctx := r.Context()
		for _, txReq := range req.Transactions {
			result, err := ingester.Ingest(ctx, txReq)
			if err != nil {
				resp.Errors++
				resp.Results = append(resp.Results, domain.IngestResult{Status: domain.IngestStatusError})
				continue
			}
			switch result.Status {
			case domain.IngestStatusCreated:
				resp.Created++
			case domain.IngestStatusDuplicate:
				resp.Skipped++
			default:
				resp.Errors++
			}
			resp.Results = append(resp.Results, result)
		}

		RespondJSON(w, http.StatusOK, resp)
	}
}

// ---------------------------------------------------------------------------
// Validation helper used by tests — exported so tests can reuse
// ---------------------------------------------------------------------------

func validIngestRequest() domain.IngestRequest {
	return domain.IngestRequest{
		SourceID:   "src-1",
		ExternalID: "ext-1",
		Amount:     4999,
		Currency:   "USD",
		Direction:  "credit",
		OccurredAt: time.Now().Add(-1 * time.Hour),
	}
}
