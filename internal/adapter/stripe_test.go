package adapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// stripeFixtureDir returns the absolute path to the stripe fixture directory.
func stripeFixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "tests", "fixtures", "stripe")
}

// loadStripeFixture reads a fixture file from the stripe directory.
func loadStripeFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stripeFixtureDir(t), name))
	if err != nil {
		t.Fatalf("failed to load stripe fixture %s: %v", name, err)
	}
	return data
}

// --- StripeAdapter basic tests ---

func TestStripeAdapter_Name(t *testing.T) {
	a := NewStripeAdapter("sk_test_fake", "https://api.stripe.com", nil)
	if a.Name() != "stripe" {
		t.Errorf("expected name %q, got %q", "stripe", a.Name())
	}
}

func TestStripeAdapter_ParseFile_ReturnsNotSupported(t *testing.T) {
	a := NewStripeAdapter("sk_test_fake", "https://api.stripe.com", nil)
	_, err := a.ParseFile(context.Background(), []byte("data"), "src-1")
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("expected ErrNotSupported, got %v", err)
	}
}

// --- FetchTransactions tests ---

func TestStripeAdapter_FetchTransactions_Pagination(t *testing.T) {
	page1 := loadStripeFixture(t, "balance_transactions_page1.json")
	page2 := loadStripeFixture(t, "balance_transactions_page2.json")

	var requestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		// Verify auth header.
		auth := r.Header.Get("Authorization")
		if auth != "Bearer sk_test_pagination" {
			t.Errorf("expected Bearer auth, got %q", auth)
		}

		// Return page based on starting_after parameter.
		startingAfter := r.URL.Query().Get("starting_after")
		w.Header().Set("Content-Type", "application/json")
		if startingAfter == "" {
			w.Write(page1)
		} else {
			w.Write(page2)
		}
	}))
	defer srv.Close()

	a := NewStripeAdapter("sk_test_pagination", srv.URL, srv.Client())
	req := FetchRequest{
		SourceID: "src-stripe-1",
		DateFrom: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	result, err := a.FetchTransactions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// page1 has 3 txns, page2 has 2 txns = 5 total.
	if result.TotalRecords != 5 {
		t.Errorf("expected 5 total records, got %d", result.TotalRecords)
	}
	if len(result.Transactions) != 5 {
		t.Fatalf("expected 5 transactions, got %d", len(result.Transactions))
	}

	// Verify 2 API requests were made.
	if atomic.LoadInt32(&requestCount) != 2 {
		t.Errorf("expected 2 API requests, got %d", requestCount)
	}

	// All transactions should have the correct source ID.
	for i, tx := range result.Transactions {
		if tx.SourceID != "src-stripe-1" {
			t.Errorf("tx[%d] SourceID = %q, want %q", i, tx.SourceID, "src-stripe-1")
		}
	}
}

func TestStripeAdapter_FetchTransactions_EmptyResult(t *testing.T) {
	emptyResp := loadStripeFixture(t, "balance_transactions_empty.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(emptyResp)
	}))
	defer srv.Close()

	a := NewStripeAdapter("sk_test_empty", srv.URL, srv.Client())
	req := FetchRequest{
		SourceID: "src-stripe-2",
		DateFrom: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	result, err := a.FetchTransactions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalRecords != 0 {
		t.Errorf("expected 0 total records, got %d", result.TotalRecords)
	}
	if len(result.Transactions) != 0 {
		t.Errorf("expected 0 transactions, got %d", len(result.Transactions))
	}
}

func TestStripeAdapter_FetchTransactions_RateLimit429(t *testing.T) {
	error429 := loadStripeFixture(t, "error_429.json")

	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)

		w.Header().Set("Content-Type", "application/json")

		// First call returns 429, second call succeeds.
		if count == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write(error429)
			return
		}

		// Return page1 with has_more=false for simplicity — override fixture.
		// We need a response with has_more: false to end pagination.
		resp := []byte(`{
			"object": "list",
			"data": [
				{
					"id": "txn_1OqR2uJ9aQkP4LpW",
					"object": "balance_transaction",
					"amount": 15000,
					"currency": "usd",
					"description": "Payment from Acme Corp",
					"fee": 465,
					"net": 14535,
					"type": "charge",
					"status": "available",
					"created": 1709510400,
					"available_on": 1709596800,
					"source": "ch_3OqR2uJ9aQkP4LpW0xFGHijk"
				}
			],
			"has_more": false,
			"url": "/v1/balance_transactions"
		}`)
		w.Write(resp)
	}))
	defer srv.Close()

	a := NewStripeAdapter("sk_test_ratelimit", srv.URL, srv.Client())
	req := FetchRequest{
		SourceID: "src-stripe-3",
		DateFrom: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	result, err := a.FetchTransactions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error after 429 retry: %v", err)
	}

	// Should have retried and gotten 1 transaction.
	if result.TotalRecords != 1 {
		t.Errorf("expected 1 total record after retry, got %d", result.TotalRecords)
	}

	// Should have made 2 calls total (1 × 429 + 1 × success).
	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("expected 2 API calls (429 + success), got %d", callCount)
	}
}

