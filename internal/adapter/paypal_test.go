package adapter

import (
	"context"
	"encoding/base64"
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

// --- PayPalAdapter basic tests ---

func TestPayPalAdapter_Name(t *testing.T) {
	a := NewPayPalAdapter("client-id", "client-secret", "https://api.paypal.com", nil)
	if a.Name() != "paypal" {
		t.Errorf("expected name %q, got %q", "paypal", a.Name())
	}
}

func TestPayPalAdapter_ParseFile_ReturnsNotSupported(t *testing.T) {
	a := NewPayPalAdapter("client-id", "client-secret", "https://api.paypal.com", nil)
	_, err := a.ParseFile(context.Background(), []byte("data"), "src-1")
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("expected ErrNotSupported, got %v", err)
	}
}

// --- FetchTransactions tests ---

func TestPayPalAdapter_FetchTransactions_Success(t *testing.T) {
	tokenJSON := loadPayPalFixture(t, "oauth_token.json")
	page1JSON := loadPayPalFixture(t, "transactions_page1.json")

	var oauthCalled int32
	var fetchCalled int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/v1/oauth2/token":
			atomic.AddInt32(&oauthCalled, 1)
			// Verify Basic auth header.
			auth := r.Header.Get("Authorization")
			expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("test-client-id:test-client-secret"))
			if auth != expected {
				t.Errorf("OAuth auth header = %q, want %q", auth, expected)
			}
			// Verify grant_type.
			if err := r.ParseForm(); err == nil {
				if r.FormValue("grant_type") != "client_credentials" {
					t.Errorf("grant_type = %q, want %q", r.FormValue("grant_type"), "client_credentials")
				}
			}
			w.Write(tokenJSON)

		case "/v1/reporting/transactions":
			atomic.AddInt32(&fetchCalled, 1)
			// Verify Bearer token.
			auth := r.Header.Get("Authorization")
			if auth != "Bearer A21AAKx7r8qNiL9EXAMPLE_FIXTURE_TOKEN_NOT_REAL_xyzABCdef123456" {
				t.Errorf("Bearer auth = %q", auth)
			}
			w.Write(page1JSON)

		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	a := NewPayPalAdapter("test-client-id", "test-client-secret", srv.URL, srv.Client())
	req := FetchRequest{
		SourceID: "src-paypal-1",
		DateFrom: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	result, err := a.FetchTransactions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Page1 has 3 transactions.
	if result.TotalRecords != 3 {
		t.Errorf("expected 3 total records, got %d", result.TotalRecords)
	}
	if len(result.Transactions) != 3 {
		t.Fatalf("expected 3 transactions, got %d", len(result.Transactions))
	}

	// OAuth should have been called once.
	if atomic.LoadInt32(&oauthCalled) != 1 {
		t.Errorf("expected 1 OAuth call, got %d", oauthCalled)
	}

	// Fetch should have been called once (single page).
	if atomic.LoadInt32(&fetchCalled) != 1 {
		t.Errorf("expected 1 fetch call, got %d", fetchCalled)
	}

	// All transactions have correct SourceID.
	for i, tx := range result.Transactions {
		if tx.SourceID != "src-paypal-1" {
			t.Errorf("tx[%d] SourceID = %q, want %q", i, tx.SourceID, "src-paypal-1")
		}
	}
}

func TestPayPalAdapter_FetchTransactions_EmptyResult(t *testing.T) {
	tokenJSON := loadPayPalFixture(t, "oauth_token.json")
	emptyJSON := loadPayPalFixture(t, "transactions_empty.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/oauth2/token":
			w.Write(tokenJSON)
		case "/v1/reporting/transactions":
			w.Write(emptyJSON)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	a := NewPayPalAdapter("cid", "csecret", srv.URL, srv.Client())
	req := FetchRequest{
		SourceID: "src-paypal-2",
		DateFrom: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
	}

	result, err := a.FetchTransactions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalRecords != 0 {
		t.Errorf("expected 0 records, got %d", result.TotalRecords)
	}
	if len(result.Transactions) != 0 {
		t.Errorf("expected 0 transactions, got %d", len(result.Transactions))
	}
}

func TestPayPalAdapter_FetchTransactions_RateLimit429(t *testing.T) {
	tokenJSON := loadPayPalFixture(t, "oauth_token.json")
	error429JSON := loadPayPalFixture(t, "error_429.json")
	page1JSON := loadPayPalFixture(t, "transactions_page1.json")

	var fetchCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/oauth2/token":
			w.Write(tokenJSON)
		case "/v1/reporting/transactions":
			count := atomic.AddInt32(&fetchCount, 1)
			if count == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write(error429JSON)
				return
			}
			w.Write(page1JSON)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	a := NewPayPalAdapter("cid", "csecret", srv.URL, srv.Client())
	req := FetchRequest{
		SourceID: "src-paypal-3",
		DateFrom: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	result, err := a.FetchTransactions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error after 429 retry: %v", err)
	}
	if result.TotalRecords != 3 {
		t.Errorf("expected 3 records after retry, got %d", result.TotalRecords)
	}
	if atomic.LoadInt32(&fetchCount) != 2 {
		t.Errorf("expected 2 fetch calls (429 + success), got %d", fetchCount)
	}
}

