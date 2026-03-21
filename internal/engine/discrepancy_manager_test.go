package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock for DiscrepancyManagerStore
// ---------------------------------------------------------------------------

type mockDiscrepancyMgrStore struct {
	inserted         []domain.Discrepancy
	updatedStatuses  []statusUpdate
	updatedAges      []ageUpdate
	openByTxn        map[string]*domain.Discrepancy // txnID → existing open disc
	allOpen          []domain.Discrepancy
	insertErr        error
	updateStatusErr  error
	findOpenByTxnErr error
}

type statusUpdate struct {
	id, status, resolvedBy, note string
}

type ageUpdate struct {
	id       string
	ageDays  int
	severity string
}

func (m *mockDiscrepancyMgrStore) Insert(ctx context.Context, d *domain.Discrepancy) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.inserted = append(m.inserted, *d)
	return nil
}

func (m *mockDiscrepancyMgrStore) UpdateStatus(ctx context.Context, id, status, resolvedBy, note string) error {
	if m.updateStatusErr != nil {
		return m.updateStatusErr
	}
	m.updatedStatuses = append(m.updatedStatuses, statusUpdate{id, status, resolvedBy, note})
	return nil
}

func (m *mockDiscrepancyMgrStore) FindOpenByTransactionID(ctx context.Context, txnID string) (*domain.Discrepancy, error) {
	if m.findOpenByTxnErr != nil {
		return nil, m.findOpenByTxnErr
	}
	if m.openByTxn != nil {
		if d, ok := m.openByTxn[txnID]; ok {
			return d, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (m *mockDiscrepancyMgrStore) FindOpen(ctx context.Context) ([]domain.Discrepancy, error) {
	return m.allOpen, nil
}

func (m *mockDiscrepancyMgrStore) UpdateAgeAndSeverity(ctx context.Context, id string, ageDays int, severity string) error {
	m.updatedAges = append(m.updatedAges, ageUpdate{id, ageDays, severity})
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mgrDate() time.Time {
	return time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
}

func mgrTx(id string, amount int64, currency string, date time.Time) domain.Transaction {
	return domain.Transaction{
		ID:         id,
		Amount:     amount,
		Currency:   currency,
		OccurredAt: date,
	}
}

// ---------------------------------------------------------------------------
// Tests — Categorize
// ---------------------------------------------------------------------------

func TestDiscrepancyMgr_Categorize_AmountMismatch(t *testing.T) {
	mgr := NewDiscrepancyManager(nil, 100_00, 1000_00)
	d := mgrDate()
	gw := mgrTx("gw-1", 5000, "USD", d)
	lg := mgrTx("lg-1", 5200, "USD", d)

	disc := mgr.Categorize(gw, lg, "run-1")

	assert.Equal(t, domain.DiscrepancyAmountMismatch, disc.DiscrepancyType)
	assert.Equal(t, "5000", disc.ExpectedValue)
	assert.Equal(t, "5200", disc.ActualValue)
	assert.Equal(t, domain.SeverityMedium, disc.Severity)
	assert.Equal(t, domain.StatusOpen, disc.Status)
	assert.Equal(t, "gw-1", disc.TransactionID)
}

func TestDiscrepancyMgr_Categorize_DateMismatch(t *testing.T) {
	mgr := NewDiscrepancyManager(nil, 100_00, 1000_00)
	d := mgrDate()
	gw := mgrTx("gw-1", 5000, "USD", d)
	lg := mgrTx("lg-1", 5000, "USD", d.Add(48*time.Hour))

	disc := mgr.Categorize(gw, lg, "run-1")

	assert.Equal(t, domain.DiscrepancyDateMismatch, disc.DiscrepancyType)
	assert.Contains(t, disc.ExpectedValue, "2025-06-15")
	assert.Contains(t, disc.ActualValue, "2025-06-17")
}

// ---------------------------------------------------------------------------
// Tests — PreventDuplicate
// ---------------------------------------------------------------------------

func TestDiscrepancyMgr_PreventDuplicate_New(t *testing.T) {
	store := &mockDiscrepancyMgrStore{openByTxn: map[string]*domain.Discrepancy{}}
	mgr := NewDiscrepancyManager(store, 100_00, 1000_00)

	disc, isNew, err := mgr.PreventDuplicate(context.Background(), "txn-1", "run-1", domain.DiscrepancyUnmatchedGateway, domain.SeverityMedium)

	require.NoError(t, err)
	assert.True(t, isNew)
	assert.Equal(t, "txn-1", disc.TransactionID)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, domain.DiscrepancyUnmatchedGateway, store.inserted[0].DiscrepancyType)
}

func TestDiscrepancyMgr_PreventDuplicate_ExistingOpen(t *testing.T) {
	existing := &domain.Discrepancy{
		ID:              "disc-existing",
		TransactionID:   "txn-1",
		DiscrepancyType: domain.DiscrepancyUnmatchedGateway,
		Severity:        domain.SeverityMedium,
		Status:          domain.StatusOpen,
		AgeDays:         3,
		CreatedAt:       mgrDate().Add(-72 * time.Hour),
	}
	store := &mockDiscrepancyMgrStore{
		openByTxn: map[string]*domain.Discrepancy{"txn-1": existing},
	}
	mgr := NewDiscrepancyManager(store, 100_00, 1000_00)

	disc, isNew, err := mgr.PreventDuplicate(context.Background(), "txn-1", "run-2", domain.DiscrepancyUnmatchedGateway, domain.SeverityMedium)

	require.NoError(t, err)
	assert.False(t, isNew, "should NOT be new — existing open disc found")
	assert.Equal(t, "disc-existing", disc.ID)
	assert.Len(t, store.inserted, 0, "should NOT insert a duplicate")
}

// ---------------------------------------------------------------------------
// Tests — Resolve
// ---------------------------------------------------------------------------

func TestDiscrepancyMgr_Resolve_WithNote(t *testing.T) {
	store := &mockDiscrepancyMgrStore{}
	mgr := NewDiscrepancyManager(store, 100_00, 1000_00)

	err := mgr.Resolve(context.Background(), "disc-1", "user@example.com", "Manually verified in bank statement")

	require.NoError(t, err)
	require.Len(t, store.updatedStatuses, 1)
	assert.Equal(t, "disc-1", store.updatedStatuses[0].id)
	assert.Equal(t, domain.StatusResolved, store.updatedStatuses[0].status)
	assert.Equal(t, "Manually verified in bank statement", store.updatedStatuses[0].note)
}

func TestDiscrepancyMgr_Resolve_EmptyNote_Rejected(t *testing.T) {
	store := &mockDiscrepancyMgrStore{}
	mgr := NewDiscrepancyManager(store, 100_00, 1000_00)

	err := mgr.Resolve(context.Background(), "disc-1", "user@example.com", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolution_note")
	assert.Len(t, store.updatedStatuses, 0, "should NOT call UpdateStatus with empty note")
}

// ---------------------------------------------------------------------------
// Tests — AutoResolve
// ---------------------------------------------------------------------------

func TestDiscrepancyMgr_AutoResolve_MatchedTransactions(t *testing.T) {
	store := &mockDiscrepancyMgrStore{
		allOpen: []domain.Discrepancy{
			{ID: "disc-1", TransactionID: "txn-1", Status: domain.StatusOpen},
			{ID: "disc-2", TransactionID: "txn-2", Status: domain.StatusOpen},
			{ID: "disc-3", TransactionID: "txn-3", Status: domain.StatusOpen},
		},
	}
	mgr := NewDiscrepancyManager(store, 100_00, 1000_00)

	// Only txn-1 and txn-3 were matched in this run
	resolved, err := mgr.AutoResolve(context.Background(), []string{"txn-1", "txn-3"})

	require.NoError(t, err)
	assert.Equal(t, 2, resolved)
	require.Len(t, store.updatedStatuses, 2)
	for _, u := range store.updatedStatuses {
		assert.Equal(t, domain.StatusResolved, u.status)
		assert.Equal(t, "system", u.resolvedBy)
		assert.Contains(t, u.note, "auto-resolved")
	}
}

func TestDiscrepancyMgr_AutoResolve_NoOpenDiscrepancies(t *testing.T) {
	store := &mockDiscrepancyMgrStore{allOpen: []domain.Discrepancy{}}
	mgr := NewDiscrepancyManager(store, 100_00, 1000_00)

	resolved, err := mgr.AutoResolve(context.Background(), []string{"txn-1"})

	require.NoError(t, err)
	assert.Equal(t, 0, resolved)
	assert.Len(t, store.updatedStatuses, 0)
}

// ---------------------------------------------------------------------------
// Tests — AgeOpenDiscrepancies
// ---------------------------------------------------------------------------

func TestDiscrepancyMgr_Age_EscalatesAfter7Days(t *testing.T) {
	store := &mockDiscrepancyMgrStore{
		allOpen: []domain.Discrepancy{
			{
				ID:        "disc-8d",
				Severity:  domain.SeverityMedium,
				Status:    domain.StatusOpen,
				CreatedAt: time.Now().Add(-8 * 24 * time.Hour), // 8 days old
			},
		},
	}
	mgr := NewDiscrepancyManager(store, 100_00, 1000_00)

	updated, err := mgr.AgeOpenDiscrepancies(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, updated)
	require.Len(t, store.updatedAges, 1)
	assert.Equal(t, domain.SeverityHigh, store.updatedAges[0].severity)
	assert.Equal(t, 8, store.updatedAges[0].ageDays)
}

func TestDiscrepancyMgr_Age_EscalatesAfter30Days(t *testing.T) {
	store := &mockDiscrepancyMgrStore{
		allOpen: []domain.Discrepancy{
			{
				ID:        "disc-31d",
				Severity:  domain.SeverityHigh,
				Status:    domain.StatusOpen,
				CreatedAt: time.Now().Add(-31 * 24 * time.Hour), // 31 days old
			},
		},
	}
	mgr := NewDiscrepancyManager(store, 100_00, 1000_00)

	updated, err := mgr.AgeOpenDiscrepancies(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, updated)
	require.Len(t, store.updatedAges, 1)
	assert.Equal(t, domain.SeverityCritical, store.updatedAges[0].severity)
	assert.Equal(t, 31, store.updatedAges[0].ageDays)
}

// Suppress unused import warning for errors and fmt
var _ = errors.New
var _ = fmt.Sprintf
