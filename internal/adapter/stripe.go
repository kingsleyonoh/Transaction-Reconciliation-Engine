package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kingsleyonoh/transaction-reconciliation-engine/internal/domain"
)

// StripeAdapter implements SourceAdapter for Stripe Balance Transactions.
// It pulls transactions via GET /v1/balance_transactions with cursor-based pagination.
type StripeAdapter struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	maxRetries int
}

// NewStripeAdapter creates a new StripeAdapter.
// If httpClient is nil, a default client with 30s timeout is used.
func NewStripeAdapter(apiKey, baseURL string, httpClient *http.Client) *StripeAdapter {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://api.stripe.com"
	}
	return &StripeAdapter{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: httpClient,
		maxRetries: 5,
	}
}

// Name returns the adapter identifier.
func (a *StripeAdapter) Name() string { return "stripe" }

// ParseFile is not supported — Stripe is an API-based adapter.
func (a *StripeAdapter) ParseFile(_ context.Context, _ []byte, _ string) (*FetchResult, error) {
	return nil, ErrNotSupported
}

// FetchTransactions pulls balance transactions from Stripe for the given date range.
// It paginates through all results using cursor-based pagination (starting_after).
func (a *StripeAdapter) FetchTransactions(ctx context.Context, req FetchRequest) (*FetchResult, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("stripe: invalid request: %w", err)
	}

	var allTxns []domain.IngestRequest
	var cursor string

	for {
		resp, err := a.fetchPage(ctx, req, cursor)
		if err != nil {
			return nil, err
		}

		for _, bt := range resp.Data {
			allTxns = append(allTxns, a.mapTransaction(bt, req.SourceID))
		}

		if !resp.HasMore || len(resp.Data) == 0 {
			break
		}

		// Set cursor to last transaction ID for next page.
		cursor = resp.Data[len(resp.Data)-1].ID
	}

	return &FetchResult{
		Transactions: allTxns,
		TotalRecords: len(allTxns),
	}, nil
}

// fetchPage makes a single API call to Stripe and handles retries for transient errors.
func (a *StripeAdapter) fetchPage(ctx context.Context, req FetchRequest, cursor string) (*stripeListResponse, error) {
	url := a.buildURL(req, cursor)

	for attempt := 0; attempt <= a.maxRetries; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("stripe: failed to create request: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)

		resp, err := a.httpClient.Do(httpReq)
		if err != nil {
			// Network error — retry with backoff.
			if attempt < a.maxRetries {
				a.backoff(attempt)
				continue
			}
			return nil, fmt.Errorf("stripe: request failed after %d retries: %w", a.maxRetries, err)
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("stripe: failed to read response body: %w", readErr)
		}

		switch {
		case resp.StatusCode == http.StatusOK:
			var listResp stripeListResponse
			if err := json.Unmarshal(body, &listResp); err != nil {
				return nil, fmt.Errorf("stripe: failed to parse response: %w", err)
			}
			return &listResp, nil

		case resp.StatusCode == http.StatusUnauthorized:
			// 401 — never retry, halt immediately.
			var errResp stripeErrorResponse
			if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
				return nil, fmt.Errorf("stripe: authentication failed (401): %s", errResp.Error.Message)
			}
			return nil, fmt.Errorf("stripe: authentication failed (401)")

		case resp.StatusCode == http.StatusTooManyRequests:
			// 429 — retry with exponential backoff.
			if attempt < a.maxRetries {
				a.backoff(attempt)
				continue
			}
			return nil, fmt.Errorf("stripe: rate limited (429) after %d retries", a.maxRetries)

		case resp.StatusCode >= 500:
			// 5xx — retry with backoff.
			if attempt < a.maxRetries {
				a.backoff(attempt)
				continue
			}
			return nil, fmt.Errorf("stripe: server error (%d) after %d retries", resp.StatusCode, a.maxRetries)

		default:
			return nil, fmt.Errorf("stripe: unexpected status %d: %s", resp.StatusCode, string(body))
		}
	}

	return nil, fmt.Errorf("stripe: exhausted retries")
}

// buildURL constructs the Stripe API URL with query parameters.
func (a *StripeAdapter) buildURL(req FetchRequest, cursor string) string {
	url := a.baseURL + "/v1/balance_transactions?limit=100"
	url += "&created[gte]=" + fmt.Sprintf("%d", req.DateFrom.Unix())
	url += "&created[lte]=" + fmt.Sprintf("%d", req.DateTo.Unix())
	if cursor != "" {
		url += "&starting_after=" + cursor
	}
	return url
}

// backoff sleeps for exponential backoff duration: 1s, 2s, 4s, 8s, 16s.
func (a *StripeAdapter) backoff(attempt int) {
	duration := time.Duration(1<<uint(attempt)) * time.Second
	time.Sleep(duration)
}

// mapTransaction converts a Stripe balance transaction to a canonical IngestRequest.
func (a *StripeAdapter) mapTransaction(bt stripeBalanceTransaction, sourceID string) domain.IngestRequest {
	direction := domain.DirectionCredit
	if bt.Type == "refund" || bt.Type == "payout" || bt.Type == "transfer" ||
		bt.Type == "adjustment" || bt.Type == "stripe_fee" {
		direction = domain.DirectionDebit
	}

	rawData, _ := json.Marshal(bt)

	return domain.IngestRequest{
		SourceID:     sourceID,
		ExternalID:   bt.ID,
		Amount:       bt.Amount,
		Currency:     strings.ToUpper(bt.Currency),
		Direction:    direction,
		Description:  bt.Description,
		Counterparty: bt.Source,
		OccurredAt:   time.Unix(bt.Created, 0).UTC(),
		RawData:      rawData,
	}
}

// --- Stripe API response types (private) ---

// stripeListResponse is the Stripe list API response envelope.
type stripeListResponse struct {
	Data    []stripeBalanceTransaction `json:"data"`
	HasMore bool                       `json:"has_more"`
	URL     string                     `json:"url"`
}

// stripeBalanceTransaction represents a single Stripe balance transaction.
type stripeBalanceTransaction struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	Description string `json:"description"`
	Fee         int64  `json:"fee"`
	Net         int64  `json:"net"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Created     int64  `json:"created"`
	AvailableOn int64  `json:"available_on"`
	Source      string `json:"source"`
}

// stripeErrorResponse is the Stripe error response envelope.
type stripeErrorResponse struct {
	Error stripeErrorDetail `json:"error"`
}

// stripeErrorDetail is the error detail within a Stripe error response.
type stripeErrorDetail struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
	DocURL  string `json:"doc_url"`
}
