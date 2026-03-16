package domain

import "time"

// Transaction represents a normalized transaction from any source.
type Transaction struct {
	ID           string    `db:"id" json:"id"`
	SourceID     string    `db:"source_id" json:"source_id"`
	ExternalID   string    `db:"external_id" json:"external_id"`
	Amount       int64     `db:"amount" json:"amount"`               // cents — never floats for money
	Currency     string    `db:"currency" json:"currency"`           // ISO 4217
	Direction    string    `db:"direction" json:"direction"`         // "credit" or "debit"
	Description  string    `db:"description" json:"description"`
	Counterparty string    `db:"counterparty" json:"counterparty"`
	OccurredAt   time.Time `db:"occurred_at" json:"occurred_at"`
	RawData      []byte    `db:"raw_data" json:"raw_data"`           // JSONB
	DedupKey     string    `db:"dedup_key" json:"dedup_key"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}

// IngestRequest is the input for the Transaction Ingester.
type IngestRequest struct {
	SourceID     string    `json:"source_id"`
	ExternalID   string    `json:"external_id"`
	Amount       int64     `json:"amount"`
	Currency     string    `json:"currency"`
	Direction    string    `json:"direction"`
	Description  string    `json:"description,omitempty"`
	Counterparty string    `json:"counterparty,omitempty"`
	OccurredAt   time.Time `json:"occurred_at"`
	RawData      []byte    `json:"raw_data"`
}

// IngestResult is the output of the Transaction Ingester.
type IngestResult struct {
	TransactionID string `json:"transaction_id"`
	Status        string `json:"status"` // "created", "duplicate", "error"
}

// IngestStatus constants.
const (
	IngestStatusCreated   = "created"
	IngestStatusDuplicate = "duplicate"
	IngestStatusError     = "error"
)

// Direction constants.
const (
	DirectionCredit = "credit"
	DirectionDebit  = "debit"
)
