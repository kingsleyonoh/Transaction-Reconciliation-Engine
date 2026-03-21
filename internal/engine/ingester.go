package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/repository"
)

// redisDedupTTL is the cache duration for dedup keys in Redis.
const redisDedupTTL = 24 * time.Hour

// Ingester handles normalizing and storing transactions from any source.
type Ingester struct {
	repo   repository.TransactionRepository
	redis  *redis.Client // nil = Redis unavailable, fall back to PostgreSQL only
	logger zerolog.Logger
}

// NewIngester creates a new Ingester. The redis client may be nil.
func NewIngester(repo repository.TransactionRepository, redisClient *redis.Client, logger zerolog.Logger) *Ingester {
	return &Ingester{
		repo:   repo,
		redis:  redisClient,
		logger: logger,
	}
}

// Ingest validates, deduplicates, and stores a single transaction.
func (ing *Ingester) Ingest(ctx context.Context, req domain.IngestRequest) (domain.IngestResult, error) {
	// 1. Validate
	if err := validate(req); err != nil {
		return domain.IngestResult{Status: domain.IngestStatusError}, err
	}

	// 2. Generate dedup key
	key := DedupKey(req.SourceID, req.ExternalID)

	// 3. Redis dedup check
	if ing.redis != nil {
		cachedID, err := ing.redis.Get(ctx, "dedup:"+key).Result()
		if err == nil && cachedID != "" {
			return domain.IngestResult{
				TransactionID: cachedID,
				Status:        domain.IngestStatusDuplicate,
			}, nil
		}
		if err != nil && err != redis.Nil {
			ing.logger.Warn().Str("key", key).Err(err).Msg("redis GET dedup failed, falling through to PostgreSQL")
		}
	}

	// 4. PostgreSQL dedup fallback
	existing, err := ing.repo.FindByDedupKey(ctx, key)
	if err != nil {
		return domain.IngestResult{Status: domain.IngestStatusError}, fmt.Errorf("checking dedup in DB: %w", err)
	}
	if existing != nil {
		// Re-populate Redis cache if available
		if ing.redis != nil {
			if err := ing.redis.Set(ctx, "dedup:"+key, existing.ID, redisDedupTTL).Err(); err != nil {
				ing.logger.Warn().Str("key", key).Err(err).Msg("redis SET dedup failed")
			}
		}
		return domain.IngestResult{
			TransactionID: existing.ID,
			Status:        domain.IngestStatusDuplicate,
		}, nil
	}

	// 5. Build transaction
	tx := domain.Transaction{
		ID:           uuid.New().String(),
		SourceID:     req.SourceID,
		ExternalID:   req.ExternalID,
		Amount:       req.Amount,
		Currency:     req.Currency,
		Direction:    req.Direction,
		Description:  req.Description,
		Counterparty: req.Counterparty,
		OccurredAt:   req.OccurredAt,
		RawData:      req.RawData,
		DedupKey:     key,
	}

	// 6. Insert (ON CONFLICT DO NOTHING handles races)
	inserted, err := ing.repo.Insert(ctx, &tx)
	if err != nil {
		return domain.IngestResult{Status: domain.IngestStatusError}, fmt.Errorf("inserting transaction: %w", err)
	}

	// Race condition: another goroutine inserted between our check and insert
	if !inserted {
		return domain.IngestResult{
			TransactionID: tx.ID,
			Status:        domain.IngestStatusDuplicate,
		}, nil
	}

	// 7. Cache dedup key in Redis
	if ing.redis != nil {
		if err := ing.redis.Set(ctx, "dedup:"+key, tx.ID, redisDedupTTL).Err(); err != nil {
			ing.logger.Warn().Str("key", key).Err(err).Msg("redis SET dedup failed")
		}
	}

	// 8. Return result
	return domain.IngestResult{
		TransactionID: tx.ID,
		Status:        domain.IngestStatusCreated,
	}, nil
}

// DedupKey computes the sha256 deduplication key for a source_id + external_id pair.
func DedupKey(sourceID, externalID string) string {
	h := sha256.Sum256([]byte(sourceID + externalID))
	return hex.EncodeToString(h[:])
}

// validate checks all required fields and business rules on an IngestRequest.
func validate(req domain.IngestRequest) error {
	if req.SourceID == "" {
		return fmt.Errorf("source_id is required")
	}
	if req.ExternalID == "" {
		return fmt.Errorf("external_id is required")
	}
	if !domain.ValidCurrency(req.Currency) {
		return fmt.Errorf("invalid currency code: %q", req.Currency)
	}
	if req.Direction != domain.DirectionCredit && req.Direction != domain.DirectionDebit {
		return fmt.Errorf("invalid direction: %q (must be %q or %q)", req.Direction, domain.DirectionCredit, domain.DirectionDebit)
	}
	if req.OccurredAt.After(time.Now()) {
		return fmt.Errorf("occurred_at cannot be in the future")
	}
	return nil
}
