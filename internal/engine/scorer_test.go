package engine

import (
	"testing"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func baseGatewayTx() domain.Transaction {
	return domain.Transaction{
		ID:          "gw-001",
		ExternalID:  "EXT-123",
		Amount:      10000, // $100.00
		Currency:    "USD",
		Direction:   "credit",
		Counterparty: "Acme Corp",
		OccurredAt:  time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC),
	}
}

func baseLedgerTx() domain.Transaction {
	return domain.Transaction{
		ID:          "lg-001",
		ExternalID:  "LG-456",
		Amount:      10000,
		Currency:    "USD",
		Direction:   "credit",
		Counterparty: "Acme Corp",
		OccurredAt:  time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC),
	}
}

func defaultRules() []MatchRule {
	return []MatchRule{
		NewExactMatchRule(),
		NewAmountDateMatchRule(2),
		NewFuzzyAmountMatchRule(0.005, 3),
		NewReferenceMatchRule(),
	}
}

// ---------------------------------------------------------------------------
// Score tests
// ---------------------------------------------------------------------------

func TestScorer_ExactMatch_ReturnsHighestConfidence(t *testing.T) {
	scorer := NewScorer(defaultRules(), 0.70)

	gw := baseGatewayTx()
	lg := baseLedgerTx()

	result, ok := scorer.Score(gw, lg)
	require.True(t, ok, "expected a match")
	assert.Equal(t, 1.0, result.Confidence)
	assert.Equal(t, domain.MatchTypeExact, result.MatchType)
	assert.Equal(t, "gw-001", result.GatewayTxID)
	assert.Equal(t, "lg-001", result.LedgerTxID)
}

func TestScorer_FuzzyMatch_AboveThreshold(t *testing.T) {
	scorer := NewScorer(defaultRules(), 0.70)

	gw := baseGatewayTx()
	lg := baseLedgerTx()
	lg.Amount = 10040 // within 0.5% tolerance of 10000
	lg.Counterparty = "Different Corp"
	lg.OccurredAt = gw.OccurredAt.Add(72 * time.Hour) // 3 days

	result, ok := scorer.Score(gw, lg)
	require.True(t, ok, "expected a fuzzy match")
	assert.Equal(t, 0.75, result.Confidence)
	assert.Equal(t, domain.MatchTypeFuzzyAmount, result.MatchType)
}

func TestScorer_BelowThreshold_ReturnsNoMatch(t *testing.T) {
	// Set threshold above fuzzy confidence (0.75)
	scorer := NewScorer(defaultRules(), 0.80)

	gw := baseGatewayTx()
	lg := baseLedgerTx()
	lg.Amount = 10040
	lg.Counterparty = "Different Corp"
	lg.OccurredAt = gw.OccurredAt.Add(72 * time.Hour)

	_, ok := scorer.Score(gw, lg)
	assert.False(t, ok, "expected no match — confidence below threshold")
}

func TestScorer_MultipleRules_PicksHighest(t *testing.T) {
	scorer := NewScorer(defaultRules(), 0.70)

	// This pair matches exact (1.0) AND amount+date (0.90)
	gw := baseGatewayTx()
	lg := baseLedgerTx()

	result, ok := scorer.Score(gw, lg)
	require.True(t, ok)
	assert.Equal(t, 1.0, result.Confidence, "should pick highest confidence")
	assert.Equal(t, domain.MatchTypeExact, result.MatchType)
}

func TestScorer_MatchedBy_AutoExact(t *testing.T) {
	scorer := NewScorer(defaultRules(), 0.70)

	gw := baseGatewayTx()
	lg := baseLedgerTx()

	result, ok := scorer.Score(gw, lg)
	require.True(t, ok)
	assert.Equal(t, domain.MatchedByAutoExact, result.MatchedBy)
}

func TestScorer_MatchedBy_AutoFuzzy(t *testing.T) {
	scorer := NewScorer(defaultRules(), 0.70)

	gw := baseGatewayTx()
	lg := baseLedgerTx()
	lg.OccurredAt = gw.OccurredAt.Add(48 * time.Hour) // 2 days apart
	lg.Counterparty = "Other"

	result, ok := scorer.Score(gw, lg)
	require.True(t, ok)
	// Best match is AmountDate (0.90) — not exact since date differs
	assert.Equal(t, domain.MatchedByAutoFuzzy, result.MatchedBy)
}

func TestScorer_NoRulesMatch_ReturnsFalse(t *testing.T) {
	scorer := NewScorer(defaultRules(), 0.70)

	gw := baseGatewayTx()
	lg := baseLedgerTx()
	lg.Amount = 99999 // completely different amount
	lg.Currency = "EUR" // different currency

	_, ok := scorer.Score(gw, lg)
	assert.False(t, ok, "expected no match when no rules fire")
}

// ---------------------------------------------------------------------------
// ScoreAll tests
// ---------------------------------------------------------------------------

func TestScoreAll_SortsDescending(t *testing.T) {
	scorer := NewScorer(defaultRules(), 0.70)

	gw := baseGatewayTx()

	lg1 := baseLedgerTx()
	lg1.ID = "lg-exact"
	// exact match → 1.0

	lg2 := baseLedgerTx()
	lg2.ID = "lg-fuzzy"
	lg2.OccurredAt = gw.OccurredAt.Add(48 * time.Hour)
	lg2.Counterparty = "Other"
	// amount+date → 0.90

	results := scorer.ScoreAll(gw, []domain.Transaction{lg2, lg1})
	require.Len(t, results, 2)
	assert.Equal(t, "lg-exact", results[0].LedgerTxID, "highest confidence first")
	assert.Equal(t, "lg-fuzzy", results[1].LedgerTxID)
	assert.True(t, results[0].Confidence >= results[1].Confidence)
}

func TestScoreAll_FiltersSubThreshold(t *testing.T) {
	scorer := NewScorer(defaultRules(), 0.80)

	gw := baseGatewayTx()

	lg1 := baseLedgerTx()
	lg1.ID = "lg-exact"
	// exact match → 1.0 ✓

	lg2 := baseLedgerTx()
	lg2.ID = "lg-fuzzy-amt"
	lg2.Amount = 10040
	lg2.Counterparty = "Different Corp"
	lg2.OccurredAt = gw.OccurredAt.Add(72 * time.Hour)
	// fuzzy → 0.75 ✗ (below 0.80 threshold)

	results := scorer.ScoreAll(gw, []domain.Transaction{lg1, lg2})
	require.Len(t, results, 1, "sub-threshold should be filtered")
	assert.Equal(t, "lg-exact", results[0].LedgerTxID)
}

func TestScoreAll_EmptyLedgers_ReturnsEmpty(t *testing.T) {
	scorer := NewScorer(defaultRules(), 0.70)
	gw := baseGatewayTx()

	results := scorer.ScoreAll(gw, []domain.Transaction{})
	assert.Empty(t, results)
}

func TestScoreAll_NilLedgers_ReturnsEmpty(t *testing.T) {
	scorer := NewScorer(defaultRules(), 0.70)
	gw := baseGatewayTx()

	results := scorer.ScoreAll(gw, nil)
	assert.Empty(t, results)
}
