package blockrun

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

const (
	// DefaultAPIURL is the default BlockRun API endpoint.
	DefaultAPIURL = "https://blockrun.ai/api"

	// DefaultMaxTokens is the default max tokens for chat completions.
	DefaultMaxTokens = 1024

	// DefaultTimeout is the default HTTP timeout.
	DefaultTimeout = 60 * time.Second
)

// LLMClient is the BlockRun LLM gateway client.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine - only signatures are transmitted.
type LLMClient struct {
	privateKey      *ecdsa.PrivateKey
	address         string
	apiURL          string
	httpClient      *http.Client
	sessionTotalUSD float64
	sessionCalls    int
}

// Spending represents session spending information.
type Spending struct {
	TotalUSD float64
	Calls    int
}

// ClientOption is a function that configures an LLMClient.
type ClientOption func(*LLMClient)

// WithAPIURL sets a custom API URL.
func WithAPIURL(url string) ClientOption {
	return func(c *LLMClient) {
		c.apiURL = strings.TrimSuffix(url, "/")
	}
}

// WithTimeout sets the HTTP timeout.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *LLMClient) {
		c.httpClient.Timeout = timeout
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *LLMClient) {
		c.httpClient = client
	}
}

// NewLLMClient creates a new BlockRun LLM client.
//
// If privateKey is empty, it will be read from the BASE_CHAIN_WALLET_KEY
// environment variable.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine - only signatures are transmitted.
func NewLLMClient(privateKey string, opts ...ClientOption) (*LLMClient, error) {
	// Get private key from param or environment
	key := privateKey
	if key == "" {
		key = os.Getenv("BASE_CHAIN_WALLET_KEY")
	}
	if key == "" {
		return nil, &ValidationError{
			Field:   "privateKey",
			Message: "Private key required. Pass privateKey parameter or set BASE_CHAIN_WALLET_KEY environment variable. NOTE: Your key never leaves your machine - only signatures are sent.",
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

	// Create client with defaults
	client := &LLMClient{
		privateKey: ecdsaKey,
		address:    address,
		apiURL:     DefaultAPIURL,
		httpClient: &http.Client{Timeout: DefaultTimeout},
	}

	// Apply options
	for _, opt := range opts {
		opt(client)
	}

	// Check for custom API URL in environment
	if envURL := os.Getenv("BLOCKRUN_API_URL"); envURL != "" && client.apiURL == DefaultAPIURL {
		client.apiURL = strings.TrimSuffix(envURL, "/")
	}

	return client, nil
}

// Chat sends a simple 1-line chat request.
//
// This is a convenience method that wraps ChatCompletion for simple use cases.
func (c *LLMClient) Chat(model, prompt string) (string, error) {
	return c.ChatWithSystem(model, prompt, "")
}

// ChatWithSystem sends a chat request with an optional system prompt.
func (c *LLMClient) ChatWithSystem(model, prompt, system string) (string, error) {
	messages := []ChatMessage{}

	if system != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: system})
	}
	messages = append(messages, ChatMessage{Role: "user", Content: prompt})

	resp, err := c.ChatCompletion(model, messages, nil)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", &APIError{Message: "No choices in response"}
	}

	return resp.Choices[0].Message.Content, nil
}

// ChatCompletion sends a full chat completion request (OpenAI-compatible).
func (c *LLMClient) ChatCompletion(model string, messages []ChatMessage, opts *ChatCompletionOptions) (*ChatResponse, error) {
	// Validate inputs
	if model == "" {
		return nil, &ValidationError{Field: "model", Message: "Model is required"}
	}
	if len(messages) == 0 {
		return nil, &ValidationError{Field: "messages", Message: "At least one message is required"}
	}

	// Build request body
	body := map[string]any{
		"model":    model,
		"messages": messages,
	}

	// Apply options
	maxTokens := DefaultMaxTokens
	if opts != nil {
		if opts.MaxTokens > 0 {
			maxTokens = opts.MaxTokens
		}
		if opts.Temperature > 0 {
			body["temperature"] = opts.Temperature
		}
		if opts.TopP > 0 {
			body["top_p"] = opts.TopP
		}
		// Handle xAI Live Search parameters
		if opts.SearchParameters != nil {
			body["search_parameters"] = opts.SearchParameters
		} else if opts.Search {
			// Simple shortcut: Search=true enables live search with defaults
			body["search_parameters"] = map[string]string{"mode": "on"}
		}
	}
	body["max_tokens"] = maxTokens

	// Make request with payment handling
	return c.requestWithPayment("/v1/chat/completions", body)
}

