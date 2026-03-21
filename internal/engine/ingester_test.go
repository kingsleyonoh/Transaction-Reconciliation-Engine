package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/repository"
)

// testSourceID is a fixed UUID used for all tests — inserted in setupTestDB.
const testSourceID = "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"

// setupTestDB returns a connected sqlx.DB and cleans up test data.
func setupTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://recon:recon_dev@localhost:5432/reconciliation?sslmode=disable"
	}

	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		t.Fatalf("connecting to test DB: %v", err)
	}

	// Clean up test data (order matters for FK constraints)
	_, _ = db.Exec("DELETE FROM matches WHERE gateway_tx_id IN (SELECT id FROM transactions WHERE source_id = $1)", testSourceID)
	_, _ = db.Exec("DELETE FROM discrepancies WHERE transaction_id IN (SELECT id FROM transactions WHERE source_id = $1)", testSourceID)
	_, _ = db.Exec("DELETE FROM transactions WHERE source_id = $1", testSourceID)
	_, _ = db.Exec("DELETE FROM ingestion_logs WHERE source_id = $1", testSourceID)
	_, _ = db.Exec("DELETE FROM sources WHERE id = $1", testSourceID)

	// Insert test source
	_, err = db.Exec(`
		INSERT INTO sources (id, name, source_type, config)
		VALUES ($1, 'test-source', 'gateway', '{}')
		ON CONFLICT (id) DO NOTHING`, testSourceID)
	if err != nil {
		t.Fatalf("inserting test source: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM matches WHERE gateway_tx_id IN (SELECT id FROM transactions WHERE source_id = $1)", testSourceID)
		_, _ = db.Exec("DELETE FROM discrepancies WHERE transaction_id IN (SELECT id FROM transactions WHERE source_id = $1)", testSourceID)
		_, _ = db.Exec("DELETE FROM transactions WHERE source_id = $1", testSourceID)
		_, _ = db.Exec("DELETE FROM ingestion_logs WHERE source_id = $1", testSourceID)
		_, _ = db.Exec("DELETE FROM sources WHERE id = $1", testSourceID)
		db.Close()
	})

	return db
}

// setupTestRedis returns a connected Redis client and cleans up dedup keys.
func setupTestRedis(t *testing.T) *redis.Client {
	t.Helper()

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379/0"
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parsing redis URL: %v", err)
	}

	client := redis.NewClient(opts)

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("pinging redis: %v", err)
	}

	t.Cleanup(func() {
		// Clean up any dedup keys created during tests
		keys, _ := client.Keys(ctx, "dedup:*").Result()
		if len(keys) > 0 {
			client.Del(ctx, keys...)
		}
		client.Close()
	})

	return client
}

// validRequest returns a valid IngestRequest for testing.
func validRequest() domain.IngestRequest {
	return domain.IngestRequest{
		SourceID:     testSourceID,
		ExternalID:   fmt.Sprintf("ext-%d", time.Now().UnixNano()),
		Amount:       10050, // $100.50
		Currency:     "USD",
		Direction:    domain.DirectionCredit,
		Description:  "Test payment",
		Counterparty: "Acme Corp",
		OccurredAt:   time.Now().Add(-1 * time.Hour),
		RawData:      []byte(`{"test": true}`),
	}
}

func TestIngester_HappyPath_CreatesTransaction(t *testing.T) {
	db := setupTestDB(t)
	rc := setupTestRedis(t)
	repo := repository.NewTransactionRepo(db)
	ingester := NewIngester(repo, rc, zerolog.Nop())

	req := validRequest()
	result, err := ingester.Ingest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != domain.IngestStatusCreated {
		t.Errorf("expected status %q, got %q", domain.IngestStatusCreated, result.Status)
	}
	if result.TransactionID == "" {
		t.Error("expected non-empty TransactionID")
	}

	// Verify transaction exists in DB
	tx, err := repo.FindByDedupKey(context.Background(), dedupKey(req.SourceID, req.ExternalID))
	if err != nil {
		t.Fatalf("finding transaction: %v", err)
	}
	if tx == nil {
		t.Fatal("expected transaction in DB, got nil")
	}
	if tx.Amount != req.Amount {
		t.Errorf("expected amount %d, got %d", req.Amount, tx.Amount)
	}
}

func TestIngester_Duplicate_RedisHit(t *testing.T) {
	db := setupTestDB(t)
	rc := setupTestRedis(t)
	repo := repository.NewTransactionRepo(db)
	ingester := NewIngester(repo, rc, zerolog.Nop())

	req := validRequest()

	// First ingest — should succeed
	result1, err := ingester.Ingest(context.Background(), req)
	if err != nil {
		t.Fatalf("first ingest error: %v", err)
	}
	if result1.Status != domain.IngestStatusCreated {
		t.Fatalf("first ingest: expected %q, got %q", domain.IngestStatusCreated, result1.Status)
	}

	// Second ingest — same request, should hit Redis cache
	result2, err := ingester.Ingest(context.Background(), req)
	if err != nil {
		t.Fatalf("second ingest error: %v", err)
	}
	if result2.Status != domain.IngestStatusDuplicate {
		t.Errorf("second ingest: expected %q, got %q", domain.IngestStatusDuplicate, result2.Status)
	}
	if result2.TransactionID != result1.TransactionID {
		t.Errorf("expected same TransactionID %q, got %q", result1.TransactionID, result2.TransactionID)
	}
}

