package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// fixtureDir returns the absolute path to the bankfiles fixture directory.
func fixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "tests", "fixtures", "bankfiles")
}

// loadFixture reads a fixture file from the bankfiles directory.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir(t), name))
	if err != nil {
		t.Fatalf("failed to load fixture %s: %v", name, err)
	}
	return data
}

// --- BankFileAdapter basic tests ---

func TestBankFileAdapter_Name(t *testing.T) {
	a := NewBankFileAdapter()
	if a.Name() != "bankfile" {
		t.Errorf("expected name %q, got %q", "bankfile", a.Name())
	}
}

func TestBankFileAdapter_FetchTransactions_ReturnsNotSupported(t *testing.T) {
	a := NewBankFileAdapter()
	_, err := a.FetchTransactions(context.Background(), FetchRequest{SourceID: "src-1"})
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("expected ErrNotSupported, got %v", err)
	}
}

// --- MT940 parsing tests ---

func TestBankFileAdapter_ParseMT940_SingleStatement(t *testing.T) {
	a := NewBankFileAdapter()
	data := loadFixture(t, "sample_mt940_single.txt")

	result, err := a.ParseFile(context.Background(), data, "source-bank-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TotalRecords != 3 {
		t.Errorf("expected 3 records, got %d", result.TotalRecords)
	}
	if len(result.Transactions) != 3 {
		t.Fatalf("expected 3 transactions, got %d", len(result.Transactions))
	}

	// Verify first transaction (credit, EUR 150.00)
	tx0 := result.Transactions[0]
	if tx0.SourceID != "source-bank-1" {
		t.Errorf("tx[0] SourceID = %q, want %q", tx0.SourceID, "source-bank-1")
	}
	if tx0.ExternalID != "BANKREF001" {
		t.Errorf("tx[0] ExternalID = %q, want %q", tx0.ExternalID, "BANKREF001")
	}
	if tx0.Amount != 15000 {
		t.Errorf("tx[0] Amount = %d, want %d", tx0.Amount, 15000)
	}
	if tx0.Currency != "EUR" {
		t.Errorf("tx[0] Currency = %q, want %q", tx0.Currency, "EUR")
	}
	if tx0.Direction != domain.DirectionCredit {
		t.Errorf("tx[0] Direction = %q, want %q", tx0.Direction, domain.DirectionCredit)
	}

	// Verify second transaction (debit, EUR 75.50)
	tx1 := result.Transactions[1]
	if tx1.Direction != domain.DirectionDebit {
		t.Errorf("tx[1] Direction = %q, want %q", tx1.Direction, domain.DirectionDebit)
	}
	if tx1.Amount != 7550 {
		t.Errorf("tx[1] Amount = %d, want %d", tx1.Amount, 7550)
	}
	if tx1.ExternalID != "BANKREF002" {
		t.Errorf("tx[1] ExternalID = %q, want %q", tx1.ExternalID, "BANKREF002")
	}

	// Verify third transaction (credit, EUR 2500.00)
	tx2 := result.Transactions[2]
	if tx2.Amount != 250000 {
		t.Errorf("tx[2] Amount = %d, want %d", tx2.Amount, 250000)
	}
	if tx2.Direction != domain.DirectionCredit {
		t.Errorf("tx[2] Direction = %q, want %q", tx2.Direction, domain.DirectionCredit)
	}
}

