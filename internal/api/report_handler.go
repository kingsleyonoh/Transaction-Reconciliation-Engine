package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// ---------------------------------------------------------------------------
// Handler-level interfaces
// ---------------------------------------------------------------------------

// ReportGenerator generates settlement or discrepancy reports.
type ReportGenerator interface {
	BuildReport(dateFrom, dateTo string) (*domain.Report, error)
}

// ---------------------------------------------------------------------------
// Static report generator (stub — delegates to real engine when available)
// ---------------------------------------------------------------------------

// staticReportGenerator returns a fixed report. It will be replaced by the
// real engine.ReportGenerator when that module is implemented.
type staticReportGenerator struct{}

func (s *staticReportGenerator) BuildReport(dateFrom, dateTo string) (*domain.Report, error) {
	return &domain.Report{
		Summary: domain.ReportSummary{
			TotalMatched:          0,
			TotalUnmatched:        0,
			MatchRate:             0,
			TotalDiscrepancyValue: 0,
		},
		PerSourceBreakdown: []domain.SourceBreakdown{},
		AgingSummary:       domain.AgingSummary{},
	}, nil
}

// NewStaticReportGenerator returns a no-op report generator stub.
func NewStaticReportGenerator() ReportGenerator {
	return &staticReportGenerator{}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// SettlementReportHandler returns an http.HandlerFunc for settlement reports.
func SettlementReportHandler(gen ReportGenerator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dateFrom := r.URL.Query().Get("date_from")
		dateTo := r.URL.Query().Get("date_to")
		format := r.URL.Query().Get("format")
		if format == "" {
			format = domain.ReportFormatJSON
		}

		if dateFrom == "" || dateTo == "" {
			RespondError(w, http.StatusBadRequest, NewValidationError("date_from and date_to are required", nil))
			return
		}

		report, err := gen.BuildReport(dateFrom, dateTo)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, NewInternalError(err.Error()))
			return
		}

		if format == domain.ReportFormatCSV {
			writeCSVReport(w, report, "settlement_report.csv")
			return
		}

		RespondJSON(w, http.StatusOK, report)
	}
}

// DiscrepancyReportHandler returns an http.HandlerFunc for discrepancy reports.
func DiscrepancyReportHandler(gen ReportGenerator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dateFrom := r.URL.Query().Get("date_from")
		dateTo := r.URL.Query().Get("date_to")
		format := r.URL.Query().Get("format")
		if format == "" {
			format = domain.ReportFormatJSON
		}

		if dateFrom == "" || dateTo == "" {
			RespondError(w, http.StatusBadRequest, NewValidationError("date_from and date_to are required", nil))
			return
		}

		report, err := gen.BuildReport(dateFrom, dateTo)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, NewInternalError(err.Error()))
			return
		}

		if format == domain.ReportFormatCSV {
			writeCSVReport(w, report, "discrepancy_report.csv")
			return
		}

		RespondJSON(w, http.StatusOK, report)
	}
}

// writeCSVReport writes a domain.Report as CSV to the response.
func writeCSVReport(w http.ResponseWriter, report *domain.Report, filename string) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Header row
	_ = writer.Write([]string{"source_id", "source_name", "matched", "unmatched", "match_rate"})

	// Data rows
	for _, b := range report.PerSourceBreakdown {
		_ = writer.Write([]string{
			b.SourceID,
			b.SourceName,
			strconv.Itoa(b.Matched),
			strconv.Itoa(b.Unmatched),
			fmt.Sprintf("%.2f", b.MatchRate),
		})
	}
}
