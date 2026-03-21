package report

import (
	"encoding/csv"
	"fmt"
	"io"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// WriteSettlementCSV writes a settlement report as CSV.
func WriteSettlementCSV(w io.Writer, r domain.Report) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	// Summary section
	if err := cw.Write([]string{"Settlement Report Summary"}); err != nil {
		return err
	}
	if err := cw.Write([]string{"Metric", "Value"}); err != nil {
		return err
	}
	rows := [][]string{
		{"Total Matched", fmt.Sprintf("%d", r.Summary.TotalMatched)},
		{"Total Unmatched", fmt.Sprintf("%d", r.Summary.TotalUnmatched)},
		{"Match Rate", fmt.Sprintf("%.4f", r.Summary.MatchRate)},
		{"Total Discrepancy Value (cents)", fmt.Sprintf("%d", r.Summary.TotalDiscrepancyValue)},
	}
	if err := cw.WriteAll(rows); err != nil {
		return err
	}

	// Per-source breakdown
	if len(r.PerSourceBreakdown) > 0 {
		if err := cw.Write([]string{}); err != nil {
			return err
		}
		if err := cw.Write([]string{"Per-Source Breakdown"}); err != nil {
			return err
		}
		if err := cw.Write([]string{"Source ID", "Source Name", "Matched", "Unmatched", "Match Rate"}); err != nil {
			return err
		}
		for _, s := range r.PerSourceBreakdown {
			if err := cw.Write([]string{
				s.SourceID,
				s.SourceName,
				fmt.Sprintf("%d", s.Matched),
				fmt.Sprintf("%d", s.Unmatched),
				fmt.Sprintf("%.4f", s.MatchRate),
			}); err != nil {
				return err
			}
		}
	}

	// Aging summary
	if err := cw.Write([]string{}); err != nil {
		return err
	}
	if err := cw.Write([]string{"Aging Summary"}); err != nil {
		return err
	}
	if err := cw.Write([]string{"Category", "Count"}); err != nil {
		return err
	}
	agingRows := [][]string{
		{"Total Open", fmt.Sprintf("%d", r.AgingSummary.TotalOpen)},
		{"Open > 7 Days", fmt.Sprintf("%d", r.AgingSummary.OpenOver7Days)},
		{"Open > 30 Days", fmt.Sprintf("%d", r.AgingSummary.OpenOver30Days)},
	}
	return cw.WriteAll(agingRows)
}

// WriteDiscrepancyCSV writes a discrepancy report as CSV.
func WriteDiscrepancyCSV(w io.Writer, r domain.Report) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	// Discrepancy list
	if err := cw.Write([]string{
		"ID", "Transaction ID", "Type", "Expected Value", "Actual Value",
		"Severity", "Status", "Run ID", "Created At",
	}); err != nil {
		return err
	}

	for _, d := range r.DiscrepancyList {
		if err := cw.Write([]string{
			d.ID,
			d.TransactionID,
			d.DiscrepancyType,
			d.ExpectedValue,
			d.ActualValue,
			d.Severity,
			d.Status,
			d.RunID,
			d.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}); err != nil {
			return err
		}
	}

	cw.Flush()

	// Aging summary at the bottom
	if err := cw.Write([]string{}); err != nil {
		return err
	}
	if err := cw.Write([]string{"Aging Summary"}); err != nil {
		return err
	}
	if err := cw.Write([]string{"Category", "Count"}); err != nil {
		return err
	}
	agingRows := [][]string{
		{"Total Open", fmt.Sprintf("%d", r.AgingSummary.TotalOpen)},
		{"Open > 7 Days", fmt.Sprintf("%d", r.AgingSummary.OpenOver7Days)},
		{"Open > 30 Days", fmt.Sprintf("%d", r.AgingSummary.OpenOver30Days)},
	}
	return cw.WriteAll(agingRows)
}
