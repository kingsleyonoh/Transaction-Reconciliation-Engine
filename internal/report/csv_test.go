package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestWriteSettlementCSV_Structure(t *testing.T) {
	r := domain.Report{
		Summary: domain.ReportSummary{
			TotalMatched:          80,
			TotalUnmatched:        20,
			MatchRate:             0.8,
			TotalDiscrepancyValue: 150000,
		},
		AgingSummary: domain.AgingSummary{
			TotalOpen:      5,
			OpenOver7Days:  3,
			OpenOver30Days: 1,
		},
	}

	var buf bytes.Buffer
	err := WriteSettlementCSV(&buf, r)

	assert.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Settlement Report Summary")
	assert.Contains(t, output, "Total Matched,80")
	assert.Contains(t, output, "Total Unmatched,20")
	assert.Contains(t, output, "Match Rate,0.8000")
	assert.Contains(t, output, "Total Discrepancy Value (cents),150000")
	assert.Contains(t, output, "Aging Summary")
	assert.Contains(t, output, "Total Open,5")
	assert.Contains(t, output, "Open > 7 Days,3")
	assert.Contains(t, output, "Open > 30 Days,1")
}

func TestWriteSettlementCSV_WithSources(t *testing.T) {
	r := domain.Report{
		Summary: domain.ReportSummary{
			TotalMatched: 100,
		},
		PerSourceBreakdown: []domain.SourceBreakdown{
			{SourceID: "stripe", SourceName: "Stripe", Matched: 60, Unmatched: 10, MatchRate: 0.857},
			{SourceID: "paypal", SourceName: "PayPal", Matched: 40, Unmatched: 10, MatchRate: 0.8},
		},
	}

	var buf bytes.Buffer
	err := WriteSettlementCSV(&buf, r)

	assert.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Per-Source Breakdown")
	assert.Contains(t, output, "stripe,Stripe,60,10")
	assert.Contains(t, output, "paypal,PayPal,40,10")
}

func TestWriteDiscrepancyCSV_Structure(t *testing.T) {
	ts := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
	r := domain.Report{
		DiscrepancyList: []domain.Discrepancy{
			{
				ID:              "d1",
				TransactionID:   "tx1",
				DiscrepancyType: domain.DiscrepancyUnmatchedGateway,
				ExpectedValue:   "10000",
				ActualValue:     "",
				Severity:        domain.SeverityHigh,
				Status:          domain.StatusOpen,
				RunID:           "run1",
				CreatedAt:       ts,
			},
		},
		AgingSummary: domain.AgingSummary{
			TotalOpen:      1,
			OpenOver7Days:  0,
			OpenOver30Days: 0,
		},
	}

	var buf bytes.Buffer
	err := WriteDiscrepancyCSV(&buf, r)

	assert.NoError(t, err)
	output := buf.String()

	// Check header
	lines := strings.Split(strings.TrimSpace(output), "\n")
	assert.True(t, len(lines) >= 2, "Expected at least header + 1 data row")
	assert.Contains(t, lines[0], "ID,Transaction ID,Type")

	// Check data row
	assert.Contains(t, output, "d1,tx1,unmatched_gateway,10000,,high,open,run1")
	assert.Contains(t, output, "2024-03-15T10:30:00Z")

	// Check aging
	assert.Contains(t, output, "Total Open,1")
}

func TestWriteDiscrepancyCSV_EmptyList(t *testing.T) {
	r := domain.Report{
		DiscrepancyList: nil,
		AgingSummary:    domain.AgingSummary{},
	}

	var buf bytes.Buffer
	err := WriteDiscrepancyCSV(&buf, r)

	assert.NoError(t, err)
	output := buf.String()
	// Should still have header
	assert.Contains(t, output, "ID,Transaction ID,Type")
	assert.Contains(t, output, "Total Open,0")
}
