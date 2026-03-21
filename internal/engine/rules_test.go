package engine

import (
	"testing"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// baseTx returns a transaction with sensible defaults for rule testing.
func baseTx(sourceID, extID string, amount int64, currency, direction string, date time.Time) domain.Transaction {
	return domain.Transaction{
		ID:           "tx-" + extID,
		SourceID:     sourceID,
		ExternalID:   extID,
		Amount:       amount,
		Currency:     currency,
		Direction:    direction,
		Description:  "payment for order " + extID,
		Counterparty: "Acme Corp",
		OccurredAt:   date,
	}
}

var refDate = time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

// ---------------------------------------------------------------------------
// ExactMatchRule
// ---------------------------------------------------------------------------

func TestMatchRule_ExactMatch_Identical_Matches(t *testing.T) {
	rule := NewExactMatchRule()
	gw := baseTx("stripe", "ext-001", 10050, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10050, "USD", domain.DirectionCredit, refDate)

	result := rule.Evaluate(gw, lg)
	if !result.Matched {
		t.Fatal("expected match for identical transactions")
	}
	if result.Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %f", result.Confidence)
	}
	if result.MatchType != domain.MatchTypeExact {
		t.Errorf("expected match type %q, got %q", domain.MatchTypeExact, result.MatchType)
	}
}

func TestMatchRule_ExactMatch_DifferentDate_NoMatch(t *testing.T) {
	rule := NewExactMatchRule()
	gw := baseTx("stripe", "ext-001", 10050, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10050, "USD", domain.DirectionCredit, refDate.AddDate(0, 0, 1))

	result := rule.Evaluate(gw, lg)
	if result.Matched {
		t.Fatal("expected no match when dates differ by 1 day")
	}
}

func TestMatchRule_ExactMatch_DifferentCurrency_NoMatch(t *testing.T) {
	rule := NewExactMatchRule()
	gw := baseTx("stripe", "ext-001", 10050, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10050, "EUR", domain.DirectionCredit, refDate)

	result := rule.Evaluate(gw, lg)
	if result.Matched {
		t.Fatal("expected no match for different currencies")
	}
}

func TestMatchRule_ExactMatch_EmptyCounterparty_StillMatches(t *testing.T) {
	rule := NewExactMatchRule()
	gw := baseTx("stripe", "ext-001", 10050, "USD", domain.DirectionCredit, refDate)
	gw.Counterparty = ""
	lg := baseTx("ledger", "lg-001", 10050, "USD", domain.DirectionCredit, refDate)
	lg.Counterparty = ""

	result := rule.Evaluate(gw, lg)
	if !result.Matched {
		t.Fatal("expected match when both counterparties are empty")
	}
}

func TestMatchRule_ExactMatch_RefundSignReversal_Matches(t *testing.T) {
	rule := NewExactMatchRule()
	gw := baseTx("stripe", "ext-001", -5000, "USD", domain.DirectionDebit, refDate)
	lg := baseTx("ledger", "lg-001", 5000, "USD", domain.DirectionCredit, refDate)

	result := rule.Evaluate(gw, lg)
	if !result.Matched {
		t.Fatal("expected match for refund sign reversal (opposite directions, abs amounts equal)")
	}
}

func TestMatchRule_ExactMatch_DifferentAmount_NoMatch(t *testing.T) {
	rule := NewExactMatchRule()
	gw := baseTx("stripe", "ext-001", 10050, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10051, "USD", domain.DirectionCredit, refDate)

	result := rule.Evaluate(gw, lg)
	if result.Matched {
		t.Fatal("expected no match for different amounts")
	}
}

// ---------------------------------------------------------------------------
// AmountDateMatchRule
// ---------------------------------------------------------------------------

func TestMatchRule_AmountDate_Within2Days_Matches(t *testing.T) {
	rule := NewAmountDateMatchRule(2)
	gw := baseTx("stripe", "ext-001", 10050, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10050, "USD", domain.DirectionCredit, refDate.AddDate(0, 0, 2))

	result := rule.Evaluate(gw, lg)
	if !result.Matched {
		t.Fatal("expected match for same amount within 2-day window")
	}
	if result.Confidence != 0.90 {
		t.Errorf("expected confidence 0.90, got %f", result.Confidence)
	}
}

func TestMatchRule_AmountDate_Exactly2Days_Matches(t *testing.T) {
	rule := NewAmountDateMatchRule(2)
	gw := baseTx("stripe", "ext-001", 10050, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10050, "USD", domain.DirectionCredit, refDate.AddDate(0, 0, -2))

	result := rule.Evaluate(gw, lg)
	if !result.Matched {
		t.Fatal("expected match at exactly 2-day boundary")
	}
}

