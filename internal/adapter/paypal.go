package adapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// PayPalAdapter implements SourceAdapter for PayPal Transaction Search.
// It pulls transactions via GET /v1/reporting/transactions with OAuth 2.0 authentication.
type PayPalAdapter struct {
	clientID     string
	clientSecret string
	baseURL      string
	httpClient   *http.Client
	maxRetries   int

	// Token cache (protected by mutex).
	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

// NewPayPalAdapter creates a new PayPalAdapter.
// If httpClient is nil, a default client with 30s timeout is used.
func NewPayPalAdapter(clientID, clientSecret, baseURL string, httpClient *http.Client) *PayPalAdapter {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://api-m.paypal.com"
	}
	return &PayPalAdapter{
		clientID:     clientID,
		clientSecret: clientSecret,
		baseURL:      baseURL,
		httpClient:   httpClient,
		maxRetries:   5,
	}
}

// Name returns the adapter identifier.
func (a *PayPalAdapter) Name() string { return "paypal" }

// ParseFile is not supported — PayPal is an API-based adapter.
func (a *PayPalAdapter) ParseFile(_ context.Context, _ []byte, _ string) (*FetchResult, error) {
	return nil, ErrNotSupported
}

// FetchTransactions pulls transactions from PayPal for the given date range.
// It splits the range into 31-day windows and paginates within each window.
func (a *PayPalAdapter) FetchTransactions(ctx context.Context, req FetchRequest) (*FetchResult, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("paypal: invalid request: %w", err)
	}

	// Authenticate before fetching.
	if err := a.authenticate(ctx); err != nil {
		return nil, fmt.Errorf("paypal: authentication failed: %w", err)
	}

	var allTxns []domain.IngestRequest

	// Split date range into 31-day windows.
	windows := splitDateRange(req.DateFrom, req.DateTo, 31)

	for _, w := range windows {
		txns, err := a.fetchWindow(ctx, req.SourceID, w.start, w.end)
		if err != nil {
			return nil, err
		}
		allTxns = append(allTxns, txns...)
	}

	return &FetchResult{
		Transactions: allTxns,
		TotalRecords: len(allTxns),
	}, nil
}

// fetchWindow fetches all pages of transactions within a single date window.
func (a *PayPalAdapter) fetchWindow(ctx context.Context, sourceID string, start, end time.Time) ([]domain.IngestRequest, error) {
	var allTxns []domain.IngestRequest
	page := 1

	for {
		resp, err := a.fetchPage(ctx, start, end, page)
		if err != nil {
			return nil, err
		}

		for _, detail := range resp.TransactionDetails {
			allTxns = append(allTxns, a.mapTransaction(detail, sourceID))
		}

		if page >= resp.TotalPages || resp.TotalPages == 0 {
			break
		}
		page++
	}

	return allTxns, nil
}

// fetchPage makes a single API call to PayPal with retries and token refresh.
func (a *PayPalAdapter) fetchPage(ctx context.Context, start, end time.Time, page int) (*paypalTransactionResponse, error) {
	url := fmt.Sprintf(
		"%s/v1/reporting/transactions?start_date=%s&end_date=%s&page_size=500&page=%d&fields=all",
		a.baseURL,
		start.Format("2006-01-02T15:04:05-0700"),
		end.Format("2006-01-02T15:04:05-0700"),
		page,
	)

	tokenRefreshed := false

	for attempt := 0; attempt <= a.maxRetries; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("paypal: failed to create request: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+a.getToken())
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := a.httpClient.Do(httpReq)
		if err != nil {
			// Network error — retry with backoff.
			if attempt < a.maxRetries {
				a.backoff(attempt)
				continue
			}
			return nil, fmt.Errorf("paypal: request failed after %d retries: %w", a.maxRetries, err)
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("paypal: failed to read response body: %w", readErr)
		}

		switch {
		case resp.StatusCode == http.StatusOK:
			var txnResp paypalTransactionResponse
			if err := json.Unmarshal(body, &txnResp); err != nil {
				return nil, fmt.Errorf("paypal: failed to parse response: %w", err)
			}
			return &txnResp, nil

		case resp.StatusCode == http.StatusUnauthorized:
			// Token expired — refresh once and retry.
			if !tokenRefreshed {
				tokenRefreshed = true
				a.clearToken()
				if err := a.authenticate(ctx); err != nil {
					return nil, fmt.Errorf("paypal: token refresh failed: %w", err)
				}
				continue
			}
			return nil, fmt.Errorf("paypal: authentication failed (401) after token refresh")

		case resp.StatusCode == http.StatusTooManyRequests:
			// 429 — retry with exponential backoff.
			if attempt < a.maxRetries {
				a.backoff(attempt)
				continue
			}
			return nil, fmt.Errorf("paypal: rate limited (429) after %d retries", a.maxRetries)

		case resp.StatusCode >= 500:
			// 5xx — retry with backoff.
			if attempt < a.maxRetries {
				a.backoff(attempt)
				continue
			}
			return nil, fmt.Errorf("paypal: server error (%d) after %d retries", resp.StatusCode, a.maxRetries)

		default:
			return nil, fmt.Errorf("paypal: unexpected status %d: %s", resp.StatusCode, string(body))
		}
	}

	return nil, fmt.Errorf("paypal: exhausted retries")
}

