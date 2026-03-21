package domain

import "time"

// ReconciliationRun represents a single reconciliation execution.
type ReconciliationRun struct {
	ID               string     `db:"id" json:"id"`
	StartedAt        time.Time  `db:"started_at" json:"started_at"`
	CompletedAt      *time.Time `db:"completed_at" json:"completed_at,omitempty"`
	Status           string     `db:"status" json:"status"`
	TotalGateway     int        `db:"total_gateway" json:"total_gateway"`
	TotalLedger      int        `db:"total_ledger" json:"total_ledger"`
	MatchedCount     int        `db:"matched_count" json:"matched_count"`
	DiscrepancyCount int        `db:"discrepancy_count" json:"discrepancy_count"`
	MatchRate        float64    `db:"match_rate" json:"match_rate"`
	ConfigSnapshot   []byte     `db:"config_snapshot" json:"config_snapshot"` // JSONB
	ErrorMessage     string     `db:"error_message" json:"error_message,omitempty"`
	DurationMs       int        `db:"duration_ms" json:"duration_ms"`
	CreatedAt        time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at" json:"updated_at"`
}

// ReconcileRequest is the input for triggering a reconciliation run.
type ReconcileRequest struct {
	DateFrom  time.Time `json:"date_from"`
	DateTo    time.Time `json:"date_to"`
	SourceIDs []string  `json:"source_ids,omitempty"`
}

// ReconcileResult is the output of a reconciliation run.
type ReconcileResult struct {
	RunID            string `json:"run_id"`
	Matched          int    `json:"matched"`
	UnmatchedGateway int    `json:"unmatched_gateway"`
	UnmatchedLedger  int    `json:"unmatched_ledger"`
	DurationMs       int    `json:"duration_ms"`
}

// RunStatus constants.
const (
	RunStatusRunning   = "running"
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
)
