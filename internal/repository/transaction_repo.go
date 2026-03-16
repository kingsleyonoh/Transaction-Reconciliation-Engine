package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// TransactionRepository defines data access operations for transactions.
type TransactionRepository interface {
	// Insert inserts a transaction. Returns true if the row was inserted (not a duplicate).
	Insert(ctx context.Context, tx *domain.Transaction) (bool, error)
	// InsertBatch inserts multiple transactions at once. Returns the count of actually inserted rows.
	InsertBatch(ctx context.Context, txs []domain.Transaction) (int, error)
	// FindByDedupKey returns a transaction by its dedup_key, or nil if not found.
	FindByDedupKey(ctx context.Context, dedupKey string) (*domain.Transaction, error)
	// FindUnmatched returns transactions for a source that have no matching entry, within the date range.
	FindUnmatched(ctx context.Context, sourceID string, from, to time.Time) ([]domain.Transaction, error)
}

type transactionRepo struct {
	db *sqlx.DB
}

// NewTransactionRepo creates a new TransactionRepository backed by PostgreSQL.
func NewTransactionRepo(db *sqlx.DB) TransactionRepository {
	return &transactionRepo{db: db}
}

func (r *transactionRepo) Insert(ctx context.Context, tx *domain.Transaction) (bool, error) {
	query := `
		INSERT INTO transactions (id, source_id, external_id, amount, currency, direction,
			description, counterparty, occurred_at, raw_data, dedup_key)
		VALUES (:id, :source_id, :external_id, :amount, :currency, :direction,
			:description, :counterparty, :occurred_at, :raw_data, :dedup_key)
		ON CONFLICT (dedup_key) DO NOTHING`

	result, err := r.db.NamedExecContext(ctx, query, tx)
	if err != nil {
		return false, fmt.Errorf("inserting transaction: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("checking rows affected: %w", err)
	}

	return rows > 0, nil
}

func (r *transactionRepo) InsertBatch(ctx context.Context, txs []domain.Transaction) (int, error) {
	if len(txs) == 0 {
		return 0, nil
	}

	// Build a multi-row INSERT with ON CONFLICT DO NOTHING
	query := `
		INSERT INTO transactions (id, source_id, external_id, amount, currency, direction,
			description, counterparty, occurred_at, raw_data, dedup_key)
		VALUES `

	var values []string
	var args []interface{}
	argIdx := 1

	for _, tx := range txs {
		placeholder := fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			argIdx, argIdx+1, argIdx+2, argIdx+3, argIdx+4, argIdx+5,
			argIdx+6, argIdx+7, argIdx+8, argIdx+9, argIdx+10)
		values = append(values, placeholder)
		args = append(args,
			tx.ID, tx.SourceID, tx.ExternalID, tx.Amount, tx.Currency, tx.Direction,
			tx.Description, tx.Counterparty, tx.OccurredAt, tx.RawData, tx.DedupKey,
		)
		argIdx += 11
	}

	query += strings.Join(values, ", ") + " ON CONFLICT (dedup_key) DO NOTHING"

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("batch inserting transactions: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("checking rows affected: %w", err)
	}

	return int(rows), nil
}

func (r *transactionRepo) FindByDedupKey(ctx context.Context, dedupKey string) (*domain.Transaction, error) {
	var tx domain.Transaction
	err := r.db.GetContext(ctx, &tx, "SELECT * FROM transactions WHERE dedup_key = $1", dedupKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("finding transaction by dedup key: %w", err)
	}
	return &tx, nil
}

func (r *transactionRepo) FindUnmatched(ctx context.Context, sourceID string, from, to time.Time) ([]domain.Transaction, error) {
	query := `
		SELECT t.* FROM transactions t
		WHERE t.source_id = $1
		  AND t.occurred_at >= $2
		  AND t.occurred_at <= $3
		  AND t.id NOT IN (
			SELECT gateway_tx_id FROM matches
			UNION
			SELECT ledger_tx_id FROM matches
		  )
		ORDER BY t.occurred_at`

	var txs []domain.Transaction
	err := r.db.SelectContext(ctx, &txs, query, sourceID, from, to)
	if err != nil {
		return nil, fmt.Errorf("finding unmatched transactions: %w", err)
	}
	return txs, nil
}