func TestBankFileAdapter_ParseMT940_MultiStatement(t *testing.T) {
	a := NewBankFileAdapter()
	data := loadFixture(t, "sample_mt940_multi.txt")

	result, err := a.ParseFile(context.Background(), data, "source-bank-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 3 statements: 1 tx + 2 tx + 2 tx = 5 total
	if result.TotalRecords != 5 {
		t.Errorf("expected 5 records, got %d", result.TotalRecords)
	}

	// Check currencies across statements
	currencies := map[string]int{}
	for _, tx := range result.Transactions {
		currencies[tx.Currency]++
	}
	if currencies["EUR"] != 1 {
		t.Errorf("expected 1 EUR transaction, got %d", currencies["EUR"])
	}
	if currencies["USD"] != 2 {
		t.Errorf("expected 2 USD transactions, got %d", currencies["USD"])
	}
	if currencies["GBP"] != 2 {
		t.Errorf("expected 2 GBP transactions, got %d", currencies["GBP"])
	}
}

func TestBankFileAdapter_ParseMT940_AmountConversion(t *testing.T) {
	a := NewBankFileAdapter()
	data := loadFixture(t, "sample_mt940_single.txt")

	result, err := a.ParseFile(context.Background(), data, "src")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 150,00 → 15000 cents
	if result.Transactions[0].Amount != 15000 {
		t.Errorf("150,00 → expected 15000 cents, got %d", result.Transactions[0].Amount)
	}
	// 75,50 → 7550 cents
	if result.Transactions[1].Amount != 7550 {
		t.Errorf("75,50 → expected 7550 cents, got %d", result.Transactions[1].Amount)
	}
	// 2500,00 → 250000 cents
	if result.Transactions[2].Amount != 250000 {
		t.Errorf("2500,00 → expected 250000 cents, got %d", result.Transactions[2].Amount)
	}
}

func TestBankFileAdapter_ParseMT940_DirectionMapping(t *testing.T) {
	a := NewBankFileAdapter()
	data := loadFixture(t, "sample_mt940_single.txt")

	result, err := a.ParseFile(context.Background(), data, "src")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		index     int
		direction string
	}{
		{0, domain.DirectionCredit}, // C → credit
		{1, domain.DirectionDebit},  // D → debit
		{2, domain.DirectionCredit}, // C → credit
	}

	for _, tc := range tests {
		if result.Transactions[tc.index].Direction != tc.direction {
			t.Errorf("tx[%d] Direction = %q, want %q",
				tc.index, result.Transactions[tc.index].Direction, tc.direction)
		}
	}
}

func TestBankFileAdapter_ParseMT940_CounterpartyExtraction(t *testing.T) {
	a := NewBankFileAdapter()
	data := loadFixture(t, "sample_mt940_single.txt")

	result, err := a.ParseFile(context.Background(), data, "src")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"Acme Corp", "Beta Ltd", "Gamma GmbH"}
	for i, want := range expected {
		if result.Transactions[i].Counterparty != want {
			t.Errorf("tx[%d] Counterparty = %q, want %q",
				i, result.Transactions[i].Counterparty, want)
		}
	}
}

func TestBankFileAdapter_ParseMT940_DescriptionExtraction(t *testing.T) {
	a := NewBankFileAdapter()
	data := loadFixture(t, "sample_mt940_single.txt")

	result, err := a.ParseFile(context.Background(), data, "src")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"Invoice 1001", "Supplier payment", "Annual subscription"}
	for i, want := range expected {
		if result.Transactions[i].Description != want {
			t.Errorf("tx[%d] Description = %q, want %q",
				i, result.Transactions[i].Description, want)
		}
	}
}

func TestBankFileAdapter_ParseMT940_Malformed(t *testing.T) {
	a := NewBankFileAdapter()
	data := loadFixture(t, "malformed_mt940.txt")

	result, err := a.ParseFile(context.Background(), data, "src")

	// Should return partial results with warnings, not a hard error
	// for recoverable parse issues. Hard errors only for completely unparseable files.
	if err != nil && result == nil {
		// Hard error is also acceptable for malformed files
		return
	}

	// If partial results returned, should have warnings
	if result != nil && len(result.Warnings) == 0 {
		t.Error("expected warnings for malformed MT940, got none")
	}
}

// --- CAMT.053 parsing tests ---

