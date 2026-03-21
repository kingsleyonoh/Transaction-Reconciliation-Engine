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
// Integration tests — full reconciler → scorer → rules pipeline
// ---------------------------------------------------------------------------

func integrationTx(id string, amount int64, currency string, date time.Time, counterparty, extID, desc string) domain.Transaction {
	return domain.Transaction{
		ID:           id,
		SourceID:     "src-int",
		ExternalID:   extID,
		Amount:       amount,
		Currency:     currency,
		Direction:    domain.DirectionCredit,
		Counterparty: counterparty,
		Description:  desc,
		OccurredAt:   date,
	}
}

func intDate() time.Time {
	return time.Date(2025, 6, 10, 14, 0, 0, 0, time.UTC)
}

func intReq() domain.ReconcileRequest {
	return domain.ReconcileRequest{
		DateFrom:  intDate().Add(-7 * 24 * time.Hour),
		DateTo:    intDate().Add(7 * 24 * time.Hour),
		SourceIDs: []string{"src-int"},
	}
}

func newIntegrationReconciler(
	gwTxns, lgTxns []domain.Transaction,
	ms *mockMatchStore,
	ds *mockDiscrepancyStore,
	rs *mockRunStore,
	lk *mockLocker,
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
		&mockTransactionFetcher{txns: gwTxns},
		&mockTransactionFetcher{txns: lgTxns},
		ms, ds, rs, lk,
		100_00,  // highThreshold = $100
		1000_00, // criticalThreshold = $1000
	)
}

// --- Test 1: Exact match end-to-end ---

func TestIntegration_ExactMatchFound(t *testing.T) {
	d := intDate()
	gw := []domain.Transaction{integrationTx("gw-e1", 5000, "USD", d, "ACME Inc", "ext-1", "Payment")}
	lg := []domain.Transaction{integrationTx("lg-e1", 5000, "USD", d, "ACME Inc", "ext-2", "Receipt")}
	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}
	lk := &mockLocker{}

	rec := newIntegrationReconciler(gw, lg, ms, ds, rs, lk)
	result, err := rec.Reconcile(context.Background(), intReq())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Matched)
	assert.Equal(t, 0, result.UnmatchedGateway)
	assert.Equal(t, 0, result.UnmatchedLedger)
	require.Len(t, ms.inserted, 1)
	assert.Equal(t, 1.0, ms.inserted[0].Confidence)
	assert.Equal(t, domain.MatchTypeExact, ms.inserted[0].MatchType)
	assert.Equal(t, domain.MatchedByAutoExact, ms.inserted[0].MatchedBy)
	assert.Len(t, ds.inserted, 0)
}

// --- Test 2: Fuzzy amount — rounding difference within 0.5% tolerance ---

func TestIntegration_FuzzyAmount_RoundingDifference(t *testing.T) {
	d := intDate()
	// $50.00 vs $50.24 = 0.48% difference, within 0.5% tolerance
	gw := []domain.Transaction{integrationTx("gw-fa1", 5000, "USD", d, "", "ext-1", "Payment")}
	lg := []domain.Transaction{integrationTx("lg-fa1", 5024, "USD", d, "", "ext-2", "Receipt")}
	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}
	lk := &mockLocker{}

	rec := newIntegrationReconciler(gw, lg, ms, ds, rs, lk)
	result, err := rec.Reconcile(context.Background(), intReq())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Matched)
	require.Len(t, ms.inserted, 1)
	assert.Equal(t, 0.75, ms.inserted[0].Confidence)
	assert.Equal(t, domain.MatchTypeFuzzyAmount, ms.inserted[0].MatchType)
	assert.Equal(t, domain.MatchedByAutoFuzzy, ms.inserted[0].MatchedBy)
	assert.Len(t, ds.inserted, 0)
}

// --- Test 3: Fuzzy date — date drift of 2 days ---

func TestIntegration_FuzzyDate_DateDrift(t *testing.T) {
	d := intDate()
	// Same amount, 2-day date drift → AmountDateMatchRule (confidence 0.90)
	gw := []domain.Transaction{integrationTx("gw-fd1", 7500, "EUR", d, "", "ext-1", "Payment")}
	lg := []domain.Transaction{integrationTx("lg-fd1", 7500, "EUR", d.Add(48*time.Hour), "", "ext-2", "Receipt")}
	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}
	lk := &mockLocker{}

	rec := newIntegrationReconciler(gw, lg, ms, ds, rs, lk)
	result, err := rec.Reconcile(context.Background(), intReq())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Matched)
	require.Len(t, ms.inserted, 1)
	assert.Equal(t, 0.90, ms.inserted[0].Confidence)
	assert.Equal(t, domain.MatchTypeFuzzyDate, ms.inserted[0].MatchType)
	assert.Equal(t, domain.MatchedByAutoFuzzy, ms.inserted[0].MatchedBy)
}

// --- Test 4: Reference match — external_id substring in description ---

