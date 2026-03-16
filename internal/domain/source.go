package domain

import "time"

// Source represents a payment gateway or bank source.
type Source struct {
	ID         string    `db:"id" json:"id"`
	Name       string    `db:"name" json:"name"`
	SourceType string    `db:"source_type" json:"source_type"` // "gateway", "bank", "ledger"
	Config     []byte    `db:"config" json:"config"`           // JSONB
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
	UpdatedAt  time.Time `db:"updated_at" json:"updated_at"`
}

// SourceType constants.
const (
	SourceTypeGateway = "gateway"
	SourceTypeBank    = "bank"
	SourceTypeLedger  = "ledger"
)

// IngestionLog records the result of a batch ingestion.
type IngestionLog struct {
	ID             string     `db:"id" json:"id"`
	SourceID       string     `db:"source_id" json:"source_id"`
	FileName       string     `db:"file_name" json:"file_name,omitempty"`
	RecordsTotal   int        `db:"records_total" json:"records_total"`
	RecordsNew     int        `db:"records_new" json:"records_new"`
	RecordsSkipped int        `db:"records_skipped" json:"records_skipped"`
	RecordsFailed  int        `db:"records_failed" json:"records_failed"`
	Errors         []byte     `db:"errors" json:"errors"`           // JSONB array
	StartedAt      time.Time  `db:"started_at" json:"started_at"`
	CompletedAt    *time.Time `db:"completed_at" json:"completed_at,omitempty"`
	Status         string     `db:"status" json:"status"`
	CreatedAt      time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at" json:"updated_at"`
}