func TestMatchRule_AmountDate_3Days_NoMatch(t *testing.T) {
	rule := NewAmountDateMatchRule(2)
	gw := baseTx("stripe", "ext-001", 10050, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10050, "USD", domain.DirectionCredit, refDate.AddDate(0, 0, 3))

	result := rule.Evaluate(gw, lg)
	if result.Matched {
		t.Fatal("expected no match for 3-day difference with 2-day tolerance")
	}
}

func TestMatchRule_AmountDate_DifferentAmount_NoMatch(t *testing.T) {
	rule := NewAmountDateMatchRule(2)
	gw := baseTx("stripe", "ext-001", 10050, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10051, "USD", domain.DirectionCredit, refDate)

	result := rule.Evaluate(gw, lg)
	if result.Matched {
		t.Fatal("expected no match for different amounts")
	}
}

// ---------------------------------------------------------------------------
// FuzzyAmountMatchRule
// ---------------------------------------------------------------------------

func TestMatchRule_FuzzyAmount_WithinTolerance_Matches(t *testing.T) {
	rule := NewFuzzyAmountMatchRule(0.005, 3) // 0.5% tolerance, 3 days
	gw := baseTx("stripe", "ext-001", 10000, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10050, "USD", domain.DirectionCredit, refDate) // 0.5% diff

	result := rule.Evaluate(gw, lg)
	if !result.Matched {
		t.Fatal("expected match for amount within 0.5% tolerance")
	}
	if result.Confidence != 0.75 {
		t.Errorf("expected confidence 0.75, got %f", result.Confidence)
	}
}

func TestMatchRule_FuzzyAmount_ExceedsTolerance_NoMatch(t *testing.T) {
	rule := NewFuzzyAmountMatchRule(0.005, 3)
	gw := baseTx("stripe", "ext-001", 10000, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10200, "USD", domain.DirectionCredit, refDate) // 2% diff

	result := rule.Evaluate(gw, lg)
	if result.Matched {
		t.Fatal("expected no match for amount exceeding 0.5% tolerance")
	}
}

func TestMatchRule_FuzzyAmount_DateWithin3_Matches(t *testing.T) {
	rule := NewFuzzyAmountMatchRule(0.005, 3)
	gw := baseTx("stripe", "ext-001", 10000, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10000, "USD", domain.DirectionCredit, refDate.AddDate(0, 0, 3))

	result := rule.Evaluate(gw, lg)
	if !result.Matched {
		t.Fatal("expected match for amount within tolerance and date within 3 days")
	}
}

func TestMatchRule_FuzzyAmount_CrossCurrency_NoMatch(t *testing.T) {
	rule := NewFuzzyAmountMatchRule(0.005, 3)
	gw := baseTx("stripe", "ext-001", 10000, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10000, "EUR", domain.DirectionCredit, refDate)

	result := rule.Evaluate(gw, lg)
	if result.Matched {
		t.Fatal("expected no match for cross-currency")
	}
}

// ---------------------------------------------------------------------------
// ReferenceMatchRule
// ---------------------------------------------------------------------------

func TestMatchRule_Reference_ExternalIDInDescription_Matches(t *testing.T) {
	rule := NewReferenceMatchRule()
	gw := baseTx("stripe", "ch_abc123", 10050, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10050, "USD", domain.DirectionCredit, refDate)
	lg.Description = "Stripe charge ch_abc123 for order 42"

	result := rule.Evaluate(gw, lg)
	if !result.Matched {
		t.Fatal("expected match when external_id appears in description")
	}
	if result.Confidence != 0.80 {
		t.Errorf("expected confidence 0.80, got %f", result.Confidence)
	}
}

func TestMatchRule_Reference_NoSubstringMatch_NoMatch(t *testing.T) {
	rule := NewReferenceMatchRule()
	gw := baseTx("stripe", "ch_abc123", 10050, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10050, "USD", domain.DirectionCredit, refDate)
	lg.Description = "Payment received for invoice 999"

	result := rule.Evaluate(gw, lg)
	if result.Matched {
		t.Fatal("expected no match when external_id not in description")
	}
}

func TestMatchRule_Reference_CaseInsensitive_Matches(t *testing.T) {
	rule := NewReferenceMatchRule()
	gw := baseTx("stripe", "CH_ABC123", 10050, "USD", domain.DirectionCredit, refDate)
	lg := baseTx("ledger", "lg-001", 10050, "USD", domain.DirectionCredit, refDate)
	lg.Description = "stripe charge ch_abc123 for order 42"

	result := rule.Evaluate(gw, lg)
	if !result.Matched {
		t.Fatal("expected case-insensitive match")
	}
}
