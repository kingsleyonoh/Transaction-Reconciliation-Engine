package repository

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// IngestionLogRepository defines data access operations for ingestion logs.
type IngestionLogRepository interface {
	Create(ctx context.Context, log *domain.IngestionLog) error
	Update(ctx context.Context, log *domain.IngestionLog) error
	FindBySourceID(ctx context.Context, sourceID string, limit, offset int) ([]domain.IngestionLog, error)
}

type ingestionLogRepo struct {
	db *sqlx.DB
}

// NewIngestionLogRepo creates a new IngestionLogRepository backed by PostgreSQL.
func NewIngestionLogRepo(db *sqlx.DB) IngestionLogRepository {
	return &ingestionLogRepo{db: db}
}

func (r *ingestionLogRepo) Create(ctx context.Context, log *domain.IngestionLog) error {
	query := `
		INSERT INTO ingestion_logs (id, source_id, file_name, status, started_at)
		VALUES (:id, :source_id, :file_name, :status, :started_at)`

	_, err := r.db.NamedExecContext(ctx, query, log)
	if err != nil {
		return fmt.Errorf("creating ingestion log: %w", err)
	}
	return nil
}

func (r *ingestionLogRepo) Update(ctx context.Context, log *domain.IngestionLog) error {
	query := `
		UPDATE ingestion_logs
		SET records_total = :records_total,
			records_new = :records_new,
			records_skipped = :records_skipped,
			records_failed = :records_failed,
			errors = :errors,
			completed_at = :completed_at,
			status = :status
		WHERE id = :id`

	result, err := r.db.NamedExecContext(ctx, query, log)
	if err != nil {
		return fmt.Errorf("updating ingestion log: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("ingestion log %s not found", log.ID)
	}

	return nil
}

func (r *ingestionLogRepo) FindBySourceID(ctx context.Context, sourceID string, limit, offset int) ([]domain.IngestionLog, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}

	var logs []domain.IngestionLog
	err := r.db.SelectContext(ctx, &logs,
		"SELECT * FROM ingestion_logs WHERE source_id = $1 ORDER BY started_at DESC LIMIT $2 OFFSET $3",
		sourceID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("finding ingestion logs by source ID: %w", err)
	}
	return logs, nil
}
