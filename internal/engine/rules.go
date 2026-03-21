package engine

import (
	"math"
	"strings"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// MatchRule evaluates whether two transactions should be matched.
type MatchRule interface {
	Name() string
	Evaluate(gateway, ledger domain.Transaction) MatchCandidate
}

// MatchCandidate holds the result of a single rule evaluation.
type MatchCandidate struct {
	Matched    bool
	Confidence float64
	MatchType  string // maps to domain.MatchType* constants
	MatchRule  string // human-readable rule name
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// normalizeCounterparty lowercases and trims whitespace for comparison.
func normalizeCounterparty(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// dateDiffDays returns the absolute difference in calendar days.
func dateDiffDays(a, b time.Time) int {
	aDay := a.Truncate(24 * time.Hour)
	bDay := b.Truncate(24 * time.Hour)
	diff := aDay.Sub(bDay)
	if diff < 0 {
		diff = -diff
	}
	return int(diff / (24 * time.Hour))
}

// sameCurrency checks whether two transactions use the same currency.
func sameCurrency(a, b domain.Transaction) bool {
	return strings.EqualFold(a.Currency, b.Currency)
}

// amountsMatch compares absolute amounts, handling sign reversal for refunds.
func amountsMatch(gateway, ledger domain.Transaction) bool {
	gAbs := absInt64(gateway.Amount)
	lAbs := absInt64(ledger.Amount)
	return gAbs == lAbs
}

// amountsWithinTolerance checks if amounts are within a percentage tolerance.
func amountsWithinTolerance(gateway, ledger domain.Transaction, tolerance float64) bool {
	gAbs := float64(absInt64(gateway.Amount))
	lAbs := float64(absInt64(ledger.Amount))
	maxAmt := math.Max(gAbs, 1) // avoid division by zero
	diff := math.Abs(gAbs - lAbs)
	return diff/maxAmt <= tolerance
}

func absInt64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// ---------------------------------------------------------------------------
// ExactMatchRule — confidence 1.0
// ---------------------------------------------------------------------------

// ExactMatchRule matches on same amount, currency, date, and counterparty.
type ExactMatchRule struct{}

// NewExactMatchRule creates an ExactMatchRule.
func NewExactMatchRule() *ExactMatchRule {
	return &ExactMatchRule{}
}

// Name returns the rule name.
func (r *ExactMatchRule) Name() string { return "exact_match" }

// Evaluate checks for an exact match.
func (r *ExactMatchRule) Evaluate(gateway, ledger domain.Transaction) MatchCandidate {
	noMatch := MatchCandidate{}

	if !sameCurrency(gateway, ledger) {
		return noMatch
	}
	if !amountsMatch(gateway, ledger) {
		return noMatch
	}
	if dateDiffDays(gateway.OccurredAt, ledger.OccurredAt) != 0 {
		return noMatch
	}

	// Counterparty: skip if both are empty, otherwise require match
	gCP := normalizeCounterparty(gateway.Counterparty)
	lCP := normalizeCounterparty(ledger.Counterparty)
	if gCP != "" && lCP != "" && gCP != lCP {
		return noMatch
	}

	return MatchCandidate{
		Matched:    true,
		Confidence: 1.0,
		MatchType:  domain.MatchTypeExact,
		MatchRule:  r.Name(),
	}
}

// ---------------------------------------------------------------------------
// AmountDateMatchRule — confidence 0.90
// ---------------------------------------------------------------------------

// AmountDateMatchRule matches on same amount and date within tolerance.
type AmountDateMatchRule struct {
	dateTolDays int
}

// NewAmountDateMatchRule creates an AmountDateMatchRule.
func NewAmountDateMatchRule(dateTolDays int) *AmountDateMatchRule {
	return &AmountDateMatchRule{dateTolDays: dateTolDays}
}

// Name returns the rule name.
func (r *AmountDateMatchRule) Name() string { return "amount_date_match" }

// Evaluate checks for an amount + date match.
func (r *AmountDateMatchRule) Evaluate(gateway, ledger domain.Transaction) MatchCandidate {
	noMatch := MatchCandidate{}

	if !sameCurrency(gateway, ledger) {
		return noMatch
	}
	if !amountsMatch(gateway, ledger) {
		return noMatch
	}
	if dateDiffDays(gateway.OccurredAt, ledger.OccurredAt) > r.dateTolDays {
		return noMatch
	}

	return MatchCandidate{
		Matched:    true,
		Confidence: 0.90,
		MatchType:  domain.MatchTypeFuzzyDate,
		MatchRule:  r.Name(),
	}
}

// ---------------------------------------------------------------------------
// FuzzyAmountMatchRule — confidence 0.75
// ---------------------------------------------------------------------------

// FuzzyAmountMatchRule matches on approximate amount and date within tolerance.
type FuzzyAmountMatchRule struct {
	amtTolerance float64
	dateTolDays  int
}

// NewFuzzyAmountMatchRule creates a FuzzyAmountMatchRule.
func NewFuzzyAmountMatchRule(amtTolerance float64, dateTolDays int) *FuzzyAmountMatchRule {
	return &FuzzyAmountMatchRule{
		amtTolerance: amtTolerance,
		dateTolDays:  dateTolDays,
	}
}

// Name returns the rule name.
func (r *FuzzyAmountMatchRule) Name() string { return "fuzzy_amount_match" }

// Evaluate checks for a fuzzy amount match.
func (r *FuzzyAmountMatchRule) Evaluate(gateway, ledger domain.Transaction) MatchCandidate {
	noMatch := MatchCandidate{}

	if !sameCurrency(gateway, ledger) {
		return noMatch
	}
	if !amountsWithinTolerance(gateway, ledger, r.amtTolerance) {
		return noMatch
	}
	if dateDiffDays(gateway.OccurredAt, ledger.OccurredAt) > r.dateTolDays {
		return noMatch
	}

	return MatchCandidate{
		Matched:    true,
		Confidence: 0.75,
		MatchType:  domain.MatchTypeFuzzyAmount,
		MatchRule:  r.Name(),
	}
}

// ---------------------------------------------------------------------------
// ReferenceMatchRule — confidence 0.80
// ---------------------------------------------------------------------------

// ReferenceMatchRule matches when external_id appears in the other's description.
type ReferenceMatchRule struct{}

// NewReferenceMatchRule creates a ReferenceMatchRule.
func NewReferenceMatchRule() *ReferenceMatchRule {
	return &ReferenceMatchRule{}
}

// Name returns the rule name.
func (r *ReferenceMatchRule) Name() string { return "reference_match" }

// Evaluate checks for a reference substring match.
func (r *ReferenceMatchRule) Evaluate(gateway, ledger domain.Transaction) MatchCandidate {
	noMatch := MatchCandidate{}

	gwExt := strings.ToLower(strings.TrimSpace(gateway.ExternalID))
	lgDesc := strings.ToLower(ledger.Description)
	lgExt := strings.ToLower(strings.TrimSpace(ledger.ExternalID))
	gwDesc := strings.ToLower(gateway.Description)

	if gwExt == "" && lgExt == "" {
		return noMatch
	}

	matched := false
	if gwExt != "" && strings.Contains(lgDesc, gwExt) {
		matched = true
	}
	if lgExt != "" && strings.Contains(gwDesc, lgExt) {
		matched = true
	}

	if !matched {
		return noMatch
	}

	return MatchCandidate{
		Matched:    true,
		Confidence: 0.80,
		MatchType:  domain.MatchTypeReference,
		MatchRule:  r.Name(),
	}
}