func TestIngester_Duplicate_PostgresFallback(t *testing.T) {
	db := setupTestDB(t)
	rc := setupTestRedis(t)
	repo := repository.NewTransactionRepo(db)
	ingester := NewIngester(repo, rc, zerolog.Nop())

	req := validRequest()

	// First ingest
	result1, err := ingester.Ingest(context.Background(), req)
	if err != nil {
		t.Fatalf("first ingest error: %v", err)
	}

	// Clear Redis dedup key — force PostgreSQL fallback
	key := dedupKey(req.SourceID, req.ExternalID)
	rc.Del(context.Background(), "dedup:"+key)

	// Second ingest — should hit PostgreSQL fallback
	result2, err := ingester.Ingest(context.Background(), req)
	if err != nil {
		t.Fatalf("second ingest error: %v", err)
	}
	if result2.Status != domain.IngestStatusDuplicate {
		t.Errorf("expected %q, got %q", domain.IngestStatusDuplicate, result2.Status)
	}

	// Verify Redis was re-populated
	cached, err := rc.Get(context.Background(), "dedup:"+key).Result()
	if err != nil {
		t.Fatalf("redis GET after fallback: %v", err)
	}
	if cached != result1.TransactionID {
		t.Errorf("expected cached ID %q, got %q", result1.TransactionID, cached)
	}
}

func TestIngester_InvalidCurrency_RejectsWithError(t *testing.T) {
	db := setupTestDB(t)
	rc := setupTestRedis(t)
	repo := repository.NewTransactionRepo(db)
	ingester := NewIngester(repo, rc, zerolog.Nop())

	req := validRequest()
	req.Currency = "ZZZ"

	_, err := ingester.Ingest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for invalid currency, got nil")
	}
}

func TestIngester_FutureDate_RejectsWithError(t *testing.T) {
	db := setupTestDB(t)
	rc := setupTestRedis(t)
	repo := repository.NewTransactionRepo(db)
	ingester := NewIngester(repo, rc, zerolog.Nop())

	req := validRequest()
	req.OccurredAt = time.Now().Add(24 * time.Hour) // tomorrow

	_, err := ingester.Ingest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for future date, got nil")
	}
}

func TestIngester_EmptySourceID_RejectsWithError(t *testing.T) {
	db := setupTestDB(t)
	rc := setupTestRedis(t)
	repo := repository.NewTransactionRepo(db)
	ingester := NewIngester(repo, rc, zerolog.Nop())

	req := validRequest()
	req.SourceID = ""

	_, err := ingester.Ingest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for empty source_id, got nil")
	}
}

func TestIngester_EmptyExternalID_RejectsWithError(t *testing.T) {
	db := setupTestDB(t)
	rc := setupTestRedis(t)
	repo := repository.NewTransactionRepo(db)
	ingester := NewIngester(repo, rc, zerolog.Nop())

	req := validRequest()
	req.ExternalID = ""

	_, err := ingester.Ingest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for empty external_id, got nil")
	}
}

func TestIngester_InvalidDirection_RejectsWithError(t *testing.T) {
	db := setupTestDB(t)
	rc := setupTestRedis(t)
	repo := repository.NewTransactionRepo(db)
	ingester := NewIngester(repo, rc, zerolog.Nop())

	req := validRequest()
	req.Direction = "refund"

	_, err := ingester.Ingest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for invalid direction, got nil")
	}
}

func TestIngester_ZeroAmount_Accepted(t *testing.T) {
	db := setupTestDB(t)
	rc := setupTestRedis(t)
	repo := repository.NewTransactionRepo(db)
	ingester := NewIngester(repo, rc, zerolog.Nop())

	req := validRequest()
	req.Amount = 0

	result, err := ingester.Ingest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error for zero amount: %v", err)
	}
	if result.Status != domain.IngestStatusCreated {
		t.Errorf("expected %q, got %q", domain.IngestStatusCreated, result.Status)
	}
}

func TestIngester_RedisUnavailable_FallsBackToPostgres(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewTransactionRepo(db)
	ingester := NewIngester(repo, nil, zerolog.Nop()) // nil Redis client

	req := validRequest()
	result, err := ingester.Ingest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != domain.IngestStatusCreated {
		t.Errorf("expected %q, got %q", domain.IngestStatusCreated, result.Status)
	}

	// Verify it's in DB
	tx, err := repo.FindByDedupKey(context.Background(), dedupKey(req.SourceID, req.ExternalID))
	if err != nil {
		t.Fatalf("finding transaction: %v", err)
	}
	if tx == nil {
		t.Fatal("expected transaction in DB, got nil")
	}
}

func TestIngester_DedupKeyIsConsistent(t *testing.T) {
	sourceID := "source-abc"
	externalID := "ext-123"

	key1 := dedupKey(sourceID, externalID)
	key2 := dedupKey(sourceID, externalID)

	if key1 != key2 {
		t.Errorf("dedup keys should be consistent: %q != %q", key1, key2)
	}

	// Verify it's a valid sha256 hex string (64 chars)
	if len(key1) != 64 {
		t.Errorf("expected 64-char hex string, got %d chars: %q", len(key1), key1)
	}
}

// dedupKey is a test helper that computes the expected dedup key.
// The implementation should produce the same result.
func dedupKey(sourceID, externalID string) string {
	h := sha256.Sum256([]byte(sourceID + externalID))
	return hex.EncodeToString(h[:])
}
