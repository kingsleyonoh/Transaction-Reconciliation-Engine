package domain

import "time"

// Match represents a confirmed match between a gateway and ledger transaction.
type Match struct {
	ID          string    `db:"id" json:"id"`
	GatewayTxID string    `db:"gateway_tx_id" json:"gateway_tx_id"`
	LedgerTxID  string    `db:"ledger_tx_id" json:"ledger_tx_id"`
	MatchType   string    `db:"match_type" json:"match_type"`       // "exact", "fuzzy_amount", "fuzzy_date", "manual"
	Confidence  float64   `db:"confidence" json:"confidence"`       // 0.0 to 1.0
	MatchedBy   string    `db:"matched_by" json:"matched_by"`       // "auto_exact", "auto_fuzzy", "manual_user"
	MatchRule   string    `db:"match_rule" json:"match_rule"`
	RunID       string    `db:"run_id" json:"run_id"`
	Notes       string    `db:"notes" json:"notes,omitempty"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

// MatchType constants for the matching cascade.
const (
	MatchTypeExact       = "exact"
	MatchTypeFuzzyAmount = "fuzzy_amount"
	MatchTypeFuzzyDate   = "fuzzy_date"
	MatchTypeReference   = "reference"
	MatchTypeManual      = "manual"
)

// MatchedBy constants.
const (
	MatchedByAutoExact = "auto_exact"
	MatchedByAutoFuzzy = "auto_fuzzy"
	MatchedByManual    = "manual_user"
)
