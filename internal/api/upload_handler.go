package api

import (
	"io"
	"net/http"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/adapter"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// FileUploadIngester handles both file parsing and transaction ingestion.
type FileUploadIngester interface {
	TransactionIngester
}

// FileAdapter parses uploaded bank files.
type FileAdapter interface {
	ParseFile(r *http.Request) ([]domain.IngestRequest, string, error)
}

// ---------------------------------------------------------------------------
// Response type
// ---------------------------------------------------------------------------

type uploadResponse struct {
	IngestionLogID string `json:"ingestion_log_id,omitempty"`
	FileName       string `json:"file_name"`
	RecordsTotal   int    `json:"records_total"`
	RecordsCreated int    `json:"records_created"`
	RecordsSkipped int    `json:"records_skipped"`
	RecordsErrors  int    `json:"records_errors"`
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// UploadHandler returns an http.HandlerFunc that accepts multipart file uploads,
// delegates to the appropriate adapter for parsing, then ingests all transactions.
func UploadHandler(ingester TransactionIngester, adapters map[string]adapter.SourceAdapter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse multipart form (max 32MB)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			RespondError(w, http.StatusBadRequest, NewValidationError("invalid multipart form", nil))
			return
		}

		sourceID := r.FormValue("source_id")
		if sourceID == "" {
			RespondError(w, http.StatusBadRequest, NewValidationError("source_id is required", nil))
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			RespondError(w, http.StatusBadRequest, NewValidationError("file is required", nil))
			return
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, NewInternalError("failed to read uploaded file"))
			return
		}

		// Try each adapter until one succeeds
		var txns []domain.IngestRequest
		var parsed bool
		for _, a := range adapters {
			result, err := a.ParseFile(r.Context(), data, sourceID)
			if err != nil {
				continue // adapter doesn't support this format
			}
			txns = result.Transactions
			parsed = true
			break
		}

		if !parsed {
			RespondError(w, http.StatusBadRequest, NewValidationError("unsupported file format", nil))
			return
		}

		// Ingest all parsed transactions
		resp := uploadResponse{
			FileName:     header.Filename,
			RecordsTotal: len(txns),
		}

		ctx := r.Context()
		for _, txReq := range txns {
			result, err := ingester.Ingest(ctx, txReq)
			if err != nil {
				resp.RecordsErrors++
				continue
			}
			switch result.Status {
			case domain.IngestStatusCreated:
				resp.RecordsCreated++
			case domain.IngestStatusDuplicate:
				resp.RecordsSkipped++
			default:
				resp.RecordsErrors++
			}
		}

		RespondJSON(w, http.StatusOK, resp)
	}
}
