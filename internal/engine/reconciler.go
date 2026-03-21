package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// ---------------------------------------------------------------------------
// Dependency interfaces (slim, testable)
// ---------------------------------------------------------------------------

// TransactionFetcher retrieves unmatched transactions from a data source.
type TransactionFetcher interface {
	FindUnmatched(ctx context.Context, sourceID string, from, to time.Time) ([]domain.Transaction, error)
}

// MatchStore persists confirmed matches.
type MatchStore interface {
	Insert(ctx context.Context, match *domain.Match) error
}

// DiscrepancyStore persists discrepancy records.
type DiscrepancyStore interface {
	Insert(ctx context.Context, d *domain.Discrepancy) error
}

// RunStore manages reconciliation run records.
type RunStore interface {
	Create(ctx context.Context, run *domain.ReconciliationRun) error
	Update(ctx context.Context, run *domain.ReconciliationRun) error
}

// DistributedLocker provides mutual exclusion across processes.
type DistributedLocker interface {
	Lock(ctx context.Context, key string, ttl time.Duration) (bool, error)
	Unlock(ctx context.Context, key string) error
}

// ---------------------------------------------------------------------------
// Reconciler
// ---------------------------------------------------------------------------

// Reconciler orchestrates the 4-pass matching cascade.
type Reconciler struct {
	scorer            *Scorer
	gwFetcher         TransactionFetcher
	lgFetcher         TransactionFetcher
	matchStore        MatchStore
	discStore         DiscrepancyStore
	runStore          RunStore
	locker            DistributedLocker
	highThreshold     int64
	criticalThreshold int64
}

// NewReconciler creates a Reconciler with all its dependencies.
func NewReconciler(
	scorer *Scorer,
	gwFetcher TransactionFetcher,
	lgFetcher TransactionFetcher,
	matchStore MatchStore,
	discStore DiscrepancyStore,
	runStore RunStore,
	locker DistributedLocker,
	highThreshold int64,
	criticalThreshold int64,
) *Reconciler {
	return &Reconciler{
		scorer:            scorer,
		gwFetcher:         gwFetcher,
		lgFetcher:         lgFetcher,
		matchStore:        matchStore,
		discStore:         discStore,
		runStore:          runStore,
		locker:            locker,
		highThreshold:     highThreshold,
		criticalThreshold: criticalThreshold,
	}
}

