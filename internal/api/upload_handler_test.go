package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/adapter"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// ---------------------------------------------------------------------------
// Mock adapter
// ---------------------------------------------------------------------------

type mockAdapter struct {
	result *adapter.FetchResult
	err    error
}

func (m *mockAdapter) Name() string { return "mock" }

func (m *mockAdapter) FetchTransactions(_ context.Context, _ adapter.FetchRequest) (*adapter.FetchResult, error) {
	return nil, adapter.ErrNotSupported
}

func (m *mockAdapter) ParseFile(_ context.Context, _ []byte, _ string) (*adapter.FetchResult, error) {
	return m.result, m.err
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestUploadHandler_Success(t *testing.T) {
	ing := &mockIngester{result: domain.IngestResult{
		TransactionID: "tx-1",
		Status:        domain.IngestStatusCreated,
	}}
	adp := &mockAdapter{result: &adapter.FetchResult{
		Transactions: []domain.IngestRequest{validIngestRequest()},
		TotalRecords: 1,
	}}
	adapters := map[string]adapter.SourceAdapter{"mock": adp}

	handler := UploadHandler(ing, adapters)

	// Build multipart form
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("source_id", "src-1")
	fw, _ := mw.CreateFormFile("file", "test.csv")
	_, _ = fw.Write([]byte("fake,csv,data"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/files/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp uploadResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.RecordsTotal != 1 || resp.RecordsCreated != 1 {
		t.Fatalf("unexpected counts: total=%d created=%d", resp.RecordsTotal, resp.RecordsCreated)
	}
}

func TestUploadHandler_MissingSourceID(t *testing.T) {
	handler := UploadHandler(&mockIngester{}, nil)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "test.csv")
	_, _ = fw.Write([]byte("data"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing source_id, got %d", rr.Code)
	}
}

func TestUploadHandler_MissingFile(t *testing.T) {
	handler := UploadHandler(&mockIngester{}, nil)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("source_id", "src-1")
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing file, got %d", rr.Code)
	}
}

func TestUploadHandler_UnsupportedFormat(t *testing.T) {
	ing := &mockIngester{}
	adp := &mockAdapter{err: adapter.ErrNotSupported}
	adapters := map[string]adapter.SourceAdapter{"mock": adp}

	handler := UploadHandler(ing, adapters)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("source_id", "src-1")
	fw, _ := mw.CreateFormFile("file", "test.xyz")
	_, _ = fw.Write([]byte("unknown data"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported format, got %d", rr.Code)
	}
}
