package adapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// ErrNotSupported is returned when an adapter does not support a particular operation.
// API adapters return this from ParseFile; file adapters return this from FetchTransactions.
var ErrNotSupported = errors.New("operation not supported by this adapter")

// SourceAdapter is the contract all source-specific adapters must implement.
// Each adapter transforms source-specific data into canonical IngestRequest(s).
type SourceAdapter interface {
	// Name returns the adapter's identifier (e.g., "stripe", "paypal", "mt940", "camt053").
	Name() string

	// FetchTransactions pulls transactions from an external API for the given date range.
	// Used by API-based adapters (Stripe, PayPal).
	// File-based adapters should return ErrNotSupported.
	FetchTransactions(ctx context.Context, req FetchRequest) (*FetchResult, error)

	// ParseFile parses raw file content (MT940/CAMT.053) into canonical transactions.
	// Used by file-based adapters (bank files).
	// API-based adapters should return ErrNotSupported.
	ParseFile(ctx context.Context, data []byte, sourceID string) (*FetchResult, error)
}

// FetchRequest is the input for API-based adapters that pull transactions from external sources.
type FetchRequest struct {
	SourceID string    // Source UUID — identifies which source owns these transactions.
	DateFrom time.Time // Start of the date range (inclusive).
	DateTo   time.Time // End of the date range (inclusive).
}

// Validate checks that the FetchRequest has all required fields and valid date range.
func (r FetchRequest) Validate() error {
	if r.SourceID == "" {
		return fmt.Errorf("source_id is required")
	}
	if r.DateFrom.After(r.DateTo) {
		return fmt.Errorf("date_from (%s) must be before date_to (%s)",
			r.DateFrom.Format(time.RFC3339), r.DateTo.Format(time.RFC3339))
	}
	return nil
}

// FetchResult is the output of both FetchTransactions and ParseFile.
// It contains the parsed transactions and metadata about the operation.
type FetchResult struct {
	Transactions []domain.IngestRequest // Canonical transactions ready for the Ingester.
	TotalRecords int                    // Number of records parsed/fetched.
	Warnings     []string              // Non-fatal issues encountered during processing.
}