func TestBankFileAdapter_ParseCAMT053_SingleStatement(t *testing.T) {
	a := NewBankFileAdapter()
	data := loadFixture(t, "sample_camt053.xml")

	result, err := a.ParseFile(context.Background(), data, "source-bank-camt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TotalRecords != 3 {
		t.Errorf("expected 3 records, got %d", result.TotalRecords)
	}
	if len(result.Transactions) != 3 {
		t.Fatalf("expected 3 transactions, got %d", len(result.Transactions))
	}

	// Verify first transaction (credit, EUR 150.00)
	tx0 := result.Transactions[0]
	if tx0.SourceID != "source-bank-camt" {
		t.Errorf("tx[0] SourceID = %q, want %q", tx0.SourceID, "source-bank-camt")
	}
	if tx0.ExternalID != "CAMTREF001" {
		t.Errorf("tx[0] ExternalID = %q, want %q", tx0.ExternalID, "CAMTREF001")
	}
	if tx0.Amount != 15000 {
		t.Errorf("tx[0] Amount = %d, want %d", tx0.Amount, 15000)
	}
	if tx0.Currency != "EUR" {
		t.Errorf("tx[0] Currency = %q, want %q", tx0.Currency, "EUR")
	}
	if tx0.Direction != domain.DirectionCredit {
		t.Errorf("tx[0] Direction = %q, want %q", tx0.Direction, domain.DirectionCredit)
	}

	// Verify second transaction (debit, EUR 75.50)
	tx1 := result.Transactions[1]
	if tx1.Direction != domain.DirectionDebit {
		t.Errorf("tx[1] Direction = %q, want %q", tx1.Direction, domain.DirectionDebit)
	}
	if tx1.Amount != 7550 {
		t.Errorf("tx[1] Amount = %d, want %d", tx1.Amount, 7550)
	}
	if tx1.ExternalID != "CAMTREF002" {
		t.Errorf("tx[1] ExternalID = %q, want %q", tx1.ExternalID, "CAMTREF002")
	}

	// Verify third transaction (credit, EUR 2500.00)
	tx2 := result.Transactions[2]
	if tx2.Amount != 250000 {
		t.Errorf("tx[2] Amount = %d, want %d", tx2.Amount, 250000)
	}
	if tx2.Direction != domain.DirectionCredit {
		t.Errorf("tx[2] Direction = %q, want %q", tx2.Direction, domain.DirectionCredit)
	}
}

func TestBankFileAdapter_ParseCAMT053_Multicurrency(t *testing.T) {
	a := NewBankFileAdapter()
	data := loadFixture(t, "sample_camt053_multicurrency.xml")

	result, err := a.ParseFile(context.Background(), data, "source-mc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TotalRecords != 2 {
		t.Errorf("expected 2 records, got %d", result.TotalRecords)
	}

	// Both entries booked in EUR (account currency)
	for i, tx := range result.Transactions {
		if tx.Currency != "EUR" {
			t.Errorf("tx[%d] Currency = %q, want %q (account currency)", i, tx.Currency, "EUR")
		}
	}

	// First: EUR 1500.00
	if result.Transactions[0].Amount != 150000 {
		t.Errorf("tx[0] Amount = %d, want %d", result.Transactions[0].Amount, 150000)
	}
	// Second: EUR 1000.00
	if result.Transactions[1].Amount != 100000 {
		t.Errorf("tx[1] Amount = %d, want %d", result.Transactions[1].Amount, 100000)
	}
}

func TestBankFileAdapter_ParseCAMT053_AmountConversion(t *testing.T) {
	a := NewBankFileAdapter()
	data := loadFixture(t, "sample_camt053.xml")

	result, err := a.ParseFile(context.Background(), data, "src")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 150.00 → 15000 cents
	if result.Transactions[0].Amount != 15000 {
		t.Errorf("150.00 → expected 15000 cents, got %d", result.Transactions[0].Amount)
	}
	// 75.50 → 7550 cents
	if result.Transactions[1].Amount != 7550 {
		t.Errorf("75.50 → expected 7550 cents, got %d", result.Transactions[1].Amount)
	}
	// 2500.00 → 250000 cents
	if result.Transactions[2].Amount != 250000 {
		t.Errorf("2500.00 → expected 250000 cents, got %d", result.Transactions[2].Amount)
	}
}

