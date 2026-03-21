package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// RunRepository defines data access operations for reconciliation runs.
type RunRepository interface {
	Create(ctx context.Context, run *domain.ReconciliationRun) error
	Update(ctx context.Context, run *domain.ReconciliationRun) error
	FindByID(ctx context.Context, id string) (*domain.ReconciliationRun, error)
	List(ctx context.Context, limit, offset int) ([]domain.ReconciliationRun, error)
	FindByDateRange(ctx context.Context, from, to time.Time) ([]domain.ReconciliationRun, error)
}

type runRepo struct {
	db *sqlx.DB
}

// NewRunRepo creates a new RunRepository backed by PostgreSQL.
func NewRunRepo(db *sqlx.DB) RunRepository {
	return &runRepo{db: db}
}

func (r *runRepo) Create(ctx context.Context, run *domain.ReconciliationRun) error {
	query := `
		INSERT INTO reconciliation_runs (id, started_at, status, config_snapshot)
		VALUES (:id, :started_at, :status, :config_snapshot)`

	_, err := r.db.NamedExecContext(ctx, query, run)
	if err != nil {
		return fmt.Errorf("creating reconciliation run: %w", err)
	}
	return nil
}

func (r *runRepo) Update(ctx context.Context, run *domain.ReconciliationRun) error {
	query := `
		UPDATE reconciliation_runs
		SET completed_at = :completed_at,
			status = :status,
			total_gateway = :total_gateway,
			total_ledger = :total_ledger,
			matched_count = :matched_count,
			discrepancy_count = :discrepancy_count,
			match_rate = :match_rate,
			error_message = :error_message,
			duration_ms = :duration_ms
		WHERE id = :id`

	result, err := r.db.NamedExecContext(ctx, query, run)
	if err != nil {
		return fmt.Errorf("updating reconciliation run: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("reconciliation run %s not found", run.ID)
	}

	return nil
}

func (r *runRepo) FindByID(ctx context.Context, id string) (*domain.ReconciliationRun, error) {
	var run domain.ReconciliationRun
	err := r.db.GetContext(ctx, &run, "SELECT * FROM reconciliation_runs WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *runRepo) List(ctx context.Context, limit, offset int) ([]domain.ReconciliationRun, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}

	var runs []domain.ReconciliationRun
	err := r.db.SelectContext(ctx, &runs,
		"SELECT * FROM reconciliation_runs ORDER BY started_at DESC LIMIT $1 OFFSET $2",
		limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing reconciliation runs: %w", err)
	}
	return runs, nil
}

func (r *runRepo) FindByDateRange(ctx context.Context, from, to time.Time) ([]domain.ReconciliationRun, error) {
	var runs []domain.ReconciliationRun
	err := r.db.SelectContext(ctx, &runs,
		"SELECT * FROM reconciliation_runs WHERE started_at >= $1 AND started_at <= $2 ORDER BY started_at DESC",
		from, to)
	if err != nil {
		return nil, fmt.Errorf("finding runs by date range: %w", err)
	}
	return runs, nil
}
