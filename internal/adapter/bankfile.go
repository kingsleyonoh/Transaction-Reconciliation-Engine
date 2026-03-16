package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// BankFileAdapter implements SourceAdapter for bank statement files (MT940, CAMT.053).
type BankFileAdapter struct{}

// NewBankFileAdapter creates a new BankFileAdapter.
func NewBankFileAdapter() *BankFileAdapter {
	return &BankFileAdapter{}
}

// Name returns the adapter identifier.
func (a *BankFileAdapter) Name() string { return "bankfile" }

// FetchTransactions is not supported — bank files are uploaded, not fetched.
func (a *BankFileAdapter) FetchTransactions(_ context.Context, _ FetchRequest) (*FetchResult, error) {
	return nil, ErrNotSupported
}

// ParseFile detects the file format and delegates to the appropriate parser.
func (a *BankFileAdapter) ParseFile(ctx context.Context, data []byte, sourceID string) (*FetchResult, error) {
	content := string(data)

	// Detect MT940 format by looking for characteristic tags.
	if isMT940(content) {
		return a.parseMT940(content, sourceID)
	}

	return nil, fmt.Errorf("unsupported bank file format")
}

// isMT940 checks if the content looks like an MT940 bank statement.
func isMT940(content string) bool {
	return strings.Contains(content, ":20:") && strings.Contains(content, ":60F:")
}

// mt940Statement represents a single MT940 bank statement block.
type mt940Statement struct {
	currency     string
	transactions []mt940Transaction
}

// mt940Transaction represents a single parsed MT940 transaction entry.
type mt940Transaction struct {
	date         time.Time
	direction    string
	amount       int64
	reference    string
	description  string
	counterparty string
	rawField61   string
	rawField86   string
}

// parseMT940 parses MT940 bank statement data into canonical IngestRequests.
func (a *BankFileAdapter) parseMT940(content string, sourceID string) (*FetchResult, error) {
	// Normalize line endings to \n.
	content = strings.ReplaceAll(content, "\r\n", "\n")

	// Split into statement blocks. Each statement starts with {4: or begins the file.
	blocks := splitMT940Blocks(content)
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no MT940 statement blocks found")
	}

	var allTxns []domain.IngestRequest
	var warnings []string

	for blockIdx, block := range blocks {
		stmt, blockWarnings, err := parseMT940Block(block)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("block %d: %s", blockIdx, err.Error()))
			continue
		}
		warnings = append(warnings, blockWarnings...)

		for _, tx := range stmt.transactions {
			rawData, _ := json.Marshal(map[string]string{
				"field_61": tx.rawField61,
				"field_86": tx.rawField86,
			})

			allTxns = append(allTxns, domain.IngestRequest{
				SourceID:     sourceID,
				ExternalID:   tx.reference,
				Amount:       tx.amount,
				Currency:     stmt.currency,
				Direction:    tx.direction,
				Description:  tx.description,
				Counterparty: tx.counterparty,
				OccurredAt:   tx.date,
				RawData:      rawData,
			})
		}
	}

	if len(allTxns) == 0 && len(warnings) > 0 {
		return &FetchResult{
			TotalRecords: 0,
			Warnings:     warnings,
		}, fmt.Errorf("failed to parse any transactions: %s", strings.Join(warnings, "; "))
	}

	return &FetchResult{
		Transactions: allTxns,
		TotalRecords: len(allTxns),
		Warnings:     warnings,
	}, nil
}

// splitMT940Blocks splits content into individual statement blocks delimited by {4: ... -}.
func splitMT940Blocks(content string) []string {
	var blocks []string
	parts := strings.Split(content, "{4:")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Remove trailing -} if present.
		if idx := strings.Index(part, "-}"); idx != -1 {
			part = part[:idx]
		}
		blocks = append(blocks, part)
	}
	return blocks
}

