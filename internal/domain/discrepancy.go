package domain

import "time"

// Discrepancy represents an unmatched or mismatched transaction.
type Discrepancy struct {
	ID              string     `db:"id" json:"id"`
	TransactionID   string     `db:"transaction_id" json:"transaction_id"`
	DiscrepancyType string     `db:"discrepancy_type" json:"discrepancy_type"`
	ExpectedValue   string     `db:"expected_value" json:"expected_value,omitempty"`
	ActualValue     string     `db:"actual_value" json:"actual_value,omitempty"`
	Severity        string     `db:"severity" json:"severity"`
	Status          string     `db:"status" json:"status"`
	ResolvedAt      *time.Time `db:"resolved_at" json:"resolved_at,omitempty"`
	ResolvedBy      string     `db:"resolved_by" json:"resolved_by,omitempty"`
	ResolutionNote  string     `db:"resolution_note" json:"resolution_note,omitempty"`
	RunID           string     `db:"run_id" json:"run_id"`
	CreatedAt       time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at" json:"updated_at"`
}

// DiscrepancyType constants.
const (
	DiscrepancyUnmatchedGateway = "unmatched_gateway"
	DiscrepancyUnmatchedLedger  = "unmatched_ledger"
	DiscrepancyAmountMismatch   = "amount_mismatch"
	DiscrepancyDateMismatch     = "date_mismatch"
)

// Severity constants.
const (
	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

// DiscrepancyStatus constants.
const (
	StatusOpen          = "open"
	StatusInvestigating = "investigating"
	StatusResolved      = "resolved"
	StatusIgnored       = "ignored"
)