func TestIntegration_ReferenceMatch_ExternalIDInDescription(t *testing.T) {
	d := intDate()
	// GW external_id matches in LG description, but amounts differ significantly
	gw := []domain.Transaction{integrationTx("gw-ref1", 3000, "USD", d, "", "TXN-12345", "Gateway payment")}
	lg := []domain.Transaction{integrationTx("lg-ref1", 8000, "USD", d, "", "lg-ext-1", "Payment for TXN-12345 order")}
	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}
	lk := &mockLocker{}

	rec := newIntegrationReconciler(gw, lg, ms, ds, rs, lk)
	result, err := rec.Reconcile(context.Background(), intReq())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Matched)
	require.Len(t, ms.inserted, 1)
	assert.Equal(t, 0.80, ms.inserted[0].Confidence)
	assert.Equal(t, domain.MatchTypeReference, ms.inserted[0].MatchType)
	assert.Equal(t, "reference_match", ms.inserted[0].MatchRule)
}

// --- Test 5: No match → discrepancies created ---

func TestIntegration_NoMatch_CreatesDiscrepancies(t *testing.T) {
	d := intDate()
	// Different amounts AND different currencies — no rule can match
	gw := []domain.Transaction{integrationTx("gw-nm1", 10000, "USD", d, "ACME", "ext-1", "Payment")}
	lg := []domain.Transaction{integrationTx("lg-nm1", 20000, "EUR", d, "OTHER", "ext-2", "Receipt")}
	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}
	lk := &mockLocker{}

	rec := newIntegrationReconciler(gw, lg, ms, ds, rs, lk)
	result, err := rec.Reconcile(context.Background(), intReq())

	require.NoError(t, err)
	assert.Equal(t, 0, result.Matched)
	assert.Equal(t, 1, result.UnmatchedGateway)
	assert.Equal(t, 1, result.UnmatchedLedger)
	require.Len(t, ds.inserted, 2)

	var hasGW, hasLG bool
	for _, disc := range ds.inserted {
		assert.Equal(t, domain.StatusOpen, disc.Status)
		if disc.DiscrepancyType == domain.DiscrepancyUnmatchedGateway {
			hasGW = true
			assert.Equal(t, "gw-nm1", disc.TransactionID)
		}
		if disc.DiscrepancyType == domain.DiscrepancyUnmatchedLedger {
			hasLG = true
			assert.Equal(t, "lg-nm1", disc.TransactionID)
		}
	}
	assert.True(t, hasGW, "expected unmatched_gateway discrepancy")
	assert.True(t, hasLG, "expected unmatched_ledger discrepancy")
}

// --- Test 6: Refund sign reversal matches correctly ---

func TestIntegration_RefundSignReversal_MatchesCorrectly(t *testing.T) {
	d := intDate()
	// GW +5000 (credit), LG -5000 (debit) — absolute amounts match
	gwTx := integrationTx("gw-ref-rev1", 5000, "USD", d, "ACME", "ext-1", "Payment")
	gwTx.Direction = domain.DirectionCredit

	lgTx := integrationTx("lg-ref-rev1", -5000, "USD", d, "ACME", "ext-2", "Refund")
	lgTx.Direction = domain.DirectionDebit

	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}
	lk := &mockLocker{}

	rec := newIntegrationReconciler(
		[]domain.Transaction{gwTx},
		[]domain.Transaction{lgTx},
		ms, ds, rs, lk,
	)
	result, err := rec.Reconcile(context.Background(), intReq())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Matched)
	assert.Equal(t, 0, result.UnmatchedGateway)
	assert.Equal(t, 0, result.UnmatchedLedger)
	require.Len(t, ms.inserted, 1)
	assert.Equal(t, 1.0, ms.inserted[0].Confidence, "sign-reversed match should still be exact")
}

// --- Test 7: Cross-currency never matches ---

func TestIntegration_CrossCurrency_NeverMatches(t *testing.T) {
	d := intDate()
	// Same absolute amount, same date, same counterparty — BUT different currency
	gw := []domain.Transaction{integrationTx("gw-cc1", 5000, "USD", d, "ACME", "ext-1", "Payment")}
	lg := []domain.Transaction{integrationTx("lg-cc1", 5000, "EUR", d, "ACME", "ext-2", "Receipt")}
	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}
	lk := &mockLocker{}

	rec := newIntegrationReconciler(gw, lg, ms, ds, rs, lk)
	result, err := rec.Reconcile(context.Background(), intReq())

	require.NoError(t, err)
	assert.Equal(t, 0, result.Matched, "cross-currency should never match")
	assert.Equal(t, 1, result.UnmatchedGateway)
	assert.Equal(t, 1, result.UnmatchedLedger)
	require.Len(t, ms.inserted, 0)
	require.Len(t, ds.inserted, 2)
}

// --- Test 8: Concurrent run prevention — lock error ---

func TestIntegration_ConcurrentRunPrevention_LockError(t *testing.T) {
	lk := &mockLocker{lockErr: errors.New("lock held by another process")}
	ms := &mockMatchStore{}
	ds := &mockDiscrepancyStore{}
	rs := &mockRunStore{}

	rec := newIntegrationReconciler(nil, nil, ms, ds, rs, lk)
	_, err := rec.Reconcile(context.Background(), intReq())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "lock")
	assert.Len(t, ms.inserted, 0, "no matches should be created when locked")
	assert.Len(t, ds.inserted, 0, "no discrepancies should be created when locked")
}
