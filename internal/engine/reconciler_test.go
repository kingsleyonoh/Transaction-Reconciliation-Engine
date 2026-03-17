package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type mockTransactionFetcher struct {
	txns []domain.Transaction
	err  error
}

func (m *mockTransactionFetcher) FindUnmatched(ctx context.Context, sourceID string, from, to time.Time) ([]domain.Transaction, error) {
	return m.txns, m.err
}

type mockMatchStore struct {
	inserted []domain.Match
}

func (m *mockMatchStore) Insert(ctx context.Context, match *domain.Match) error {
	m.inserted = append(m.inserted, *match)
	return nil
}

type mockDiscrepancyStore struct {
	inserted []domain.Discrepancy
}

func (m *mockDiscrepancyStore) Insert(ctx context.Context, d *domain.Discrepancy) error {
	m.inserted = append(m.inserted, *d)
	return nil
}

type mockRunStore struct {
	created *domain.ReconciliationRun
	updated *domain.ReconciliationRun
}

func (m *mockRunStore) Create(ctx context.Context, run *domain.ReconciliationRun) error {
	cp := *run
	m.created = &cp
	return nil
}

func (m *mockRunStore) Update(ctx context.Context, run *domain.ReconciliationRun) error {
	cp := *run
	m.updated = &cp
	return nil
}

type mockLocker struct {
	locked   bool
	unlocked bool
	lockErr  error
}

func (m *mockLocker) Lock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if m.lockErr != nil {
		return false, m.lockErr
	}
	m.locked = true
	return true, nil
}

func (m *mockLocker) Unlock(ctx context.Context, key string) error {
	m.unlocked = true
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func reconcilerTx(id string, amount int64, currency string, date time.Time, counterparty string) domain.Transaction {
	return domain.Transaction{
		ID:           id,
		SourceID:     "src-1",
		ExternalID:   id,
		Amount:       amount,
		Currency:     currency,
		Direction:    domain.DirectionCredit,
		Counterparty: counterparty,
		OccurredAt:   date,
	}
}

func newTestReconciler(
	gwFetcher *mockTransactionFetcher,
	lgFetcher *mockTransactionFetcher,
	matchStore *mockMatchStore,
	discStore *mockDiscrepancyStore,
	runStore *mockRunStore,
	locker *mockLocker,
) *Reconciler {
	rules := []MatchRule{
		NewExactMatchRule(),
		NewAmountDateMatchRule(2),
		NewFuzzyAmountMatchRule(0.005, 3),
		NewReferenceMatchRule(),
	}
	scorer := NewScorer(rules, 0.70)
	return NewReconciler(
		scorer,
		gwFetcher,
		lgFetcher,
		matchStore,
		discStore,
		runStore,
		locker,
		100_00,   // highThreshold = $100
		1000_00,  // criticalThreshold = $1000
	)
}

func defaultDate() time.Time {
	return time.Date(2025, 3, 15, 12, 0, 0, 0, time.UTC)
}

func defaultReq() domain.ReconcileRequest {
	return domain.ReconcileRequest{
		DateFrom:  defaultDate().Add(-24 * time.Hour),
		DateTo:    defaultDate().Add(24 * time.Hour),
		SourceIDs: []string{"src-1"},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestReconcile_ExactMatchFound(t *testing.T) {
	d := defaultDate()
	gw := &mockTransactionFetcher{txns: []domain.Transaction{reconcilerTx("gw-1", 5000, "USD", d, "ACME Inc")}}
	lg := &mockTransactionFetcher{txns: []domain.Transaction{reconcilerTx("lg-1", 5000, "USD", d, "ACME Inc")}}
	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}
	lk := &mockLocker{}

	rec := newTestReconciler(gw, lg, ms, ds, rs, lk)
	result, err := rec.Reconcile(context.Background(), defaultReq())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Matched)
	assert.Equal(t, 0, result.UnmatchedGateway)
	assert.Equal(t, 0, result.UnmatchedLedger)
	require.Len(t, ms.inserted, 1)
	assert.Equal(t, "gw-1", ms.inserted[0].GatewayTxID)
	assert.Equal(t, "lg-1", ms.inserted[0].LedgerTxID)
	assert.Equal(t, 1.0, ms.inserted[0].Confidence)
	assert.Len(t, ds.inserted, 0)
}

func TestReconcile_FuzzyMatchFallback(t *testing.T) {
	d := defaultDate()
	gw := &mockTransactionFetcher{txns: []domain.Transaction{reconcilerTx("gw-1", 5000, "USD", d, "")}}
	lg := &mockTransactionFetcher{txns: []domain.Transaction{reconcilerTx("lg-1", 5000, "USD", d.Add(48*time.Hour), "")}}
	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}
	lk := &mockLocker{}

	rec := newTestReconciler(gw, lg, ms, ds, rs, lk)
	result, err := rec.Reconcile(context.Background(), defaultReq())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Matched)
	require.Len(t, ms.inserted, 1)
	assert.Equal(t, domain.MatchedByAutoFuzzy, ms.inserted[0].MatchedBy)
	require.True(t, ms.inserted[0].Confidence < 1.0, "expected confidence < 1.0")
}

