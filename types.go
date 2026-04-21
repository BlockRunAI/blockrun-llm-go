// Package blockrun provides a Go SDK for BlockRun's x402-powered LLM gateway.
//
// SECURITY NOTE - Private Key Handling:
// Your private key NEVER leaves your machine. Here's what happens:
// 1. Key stays local - only used to sign an EIP-712 typed data message
// 2. Only the SIGNATURE is sent in the PAYMENT-SIGNATURE header
// 3. BlockRun verifies the signature on-chain via Coinbase CDP facilitator
// 4. Your actual private key is NEVER transmitted to any server
package blockrun

import (
	"encoding/json"
	"fmt"
)

// jsonUnmarshal is aliased so the Model UnmarshalJSON can call encoding/json
// without risking infinite recursion through its own method.
var jsonUnmarshal = json.Unmarshal

// ChatMessage represents a message in the conversation.
type ChatMessage struct {
	Role       string     `json:"role"`                   // "system", "user", "assistant", or "tool"
	Content    string     `json:"content"`                // Message content
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // Tool calls from assistant
	ToolCallID string     `json:"tool_call_id,omitempty"` // ID of the tool call this message responds to
	// Extended fields returned by reasoning-capable upstream providers
	// (DeepSeek Reasoner, Grok 4 / 4.20 reasoning, xAI multi-agent, etc.).
	// Backend strips these from inbound requests but may forward them on
	// the response side, so we accept them as optional.
	ReasoningContent string `json:"reasoning_content,omitempty"`
	Thinking         string `json:"thinking,omitempty"`
}

// ChatCompletionOptions contains optional parameters for chat completion.
type ChatCompletionOptions struct {
	MaxTokens        int               `json:"max_tokens,omitempty"`
	Temperature      float64           `json:"temperature,omitempty"`
	TopP             float64           `json:"top_p,omitempty"`
	Search           bool              `json:"-"` // Enable xAI Live Search (shortcut)
	SearchParameters *SearchParameters `json:"search_parameters,omitempty"`
	Tools            []Tool            `json:"tools,omitempty"`
	ToolChoice       any               `json:"tool_choice,omitempty"` // string ("none","auto","required") or object
}

// SearchParameters contains xAI Live Search configuration.
type SearchParameters struct {
	Mode             string         `json:"mode,omitempty"` // "off", "auto", "on"
	Sources          []SearchSource `json:"sources,omitempty"`
	ReturnCitations  bool           `json:"return_citations,omitempty"`
	FromDate         string         `json:"from_date,omitempty"` // YYYY-MM-DD
	ToDate           string         `json:"to_date,omitempty"`   // YYYY-MM-DD
	MaxSearchResults int            `json:"max_search_results,omitempty"`
}

// SearchSource represents a search source configuration.
type SearchSource struct {
	Type             string   `json:"type"` // "web", "x", "news", "rss"
	Country          string   `json:"country,omitempty"`
	ExcludedWebsites []string `json:"excluded_websites,omitempty"`
	AllowedWebsites  []string `json:"allowed_websites,omitempty"`
	SafeSearch       bool     `json:"safe_search,omitempty"`
	// X-specific fields
	IncludedXHandles  []string `json:"included_x_handles,omitempty"`
	ExcludedXHandles  []string `json:"excluded_x_handles,omitempty"`
	PostFavoriteCount int      `json:"post_favorite_count,omitempty"`
	PostViewCount     int      `json:"post_view_count,omitempty"`
	// RSS-specific fields
	Links []string `json:"links,omitempty"`
}

// Tool represents a function tool for chat completion.
type Tool struct {
	Type     string       `json:"type"` // "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a callable function.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"` // JSON Schema
}

// ToolCall represents a tool call in an assistant message.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction contains the function name and arguments.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ChatResponse represents the API response for chat completions.
type ChatResponse struct {
	ID        string   `json:"id"`
	Object    string   `json:"object"`
	Created   int64    `json:"created"`
	Model     string   `json:"model"`
	Choices   []Choice `json:"choices"`
	Usage     Usage    `json:"usage"`
	Citations []string `json:"citations,omitempty"` // xAI Live Search citation URLs
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
	NumSourcesUsed   int `json:"num_sources_used,omitempty"` // xAI Live Search sources used
	// Anthropic prompt caching — populated on anthropic/* models when cache
	// headers are sent. Reads are cheaper; writes incur a one-time surcharge.
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
}

// ModelPricing mirrors the nested { input, output } / { flat } shape that
// /v1/models returns in the "pricing" key.
type ModelPricing struct {
	Input  float64 `json:"input,omitempty"`
	Output float64 `json:"output,omitempty"`
	Flat   float64 `json:"flat,omitempty"`
}

