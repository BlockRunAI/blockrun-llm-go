package blockrun

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

// baseClient contains the shared fields and methods for all BlockRun clients.
// It handles HTTP requests, x402 payment negotiation, and spending tracking.
type baseClient struct {
	privateKey      *ecdsa.PrivateKey
	address         string
	apiURL          string
	httpClient      *http.Client
	cache           *Cache
	mu              sync.Mutex
	sessionTotalUSD float64
	sessionCalls    int
	costLog         *CostLog
}

// newBaseClient creates a new baseClient with the given private key, API URL, and timeout.
//
// If privateKey is empty, it checks BLOCKRUN_WALLET_KEY then BASE_CHAIN_WALLET_KEY env vars.
// If apiURL is empty, DefaultAPIURL is used; BLOCKRUN_API_URL env var can override.
func newBaseClient(privateKey, apiURL string, timeout time.Duration) (*baseClient, error) {
	// Get private key from param or environment
	key := privateKey
	if key == "" {
		key = os.Getenv("BLOCKRUN_WALLET_KEY")
	}
	if key == "" {
		key = os.Getenv("BASE_CHAIN_WALLET_KEY")
	}
	if key == "" {
		return nil, &ValidationError{
			Field:   "privateKey",
			Message: "Private key required. Pass privateKey parameter or set BLOCKRUN_WALLET_KEY environment variable. NOTE: Your key never leaves your machine - only signatures are sent.",
		}
	}

	// Parse private key
	key = strings.TrimPrefix(key, "0x")
	ecdsaKey, err := crypto.HexToECDSA(key)
	if err != nil {
		return nil, &ValidationError{
			Field:   "privateKey",
			Message: fmt.Sprintf("Invalid private key format: %v", err),
		}
	}

	// Get wallet address
	address := crypto.PubkeyToAddress(ecdsaKey.PublicKey).Hex()

	// Determine API URL
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}

	bc := &baseClient{
		privateKey: ecdsaKey,
		address:    address,
		apiURL:     apiURL,
		httpClient: &http.Client{Timeout: timeout},
		costLog:    NewCostLog(),
	}

	return bc, nil
}

// checkEnvAPIURL overrides apiURL with BLOCKRUN_API_URL env var if still at default.
// Called after options are applied so user-set URLs take precedence.
func (bc *baseClient) checkEnvAPIURL() {
	if envURL := os.Getenv("BLOCKRUN_API_URL"); envURL != "" && bc.apiURL == DefaultAPIURL {
		bc.apiURL = strings.TrimSuffix(envURL, "/")
	}
}

// GetWalletAddress returns the wallet address being used for payments.
func (bc *baseClient) GetWalletAddress() string {
	return bc.address
}

// GetSpending returns session spending information.
func (bc *baseClient) GetSpending() Spending {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return Spending{
		TotalUSD: bc.sessionTotalUSD,
		Calls:    bc.sessionCalls,
	}
}

// doRequest makes a POST request to the given endpoint with automatic x402
// payment handling. It returns the raw response bytes for the caller to unmarshal.
func (bc *baseClient) doRequest(ctx context.Context, endpoint string, body map[string]any) ([]byte, error) {
	// Check cache before making request
	if bc.cache != nil {
		if cached, ok := bc.cache.Get(endpoint, body); ok {
			return cached, nil
		}
	}

	url := bc.apiURL + endpoint

	// Encode body
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request body: %w", err)
	}

	// First attempt (will likely return 402)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := bc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle 402 Payment Required
	if resp.StatusCode == http.StatusPaymentRequired {
		return bc.handlePaymentAndRetry(ctx, url, jsonBody, resp)
	}

	// Handle other errors
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("API error: %s", string(bodyBytes)),
		}
	}

	// Read successful response
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Store in cache
	if bc.cache != nil {
		bc.cache.Set(endpoint, body, data)
	}

	return data, nil
}

// doGet makes a GET request to the given endpoint and returns raw response bytes.
func (bc *baseClient) doGet(ctx context.Context, endpoint string) ([]byte, error) {
	// Check cache before making request
	if bc.cache != nil {
		if cached, ok := bc.cache.Get(endpoint, nil); ok {
			return cached, nil
		}
	}

	url := bc.apiURL + endpoint

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := bc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("API error: %s", string(bodyBytes)),
		}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Store in cache
	if bc.cache != nil {
		bc.cache.Set(endpoint, nil, data)
	}

	return data, nil
}

// handlePaymentAndRetry handles a 402 response by signing a payment and retrying.
func (bc *baseClient) handlePaymentAndRetry(ctx context.Context, url string, body []byte, resp *http.Response) ([]byte, error) {
	// Get payment required header
	paymentHeader := resp.Header.Get("payment-required")
	if paymentHeader == "" {
		// Try to get from response body
		var respBody map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&respBody); err == nil {
			if _, ok := respBody["x402"]; ok {
				// Response body contains payment info - re-encode as header
				jsonBytes, _ := json.Marshal(respBody)
				paymentHeader = string(jsonBytes)
			}
		}
	}

	if paymentHeader == "" {
		return nil, &PaymentError{Message: "402 response but no payment requirements found"}
	}

	// Parse payment requirements
	paymentReq, err := ParsePaymentRequired(paymentHeader)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to parse payment requirements: %v", err)}
	}

	// Extract payment details
	paymentOption, err := ExtractPaymentDetails(paymentReq)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to extract payment details: %v", err)}
	}

	// Determine resource URL
	resourceURL := paymentReq.Resource.URL
	if resourceURL == "" {
		resourceURL = url
	}

	// Create signed payment payload
	paymentPayload, err := CreatePaymentPayload(
		bc.privateKey,
		paymentOption.PayTo,
		paymentOption.Amount,
		paymentOption.Network,
		resourceURL,
		paymentReq.Resource.Description,
		paymentOption.MaxTimeoutSeconds,
		paymentOption.Extra,
		paymentReq.Extensions,
	)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to create payment: %v", err)}
	}

	// Retry with payment signature
	retryReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create retry request: %w", err)
	}
	retryReq.Header.Set("Content-Type", "application/json")
	retryReq.Header.Set("PAYMENT-SIGNATURE", paymentPayload)

	retryResp, err := bc.httpClient.Do(retryReq)
	if err != nil {
		return nil, fmt.Errorf("retry request failed: %w", err)
	}
	defer retryResp.Body.Close()

	// Check for payment rejection
	if retryResp.StatusCode == http.StatusPaymentRequired {
		return nil, &PaymentError{Message: "Payment was rejected. Check your wallet balance."}
	}

	// Handle other errors
	if retryResp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(retryResp.Body)
		return nil, &APIError{
			StatusCode: retryResp.StatusCode,
			Message:    fmt.Sprintf("API error after payment: %s", string(bodyBytes)),
		}
	}

	// Read successful response
	respBytes, err := io.ReadAll(retryResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Track spending - convert amount from micro-USDC to USD
	bc.mu.Lock()
	bc.sessionCalls++
	var costUSD float64
	if amountStr := paymentOption.Amount; amountStr != "" {
		var amountMicro float64
		if _, err := fmt.Sscanf(amountStr, "%f", &amountMicro); err == nil {
			costUSD = amountMicro / 1_000_000
			bc.sessionTotalUSD += costUSD
		}
	}
	bc.mu.Unlock()

	// Log cost to persistent JSONL file
	if bc.costLog != nil && costUSD > 0 {
		endpoint := strings.TrimPrefix(url, bc.apiURL)
		bc.costLog.Append(endpoint, costUSD)
	}

	return respBytes, nil
}
