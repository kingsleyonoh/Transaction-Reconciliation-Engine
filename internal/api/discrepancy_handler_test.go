package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/repository"
)

// ---------------------------------------------------------------------------
// Mock discrepancy lister
// ---------------------------------------------------------------------------

type mockDiscLister struct {
	discs []domain.Discrepancy
	err   error
}

func (m *mockDiscLister) FindByFilters(_ context.Context, _ repository.DiscrepancyFilters) ([]domain.Discrepancy, error) {
	return m.discs, m.err
}

// ---------------------------------------------------------------------------
// Mock discrepancy updater
// ---------------------------------------------------------------------------

type mockDiscUpdater struct {
	err error
}

func (m *mockDiscUpdater) UpdateStatus(_ context.Context, id, _, _, _ string) error {
	if m.err != nil {
		return m.err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tests — ListDiscrepanciesHandler
// ---------------------------------------------------------------------------

func TestListDiscrepanciesHandler_Success(t *testing.T) {
	lister := &mockDiscLister{discs: []domain.Discrepancy{
		{ID: "disc-1", Status: domain.StatusOpen},
		{ID: "disc-2", Status: domain.StatusOpen},
	}}

	handler := ListDiscrepanciesHandler(lister)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/discrepancies?status=open&page=1&per_page=10", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	data := resp["data"].([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 discrepancies, got %d", len(data))
	}
}

func TestListDiscrepanciesHandler_WithDateFilters(t *testing.T) {
	lister := &mockDiscLister{discs: []domain.Discrepancy{}}

	handler := ListDiscrepanciesHandler(lister)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/discrepancies?date_from=2025-01-01&date_to=2025-12-31", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Tests — UpdateDiscrepancyHandler
// ---------------------------------------------------------------------------

func TestUpdateDiscrepancyHandler_Success(t *testing.T) {
	updater := &mockDiscUpdater{}
	handler := UpdateDiscrepancyHandler(updater)

	body, _ := json.Marshal(patchDiscrepancyRequest{
		Status:         domain.StatusResolved,
		ResolvedBy:     "admin",
		ResolutionNote: "verified correct",
	})

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "disc-1")

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/discrepancies/disc-1", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUpdateDiscrepancyHandler_MissingStatus(t *testing.T) {
	updater := &mockDiscUpdater{}
	handler := UpdateDiscrepancyHandler(updater)

	body, _ := json.Marshal(patchDiscrepancyRequest{})

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "disc-1")

	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing status, got %d", rr.Code)
	}
}

func TestUpdateDiscrepancyHandler_ResolvedWithoutNote(t *testing.T) {
	updater := &mockDiscUpdater{}
	handler := UpdateDiscrepancyHandler(updater)

	body, _ := json.Marshal(patchDiscrepancyRequest{
		Status: domain.StatusResolved,
	})

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "disc-1")

	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for resolved without note, got %d", rr.Code)
	}
}

func TestUpdateDiscrepancyHandler_NotFound(t *testing.T) {
	updater := &mockDiscUpdater{err: fmt.Errorf("discrepancy disc-99 not found")}
	handler := UpdateDiscrepancyHandler(updater)

	body, _ := json.Marshal(patchDiscrepancyRequest{
		Status: domain.StatusInvestigating,
	})

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "disc-99")

	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestUpdateDiscrepancyHandler_InvalidStatus(t *testing.T) {
	updater := &mockDiscUpdater{}
	handler := UpdateDiscrepancyHandler(updater)

	body, _ := json.Marshal(patchDiscrepancyRequest{
		Status: "invalid_status",
	})

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "disc-1")

	req := httptest.NewRequest(http.MethodPatch, "/", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d", rr.Code)
	}
}