// Model represents an available model from the API.
//
// The struct has a custom UnmarshalJSON that accepts both the real API shape
// (owned_by / context_window / max_output / nested pricing) and the legacy
// flat shape used by older SDK code + mock tests. Callers can read either
// the new fields (Pricing.Input, ContextWindow) or the legacy aliases
// (InputPrice, ContextLimit) — they are kept in sync during unmarshalling.
type Model struct {
	ID          string   `json:"id"`
	Object      string   `json:"object,omitempty"`
	Created     int64    `json:"created,omitempty"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	// Provider is populated from either "owned_by" (real API) or "provider"
	// (legacy mock). Tag stays "owned_by" so Marshal emits the canonical key.
	Provider       string       `json:"owned_by,omitempty"`
	ContextWindow  int          `json:"context_window,omitempty"`
	MaxOutput      int          `json:"max_output,omitempty"`
	Categories     []string     `json:"categories,omitempty"`
	BillingMode    string       `json:"billing_mode,omitempty"`
	Pricing        ModelPricing `json:"pricing,omitempty"`
	// Legacy flat fields — populated from Pricing / ContextWindow for
	// backward compatibility with callers written against the old struct.
	// They carry `json:"-"` so Marshal doesn't emit duplicate keys.
	InputPrice   float64 `json:"-"`
	OutputPrice  float64 `json:"-"`
	FlatPrice    float64 `json:"-"`
	ContextLimit int     `json:"-"`
	// Type is set by ListAllModels to mark whether an entry came from the
	// LLM or image catalogue; not emitted by /v1/models itself.
	Type string `json:"type,omitempty"`
	// Hidden is reserved for deprecated/superseded entries. The backend
	// filters hidden models before sending, but downstream tooling may set
	// this when forwarding the full catalogue.
	Hidden bool `json:"hidden,omitempty"`
}

// UnmarshalJSON reads both real API responses (nested pricing, owned_by,
// context_window) and the legacy flat shape used by tests, and keeps the
// legacy aliases InputPrice / OutputPrice / ContextLimit in sync with the
// canonical nested fields.
func (m *Model) UnmarshalJSON(data []byte) error {
	type raw struct {
		ID            string          `json:"id"`
		Object        string          `json:"object,omitempty"`
		Created       int64           `json:"created,omitempty"`
		Name          string          `json:"name,omitempty"`
		Description   string          `json:"description,omitempty"`
		OwnedBy       string          `json:"owned_by,omitempty"`
		Provider      string          `json:"provider,omitempty"` // legacy
		ContextWindow int             `json:"context_window,omitempty"`
		ContextLimit  int             `json:"contextLimit,omitempty"` // legacy
		MaxOutput     int             `json:"max_output,omitempty"`
		Categories    []string        `json:"categories,omitempty"`
		BillingMode   string          `json:"billing_mode,omitempty"`
		Pricing       *ModelPricing   `json:"pricing,omitempty"`
		InputPrice    float64         `json:"inputPrice,omitempty"`  // legacy
		OutputPrice   float64         `json:"outputPrice,omitempty"` // legacy
		FlatPrice     float64         `json:"flat_price,omitempty"`  // legacy
		Type          string          `json:"type,omitempty"`
		Hidden        bool            `json:"hidden,omitempty"`
	}
	var r raw
	if err := jsonUnmarshal(data, &r); err != nil {
		return err
	}

	m.ID = r.ID
	m.Object = r.Object
	m.Created = r.Created
	m.Name = r.Name
	m.Description = r.Description
	if r.OwnedBy != "" {
		m.Provider = r.OwnedBy
	} else {
		m.Provider = r.Provider
	}
	m.Categories = r.Categories
	m.BillingMode = r.BillingMode
	m.Type = r.Type
	m.Hidden = r.Hidden

	if r.ContextWindow > 0 {
		m.ContextWindow = r.ContextWindow
	} else {
		m.ContextWindow = r.ContextLimit
	}
	m.MaxOutput = r.MaxOutput
	m.ContextLimit = m.ContextWindow

	if r.Pricing != nil {
		m.Pricing = *r.Pricing
	}
	if m.Pricing.Input == 0 && r.InputPrice > 0 {
		m.Pricing.Input = r.InputPrice
	}
	if m.Pricing.Output == 0 && r.OutputPrice > 0 {
		m.Pricing.Output = r.OutputPrice
	}
	if m.Pricing.Flat == 0 && r.FlatPrice > 0 {
		m.Pricing.Flat = r.FlatPrice
	}
	m.InputPrice = m.Pricing.Input
	m.OutputPrice = m.Pricing.Output
	m.FlatPrice = m.Pricing.Flat
	return nil
}

// AllModel represents a model from either LLM or image generation.
// Used by ListAllModels() to return a unified list.
type AllModel struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Provider      string   `json:"provider"`
	Type          string   `json:"type"` // "llm" or "image"
	// LLM-specific fields
	InputPrice    float64  `json:"inputPrice,omitempty"`
	OutputPrice   float64  `json:"outputPrice,omitempty"`
	ContextLimit  int      `json:"contextLimit,omitempty"`
	// Image-specific fields
	PricePerImage float64  `json:"pricePerImage,omitempty"`
	SupportedSizes []string `json:"supportedSizes,omitempty"`
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