func TestStripeAdapter_FetchTransactions_Unauthorized401(t *testing.T) {
	error401 := loadStripeFixture(t, "error_401.json")

	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write(error401)
	}))
	defer srv.Close()

	a := NewStripeAdapter("sk_test_expired", srv.URL, srv.Client())
	req := FetchRequest{
		SourceID: "src-stripe-4",
		DateFrom: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	_, err := a.FetchTransactions(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}

	// Error should mention 401 or authentication.
	errMsg := err.Error()
	if !contains(errMsg, "401") && !contains(errMsg, "authentication") && !contains(errMsg, "unauthorized") {
		t.Errorf("error should mention 401/auth, got: %s", errMsg)
	}

	// Must NOT retry on 401 — exactly 1 call.
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected exactly 1 API call (no retry on 401), got %d", callCount)
	}
}

func TestStripeAdapter_FetchTransactions_FieldMapping(t *testing.T) {
	page1 := loadStripeFixture(t, "balance_transactions_page1.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		startingAfter := r.URL.Query().Get("starting_after")
		if startingAfter == "" {
			w.Write(page1)
		} else {
			// Return empty to stop pagination.
			w.Write([]byte(`{"object":"list","data":[],"has_more":false,"url":"/v1/balance_transactions"}`))
		}
	}))
	defer srv.Close()

	a := NewStripeAdapter("sk_test_mapping", srv.URL, srv.Client())
	req := FetchRequest{
		SourceID: "src-stripe-5",
		DateFrom: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	result, err := a.FetchTransactions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Transactions) < 3 {
		t.Fatalf("expected at least 3 transactions from page1, got %d", len(result.Transactions))
	}

	// --- Verify first transaction (charge, USD $150.00) ---
	tx0 := result.Transactions[0]

	// ExternalID = Stripe txn ID.
	if tx0.ExternalID != "txn_1OqR2uJ9aQkP4LpW" {
		t.Errorf("tx[0] ExternalID = %q, want %q", tx0.ExternalID, "txn_1OqR2uJ9aQkP4LpW")
	}

	// Amount already in cents from Stripe.
	if tx0.Amount != 15000 {
		t.Errorf("tx[0] Amount = %d, want %d", tx0.Amount, 15000)
	}

	// Currency uppercased.
	if tx0.Currency != "USD" {
		t.Errorf("tx[0] Currency = %q, want %q", tx0.Currency, "USD")
	}

	// Charge → credit.
	if tx0.Direction != domain.DirectionCredit {
		t.Errorf("tx[0] Direction = %q, want %q", tx0.Direction, domain.DirectionCredit)
	}

	// Description preserved.
	if tx0.Description != "Payment from Acme Corp" {
		t.Errorf("tx[0] Description = %q, want %q", tx0.Description, "Payment from Acme Corp")
	}

	// OccurredAt from unix timestamp 1709510400.
	expectedTime := time.Unix(1709510400, 0).UTC()
	if !tx0.OccurredAt.Equal(expectedTime) {
		t.Errorf("tx[0] OccurredAt = %v, want %v", tx0.OccurredAt, expectedTime)
	}

	// RawData must be non-empty valid JSON.
	if len(tx0.RawData) == 0 {
		t.Error("tx[0] RawData is empty, expected JSON payload")
	}

	// SourceID set correctly.
	if tx0.SourceID != "src-stripe-5" {
		t.Errorf("tx[0] SourceID = %q, want %q", tx0.SourceID, "src-stripe-5")
	}

	// --- Verify second transaction (refund → debit) ---
	tx1 := result.Transactions[1]
	if tx1.Direction != domain.DirectionDebit {
		t.Errorf("tx[1] Direction = %q, want %q (refund should be debit)", tx1.Direction, domain.DirectionDebit)
	}
	if tx1.Amount != -5000 {
		t.Errorf("tx[1] Amount = %d, want %d", tx1.Amount, -5000)
	}
	if tx1.ExternalID != "txn_2PrS3vK0bRlQ5MqX" {
		t.Errorf("tx[1] ExternalID = %q, want %q", tx1.ExternalID, "txn_2PrS3vK0bRlQ5MqX")
	}
}

// contains checks if s contains substr (case-insensitive for flexibility).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && len(substr) > 0 && containsCI(s, substr))
}

func containsCI(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if eqCI(s[i:i+len(substr)], substr) {
			return true
		}
	}
	return false
}

func eqCI(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
