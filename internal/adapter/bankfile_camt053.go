package adapter

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// isCAMT053 checks if the content looks like a CAMT.053 bank statement.
func isCAMT053(content string) bool {
	return strings.Contains(content, "<Document") && strings.Contains(content, "camt.053")
}

// --- CAMT.053 XML struct types ---

type camt053Document struct {
	XMLName       xml.Name          `xml:"Document"`
	BkToCstmrStmt camt053BkToCstmr  `xml:"BkToCstmrStmt"`
}

type camt053BkToCstmr struct {
	Stmt []camt053Statement `xml:"Stmt"`
}

type camt053Statement struct {
	Acct    camt053Account `xml:"Acct"`
	Entries []camt053Entry `xml:"Ntry"`
}

type camt053Account struct {
	Ccy string `xml:"Ccy"`
}

type camt053Entry struct {
	Amt        camt053Amount    `xml:"Amt"`
	CdtDbtInd  string           `xml:"CdtDbtInd"`
	BookgDt    camt053Date      `xml:"BookgDt"`
	AcctSvcrRef string          `xml:"AcctSvcrRef"`
	NtryDtls   camt053NtryDtls  `xml:"NtryDtls"`
}

type camt053Amount struct {
	Value    string `xml:",chardata"`
	Currency string `xml:"Ccy,attr"`
}

type camt053Date struct {
	Dt string `xml:"Dt"`
}

type camt053NtryDtls struct {
	TxDtls []camt053TxDtls `xml:"TxDtls"`
}

type camt053TxDtls struct {
	RmtInf    camt053RmtInf    `xml:"RmtInf"`
	RltdPties camt053RltdPties `xml:"RltdPties"`
}

type camt053RmtInf struct {
	Ustrd string `xml:"Ustrd"`
}

type camt053RltdPties struct {
	Dbtr camt053Party `xml:"Dbtr"`
	Cdtr camt053Party `xml:"Cdtr"`
}

type camt053Party struct {
	Nm string `xml:"Nm"`
}

// parseCAMT053 parses CAMT.053 XML bank statement data into canonical IngestRequests.
func (a *BankFileAdapter) parseCAMT053(data []byte, sourceID string) (*FetchResult, error) {
	var doc camt053Document
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse CAMT.053 XML: %w", err)
	}

	var allTxns []domain.IngestRequest
	var warnings []string

	for _, stmt := range doc.BkToCstmrStmt.Stmt {
		for entryIdx, entry := range stmt.Entries {
			// Parse amount to cents.
			amount, err := parseCAMT053Amount(entry.Amt.Value)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("entry %d: invalid amount %q: %s", entryIdx, entry.Amt.Value, err))
				continue
			}

			// Map direction.
			direction, err := mapCAMT053Direction(entry.CdtDbtInd)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("entry %d: %s", entryIdx, err))
				continue
			}

			// Parse booking date.
			date, err := time.Parse("2006-01-02", entry.BookgDt.Dt)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("entry %d: invalid date %q: %s", entryIdx, entry.BookgDt.Dt, err))
				continue
			}

			// Use entry-level currency (Amt Ccy attribute).
			currency := entry.Amt.Currency
			if currency == "" {
				currency = stmt.Acct.Ccy
			}

			// Extract description and counterparty from the first TxDtls.
			description, counterparty := extractCAMT053Details(entry.NtryDtls, direction)

			rawData, _ := json.Marshal(map[string]string{
				"acct_svr_ref": entry.AcctSvcrRef,
				"cdt_dbt_ind":  entry.CdtDbtInd,
			})

			allTxns = append(allTxns, domain.IngestRequest{
				SourceID:     sourceID,
				ExternalID:   entry.AcctSvcrRef,
				Amount:       amount,
				Currency:     currency,
				Direction:    direction,
				Description:  description,
				Counterparty: counterparty,
				OccurredAt:   date,
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

// parseCAMT053Amount converts a decimal string (e.g., "150.00") to integer cents.
func parseCAMT053Amount(amountStr string) (int64, error) {
	amountStr = strings.TrimSpace(amountStr)
	if amountStr == "" {
		return 0, fmt.Errorf("empty amount")
	}
	f, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		return 0, err
	}
	return int64(f*100 + 0.5), nil
}

// mapCAMT053Direction maps CAMT.053 CdtDbtInd values to domain direction constants.
func mapCAMT053Direction(indicator string) (string, error) {
	switch indicator {
	case "CRDT":
		return domain.DirectionCredit, nil
	case "DBIT":
		return domain.DirectionDebit, nil
	default:
		return "", fmt.Errorf("unknown credit/debit indicator: %q", indicator)
	}
}

// extractCAMT053Details extracts description and counterparty from entry details.
func extractCAMT053Details(dtls camt053NtryDtls, direction string) (description, counterparty string) {
	if len(dtls.TxDtls) == 0 {
		return "", ""
	}
	tx := dtls.TxDtls[0]
	description = tx.RmtInf.Ustrd

	// Counterparty: for credits, use Dbtr (who sent the money); for debits, use Cdtr (who received).
	if direction == domain.DirectionCredit {
		counterparty = tx.RltdPties.Dbtr.Nm
	} else {
		counterparty = tx.RltdPties.Cdtr.Nm
	}

	return description, counterparty
}
