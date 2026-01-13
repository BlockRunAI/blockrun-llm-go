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
	// DefaultImageModel is the default image generation model.
	DefaultImageModel = "google/nano-banana"

	// DefaultImageSize is the default image size.
	DefaultImageSize = "1024x1024"

	// DefaultImageTimeout is the default timeout for image generation (images take longer).
	DefaultImageTimeout = 120 * time.Second
)

// ImageClient is the BlockRun Image Generation client.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine - only signatures are transmitted.
type ImageClient struct {
	privateKey      *ecdsa.PrivateKey
	address         string
	apiURL          string
	httpClient      *http.Client
	sessionTotalUSD float64
	sessionCalls    int
}

// ImageClientOption is a function that configures an ImageClient.
type ImageClientOption func(*ImageClient)

// WithImageAPIURL sets a custom API URL for the image client.
func WithImageAPIURL(url string) ImageClientOption {
	return func(c *ImageClient) {
		c.apiURL = strings.TrimSuffix(url, "/")
	}
}

// WithImageTimeout sets the HTTP timeout for the image client.
func WithImageTimeout(timeout time.Duration) ImageClientOption {
	return func(c *ImageClient) {
		c.httpClient.Timeout = timeout
	}
}

// WithImageHTTPClient sets a custom HTTP client for the image client.
func WithImageHTTPClient(client *http.Client) ImageClientOption {
	return func(c *ImageClient) {
		c.httpClient = client
	}
}

// NewImageClient creates a new BlockRun Image client.
//
// If privateKey is empty, it will be read from the BLOCKRUN_WALLET_KEY
// or BASE_CHAIN_WALLET_KEY environment variable.
func NewImageClient(privateKey string, opts ...ImageClientOption) (*ImageClient, error) {
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
			Message: "Private key required. Pass privateKey parameter or set BLOCKRUN_WALLET_KEY environment variable.",
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
	client := &ImageClient{
		privateKey: ecdsaKey,
		address:    address,
		apiURL:     DefaultAPIURL,
		httpClient: &http.Client{Timeout: DefaultImageTimeout},
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

// ImageGenerateOptions contains optional parameters for image generation.
type ImageGenerateOptions struct {
	Model   string `json:"model,omitempty"`
	Size    string `json:"size,omitempty"`
	N       int    `json:"n,omitempty"`
	Quality string `json:"quality,omitempty"`
}

// ImageData represents a single generated image.
type ImageData struct {
	URL           string `json:"url"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
}

// ImageResponse represents the API response for image generation.
type ImageResponse struct {
	Created int64       `json:"created"`
	Data    []ImageData `json:"data"`
}

// ImageModel represents an available image model from the API.
type ImageModel struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Provider      string   `json:"provider"`
	Description   string   `json:"description"`
	PricePerImage float64  `json:"pricePerImage"`
	SupportedSizes []string `json:"supportedSizes,omitempty"`
	MaxPromptLength int    `json:"maxPromptLength,omitempty"`
	Available     bool     `json:"available"`
}

// Generate generates an image from a text prompt.
func (c *ImageClient) Generate(prompt string, opts *ImageGenerateOptions) (*ImageResponse, error) {
	// Build request body
	body := map[string]any{
		"prompt": prompt,
		"model":  DefaultImageModel,
		"size":   DefaultImageSize,
		"n":      1,
	}

	if opts != nil {
		if opts.Model != "" {
			body["model"] = opts.Model
		}
		if opts.Size != "" {
			body["size"] = opts.Size
		}
		if opts.N > 0 {
			body["n"] = opts.N
		}
		if opts.Quality != "" {
			body["quality"] = opts.Quality
		}
	}

	// Make request with payment handling
	return c.requestWithPayment("/v1/images/generations", body)
}

// ListImageModels returns the list of available image models with pricing.
func (c *ImageClient) ListImageModels() ([]ImageModel, error) {
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

// GetWalletAddress returns the wallet address being used for payments.
func (c *ImageClient) GetWalletAddress() string {
	return c.address
}

// GetSpending returns session spending information.
func (c *ImageClient) GetSpending() Spending {
	return Spending{
		TotalUSD: c.sessionTotalUSD,
		Calls:    c.sessionCalls,
	}
}

// requestWithPayment makes a request with automatic x402 payment handling.
func (c *ImageClient) requestWithPayment(endpoint string, body map[string]any) (*ImageResponse, error) {
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
	var imageResp ImageResponse
	if err := json.NewDecoder(resp.Body).Decode(&imageResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &imageResp, nil
}

// handlePaymentAndRetry handles a 402 response by signing a payment and retrying.
func (c *ImageClient) handlePaymentAndRetry(url string, body []byte, resp *http.Response) (*ImageResponse, error) {
	// Get payment required header
	paymentHeader := resp.Header.Get("payment-required")
	if paymentHeader == "" {
		// Try to get from response body
		var respBody map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&respBody); err == nil {
			if _, ok := respBody["x402"]; ok {
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
	var imageResp ImageResponse
	if err := json.NewDecoder(retryResp.Body).Decode(&imageResp); err != nil {
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

	return &imageResp, nil
}
