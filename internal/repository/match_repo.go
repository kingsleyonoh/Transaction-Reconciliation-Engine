package repository

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// MatchRepository defines data access operations for matches.
type MatchRepository interface {
	Insert(ctx context.Context, m *domain.Match) error
	FindByRunID(ctx context.Context, runID string) ([]domain.Match, error)
	CountByRunID(ctx context.Context, runID string) (int, error)
}

type matchRepo struct {
	db *sqlx.DB
}

// NewMatchRepo creates a new MatchRepository backed by PostgreSQL.
func NewMatchRepo(db *sqlx.DB) MatchRepository {
	return &matchRepo{db: db}
}

func (r *matchRepo) Insert(ctx context.Context, m *domain.Match) error {
	query := `
		INSERT INTO matches (id, gateway_tx_id, ledger_tx_id, match_type, confidence,
			matched_by, match_rule, run_id, notes)
		VALUES (:id, :gateway_tx_id, :ledger_tx_id, :match_type, :confidence,
			:matched_by, :match_rule, :run_id, :notes)`

	_, err := r.db.NamedExecContext(ctx, query, m)
	if err != nil {
		return fmt.Errorf("inserting match: %w", err)
	}
	return nil
}

func (r *matchRepo) FindByRunID(ctx context.Context, runID string) ([]domain.Match, error) {
	var matches []domain.Match
	err := r.db.SelectContext(ctx, &matches,
		"SELECT * FROM matches WHERE run_id = $1 ORDER BY created_at", runID)
	if err != nil {
		return nil, fmt.Errorf("finding matches by run ID: %w", err)
	}
	return matches, nil
}

func (r *matchRepo) CountByRunID(ctx context.Context, runID string) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count, "SELECT COUNT(*) FROM matches WHERE run_id = $1", runID)
	if err != nil {
		return 0, fmt.Errorf("counting matches by run ID: %w", err)
	}
	return count, nil
}
