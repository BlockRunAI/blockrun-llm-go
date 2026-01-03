// Package blockrun provides a Go SDK for BlockRun's x402-powered LLM gateway.
//
// SECURITY NOTE - Private Key Handling:
// Your private key NEVER leaves your machine. Here's what happens:
// 1. Key stays local - only used to sign an EIP-712 typed data message
// 2. Only the SIGNATURE is sent in the PAYMENT-SIGNATURE header
// 3. BlockRun verifies the signature on-chain via Coinbase CDP facilitator
// 4. Your actual private key is NEVER transmitted to any server
package blockrun

import "fmt"

// ChatMessage represents a message in the conversation.
type ChatMessage struct {
	Role    string `json:"role"`    // "system", "user", or "assistant"
	Content string `json:"content"` // Message content
}

// ChatCompletionOptions contains optional parameters for chat completion.
type ChatCompletionOptions struct {
	MaxTokens   int     `json:"max_tokens,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
	TopP        float64 `json:"top_p,omitempty"`
}

// ChatResponse represents the API response for chat completions.
type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice represents a single completion choice.
type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// Usage represents token usage information.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Model represents an available model from the API.
type Model struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Provider     string  `json:"provider"`
	InputPrice   float64 `json:"inputPrice"`   // per 1M tokens
	OutputPrice  float64 `json:"outputPrice"`  // per 1M tokens
	ContextLimit int     `json:"contextLimit"` // max tokens
}

// PaymentRequirement represents the x402 payment requirements from a 402 response.
type PaymentRequirement struct {
	X402Version int                `json:"x402Version"`
	Accepts     []PaymentOption    `json:"accepts"`
	Resource    ResourceInfo       `json:"resource"`
	Extensions  map[string]any     `json:"extensions,omitempty"`
}

// PaymentOption represents a single payment option.
type PaymentOption struct {
	Scheme            string         `json:"scheme"`
	Network           string         `json:"network"`
	Amount            string         `json:"amount"`
	Asset             string         `json:"asset"`
	PayTo             string         `json:"payTo"`
	MaxTimeoutSeconds int            `json:"maxTimeoutSeconds"`
	Extra             map[string]any `json:"extra,omitempty"`
}

// ResourceInfo represents information about the resource being accessed.
type ResourceInfo struct {
	URL         string `json:"url"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

// PaymentPayload represents the signed payment payload sent to the server.
type PaymentPayload struct {
	X402Version int            `json:"x402Version"`
	Resource    ResourceInfo   `json:"resource"`
	Accepted    PaymentOption  `json:"accepted"`
	Payload     PaymentData    `json:"payload"`
	Extensions  map[string]any `json:"extensions,omitempty"`
}

// PaymentData contains the signature and authorization data.
type PaymentData struct {
	Signature     string                 `json:"signature"`
	Authorization TransferAuthorization  `json:"authorization"`
}

// TransferAuthorization contains the EIP-3009 TransferWithAuthorization parameters.
type TransferAuthorization struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Value       string `json:"value"`
	ValidAfter  string `json:"validAfter"`
	ValidBefore string `json:"validBefore"`
	Nonce       string `json:"nonce"`
}

// APIError represents an error from the BlockRun API.
type APIError struct {
	StatusCode int
	Message    string
	Body       map[string]any
}

func (e *APIError) Error() string {
	return fmt.Sprintf("BlockRun API error (status %d): %s", e.StatusCode, e.Message)
}

// PaymentError represents an error during payment processing.
type PaymentError struct {
	Message string
}

func (e *PaymentError) Error() string {
	return fmt.Sprintf("Payment error: %s", e.Message)
}

// ValidationError represents an input validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("Validation error for %s: %s", e.Field, e.Message)
}
