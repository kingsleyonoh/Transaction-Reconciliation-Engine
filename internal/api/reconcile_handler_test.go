package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// ---------------------------------------------------------------------------
// Mock reconciler
// ---------------------------------------------------------------------------

type mockReconciler struct {
	result domain.ReconcileResult
	err    error
}

func (m *mockReconciler) Reconcile(_ context.Context, _ domain.ReconcileRequest) (domain.ReconcileResult, error) {
	return m.result, m.err
}

// ---------------------------------------------------------------------------
// Mock run finder
// ---------------------------------------------------------------------------

type mockRunFinder struct {
	run  *domain.ReconciliationRun
	runs []domain.ReconciliationRun
	err  error
}

func (m *mockRunFinder) FindByID(_ context.Context, _ string) (*domain.ReconciliationRun, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.run, nil
}

func (m *mockRunFinder) List(_ context.Context, _, _ int) ([]domain.ReconciliationRun, error) {
	return m.runs, m.err
}

// ---------------------------------------------------------------------------
// Tests — ReconcileHandler
// ---------------------------------------------------------------------------

func TestReconcileHandler_Success(t *testing.T) {
	runner := &mockReconciler{result: domain.ReconcileResult{
		RunID:   "run-1",
		Matched: 10,
	}}

	handler := ReconcileHandler(runner)
	body, _ := json.Marshal(domain.ReconcileRequest{
		DateFrom: time.Now().Add(-24 * time.Hour),
		DateTo:   time.Now(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reconcile", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestReconcileHandler_MissingDates(t *testing.T) {
	handler := ReconcileHandler(&mockReconciler{})
	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reconcile", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Tests — GetRunHandler
// ---------------------------------------------------------------------------

func TestGetRunHandler_Success(t *testing.T) {
	finder := &mockRunFinder{run: &domain.ReconciliationRun{
		ID:     "run-1",
		Status: domain.RunStatusCompleted,
	}}

	handler := GetRunHandler(finder)

	// Set up chi URL param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runID", "run-1")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reconcile/run-1", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetRunHandler_NotFound(t *testing.T) {
	finder := &mockRunFinder{err: sql.ErrNoRows}

	handler := GetRunHandler(finder)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runID", "missing")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reconcile/missing", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Tests — ListRunsHandler
// ---------------------------------------------------------------------------

func TestListRunsHandler_Success(t *testing.T) {
	finder := &mockRunFinder{runs: []domain.ReconciliationRun{
		{ID: "run-1"},
		{ID: "run-2"},
	}}

	handler := ListRunsHandler(finder)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reconcile/history?page=1&per_page=10", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	data := resp["data"].([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(data))
	}
}
