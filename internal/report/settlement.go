package report

import (
	"context"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// ---------------------------------------------------------------------------
// Dependency interfaces (slim, testable)
// ---------------------------------------------------------------------------

// RunQuerier fetches reconciliation run records.
type RunQuerier interface {
	FindByDateRange(ctx context.Context, from, to time.Time) ([]domain.ReconciliationRun, error)
}

// MatchQuerier fetches match records.
type MatchQuerier interface {
	CountByRunID(ctx context.Context, runID string) (int, error)
}

// DiscrepancyQuerier fetches discrepancy records for reports.
type DiscrepancyQuerier interface {
	FindByDateRange(ctx context.Context, from, to time.Time) ([]domain.Discrepancy, error)
	CountOpenOlderThan(ctx context.Context, days int) (int, error)
}

// SourceLister lists registered sources.
type SourceLister interface {
	List(ctx context.Context) ([]domain.Source, error)
}

// ---------------------------------------------------------------------------
// Generator — implements the ReportGenerator interface used by API handlers
// ---------------------------------------------------------------------------

// Generator builds reports from repository data.
type Generator struct {
	runs   RunQuerier
	discs  DiscrepancyQuerier
}

// NewGenerator creates a report Generator.
func NewGenerator(runs RunQuerier, discs DiscrepancyQuerier) *Generator {
	return &Generator{
		runs:   runs,
		discs:  discs,
	}
}

// GenerateSettlement produces a settlement report for the given date range.
func (g *Generator) GenerateSettlement(ctx context.Context, req domain.ReportRequest) (domain.Report, error) {
	from, to, err := parseDateRange(req.DateFrom, req.DateTo)
	if err != nil {
		return domain.Report{}, err
	}

	runs, err := g.runs.FindByDateRange(ctx, from, to)
	if err != nil {
		return domain.Report{}, err
	}

	// Aggregate run stats
	totalMatched := 0
	totalUnmatched := 0
	for _, run := range runs {
		totalMatched += run.MatchedCount
		totalUnmatched += run.DiscrepancyCount
	}

	total := totalMatched + totalUnmatched
	matchRate := 0.0
	if total > 0 {
		matchRate = float64(totalMatched) / float64(total)
	}

	// Discrepancy value and aging
	discs, err := g.discs.FindByDateRange(ctx, from, to)
	if err != nil {
		return domain.Report{}, err
	}

	var totalDiscValue int64
	now := time.Now()
	aging := domain.AgingSummary{}
	for _, d := range discs {
		totalDiscValue += abs64(parseDiscrepancyValue(d))
		if d.Status == domain.StatusOpen {
			aging.TotalOpen++
			ageDays := int(now.Sub(d.CreatedAt).Hours() / 24)
			if ageDays > 30 {
				aging.OpenOver30Days++
			}
			if ageDays > 7 {
				aging.OpenOver7Days++
			}
		}
	}

	report := domain.Report{
		Summary: domain.ReportSummary{
			TotalMatched:          totalMatched,
			TotalUnmatched:        totalUnmatched,
			MatchRate:             matchRate,
			TotalDiscrepancyValue: totalDiscValue,
		},
		AgingSummary: aging,
	}

	return report, nil
}

// GenerateDiscrepancy produces a discrepancy report for the given date range.
func (g *Generator) GenerateDiscrepancy(ctx context.Context, req domain.ReportRequest) (domain.Report, error) {
	report, err := g.GenerateSettlement(ctx, req)
	if err != nil {
		return domain.Report{}, err
	}

	from, to, _ := parseDateRange(req.DateFrom, req.DateTo)
	discs, err := g.discs.FindByDateRange(ctx, from, to)
	if err != nil {
		return domain.Report{}, err
	}

	report.DiscrepancyList = discs

	return report, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func parseDateRange(fromStr, toStr string) (time.Time, time.Time, error) {
	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return from, to, nil
}

// parseDiscrepancyValue extracts an integer value from the discrepancy's
// ExpectedValue or ActualValue field.
func parseDiscrepancyValue(d domain.Discrepancy) int64 {
	if d.ExpectedValue != "" {
		// Try to parse as cents; fallback to 0
		var v int64
		for _, ch := range d.ExpectedValue {
			if ch >= '0' && ch <= '9' {
				v = v*10 + int64(ch-'0')
			}
		}
		return v
	}
	return 0
}

// BuildReport satisfies the api.ReportGenerator interface.
// It delegates to GenerateSettlement using a background context.
func (g *Generator) BuildReport(dateFrom, dateTo string) (*domain.Report, error) {
	req := domain.ReportRequest{DateFrom: dateFrom, DateTo: dateTo}
	rpt, err := g.GenerateSettlement(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return &rpt, nil
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
