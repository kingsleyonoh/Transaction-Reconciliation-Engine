package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// ---------------------------------------------------------------------------
// Dependency interface (Interface Segregation — wider than DiscrepancyStore)
// ---------------------------------------------------------------------------

// DiscrepancyManagerStore defines data access for the discrepancy manager.
type DiscrepancyManagerStore interface {
	Insert(ctx context.Context, d *domain.Discrepancy) error
	UpdateStatus(ctx context.Context, id, status, resolvedBy, resolutionNote string) error
	FindOpenByTransactionID(ctx context.Context, txnID string) (*domain.Discrepancy, error)
	FindOpen(ctx context.Context) ([]domain.Discrepancy, error)
	UpdateAgeAndSeverity(ctx context.Context, id string, ageDays int, severity string) error
}

// ---------------------------------------------------------------------------
// DiscrepancyManager
// ---------------------------------------------------------------------------

// DiscrepancyManager handles categorization, deduplication, resolution,
// auto-resolution, and aging of discrepancies.
type DiscrepancyManager struct {
	store             DiscrepancyManagerStore
	highThreshold     int64
	criticalThreshold int64
}

// NewDiscrepancyManager creates a DiscrepancyManager.
func NewDiscrepancyManager(store DiscrepancyManagerStore, highThreshold, criticalThreshold int64) *DiscrepancyManager {
	return &DiscrepancyManager{
		store:             store,
		highThreshold:     highThreshold,
		criticalThreshold: criticalThreshold,
	}
}

// ---------------------------------------------------------------------------
// 1. Categorize — detect amount_mismatch or date_mismatch
// ---------------------------------------------------------------------------

