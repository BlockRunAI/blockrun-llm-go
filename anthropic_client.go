package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AnthropicClient is a BlockRun-powered Anthropic-compatible client.
//
// Drop-in replacement for anthropic.Anthropic that routes through BlockRun's
// API gateway at /v1/messages with automatic x402 USDC micropayments on Base.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine - only signatures are transmitted.
//
// Usage:
//
//	client, err := blockrun.NewAnthropicClient("")
//	resp, err := client.Messages.Create(ctx, blockrun.AnthropicCreateParams{
//	    Model:     "claude-sonnet-4-6",
//	    MaxTokens: 1024,
//	    Messages: []blockrun.AnthropicMessage{
//	        {Role: "user", Content: "Hello!"},
//	    },
//	})
//	fmt.Println(resp.Content[0].Text)
type AnthropicClient struct {
	*baseClient
	// Messages provides access to the Anthropic Messages API.
	Messages *AnthropicMessagesAPI
}

// AnthropicClientOption is a function that configures an AnthropicClient.
type AnthropicClientOption func(*AnthropicClient)

// WithAnthropicAPIURL sets a custom API URL for the Anthropic client.
func WithAnthropicAPIURL(url string) AnthropicClientOption {
	return func(c *AnthropicClient) {
		c.apiURL = strings.TrimSuffix(url, "/")
	}
}

// WithAnthropicTimeout sets the HTTP timeout for the Anthropic client.
func WithAnthropicTimeout(timeout time.Duration) AnthropicClientOption {
	return func(c *AnthropicClient) {
		c.httpClient.Timeout = timeout
	}
}

// NewAnthropicClient creates a new BlockRun Anthropic-compatible client.
//
// If privateKey is empty, it is read from the BLOCKRUN_WALLET_KEY or
// BASE_CHAIN_WALLET_KEY environment variable.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine - only signatures are transmitted.
func NewAnthropicClient(privateKey string, opts ...AnthropicClientOption) (*AnthropicClient, error) {
	bc, err := newBaseClient(privateKey, "", DefaultTimeout)
	if err != nil {
		return nil, err
	}

	c := &AnthropicClient{baseClient: bc}
	c.Messages = &AnthropicMessagesAPI{client: bc}

	for _, opt := range opts {
		opt(c)
	}

	bc.checkEnvAPIURL()

	return c, nil
}

// =============================================================================
// Anthropic Types
// =============================================================================

// AnthropicMessage represents a message in an Anthropic conversation.
// Content can be a plain string or a list of content blocks.
type AnthropicMessage struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content any    `json:"content"` // string or []AnthropicContentBlock
}

// AnthropicContentBlock represents a single content block in a message.
type AnthropicContentBlock struct {
	Type       string                    `json:"type"`                  // "text", "image", "tool_use", "tool_result"
	Text       string                    `json:"text,omitempty"`        // for type "text"
	ID         string                    `json:"id,omitempty"`          // for type "tool_use"
	Name       string                    `json:"name,omitempty"`        // for type "tool_use"
	Input      map[string]any            `json:"input,omitempty"`       // for type "tool_use"
	ToolUseID  string                    `json:"tool_use_id,omitempty"` // for type "tool_result"
	Source     *AnthropicImageSource     `json:"source,omitempty"`      // for type "image"
}

// AnthropicImageSource describes an image source (base64 or URL).
type AnthropicImageSource struct {
	Type      string `json:"type"`                 // "base64" or "url"
	MediaType string `json:"media_type,omitempty"` // e.g. "image/png"
	Data      string `json:"data,omitempty"`       // base64-encoded data
	URL       string `json:"url,omitempty"`        // for type "url"
}

// AnthropicTool defines a tool that Claude can use.
type AnthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"` // JSON Schema
}

// AnthropicCreateParams holds the parameters for Messages.Create.
type AnthropicCreateParams struct {
	// Model is the model ID, e.g. "claude-sonnet-4-6" or "openai/gpt-5.2".
	Model string `json:"model"`
	// Messages is the conversation history.
	Messages []AnthropicMessage `json:"messages"`
	// MaxTokens is the maximum number of tokens to generate (required).
	MaxTokens int `json:"max_tokens"`
	// System is an optional system prompt.
	System string `json:"system,omitempty"`
	// Tools is an optional list of tools Claude can call.
	Tools []AnthropicTool `json:"tools,omitempty"`
	// Temperature controls randomness (0.0–1.0).
	Temperature *float64 `json:"temperature,omitempty"`
	// TopP nucleus sampling parameter.
	TopP *float64 `json:"top_p,omitempty"`
	// StopSequences are custom stop sequences.
	StopSequences []string `json:"stop_sequences,omitempty"`
}

// AnthropicUsage represents token usage in an Anthropic response.
type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// AnthropicResponse is the response from Messages.Create.
type AnthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`         // "message"
	Role         string                  `json:"role"`         // "assistant"
	Content      []AnthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`  // "end_turn", "max_tokens", "tool_use", "stop_sequence"
	StopSequence string                  `json:"stop_sequence,omitempty"`
	Usage        AnthropicUsage          `json:"usage"`
}

// Text returns the concatenated text from all text content blocks.
// Convenience method for the common case of a single text response.
func (r *AnthropicResponse) Text() string {
	var sb strings.Builder
	for _, block := range r.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return sb.String()
}

// =============================================================================
// AnthropicMessagesAPI
// =============================================================================

// AnthropicMessagesAPI provides the Messages.Create method.
type AnthropicMessagesAPI struct {
	client *baseClient
}

// Create sends a Messages request to BlockRun's /v1/messages endpoint.
// Supports all Anthropic-compatible models as well as other BlockRun models
// (openai/*, google/*, etc.) in Anthropic message format.
func (m *AnthropicMessagesAPI) Create(ctx context.Context, params AnthropicCreateParams) (*AnthropicResponse, error) {
	if params.Model == "" {
		return nil, &ValidationError{Field: "model", Message: "Model is required"}
	}
	if len(params.Messages) == 0 {
		return nil, &ValidationError{Field: "messages", Message: "At least one message is required"}
	}
	if params.MaxTokens <= 0 {
		return nil, &ValidationError{Field: "max_tokens", Message: "MaxTokens must be greater than 0"}
	}

	body := map[string]any{
		"model":      params.Model,
		"messages":   params.Messages,
		"max_tokens": params.MaxTokens,
	}

	if params.System != "" {
		body["system"] = params.System
	}
	if len(params.Tools) > 0 {
		body["tools"] = params.Tools
	}
	if params.Temperature != nil {
		body["temperature"] = *params.Temperature
	}
	if params.TopP != nil {
		body["top_p"] = *params.TopP
	}
	if len(params.StopSequences) > 0 {
		body["stop_sequences"] = params.StopSequences
	}

	respBytes, err := m.client.doRequest(ctx, "/v1/messages", body)
	if err != nil {
		return nil, err
	}

	var resp AnthropicResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &resp, nil
}
