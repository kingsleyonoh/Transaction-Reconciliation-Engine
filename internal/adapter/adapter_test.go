package adapter

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// --- Mock adapter for compile-time interface verification ---

type mockAPIAdapter struct{}

func (m *mockAPIAdapter) Name() string { return "mock-api" }

func (m *mockAPIAdapter) FetchTransactions(ctx context.Context, req FetchRequest) (*FetchResult, error) {
	return &FetchResult{
		Transactions: []domain.IngestRequest{
			{
				SourceID:   req.SourceID,
				ExternalID: "ext-001",
				Amount:     5000,
				Currency:   "USD",
				Direction:  domain.DirectionCredit,
				OccurredAt: time.Now().Add(-1 * time.Hour),
			},
		},
		TotalRecords: 1,
	}, nil
}

func (m *mockAPIAdapter) ParseFile(ctx context.Context, data []byte, sourceID string) (*FetchResult, error) {
	return nil, ErrNotSupported
}

type mockFileAdapter struct{}

func (m *mockFileAdapter) Name() string { return "mock-file" }

func (m *mockFileAdapter) FetchTransactions(ctx context.Context, req FetchRequest) (*FetchResult, error) {
	return nil, ErrNotSupported
}

func (m *mockFileAdapter) ParseFile(ctx context.Context, data []byte, sourceID string) (*FetchResult, error) {
	return &FetchResult{
		Transactions: []domain.IngestRequest{
			{
				SourceID:   sourceID,
				ExternalID: "bank-ref-001",
				Amount:     25000,
				Currency:   "EUR",
				Direction:  domain.DirectionDebit,
				OccurredAt: time.Now().Add(-2 * time.Hour),
			},
		},
		TotalRecords: 1,
	}, nil
}

// --- Tests ---

func TestSourceAdapter_InterfaceCompliance_APIAdapter(t *testing.T) {
	// Compile-time check: mockAPIAdapter satisfies SourceAdapter
	var _ SourceAdapter = (*mockAPIAdapter)(nil)

	adapter := &mockAPIAdapter{}
	if adapter.Name() != "mock-api" {
		t.Errorf("expected name %q, got %q", "mock-api", adapter.Name())
	}
}

func TestSourceAdapter_InterfaceCompliance_FileAdapter(t *testing.T) {
	// Compile-time check: mockFileAdapter satisfies SourceAdapter
	var _ SourceAdapter = (*mockFileAdapter)(nil)

	adapter := &mockFileAdapter{}
	if adapter.Name() != "mock-file" {
		t.Errorf("expected name %q, got %q", "mock-file", adapter.Name())
	}
}

func TestFetchRequest_Validate_HappyPath(t *testing.T) {
	req := FetchRequest{
		SourceID: "source-123",
		DateFrom: time.Now().Add(-24 * time.Hour),
		DateTo:   time.Now(),
	}

	err := req.Validate()
	if err != nil {
		t.Fatalf("expected no error for valid request, got: %v", err)
	}
}

func TestFetchRequest_Validate_EmptySourceID(t *testing.T) {
	req := FetchRequest{
		SourceID: "",
		DateFrom: time.Now().Add(-24 * time.Hour),
		DateTo:   time.Now(),
	}

	err := req.Validate()
	if err == nil {
		t.Fatal("expected error for empty SourceID, got nil")
	}
}

func TestFetchRequest_Validate_DateFromAfterDateTo(t *testing.T) {
	req := FetchRequest{
		SourceID: "source-123",
		DateFrom: time.Now(),
		DateTo:   time.Now().Add(-24 * time.Hour),
	}

	err := req.Validate()
	if err == nil {
		t.Fatal("expected error when DateFrom is after DateTo, got nil")
	}
}

func TestErrNotSupported_IsComparable(t *testing.T) {
	err := ErrNotSupported

	if !errors.Is(err, ErrNotSupported) {
		t.Error("ErrNotSupported should be comparable with errors.Is()")
	}

	// Wrapping should still match
	wrapped := wrappedNotSupported()
	if !errors.Is(wrapped, ErrNotSupported) {
		t.Error("wrapped ErrNotSupported should still match with errors.Is()")
	}
}

func TestFetchResult_HoldsTransactions(t *testing.T) {
	result := &FetchResult{
		Transactions: []domain.IngestRequest{
			{SourceID: "s1", ExternalID: "e1", Amount: 100, Currency: "USD", Direction: domain.DirectionCredit, OccurredAt: time.Now()},
			{SourceID: "s1", ExternalID: "e2", Amount: 200, Currency: "EUR", Direction: domain.DirectionDebit, OccurredAt: time.Now()},
		},
		TotalRecords: 2,
	}

	if len(result.Transactions) != 2 {
		t.Errorf("expected 2 transactions, got %d", len(result.Transactions))
	}
	if result.TotalRecords != 2 {
		t.Errorf("expected TotalRecords=2, got %d", result.TotalRecords)
	}
}

func TestFetchResult_EmptyResult(t *testing.T) {
	result := &FetchResult{
		Transactions: []domain.IngestRequest{},
		TotalRecords: 0,
	}

	if len(result.Transactions) != 0 {
		t.Errorf("expected 0 transactions, got %d", len(result.Transactions))
	}
}

func TestAPIAdapter_ParseFile_ReturnsNotSupported(t *testing.T) {
	adapter := &mockAPIAdapter{}
	_, err := adapter.ParseFile(context.Background(), []byte("data"), "source-123")
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("expected ErrNotSupported, got: %v", err)
	}
}

func TestFileAdapter_FetchTransactions_ReturnsNotSupported(t *testing.T) {
	adapter := &mockFileAdapter{}
	_, err := adapter.FetchTransactions(context.Background(), FetchRequest{
		SourceID: "source-123",
		DateFrom: time.Now().Add(-24 * time.Hour),
		DateTo:   time.Now(),
	})
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("expected ErrNotSupported, got: %v", err)
	}
}

// wrappedNotSupported simulates a wrapped error to test errors.Is() unwrapping.
func wrappedNotSupported() error {
	return fmt.Errorf("adapter operation: %w", ErrNotSupported)
}
