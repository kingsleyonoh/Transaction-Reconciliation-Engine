package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// DiscrepancyFilters defines query parameters for listing discrepancies.
type DiscrepancyFilters struct {
	Status   string
	Severity string
	DateFrom *time.Time
	DateTo   *time.Time
	Limit    int
	Offset   int
}

// DiscrepancyRepository defines data access operations for discrepancies.
type DiscrepancyRepository interface {
	Insert(ctx context.Context, d *domain.Discrepancy) error
	UpdateStatus(ctx context.Context, id, status, resolvedBy, resolutionNote string) error
	FindByFilters(ctx context.Context, filters DiscrepancyFilters) ([]domain.Discrepancy, error)
	FindByTransactionAndRun(ctx context.Context, transactionID, runID string) (*domain.Discrepancy, error)
	FindByDateRange(ctx context.Context, from, to time.Time) ([]domain.Discrepancy, error)
	CountOpenOlderThan(ctx context.Context, days int) (int, error)
}

type discrepancyRepo struct {
	db *sqlx.DB
}

// NewDiscrepancyRepo creates a new DiscrepancyRepository backed by PostgreSQL.
func NewDiscrepancyRepo(db *sqlx.DB) DiscrepancyRepository {
	return &discrepancyRepo{db: db}
}

func (r *discrepancyRepo) Insert(ctx context.Context, d *domain.Discrepancy) error {
	query := `
		INSERT INTO discrepancies (id, transaction_id, discrepancy_type, expected_value,
			actual_value, severity, status, run_id)
		VALUES (:id, :transaction_id, :discrepancy_type, :expected_value,
			:actual_value, :severity, :status, :run_id)`

	_, err := r.db.NamedExecContext(ctx, query, d)
	if err != nil {
		return fmt.Errorf("inserting discrepancy: %w", err)
	}
	return nil
}

func (r *discrepancyRepo) UpdateStatus(ctx context.Context, id, status, resolvedBy, resolutionNote string) error {
	query := `
		UPDATE discrepancies
		SET status = $1, resolved_by = $2, resolution_note = $3, resolved_at = NOW()
		WHERE id = $4`

	result, err := r.db.ExecContext(ctx, query, status, resolvedBy, resolutionNote, id)
	if err != nil {
		return fmt.Errorf("updating discrepancy status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("discrepancy %s not found", id)
	}

	return nil
}

func (r *discrepancyRepo) FindByFilters(ctx context.Context, filters DiscrepancyFilters) ([]domain.Discrepancy, error) {
	query := "SELECT * FROM discrepancies WHERE 1=1"
	var args []interface{}
	argIdx := 1

	if filters.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, filters.Status)
		argIdx++
	}
	if filters.Severity != "" {
		query += fmt.Sprintf(" AND severity = $%d", argIdx)
		args = append(args, filters.Severity)
		argIdx++
	}
	if filters.DateFrom != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, *filters.DateFrom)
		argIdx++
	}
	if filters.DateTo != nil {
		query += fmt.Sprintf(" AND created_at <= $%d", argIdx)
		args = append(args, *filters.DateTo)
		argIdx++
	}

	query += " ORDER BY created_at DESC"

	limit := filters.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, limit)
	argIdx++

	if filters.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIdx)
		args = append(args, filters.Offset)
	}

	var discrepancies []domain.Discrepancy
	err := r.db.SelectContext(ctx, &discrepancies, query, args...)
	if err != nil {
		return nil, fmt.Errorf("finding discrepancies by filters: %w", err)
	}
	return discrepancies, nil
}

func (r *discrepancyRepo) FindByTransactionAndRun(ctx context.Context, transactionID, runID string) (*domain.Discrepancy, error) {
	var d domain.Discrepancy
	err := r.db.GetContext(ctx, &d,
		"SELECT * FROM discrepancies WHERE transaction_id = $1 AND run_id = $2",
		transactionID, runID)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *discrepancyRepo) FindByDateRange(ctx context.Context, from, to time.Time) ([]domain.Discrepancy, error) {
	var discs []domain.Discrepancy
	err := r.db.SelectContext(ctx, &discs,
		"SELECT * FROM discrepancies WHERE created_at >= $1 AND created_at <= $2 ORDER BY created_at DESC",
		from, to)
	if err != nil {
		return nil, fmt.Errorf("finding discrepancies by date range: %w", err)
	}
	return discs, nil
}

func (r *discrepancyRepo) CountOpenOlderThan(ctx context.Context, days int) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count,
		"SELECT COUNT(*) FROM discrepancies WHERE status = 'open' AND created_at < NOW() - ($1 || ' days')::INTERVAL",
		days)
	if err != nil {
		return 0, fmt.Errorf("counting old open discrepancies: %w", err)
	}
	return count, nil
}
