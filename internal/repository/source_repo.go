package repository

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// SourceRepository defines data access operations for sources.
type SourceRepository interface {
	FindByID(ctx context.Context, id string) (*domain.Source, error)
	FindByName(ctx context.Context, name string) (*domain.Source, error)
	List(ctx context.Context) ([]domain.Source, error)
}

type sourceRepo struct {
	db *sqlx.DB
}

// NewSourceRepo creates a new SourceRepository backed by PostgreSQL.
func NewSourceRepo(db *sqlx.DB) SourceRepository {
	return &sourceRepo{db: db}
}

func (r *sourceRepo) FindByID(ctx context.Context, id string) (*domain.Source, error) {
	var s domain.Source
	err := r.db.GetContext(ctx, &s, "SELECT * FROM sources WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *sourceRepo) FindByName(ctx context.Context, name string) (*domain.Source, error) {
	var s domain.Source
	err := r.db.GetContext(ctx, &s, "SELECT * FROM sources WHERE name = $1", name)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *sourceRepo) List(ctx context.Context) ([]domain.Source, error) {
	var sources []domain.Source
	err := r.db.SelectContext(ctx, &sources, "SELECT * FROM sources ORDER BY name")
	if err != nil {
		return nil, err
	}
	return sources, nil
}