func TestPayPalAdapter_FetchTransactions_TokenExpired(t *testing.T) {
	tokenJSON := loadPayPalFixture(t, "oauth_token.json")
	expiredJSON := loadPayPalFixture(t, "error_token_expired.json")
	page1JSON := loadPayPalFixture(t, "transactions_page1.json")

	var oauthCount int32
	var fetchCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/oauth2/token":
			atomic.AddInt32(&oauthCount, 1)
			w.Write(tokenJSON)
		case "/v1/reporting/transactions":
			count := atomic.AddInt32(&fetchCount, 1)
			if count == 1 {
				// First fetch → token expired.
				w.WriteHeader(http.StatusUnauthorized)
				w.Write(expiredJSON)
				return
			}
			// After re-auth, succeed.
			w.Write(page1JSON)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	a := NewPayPalAdapter("cid", "csecret", srv.URL, srv.Client())
	req := FetchRequest{
		SourceID: "src-paypal-4",
		DateFrom: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	result, err := a.FetchTransactions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error after token refresh: %v", err)
	}
	if result.TotalRecords != 3 {
		t.Errorf("expected 3 records, got %d", result.TotalRecords)
	}
	// Should have called OAuth twice (initial + refresh).
	if atomic.LoadInt32(&oauthCount) != 2 {
		t.Errorf("expected 2 OAuth calls (initial + refresh), got %d", oauthCount)
	}
	// Should have called fetch twice (expired + success).
	if atomic.LoadInt32(&fetchCount) != 2 {
		t.Errorf("expected 2 fetch calls (expired + success), got %d", fetchCount)
	}
}

func TestPayPalAdapter_FetchTransactions_AmountConversion(t *testing.T) {
	tokenJSON := loadPayPalFixture(t, "oauth_token.json")
	page1JSON := loadPayPalFixture(t, "transactions_page1.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/oauth2/token":
			w.Write(tokenJSON)
		case "/v1/reporting/transactions":
			w.Write(page1JSON)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	a := NewPayPalAdapter("cid", "csecret", srv.URL, srv.Client())
	req := FetchRequest{
		SourceID: "src-paypal-5",
		DateFrom: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	result, err := a.FetchTransactions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Transactions) < 3 {
		t.Fatalf("expected 3 transactions, got %d", len(result.Transactions))
	}

	// --- tx[0]: "150.00" USD → 15000 cents, credit ---
	tx0 := result.Transactions[0]
	if tx0.ExternalID != "5TY05013RG002845M" {
		t.Errorf("tx[0] ExternalID = %q, want %q", tx0.ExternalID, "5TY05013RG002845M")
	}
	if tx0.Amount != 15000 {
		t.Errorf("tx[0] Amount = %d, want 15000 (from \"150.00\")", tx0.Amount)
	}
	if tx0.Currency != "USD" {
		t.Errorf("tx[0] Currency = %q, want %q", tx0.Currency, "USD")
	}
	if tx0.Direction != domain.DirectionCredit {
		t.Errorf("tx[0] Direction = %q, want %q", tx0.Direction, domain.DirectionCredit)
	}
	if tx0.Description != "Invoice #1001 - Acme Corp" {
		t.Errorf("tx[0] Description = %q, want %q", tx0.Description, "Invoice #1001 - Acme Corp")
	}
	if tx0.Counterparty != "buyer@acme.example.com" {
		t.Errorf("tx[0] Counterparty = %q, want %q", tx0.Counterparty, "buyer@acme.example.com")
	}
	if tx0.SourceID != "src-paypal-5" {
		t.Errorf("tx[0] SourceID = %q, want %q", tx0.SourceID, "src-paypal-5")
	}
	if len(tx0.RawData) == 0 {
		t.Error("tx[0] RawData is empty")
	}

	// --- tx[1]: "-75.50" USD → -7550 cents, debit (refund) ---
	tx1 := result.Transactions[1]
	if tx1.Amount != -7550 {
		t.Errorf("tx[1] Amount = %d, want -7550 (from \"-75.50\")", tx1.Amount)
	}
	if tx1.Direction != domain.DirectionDebit {
		t.Errorf("tx[1] Direction = %q, want %q (negative = debit)", tx1.Direction, domain.DirectionDebit)
	}
	if tx1.ExternalID != "8KH94682RD587301L" {
		t.Errorf("tx[1] ExternalID = %q, want %q", tx1.ExternalID, "8KH94682RD587301L")
	}

	// --- tx[2]: "2500.00" EUR → 250000 cents, credit ---
	tx2 := result.Transactions[2]
	if tx2.Amount != 250000 {
		t.Errorf("tx[2] Amount = %d, want 250000 (from \"2500.00\")", tx2.Amount)
	}
	if tx2.Currency != "EUR" {
		t.Errorf("tx[2] Currency = %q, want %q", tx2.Currency, "EUR")
	}
	if tx2.Direction != domain.DirectionCredit {
		t.Errorf("tx[2] Direction = %q, want %q", tx2.Direction, domain.DirectionCredit)
	}
}

// loadPayPalFixture reads a fixture file from the PayPal fixture directory.
func loadPayPalFixture(t *testing.T, name string) []byte {
	t.Helper()
	dir := paypalFixtureDir(t)
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("failed to load paypal fixture %s: %v", name, err)
	}
	return data
}

// paypalFixtureDir returns the absolute path to the paypal fixture directory.
func paypalFixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "tests", "fixtures", "paypal")
}