// Categorize compares a fuzzy-matched gateway and ledger transaction and
// returns a categorized discrepancy indicating the type of mismatch.
func (m *DiscrepancyManager) Categorize(gw, lg domain.Transaction, runID string) *domain.Discrepancy {
	now := time.Now()
	disc := &domain.Discrepancy{
		ID:            uuid.New().String(),
		TransactionID: gw.ID,
		Status:        domain.StatusOpen,
		RunID:         runID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// Determine mismatch type: amount takes priority over date.
	gwAbs := absAmount(gw.Amount)
	lgAbs := absAmount(lg.Amount)

	if gwAbs != lgAbs {
		disc.DiscrepancyType = domain.DiscrepancyAmountMismatch
		disc.ExpectedValue = strconv.FormatInt(gw.Amount, 10)
		disc.ActualValue = strconv.FormatInt(lg.Amount, 10)
	} else {
		disc.DiscrepancyType = domain.DiscrepancyDateMismatch
		disc.ExpectedValue = gw.OccurredAt.Format(time.RFC3339)
		disc.ActualValue = lg.OccurredAt.Format(time.RFC3339)
	}

	disc.Severity = m.classifySeverity(gwAbs)
	return disc
}

// ---------------------------------------------------------------------------
// 2. PreventDuplicate — check for existing open, don't create new
// ---------------------------------------------------------------------------

// PreventDuplicate checks for an existing open discrepancy for the given
// transaction. If found, returns it without inserting. If not found, creates
// and inserts a new one. Returns (discrepancy, isNew, error).
func (m *DiscrepancyManager) PreventDuplicate(
	ctx context.Context,
	txnID, runID, discType, severity string,
) (*domain.Discrepancy, bool, error) {
	// Check for existing open discrepancy
	existing, err := m.store.FindOpenByTransactionID(ctx, txnID)
	if err == nil && existing != nil {
		// Already exists — don't create duplicate
		return existing, false, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("checking existing discrepancy: %w", err)
	}

	// No existing — create new
	now := time.Now()
	disc := &domain.Discrepancy{
		ID:              uuid.New().String(),
		TransactionID:   txnID,
		DiscrepancyType: discType,
		Severity:        severity,
		Status:          domain.StatusOpen,
		RunID:           runID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := m.store.Insert(ctx, disc); err != nil {
		return nil, false, fmt.Errorf("inserting discrepancy: %w", err)
	}
	return disc, true, nil
}

// ---------------------------------------------------------------------------
// 3. Resolve — mandatory resolution_note
// ---------------------------------------------------------------------------

// Resolve marks a discrepancy as resolved. Rejects empty resolution_note.
func (m *DiscrepancyManager) Resolve(ctx context.Context, id, resolvedBy, resolutionNote string) error {
	if resolutionNote == "" {
		return fmt.Errorf("resolution_note is required — cannot silently close discrepancy")
	}
	return m.store.UpdateStatus(ctx, id, domain.StatusResolved, resolvedBy, resolutionNote)
}

// ---------------------------------------------------------------------------
// 4. AutoResolve — resolve open discrepancies for matched transactions
// ---------------------------------------------------------------------------

// AutoResolve marks open discrepancies as resolved when their transaction
// has been matched in a reconciliation run. Returns the count of resolved.
func (m *DiscrepancyManager) AutoResolve(ctx context.Context, matchedTxnIDs []string) (int, error) {
	if len(matchedTxnIDs) == 0 {
		return 0, nil
	}

	openDiscs, err := m.store.FindOpen(ctx)
	if err != nil {
		return 0, fmt.Errorf("finding open discrepancies: %w", err)
	}

	// Build lookup set for matched transaction IDs
	matched := make(map[string]bool, len(matchedTxnIDs))
	for _, id := range matchedTxnIDs {
		matched[id] = true
	}

	resolved := 0
	for _, disc := range openDiscs {
		if matched[disc.TransactionID] {
			err := m.store.UpdateStatus(ctx, disc.ID, domain.StatusResolved, "system", "auto-resolved: transaction matched in reconciliation run")
			if err != nil {
				return resolved, fmt.Errorf("auto-resolving discrepancy %s: %w", disc.ID, err)
			}
			resolved++
		}
	}
	return resolved, nil
}

// ---------------------------------------------------------------------------
// 5. AgeOpenDiscrepancies — batch update ages and escalate severity
// ---------------------------------------------------------------------------

// AgeOpenDiscrepancies calculates age for all open discrepancies and
// escalates severity: >7 days → high, >30 days → critical. Returns count updated.
func (m *DiscrepancyManager) AgeOpenDiscrepancies(ctx context.Context) (int, error) {
	openDiscs, err := m.store.FindOpen(ctx)
	if err != nil {
		return 0, fmt.Errorf("finding open discrepancies for aging: %w", err)
	}

	now := time.Now()
	updated := 0

	for _, disc := range openDiscs {
		ageDays := int(now.Sub(disc.CreatedAt).Hours() / 24)
		newSeverity := m.escalateSeverity(disc.Severity, ageDays)

		if ageDays != disc.AgeDays || newSeverity != disc.Severity {
			err := m.store.UpdateAgeAndSeverity(ctx, disc.ID, ageDays, newSeverity)
			if err != nil {
				return updated, fmt.Errorf("updating age for discrepancy %s: %w", disc.ID, err)
			}
			updated++
		}
	}
	return updated, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (m *DiscrepancyManager) classifySeverity(amountCents int64) string {
	if amountCents >= m.criticalThreshold {
		return domain.SeverityCritical
	}
	if amountCents >= m.highThreshold {
		return domain.SeverityHigh
	}
	return domain.SeverityMedium
}

func (m *DiscrepancyManager) escalateSeverity(current string, ageDays int) string {
	if ageDays > 30 {
		return domain.SeverityCritical
	}
	if ageDays > 7 && current == domain.SeverityMedium {
		return domain.SeverityHigh
	}
	if ageDays > 7 && current == domain.SeverityLow {
		return domain.SeverityHigh
	}
	return current
}

func absAmount(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