// Reconcile runs the 4-pass matching cascade for the given request.
func (r *Reconciler) Reconcile(ctx context.Context, req domain.ReconcileRequest) (domain.ReconcileResult, error) {
	start := time.Now()

	// 1. Acquire distributed lock
	_, err := r.locker.Lock(ctx, "reconcile:lock", 5*time.Minute)
	if err != nil {
		return domain.ReconcileResult{}, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer r.locker.Unlock(ctx, "reconcile:lock") //nolint:errcheck

	// 2. Create run record
	runID := uuid.New().String()
	run := &domain.ReconciliationRun{
		ID:             runID,
		StartedAt:      start,
		Status:         domain.RunStatusRunning,
		ConfigSnapshot: []byte("{}"),
		CreatedAt:      start,
		UpdatedAt:      start,
	}
	if err := r.runStore.Create(ctx, run); err != nil {
		return domain.ReconcileResult{}, fmt.Errorf("failed to create run: %w", err)
	}

	// 3. Fetch unmatched transactions
	gwSourceID := ""
	lgSourceID := ""
	if len(req.SourceIDs) >= 1 {
		gwSourceID = req.SourceIDs[0]
	}
	if len(req.SourceIDs) >= 2 {
		lgSourceID = req.SourceIDs[1]
	}

	gwTxns, err := r.gwFetcher.FindUnmatched(ctx, gwSourceID, req.DateFrom, req.DateTo)
	if err != nil {
		r.failRun(ctx, run, err)
		return domain.ReconcileResult{}, fmt.Errorf("failed to fetch gateway txns: %w", err)
	}

	lgTxns, err := r.lgFetcher.FindUnmatched(ctx, lgSourceID, req.DateFrom, req.DateTo)
	if err != nil {
		r.failRun(ctx, run, err)
		return domain.ReconcileResult{}, fmt.Errorf("failed to fetch ledger txns: %w", err)
	}

	run.TotalGateway = len(gwTxns)
	run.TotalLedger = len(lgTxns)

	// 4. Run matching cascade — each pass removes matched txns
	matchedCount := 0
	matchedGW := make(map[string]bool)
	matchedLG := make(map[string]bool)

	for _, gw := range gwTxns {
		if matchedGW[gw.ID] {
			continue
		}

		// Build candidates from unmatched ledger txns
		var candidates []domain.Transaction
		for _, lg := range lgTxns {
			if !matchedLG[lg.ID] {
				candidates = append(candidates, lg)
			}
		}

		scored := r.scorer.ScoreAll(gw, candidates)
		if len(scored) == 0 {
			continue
		}

		// Take best match
		best := scored[0]

		match := &domain.Match{
			ID:          uuid.New().String(),
			GatewayTxID: best.GatewayTxID,
			LedgerTxID:  best.LedgerTxID,
			MatchType:   best.MatchType,
			Confidence:  best.Confidence,
			MatchedBy:   best.MatchedBy,
			MatchRule:   best.MatchRule,
			RunID:       runID,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		if err := r.matchStore.Insert(ctx, match); err != nil {
			r.failRun(ctx, run, err)
			return domain.ReconcileResult{}, fmt.Errorf("failed to insert match: %w", err)
		}

		matchedGW[gw.ID] = true
		matchedLG[best.LedgerTxID] = true
		matchedCount++
	}

	// 5. Create discrepancies for unmatched transactions
	unmatchedGW := 0
	for _, gw := range gwTxns {
		if matchedGW[gw.ID] {
			continue
		}
		unmatchedGW++
		disc := &domain.Discrepancy{
			ID:              uuid.New().String(),
			TransactionID:   gw.ID,
			DiscrepancyType: domain.DiscrepancyUnmatchedGateway,
			Severity:        r.classifySeverity(gw.Amount),
			Status:          domain.StatusOpen,
			RunID:           runID,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		if err := r.discStore.Insert(ctx, disc); err != nil {
			r.failRun(ctx, run, err)
			return domain.ReconcileResult{}, fmt.Errorf("failed to insert gateway discrepancy: %w", err)
		}
	}

	unmatchedLG := 0
	for _, lg := range lgTxns {
		if matchedLG[lg.ID] {
			continue
		}
		unmatchedLG++
		disc := &domain.Discrepancy{
			ID:              uuid.New().String(),
			TransactionID:   lg.ID,
			DiscrepancyType: domain.DiscrepancyUnmatchedLedger,
			Severity:        r.classifySeverity(lg.Amount),
			Status:          domain.StatusOpen,
			RunID:           runID,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		if err := r.discStore.Insert(ctx, disc); err != nil {
			r.failRun(ctx, run, err)
			return domain.ReconcileResult{}, fmt.Errorf("failed to insert ledger discrepancy: %w", err)
		}
	}

	// 6. Update run record
	durationMs := int(time.Since(start).Milliseconds())
	now := time.Now()
	run.CompletedAt = &now
	run.Status = domain.RunStatusCompleted
	run.MatchedCount = matchedCount
	run.DiscrepancyCount = unmatchedGW + unmatchedLG
	run.DurationMs = durationMs
	if run.TotalGateway > 0 {
		run.MatchRate = float64(matchedCount) / float64(run.TotalGateway)
	}
	run.UpdatedAt = now

	if err := r.runStore.Update(ctx, run); err != nil {
		return domain.ReconcileResult{}, fmt.Errorf("failed to update run: %w", err)
	}

	return domain.ReconcileResult{
		RunID:            runID,
		Matched:          matchedCount,
		UnmatchedGateway: unmatchedGW,
		UnmatchedLedger:  unmatchedLG,
		DurationMs:       durationMs,
	}, nil
}

// classifySeverity maps an absolute amount to a severity level.
func (r *Reconciler) classifySeverity(amountCents int64) string {
	abs := amountCents
	if abs < 0 {
		abs = -abs
	}
	if abs >= r.criticalThreshold {
		return domain.SeverityCritical
	}
	if abs >= r.highThreshold {
		return domain.SeverityHigh
	}
	return domain.SeverityMedium
}

// failRun marks a run as failed with the given error.
func (r *Reconciler) failRun(ctx context.Context, run *domain.ReconciliationRun, originalErr error) {
	now := time.Now()
	run.Status = domain.RunStatusFailed
	run.ErrorMessage = originalErr.Error()
	run.CompletedAt = &now
	run.UpdatedAt = now
	_ = r.runStore.Update(ctx, run) // best-effort
}
