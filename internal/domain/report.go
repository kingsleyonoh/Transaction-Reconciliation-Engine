package domain

// ReportRequest is the input for generating reports.
type ReportRequest struct {
	DateFrom       string `json:"date_from"`
	DateTo         string `json:"date_to"`
	Format         string `json:"format"`          // "json" or "csv"
	IncludeDetails bool   `json:"include_details"`
}

// Report is the output of the report generator.
type Report struct {
	Summary            ReportSummary         `json:"summary"`
	PerSourceBreakdown []SourceBreakdown     `json:"per_source_breakdown"`
	DiscrepancyList    []Discrepancy         `json:"discrepancy_list,omitempty"`
	AgingSummary       AgingSummary          `json:"aging_summary"`
}

// ReportSummary contains aggregate reconciliation stats.
type ReportSummary struct {
	TotalMatched       int     `json:"total_matched"`
	TotalUnmatched     int     `json:"total_unmatched"`
	MatchRate          float64 `json:"match_rate"`
	TotalDiscrepancyValue int64 `json:"total_discrepancy_value"` // cents
}

// SourceBreakdown shows reconciliation stats per source.
type SourceBreakdown struct {
	SourceID   string  `json:"source_id"`
	SourceName string  `json:"source_name"`
	Matched    int     `json:"matched"`
	Unmatched  int     `json:"unmatched"`
	MatchRate  float64 `json:"match_rate"`
}

// AgingSummary tracks how long discrepancies have been open.
type AgingSummary struct {
	OpenOver7Days  int `json:"open_over_7_days"`
	OpenOver30Days int `json:"open_over_30_days"`
	TotalOpen      int `json:"total_open"`
}

// ReportFormat constants.
const (
	ReportFormatJSON = "json"
	ReportFormatCSV  = "csv"
)