// ListModels returns the list of available models with pricing.
func (c *LLMClient) ListModels() ([]Model, error) {
	url := c.apiURL + "/v1/models"

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    "Failed to list models",
		}
	}

	var result struct {
		Data []Model `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode models response: %w", err)
	}

	return result.Data, nil
}

// GetWalletAddress returns the wallet address being used for payments.
func (c *LLMClient) GetWalletAddress() string {
	return c.address
}

// GetSpending returns session spending information.
func (c *LLMClient) GetSpending() Spending {
	return Spending{
		TotalUSD: c.sessionTotalUSD,
		Calls:    c.sessionCalls,
	}
}

// ListImageModels returns the list of available image models with pricing.
func (c *LLMClient) ListImageModels() ([]ImageModel, error) {
	url := c.apiURL + "/v1/images/models"

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to list image models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    "Failed to list image models",
		}
	}

	var result struct {
		Data []ImageModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode image models response: %w", err)
	}

	return result.Data, nil
}

// ListAllModels returns a unified list of all available models (LLM and image).
func (c *LLMClient) ListAllModels() ([]AllModel, error) {
	// Get LLM models
	llmModels, err := c.ListModels()
	if err != nil {
		return nil, fmt.Errorf("failed to list LLM models: %w", err)
	}

	// Get image models
	imageModels, err := c.ListImageModels()
	if err != nil {
		return nil, fmt.Errorf("failed to list image models: %w", err)
	}

	// Combine into unified list
	allModels := make([]AllModel, 0, len(llmModels)+len(imageModels))

	for _, m := range llmModels {
		allModels = append(allModels, AllModel{
			ID:           m.ID,
			Name:         m.Name,
			Provider:     m.Provider,
			Type:         "llm",
			InputPrice:   m.InputPrice,
			OutputPrice:  m.OutputPrice,
			ContextLimit: m.ContextLimit,
		})
	}

	for _, m := range imageModels {
		allModels = append(allModels, AllModel{
			ID:             m.ID,
			Name:           m.Name,
			Provider:       m.Provider,
			Type:           "image",
			PricePerImage:  m.PricePerImage,
			SupportedSizes: m.SupportedSizes,
		})
	}

	return allModels, nil
}

// requestWithPayment makes a request with automatic x402 payment handling.
func (c *LLMClient) requestWithPayment(endpoint string, body map[string]any) (*ChatResponse, error) {
	url := c.apiURL + endpoint

	// Encode body
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request body: %w", err)
	}

	// First attempt (will likely return 402)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle 402 Payment Required
	if resp.StatusCode == http.StatusPaymentRequired {
		return c.handlePaymentAndRetry(url, jsonBody, resp)
	}

	// Handle other errors
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("API error: %s", string(bodyBytes)),
		}
	}

	// Parse successful response
	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &chatResp, nil
}

// handlePaymentAndRetry handles a 402 response by signing a payment and retrying.
func (c *LLMClient) handlePaymentAndRetry(url string, body []byte, resp *http.Response) (*ChatResponse, error) {
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
		c.privateKey,
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
	retryReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create retry request: %w", err)
	}
	retryReq.Header.Set("Content-Type", "application/json")
	retryReq.Header.Set("PAYMENT-SIGNATURE", paymentPayload)

	retryResp, err := c.httpClient.Do(retryReq)
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

	// Parse successful response
	var chatResp ChatResponse
	if err := json.NewDecoder(retryResp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Track spending - convert amount from micro-USDC to USD
	c.sessionCalls++
	if amountStr := paymentOption.Amount; amountStr != "" {
		// Amount is in micro-USDC (6 decimals), convert to USD
		var amountMicro float64
		if _, err := fmt.Sscanf(amountStr, "%f", &amountMicro); err == nil {
			c.sessionTotalUSD += amountMicro / 1_000_000
		}
	}

	return &chatResp, nil
}
