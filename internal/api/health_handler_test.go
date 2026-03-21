package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler_AllUp(t *testing.T) {
	// nil db and redis = not_configured
	handler := HealthHandler(nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var status healthStatus
	_ = json.Unmarshal(rr.Body.Bytes(), &status)
	if status.Status != "ok" {
		t.Fatalf("expected ok status, got %s", status.Status)
	}
	if status.DB != "not_configured" {
		t.Fatalf("expected not_configured for DB, got %s", status.DB)
	}
	if status.Redis != "not_configured" {
		t.Fatalf("expected not_configured for Redis, got %s", status.Redis)
	}
}

func TestHealthHandler_JSONContentType(t *testing.T) {
	handler := HealthHandler(nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Fatalf("expected application/json; charset=utf-8, got %s", ct)
	}
}
