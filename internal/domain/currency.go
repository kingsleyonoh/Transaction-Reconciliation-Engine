package domain

// validCurrencies contains the set of ISO 4217 currency codes accepted by the ingester.
// This is not exhaustive — it covers currencies commonly seen in payment gateways and
// European bank statements. Extend as needed when new source adapters are added.
var validCurrencies = map[string]bool{
	"AED": true, "AUD": true, "BRL": true, "CAD": true, "CHF": true,
	"CNY": true, "CZK": true, "DKK": true, "EUR": true, "GBP": true,
	"HKD": true, "HUF": true, "IDR": true, "ILS": true, "INR": true,
	"JPY": true, "KRW": true, "MXN": true, "MYR": true, "NOK": true,
	"NZD": true, "PHP": true, "PLN": true, "RON": true, "RUB": true,
	"SEK": true, "SGD": true, "THB": true, "TRY": true, "TWD": true,
	"USD": true, "ZAR": true,
}

// ValidCurrency returns true if the given code is a recognized ISO 4217 currency.
func ValidCurrency(code string) bool {
	return validCurrencies[code]
}
