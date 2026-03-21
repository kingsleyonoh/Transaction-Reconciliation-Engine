package engine

import (
	"sort"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// ScoredMatch holds the result of scoring a gateway–ledger pair.
type ScoredMatch struct {
	GatewayTxID string
	LedgerTxID  string
	Confidence  float64
	MatchType   string // domain.MatchType*
	MatchedBy   string // domain.MatchedBy*
	MatchRule   string // human-readable rule name
}

// Scorer evaluates a set of match rules and picks the best candidate.
type Scorer struct {
	rules         []MatchRule
	minConfidence float64
}

// NewScorer creates a Scorer with the given rules and minimum confidence threshold.
func NewScorer(rules []MatchRule, minConfidence float64) *Scorer {
	return &Scorer{
		rules:         rules,
		minConfidence: minConfidence,
	}
}

// Score evaluates all rules against a gateway–ledger pair and returns
// the highest-confidence match that meets the threshold.
func (s *Scorer) Score(gateway, ledger domain.Transaction) (ScoredMatch, bool) {
	var best MatchCandidate
	found := false

	for _, rule := range s.rules {
		candidate := rule.Evaluate(gateway, ledger)
		if !candidate.Matched {
			continue
		}
		if !found || candidate.Confidence > best.Confidence {
			best = candidate
			found = true
		}
	}

	if !found || best.Confidence < s.minConfidence {
		return ScoredMatch{}, false
	}

	return ScoredMatch{
		GatewayTxID: gateway.ID,
		LedgerTxID:  ledger.ID,
		Confidence:  best.Confidence,
		MatchType:   best.MatchType,
		MatchedBy:   classifyMatchedBy(best.Confidence),
		MatchRule:   best.MatchRule,
	}, true
}

// ScoreAll evaluates a gateway against multiple ledger transactions,
// returning all matches above threshold sorted by confidence descending.
func (s *Scorer) ScoreAll(gateway domain.Transaction, ledgers []domain.Transaction) []ScoredMatch {
	if len(ledgers) == 0 {
		return nil
	}

	var results []ScoredMatch
	for _, lg := range ledgers {
		if sm, ok := s.Score(gateway, lg); ok {
			results = append(results, sm)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Confidence > results[j].Confidence
	})

	return results
}

// classifyMatchedBy returns the MatchedBy constant based on confidence.
func classifyMatchedBy(confidence float64) string {
	if confidence >= 1.0 {
		return domain.MatchedByAutoExact
	}
	return domain.MatchedByAutoFuzzy
}