func TestReconcile_NoMatch_CreatesDiscrepancy(t *testing.T) {
	d := defaultDate()
	gw := &mockTransactionFetcher{txns: []domain.Transaction{reconcilerTx("gw-1", 5000, "USD", d, "ACME")}}
	lg := &mockTransactionFetcher{txns: []domain.Transaction{reconcilerTx("lg-1", 9999, "EUR", d, "OTHER")}}
	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}
	lk := &mockLocker{}

	rec := newTestReconciler(gw, lg, ms, ds, rs, lk)
	result, err := rec.Reconcile(context.Background(), defaultReq())

	require.NoError(t, err)
	assert.Equal(t, 0, result.Matched)
	assert.Equal(t, 1, result.UnmatchedGateway)
	assert.Equal(t, 1, result.UnmatchedLedger)
	require.True(t, len(ds.inserted) >= 1, "expected at least one discrepancy")
	// Verify at least one gateway and one ledger discrepancy exist
	var hasGW, hasLG bool
	for _, d := range ds.inserted {
		if d.DiscrepancyType == domain.DiscrepancyUnmatchedGateway {
			hasGW = true
		}
		if d.DiscrepancyType == domain.DiscrepancyUnmatchedLedger {
			hasLG = true
		}
	}
	assert.True(t, hasGW, "expected unmatched_gateway discrepancy")
	assert.True(t, hasLG, "expected unmatched_ledger discrepancy")
}

func TestReconcile_CascadeRemovesMatched(t *testing.T) {
	d := defaultDate()
	// gw-1 exact-matches lg-1; gw-2 should NOT match lg-1 again
	gw := &mockTransactionFetcher{txns: []domain.Transaction{
		reconcilerTx("gw-1", 5000, "USD", d, "ACME"),
		reconcilerTx("gw-2", 5000, "USD", d, "ACME"),
	}}
	lg := &mockTransactionFetcher{txns: []domain.Transaction{
		reconcilerTx("lg-1", 5000, "USD", d, "ACME"),
	}}
	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}
	lk := &mockLocker{}

	rec := newTestReconciler(gw, lg, ms, ds, rs, lk)
	result, err := rec.Reconcile(context.Background(), defaultReq())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Matched)
	assert.Equal(t, 1, result.UnmatchedGateway) // gw-2 left over
	require.Len(t, ms.inserted, 1)
}

func TestReconcile_SeverityClassification(t *testing.T) {
	d := defaultDate()
	// Amount 150_00 ($150) → above highThreshold(100_00), below criticalThreshold(1000_00) → high
	gw := &mockTransactionFetcher{txns: []domain.Transaction{reconcilerTx("gw-1", 150_00, "USD", d, "ACME")}}
	lg := &mockTransactionFetcher{txns: []domain.Transaction{}} // no match
	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}
	lk := &mockLocker{}

	rec := newTestReconciler(gw, lg, ms, ds, rs, lk)
	_, err := rec.Reconcile(context.Background(), defaultReq())

	require.NoError(t, err)
	require.Len(t, ds.inserted, 1)
	assert.Equal(t, domain.SeverityHigh, ds.inserted[0].Severity)
}

func TestReconcile_LockBlocked(t *testing.T) {
	gw := &mockTransactionFetcher{txns: nil}
	lg := &mockTransactionFetcher{txns: nil}
	lk := &mockLocker{lockErr: errors.New("lock held")}

	rec := newTestReconciler(gw, lg, &mockMatchStore{}, &mockDiscrepancyStore{}, &mockRunStore{}, lk)
	_, err := rec.Reconcile(context.Background(), defaultReq())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "lock")
}

func TestReconcile_EmptyTransactions(t *testing.T) {
	gw := &mockTransactionFetcher{txns: []domain.Transaction{}}
	lg := &mockTransactionFetcher{txns: []domain.Transaction{}}
	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}
	lk := &mockLocker{}

	rec := newTestReconciler(gw, lg, ms, ds, rs, lk)
	result, err := rec.Reconcile(context.Background(), defaultReq())

	require.NoError(t, err)
	assert.Equal(t, 0, result.Matched)
	assert.Equal(t, 0, result.UnmatchedGateway)
	assert.Equal(t, 0, result.UnmatchedLedger)
	assert.Len(t, ms.inserted, 0)
	assert.Len(t, ds.inserted, 0)
}

func TestReconcile_RunRecordUpdated(t *testing.T) {
	d := defaultDate()
	gw := &mockTransactionFetcher{txns: []domain.Transaction{reconcilerTx("gw-1", 5000, "USD", d, "ACME")}}
	lg := &mockTransactionFetcher{txns: []domain.Transaction{reconcilerTx("lg-1", 5000, "USD", d, "ACME")}}
	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}
	lk := &mockLocker{}

	rec := newTestReconciler(gw, lg, ms, ds, rs, lk)
	_, err := rec.Reconcile(context.Background(), defaultReq())

	require.NoError(t, err)
	require.NotNil(t, rs.created)
	assert.Equal(t, domain.RunStatusRunning, rs.created.Status)
	require.NotNil(t, rs.updated)
	assert.Equal(t, domain.RunStatusCompleted, rs.updated.Status)
	assert.Equal(t, 1, rs.updated.MatchedCount)
	require.True(t, rs.updated.DurationMs >= 0, "expected non-negative duration")
	assert.True(t, lk.unlocked)
}