// parseMT940Block parses a single MT940 statement block.
func parseMT940Block(block string) (*mt940Statement, []string, error) {
	lines := strings.Split(block, "\n")
	stmt := &mt940Statement{}
	var warnings []string

	var field61Lines []string
	var field86Lines []string
	var pendingTx *mt940Transaction

	flushTx := func() {
		if pendingTx != nil {
			// Parse field 86 for details.
			if len(field86Lines) > 0 {
				raw86 := strings.Join(field86Lines, "")
				pendingTx.rawField86 = raw86
				pendingTx.description, pendingTx.counterparty = parseMT940Field86(raw86)
			}
			pendingTx.rawField61 = strings.Join(field61Lines, "")
			stmt.transactions = append(stmt.transactions, *pendingTx)
			pendingTx = nil
			field61Lines = nil
			field86Lines = nil
		}
	}

	for _, line := range lines {
		line = strings.TrimRight(line, "\r ")

		switch {
		case strings.HasPrefix(line, ":60F:"):
			// Opening balance — extract currency.
			// Format: :60F:C260228EUR5000,00
			val := line[5:]
			if len(val) >= 10 {
				stmt.currency = val[7:10] // Currency code starts at position 7
			}

		case strings.HasPrefix(line, ":61:"):
			flushTx()
			val := line[4:]
			field61Lines = []string{val}
			tx, err := parseMT940Field61(val)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("field 61 parse error: %s", err.Error()))
				pendingTx = nil
				continue
			}
			pendingTx = tx

		case strings.HasPrefix(line, ":86:"):
			val := line[4:]
			field86Lines = []string{val}

		case strings.HasPrefix(line, ":"):
			// Other standard fields — ignore for now, flush any pending tx.
			if !strings.HasPrefix(line, ":86:") && !strings.HasPrefix(line, ":61:") {
				// Don't flush on :62F: etc, those come after last transaction.
			}

		default:
			// Continuation line or unexpected content.
			if pendingTx != nil && len(field86Lines) > 0 {
				field86Lines = append(field86Lines, line)
			} else if line != "" && !strings.HasPrefix(line, ":") {
				warnings = append(warnings, fmt.Sprintf("unexpected line: %q", line))
			}
		}
	}

	flushTx()

	if stmt.currency == "" {
		return stmt, warnings, fmt.Errorf("no currency found in opening balance (:60F:)")
	}

	return stmt, warnings, nil
}

// field61Pattern matches the MT940 :61: field format.
// Format: YYMMDDMMDD[C|D|RC|RD]amount[N]code[reference][//bankref]
var field61Pattern = regexp.MustCompile(
	`^(\d{6})` + // Value date YYMMDD
		`(\d{4})` + // Entry date MMDD
		`(R?[CD])` + // Direction: C, D, RC, RD
		`([\d,]+)` + // Amount with comma decimal
		`[A-Z]` + // Transaction type code (single letter)
		`\w+` + // Swift code
		`(.*)$`, // Rest including references
)

// parseMT940Field61 parses a :61: transaction line.
func parseMT940Field61(val string) (*mt940Transaction, error) {
	matches := field61Pattern.FindStringSubmatch(val)
	if matches == nil {
		return nil, fmt.Errorf("cannot parse :61: field: %q", val)
	}

	// Parse date (YYMMDD).
	dateStr := matches[1]
	date, err := time.Parse("060102", dateStr)
	if err != nil {
		return nil, fmt.Errorf("invalid date %q in :61: field", dateStr)
	}

	// Parse direction.
	dirCode := matches[3]
	direction := domain.DirectionCredit
	if dirCode == "D" || dirCode == "RD" {
		direction = domain.DirectionDebit
	}

	// Parse amount (European format: comma as decimal separator).
	amount, err := parseMT940Amount(matches[4])
	if err != nil {
		return nil, fmt.Errorf("invalid amount %q in :61: field: %w", matches[4], err)
	}

	// Extract bank reference (after //).
	reference := extractBankReference(matches[5])

	return &mt940Transaction{
		date:      date,
		direction: direction,
		amount:    amount,
		reference: reference,
	}, nil
}

// parseMT940Amount converts a European format amount string (e.g., "150,00") to cents.
func parseMT940Amount(amountStr string) (int64, error) {
	// Replace comma with dot for standard float parsing.
	amountStr = strings.ReplaceAll(amountStr, ",", ".")
	f, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		return 0, err
	}
	return int64(f*100 + 0.5), nil // Round to nearest cent.
}

// extractBankReference extracts the bank reference from the :61: field remainder.
// Reference follows "//" in the field.
func extractBankReference(rest string) string {
	if idx := strings.Index(rest, "//"); idx != -1 {
		ref := rest[idx+2:]
		// Remove any trailing content after the reference.
		if newline := strings.IndexAny(ref, "\n\r"); newline != -1 {
			ref = ref[:newline]
		}
		return strings.TrimSpace(ref)
	}
	return ""
}

// parseMT940Field86 extracts description and counterparty from the :86: detail field.
// Subfields are delimited by ? followed by a 2-digit code:
//
//	?00 = posting text, ?20-?29 = purpose, ?32 = counterparty name.
func parseMT940Field86(raw string) (description, counterparty string) {
	subfields := parseMT940Subfields(raw)

	// Description from ?20 (purpose of payment).
	if v, ok := subfields["20"]; ok {
		description = v
	}

	// Counterparty from ?32 (name).
	if v, ok := subfields["32"]; ok {
		counterparty = v
	}

	return description, counterparty
}

// parseMT940Subfields splits a :86: field value into subfields keyed by their 2-digit code.
func parseMT940Subfields(raw string) map[string]string {
	result := make(map[string]string)
	parts := strings.Split(raw, "?")
	for _, part := range parts {
		if len(part) < 2 {
			continue
		}
		code := part[:2]
		value := part[2:]
		// Only accept numeric subfield codes.
		if _, err := strconv.Atoi(code); err == nil {
			result[code] = value
		}
	}
	return result
}
