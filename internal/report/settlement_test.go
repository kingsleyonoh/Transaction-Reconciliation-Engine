package report

import (
	"context"
	"testing"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Mock dependencies
// ---------------------------------------------------------------------------

type mockRunQuerier struct {
	runs []domain.ReconciliationRun
	err  error
}

func (m *mockRunQuerier) FindByDateRange(_ context.Context, _, _ time.Time) ([]domain.ReconciliationRun, error) {
	return m.runs, m.err
}

type mockDiscQuerier struct {
	discs []domain.Discrepancy
	err   error
}

func (m *mockDiscQuerier) FindByDateRange(_ context.Context, _, _ time.Time) ([]domain.Discrepancy, error) {
	return m.discs, m.err
}

func (m *mockDiscQuerier) CountOpenOlderThan(_ context.Context, _ int) (int, error) {
	return 0, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestGenerateSettlement_EmptyRange(t *testing.T) {
	gen := NewGenerator(
		&mockRunQuerier{runs: nil},
		&mockDiscQuerier{discs: nil},
	)

	report, err := gen.GenerateSettlement(context.Background(), domain.ReportRequest{
		DateFrom: "2024-01-01",
		DateTo:   "2024-01-31",
	})

	assert.NoError(t, err)
	assert.Equal(t, 0, report.Summary.TotalMatched)
	assert.Equal(t, 0, report.Summary.TotalUnmatched)
	assert.Equal(t, 0.0, report.Summary.MatchRate)
	assert.Equal(t, int64(0), report.Summary.TotalDiscrepancyValue)
}

func TestGenerateSettlement_SingleRun(t *testing.T) {
	gen := NewGenerator(
		&mockRunQuerier{runs: []domain.ReconciliationRun{
			{MatchedCount: 80, DiscrepancyCount: 20},
		}},
		&mockDiscQuerier{discs: nil},
	)

	report, err := gen.GenerateSettlement(context.Background(), domain.ReportRequest{
		DateFrom: "2024-01-01",
		DateTo:   "2024-01-31",
	})

	assert.NoError(t, err)
	assert.Equal(t, 80, report.Summary.TotalMatched)
	assert.Equal(t, 20, report.Summary.TotalUnmatched)
	assert.InDelta(t, 0.8, report.Summary.MatchRate, 0.001)
}

func TestGenerateSettlement_MultipleRuns(t *testing.T) {
	gen := NewGenerator(
		&mockRunQuerier{runs: []domain.ReconciliationRun{
			{MatchedCount: 50, DiscrepancyCount: 10},
			{MatchedCount: 30, DiscrepancyCount: 10},
		}},
		&mockDiscQuerier{discs: nil},
	)

	report, err := gen.GenerateSettlement(context.Background(), domain.ReportRequest{
		DateFrom: "2024-01-01",
		DateTo:   "2024-01-31",
	})

	assert.NoError(t, err)
	assert.Equal(t, 80, report.Summary.TotalMatched)
	assert.Equal(t, 20, report.Summary.TotalUnmatched)
}

func TestGenerateSettlement_AgingCalculation(t *testing.T) {
	now := time.Now()
	gen := NewGenerator(
		&mockRunQuerier{runs: nil},
		&mockDiscQuerier{discs: []domain.Discrepancy{
			{Status: domain.StatusOpen, CreatedAt: now.Add(-2 * 24 * time.Hour)},    // 2 days old
			{Status: domain.StatusOpen, CreatedAt: now.Add(-10 * 24 * time.Hour)},   // 10 days old
			{Status: domain.StatusOpen, CreatedAt: now.Add(-35 * 24 * time.Hour)},   // 35 days old
			{Status: domain.StatusResolved, CreatedAt: now.Add(-40 * 24 * time.Hour)}, // resolved, ignored
		}},
	)

	report, err := gen.GenerateSettlement(context.Background(), domain.ReportRequest{
		DateFrom: "2024-01-01",
		DateTo:   "2024-12-31",
	})

	assert.NoError(t, err)
	assert.Equal(t, 3, report.AgingSummary.TotalOpen)
	assert.Equal(t, 2, report.AgingSummary.OpenOver7Days) // 10d + 35d
	assert.Equal(t, 1, report.AgingSummary.OpenOver30Days) // 35d only
}

func TestGenerateSettlement_InvalidDateRange(t *testing.T) {
	gen := NewGenerator(
		&mockRunQuerier{},
		&mockDiscQuerier{},
	)

	_, err := gen.GenerateSettlement(context.Background(), domain.ReportRequest{
		DateFrom: "not-a-date",
		DateTo:   "2024-01-31",
	})

	assert.Error(t, err)
}

func TestGenerateDiscrepancy_IncludesDiscrepancyList(t *testing.T) {
	discs := []domain.Discrepancy{
		{ID: "d1", Status: domain.StatusOpen, CreatedAt: time.Now()},
		{ID: "d2", Status: domain.StatusOpen, CreatedAt: time.Now()},
	}
	gen := NewGenerator(
		&mockRunQuerier{runs: nil},
		&mockDiscQuerier{discs: discs},
	)

	report, err := gen.GenerateDiscrepancy(context.Background(), domain.ReportRequest{
		DateFrom: "2024-01-01",
		DateTo:   "2024-12-31",
	})

	assert.NoError(t, err)
	assert.Len(t, report.DiscrepancyList, 2)
	assert.Equal(t, "d1", report.DiscrepancyList[0].ID)
}