// authenticate obtains an OAuth 2.0 access token from PayPal.
func (a *PayPalAdapter) authenticate(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Return cached token if still valid.
	if a.accessToken != "" && time.Now().Before(a.tokenExpiry) {
		return nil
	}

	url := a.baseURL + "/v1/oauth2/token"
	body := strings.NewReader("grant_type=client_credentials")

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("failed to create token request: %w", err)
	}

	// Basic auth with clientID:clientSecret.
	creds := base64.StdEncoding.EncodeToString([]byte(a.clientID + ":" + a.clientSecret))
	httpReq.Header.Set("Authorization", "Basic "+creds)
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var tokenResp paypalTokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}

	a.accessToken = tokenResp.AccessToken
	// Subtract 60s buffer to refresh before actual expiry.
	a.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)

	return nil
}

// getToken returns the current access token (thread-safe).
func (a *PayPalAdapter) getToken() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.accessToken
}

// clearToken invalidates the cached token to force re-authentication.
func (a *PayPalAdapter) clearToken() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.accessToken = ""
	a.tokenExpiry = time.Time{}
}

// backoff sleeps for exponential backoff duration: 1s, 2s, 4s, 8s, 16s.
func (a *PayPalAdapter) backoff(attempt int) {
	duration := time.Duration(1<<uint(attempt)) * time.Second
	time.Sleep(duration)
}

// mapTransaction converts a PayPal transaction detail to a canonical IngestRequest.
func (a *PayPalAdapter) mapTransaction(detail paypalTransactionDetail, sourceID string) domain.IngestRequest {
	info := detail.TransactionInfo
	payer := detail.PayerInfo

	amount := parseCents(info.TransactionAmount.Value)

	direction := domain.DirectionCredit
	if amount < 0 {
		direction = domain.DirectionDebit
	}

	occurredAt, _ := time.Parse("2006-01-02T15:04:05-0700", info.TransactionInitiationDate)

	rawData, _ := json.Marshal(detail)

	return domain.IngestRequest{
		SourceID:     sourceID,
		ExternalID:   info.TransactionID,
		Amount:       amount,
		Currency:     info.TransactionAmount.CurrencyCode,
		Direction:    direction,
		Description:  info.TransactionSubject,
		Counterparty: payer.EmailAddress,
		OccurredAt:   occurredAt.UTC(),
		RawData:      rawData,
	}
}

// parseCents converts a decimal string to integer cents.
// "150.00" → 15000, "-75.50" → -7550, "2500.00" → 250000
func parseCents(value string) int64 {
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return int64(math.Round(f * 100))
}

// --- Date range splitting ---

type dateWindow struct {
	start time.Time
	end   time.Time
}

// splitDateRange splits a date range into windows of maxDays.
func splitDateRange(from, to time.Time, maxDays int) []dateWindow {
	var windows []dateWindow
	current := from
	for current.Before(to) {
		windowEnd := current.AddDate(0, 0, maxDays)
		if windowEnd.After(to) {
			windowEnd = to
		}
		windows = append(windows, dateWindow{start: current, end: windowEnd})
		current = windowEnd
	}
	return windows
}

// --- PayPal API response types (private) ---

// paypalTokenResponse is the OAuth 2.0 token response.
type paypalTokenResponse struct {
	Scope       string `json:"scope"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	AppID       string `json:"app_id"`
	ExpiresIn   int    `json:"expires_in"`
	Nonce       string `json:"nonce"`
}

// paypalTransactionResponse is the transaction search response.
type paypalTransactionResponse struct {
	TransactionDetails []paypalTransactionDetail `json:"transaction_details"`
	AccountNumber      string                    `json:"account_number"`
	StartDate          string                    `json:"start_date"`
	EndDate            string                    `json:"end_date"`
	Page               int                       `json:"page"`
	TotalItems         int                       `json:"total_items"`
	TotalPages         int                       `json:"total_pages"`
}

// paypalTransactionDetail is a single transaction with info and payer details.
type paypalTransactionDetail struct {
	TransactionInfo paypalTransactionInfo `json:"transaction_info"`
	PayerInfo       paypalPayerInfo       `json:"payer_info"`
}

// paypalTransactionInfo contains the core transaction data.
type paypalTransactionInfo struct {
	TransactionID             string          `json:"transaction_id"`
	TransactionEventCode      string          `json:"transaction_event_code"`
	TransactionInitiationDate string          `json:"transaction_initiation_date"`
	TransactionUpdatedDate    string          `json:"transaction_updated_date"`
	TransactionAmount         paypalAmount    `json:"transaction_amount"`
	FeeAmount                 paypalAmount    `json:"fee_amount"`
	TransactionStatus         string          `json:"transaction_status"`
	TransactionSubject        string          `json:"transaction_subject"`
	TransactionNote           string          `json:"transaction_note"`
}

// paypalAmount represents a PayPal monetary amount.
type paypalAmount struct {
	CurrencyCode string `json:"currency_code"`
	Value        string `json:"value"`
}

// paypalPayerInfo contains payer/buyer details.
type paypalPayerInfo struct {
	AccountID    string         `json:"account_id"`
	EmailAddress string         `json:"email_address"`
	PayerName    paypalPayerName `json:"payer_name"`
}

// paypalPayerName contains payer name fields.
type paypalPayerName struct {
	GivenName string `json:"given_name"`
	Surname   string `json:"surname"`
}
