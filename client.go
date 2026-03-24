package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
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
	*baseClient
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

// WithCache enables or disables local response caching with per-endpoint TTL.
// Cached endpoints: /v1/x/ (1h), /v1/pm/ (30m), /v1/search (15m).
// Chat and image endpoints are never cached.
func WithCache(enabled bool) ClientOption {
	return func(c *LLMClient) {
		if enabled {
			c.cache = NewCache()
		} else {
			c.cache = nil
		}
	}
}

// NewLLMClient creates a new BlockRun LLM client.
//
// If privateKey is empty, it will be read from the BLOCKRUN_WALLET_KEY or
// BASE_CHAIN_WALLET_KEY environment variable.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine - only signatures are transmitted.
func NewLLMClient(privateKey string, opts ...ClientOption) (*LLMClient, error) {
	bc, err := newBaseClient(privateKey, "", DefaultTimeout)
	if err != nil {
		return nil, err
	}

	client := &LLMClient{baseClient: bc}

	// Apply options
	for _, opt := range opts {
		opt(client)
	}

	// Check for custom API URL in environment (after options so user-set URLs win)
	bc.checkEnvAPIURL()

	return client, nil
}

// Chat sends a simple 1-line chat request.
//
// This is a convenience method that wraps ChatCompletion for simple use cases.
func (c *LLMClient) Chat(ctx context.Context, model, prompt string) (string, error) {
	return c.ChatWithSystem(ctx, model, prompt, "")
}

// ChatWithSystem sends a chat request with an optional system prompt.
func (c *LLMClient) ChatWithSystem(ctx context.Context, model, prompt, system string) (string, error) {
	messages := []ChatMessage{}

	if system != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: system})
	}
	messages = append(messages, ChatMessage{Role: "user", Content: prompt})

	resp, err := c.ChatCompletion(ctx, model, messages, nil)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", &APIError{Message: "No choices in response"}
	}

	return resp.Choices[0].Message.Content, nil
}

// ChatCompletion sends a full chat completion request (OpenAI-compatible).
func (c *LLMClient) ChatCompletion(ctx context.Context, model string, messages []ChatMessage, opts *ChatCompletionOptions) (*ChatResponse, error) {
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
		// Handle tool/function calling
		if opts.Tools != nil {
			body["tools"] = opts.Tools
		}
		if opts.ToolChoice != nil {
			body["tool_choice"] = opts.ToolChoice
		}
	}
	body["max_tokens"] = maxTokens

	// Make request with payment handling
	respBytes, err := c.doRequest(ctx, "/v1/chat/completions", body)
	if err != nil {
		return nil, err
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &chatResp, nil
}

// GetCostSummary returns an aggregate summary of all costs logged to the persistent JSONL file.
func (c *LLMClient) GetCostSummary() (*CostSummary, error) {
	return c.costLog.Summary()
}

// ListModels returns the list of available models with pricing.
func (c *LLMClient) ListModels(ctx context.Context) ([]Model, error) {
	respBytes, err := c.doGet(ctx, "/v1/models")
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}

	var result struct {
		Data []Model `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode models response: %w", err)
	}

	return result.Data, nil
}

// ListImageModels returns the list of available image models with pricing.
func (c *LLMClient) ListImageModels(ctx context.Context) ([]ImageModel, error) {
	respBytes, err := c.doGet(ctx, "/v1/images/models")
	if err != nil {
		return nil, fmt.Errorf("failed to list image models: %w", err)
	}

	var result struct {
		Data []ImageModel `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode image models response: %w", err)
	}

	return result.Data, nil
}

// ListAllModels returns a unified list of all available models (LLM and image).
func (c *LLMClient) ListAllModels(ctx context.Context) ([]AllModel, error) {
	// Get LLM models
	llmModels, err := c.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list LLM models: %w", err)
	}

	// Get image models
	imageModels, err := c.ListImageModels(ctx)
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
			ID:              m.ID,
			Name:            m.Name,
			Provider:        m.Provider,
			Type:            "image",
			PricePerImage:   m.PricePerImage,
			SupportedSizes:  m.SupportedSizes,
		})
	}

	return allModels, nil
}