func TestBankFileAdapter_ParseCAMT053_DirectionMapping(t *testing.T) {
	a := NewBankFileAdapter()
	data := loadFixture(t, "sample_camt053.xml")

	result, err := a.ParseFile(context.Background(), data, "src")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		index     int
		direction string
	}{
		{0, domain.DirectionCredit}, // CRDT → credit
		{1, domain.DirectionDebit},  // DBIT → debit
		{2, domain.DirectionCredit}, // CRDT → credit
	}

	for _, tc := range tests {
		if result.Transactions[tc.index].Direction != tc.direction {
			t.Errorf("tx[%d] Direction = %q, want %q",
				tc.index, result.Transactions[tc.index].Direction, tc.direction)
		}
	}
}

func TestBankFileAdapter_ParseCAMT053_CounterpartyAndDescription(t *testing.T) {
	a := NewBankFileAdapter()
	data := loadFixture(t, "sample_camt053.xml")

	result, err := a.ParseFile(context.Background(), data, "src")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedCounterparty := []string{"Acme Corp", "Beta Ltd", "Gamma GmbH"}
	expectedDescription := []string{
		"Payment for Invoice 1001",
		"Supplier payment Beta Ltd",
		"Enterprise license annual subscription",
	}

	for i := range result.Transactions {
		if result.Transactions[i].Counterparty != expectedCounterparty[i] {
			t.Errorf("tx[%d] Counterparty = %q, want %q",
				i, result.Transactions[i].Counterparty, expectedCounterparty[i])
		}
		if result.Transactions[i].Description != expectedDescription[i] {
			t.Errorf("tx[%d] Description = %q, want %q",
				i, result.Transactions[i].Description, expectedDescription[i])
		}
	}
}

func TestBankFileAdapter_ParseCAMT053_Malformed(t *testing.T) {
	a := NewBankFileAdapter()
	data := loadFixture(t, "malformed_camt053.xml")

	result, err := a.ParseFile(context.Background(), data, "src")

	// Should return partial results with warnings, not a hard error
	// for recoverable parse issues. Hard errors only for completely unparseable files.
	if err != nil && result == nil {
		// Hard error is also acceptable for malformed files
		return
	}

	// If partial results returned, should have warnings
	if result != nil && len(result.Warnings) == 0 {
		t.Error("expected warnings for malformed CAMT.053, got none")
	}
}

// TestBankFileAdapter_ParseMT940_Latin1Encoding verifies that MT940 files encoded
// in Latin-1 (ISO-8859-1) parse correctly. European banks commonly use Latin-1 for
// characters like ä, ö, ü in counterparty names and descriptions.
func TestBankFileAdapter_ParseMT940_Latin1Encoding(t *testing.T) {
	adapter := NewBankFileAdapter()

	data, err := os.ReadFile(filepath.Join("../../tests/fixtures/bankfiles", "sample_mt940_latin1.txt"))
	if err != nil {
		t.Fatalf("failed to read Latin-1 MT940 fixture: %v", err)
	}

	result, err := adapter.ParseFile(context.Background(), data, "bank_latin1")
	if err != nil {
		t.Fatalf("ParseFile() returned error for Latin-1 MT940: %v", err)
	}

	if result == nil || len(result.Transactions) == 0 {
		t.Fatal("expected at least one transaction from Latin-1 MT940")
	}

	if result.Transactions[0].SourceID != "bank_latin1" {
		t.Errorf("SourceID = %q, want %q", result.Transactions[0].SourceID, "bank_latin1")
	}

	if result.Transactions[0].Amount != 25000 {
		t.Errorf("Amount = %d, want 25000 (250.00 EUR)", result.Transactions[0].Amount)
	}

	if result.Transactions[0].Direction != domain.DirectionDebit {
		t.Errorf("Direction = %q, want %q", result.Transactions[0].Direction, domain.DirectionDebit)
	}

	// The counterparty should preserve the non-ASCII character — either as raw Latin-1
	// byte (0xFC) or as the UTF-8 equivalent (ü). Both are acceptable.
	cp := result.Transactions[0].Counterparty
	if cp == "" {
		t.Error("Counterparty is empty, expected non-empty value from Latin-1 field")
	}
	if !strings.Contains(cp, "M") || !strings.Contains(cp, "ller") {
		t.Errorf("Counterparty = %q, expected to contain 'M...ller' pattern", cp)
	}
}
