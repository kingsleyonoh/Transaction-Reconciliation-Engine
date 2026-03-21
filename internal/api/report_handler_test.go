package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

func TestSettlementReportHandler_JSON(t *testing.T) {
	gen := NewStaticReportGenerator()
	handler := SettlementReportHandler(gen)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/settlement?date_from=2025-01-01&date_to=2025-12-31", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var report domain.Report
	_ = json.Unmarshal(rr.Body.Bytes(), &report)
	if report.Summary.MatchRate != 0 {
		t.Fatalf("expected 0 match rate from stub, got %f", report.Summary.MatchRate)
	}
}

func TestSettlementReportHandler_CSV(t *testing.T) {
	gen := NewStaticReportGenerator()
	handler := SettlementReportHandler(gen)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/settlement?date_from=2025-01-01&date_to=2025-12-31&format=csv", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/csv" {
		t.Fatalf("expected text/csv content type, got %s", ct)
	}
}

func TestSettlementReportHandler_MissingDates(t *testing.T) {
	handler := SettlementReportHandler(NewStaticReportGenerator())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/settlement", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDiscrepancyReportHandler_JSON(t *testing.T) {
	handler := DiscrepancyReportHandler(NewStaticReportGenerator())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/discrepancies?date_from=2025-01-01&date_to=2025-12-31", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestDiscrepancyReportHandler_CSV(t *testing.T) {
	handler := DiscrepancyReportHandler(NewStaticReportGenerator())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/discrepancies?date_from=2025-01-01&date_to=2025-12-31&format=csv", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	cd := rr.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "discrepancy_report.csv") {
		t.Fatalf("expected discrepancy_report.csv in Content-Disposition, got %s", cd)
	}
}
