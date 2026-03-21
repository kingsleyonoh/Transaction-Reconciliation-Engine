package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// ---------------------------------------------------------------------------
// Mock ingester
// ---------------------------------------------------------------------------

type mockIngester struct {
	result domain.IngestResult
	err    error
	calls  int
}

func (m *mockIngester) Ingest(_ context.Context, _ domain.IngestRequest) (domain.IngestResult, error) {
	m.calls++
	return m.result, m.err
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestIngestHandler_Success(t *testing.T) {
	ing := &mockIngester{result: domain.IngestResult{
		TransactionID: "tx-1",
		Status:        domain.IngestStatusCreated,
	}}

	handler := IngestHandler(ing)
	body, _ := json.Marshal(validIngestRequest())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rr.Code)
	}

	var result domain.IngestResult
	_ = json.Unmarshal(rr.Body.Bytes(), &result)
	if result.TransactionID != "tx-1" {
		t.Fatalf("expected tx-1, got %s", result.TransactionID)
	}
}

func TestIngestHandler_Duplicate(t *testing.T) {
	ing := &mockIngester{result: domain.IngestResult{
		TransactionID: "tx-1",
		Status:        domain.IngestStatusDuplicate,
	}}

	handler := IngestHandler(ing)
	body, _ := json.Marshal(validIngestRequest())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/ingest", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for duplicate, got %d", rr.Code)
	}
}

func TestIngestHandler_InvalidJSON(t *testing.T) {
	ing := &mockIngester{}
	handler := IngestHandler(ing)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/ingest", bytes.NewReader([]byte("not json")))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestIngestHandler_ValidationError(t *testing.T) {
	ing := &mockIngester{
		result: domain.IngestResult{Status: domain.IngestStatusError},
		err:    &validationErr{msg: "source_id is required"},
	}

	handler := IngestHandler(ing)
	body, _ := json.Marshal(domain.IngestRequest{}) // missing fields
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/ingest", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rr.Code)
	}
}

func TestBatchIngestHandler_Success(t *testing.T) {
	ing := &mockIngester{result: domain.IngestResult{
		TransactionID: "tx-1",
		Status:        domain.IngestStatusCreated,
	}}

	handler := BatchIngestHandler(ing)
	batch := batchIngestRequest{
		Transactions: []domain.IngestRequest{
			validIngestRequest(),
			validIngestRequest(),
		},
	}
	body, _ := json.Marshal(batch)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/ingest/batch", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ing.calls != 2 {
		t.Fatalf("expected 2 Ingest calls, got %d", ing.calls)
	}

	var resp batchIngestResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Created != 2 {
		t.Fatalf("expected 2 created, got %d", resp.Created)
	}
}

func TestBatchIngestHandler_EmptyArray(t *testing.T) {
	handler := BatchIngestHandler(&mockIngester{})
	batch := batchIngestRequest{Transactions: []domain.IngestRequest{}}
	body, _ := json.Marshal(batch)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty batch, got %d", rr.Code)
	}
}

func TestBatchIngestHandler_ExceedsMax(t *testing.T) {
	handler := BatchIngestHandler(&mockIngester{})
	txns := make([]domain.IngestRequest, 1001)
	for i := range txns {
		txns[i] = validIngestRequest()
		txns[i].ExternalID = time.Now().String() // varied
	}
	batch := batchIngestRequest{Transactions: txns}
	body, _ := json.Marshal(batch)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized batch, got %d", rr.Code)
	}
}

// validationErr is a simple error type for test assertions.
type validationErr struct{ msg string }

func (e *validationErr) Error() string { return e.msg }
