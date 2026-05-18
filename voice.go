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
	// DefaultVoiceTimeout is the default timeout for voice call initiation.
	// Initiation returns quickly; the call itself runs upstream — poll via GetCallStatus.
	DefaultVoiceTimeout = 60 * time.Second

	// VoiceCallPriceUSD is the settled price per outbound voice call.
	VoiceCallPriceUSD = 0.54
)

// Built-in Bland.ai voice presets. Any string is accepted by the upstream API
// (custom Bland.ai voice IDs work too) — these are just convenient defaults.
const (
	VoiceNat     = "nat"
	VoiceJosh    = "josh"
	VoiceMaya    = "maya"
	VoiceJune    = "june"
	VoicePaige   = "paige"
	VoiceDerek   = "derek"
	VoiceFlorian = "florian"
)

// CallModel is the Bland.ai conversation model tier.
type CallModel string

const (
	CallModelBase     CallModel = "base"
	CallModelEnhanced CallModel = "enhanced"
	CallModelTurbo    CallModel = "turbo"
)

// VoiceClient is the BlockRun Voice Call client. It initiates AI-powered
// outbound phone calls with automatic x402 micropayments.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine — only signatures are transmitted.
type VoiceClient struct {
	*baseClient
}

// VoiceClientOption configures a VoiceClient.
type VoiceClientOption func(*VoiceClient)

// WithVoiceAPIURL sets a custom API URL for the voice client.
func WithVoiceAPIURL(url string) VoiceClientOption {
	return func(c *VoiceClient) {
		c.apiURL = strings.TrimSuffix(url, "/")
	}
}

// WithVoiceTimeout sets the HTTP timeout for the voice client.
func WithVoiceTimeout(timeout time.Duration) VoiceClientOption {
	return func(c *VoiceClient) {
		c.httpClient.Timeout = timeout
	}
}

// WithVoiceHTTPClient sets a custom HTTP client for the voice client.
func WithVoiceHTTPClient(client *http.Client) VoiceClientOption {
	return func(c *VoiceClient) {
		c.httpClient = client
	}
}

// NewVoiceClient creates a new BlockRun Voice client.
//
// If privateKey is empty, it will be read from the BLOCKRUN_WALLET_KEY
// or BASE_CHAIN_WALLET_KEY environment variable.
func NewVoiceClient(privateKey string, opts ...VoiceClientOption) (*VoiceClient, error) {
	bc, err := newBaseClient(privateKey, "", DefaultVoiceTimeout)
	if err != nil {
		return nil, err
	}

	client := &VoiceClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	bc.checkEnvAPIURL()
	return client, nil
}

// CallOptions are the parameters for initiating an AI-powered phone call.
type CallOptions struct {
	// To is the destination phone number in E.164 format (e.g. "+14155552671").
	// US and Canada supported. Required.
	To string

	// Task describes what the AI agent should do on the call (10–4000 chars). Required.
	Task string

	// From is your provisioned BlockRun caller-ID number (E.164).
	//
	// Must be a number your wallet owns (see PhoneClient.BuyNumber). Resolution
	// rules on the backend if From is empty:
	//   - exactly one active number owned → that number is used automatically
	//   - zero active numbers           → 403 "no_active_number"
	//   - two or more active numbers    → 400 "ambiguous_from" (set From explicitly)
	From string

	// Voice is a preset name (VoiceNat, …) or any custom Bland.ai voice ID.
	Voice string

	// MaxDuration is the maximum call length in minutes (1–30, default 5).
	MaxDuration int

	// Language is a BCP-47 code for STT/TTS (default "en-US").
	Language string

	// FirstSentence is an optional opening line spoken by the agent.
	FirstSentence string

	// WaitForGreeting waits for the recipient to speak first when set.
	WaitForGreeting *bool

	// InterruptionThreshold tunes how readily the agent yields to interruptions
	// (50–500 ms). Zero leaves the server default.
	InterruptionThreshold int

	// Model selects the conversation model tier ("", "base", "enhanced", "turbo").
	Model CallModel
}

// CallInitiatedResponse is returned by Call once the AI agent has been queued.
type CallInitiatedResponse struct {
	CallID  string `json:"call_id"`
	Status  string `json:"status"`
	PollURL string `json:"poll_url"`
	Message string `json:"message,omitempty"`
	// TxHash is the on-chain payment receipt (set by the SDK from response header).
	TxHash string `json:"txHash,omitempty"`
}

// CallStatusResponse mirrors Bland.ai's call record. Most fields populate only
// after the call ends.
type CallStatusResponse struct {
	CallID                 string                   `json:"call_id,omitempty"`
	Status                 string                   `json:"status,omitempty"`
	To                     string                   `json:"to,omitempty"`
	From                   string                   `json:"from,omitempty"`
	StartedAt              *string                  `json:"started_at,omitempty"`
	EndedAt                *string                  `json:"ended_at,omitempty"`
	CallLength             float64                  `json:"call_length,omitempty"`
	RecordingURL           *string                  `json:"recording_url,omitempty"`
	ConcatenatedTranscript *string                  `json:"concatenated_transcript,omitempty"`
	Transcripts            []map[string]interface{} `json:"transcripts,omitempty"`
	EndedReason            *string                  `json:"ended_reason,omitempty"`
	// Extra captures any other fields Bland.ai returns.
	Extra map[string]interface{} `json:"-"`
}

// Call initiates an AI-powered outbound phone call. Costs $0.54 per call.
// Returns immediately once queued — poll GetCallStatus for transcript/recording.
func (c *VoiceClient) Call(ctx context.Context, opts CallOptions) (*CallInitiatedResponse, error) {
	if strings.TrimSpace(opts.To) == "" {
		return nil, fmt.Errorf("'to' phone number is required (E.164 format)")
	}
	task := strings.TrimSpace(opts.Task)
	if len(task) < 10 {
		return nil, fmt.Errorf("'task' must be at least 10 characters")
	}
	if len(task) > 4000 {
		return nil, fmt.Errorf("'task' must be at most 4000 characters")
	}

	maxDuration := opts.MaxDuration
	if maxDuration == 0 {
		maxDuration = 5
	}
	if maxDuration < 1 || maxDuration > 30 {
		return nil, fmt.Errorf("MaxDuration must be between 1 and 30 minutes")
	}

	if opts.InterruptionThreshold != 0 && (opts.InterruptionThreshold < 50 || opts.InterruptionThreshold > 500) {
		return nil, fmt.Errorf("InterruptionThreshold must be between 50 and 500")
	}

	if opts.Model != "" && opts.Model != CallModelBase && opts.Model != CallModelEnhanced && opts.Model != CallModelTurbo {
		return nil, fmt.Errorf("Model must be 'base', 'enhanced', or 'turbo'")
	}

	language := opts.Language
	if language == "" {
		language = "en-US"
	}

	body := map[string]any{
		"to":           strings.TrimSpace(opts.To),
		"task":         task,
		"max_duration": maxDuration,
		"language":     language,
	}
	if opts.From != "" {
		body["from"] = strings.TrimSpace(opts.From)
	}
	if opts.Voice != "" {
		body["voice"] = opts.Voice
	}
	if opts.FirstSentence != "" {
		body["first_sentence"] = strings.TrimSpace(opts.FirstSentence)
	}
	if opts.WaitForGreeting != nil {
		body["wait_for_greeting"] = *opts.WaitForGreeting
	}
	if opts.InterruptionThreshold != 0 {
		body["interruption_threshold"] = opts.InterruptionThreshold
	}
	if opts.Model != "" {
		body["model"] = string(opts.Model)
	}

	respBytes, err := c.doRequest(ctx, "/v1/voice/call", body)
	if err != nil {
		return nil, err
	}

	var result CallInitiatedResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// GetCallStatus polls the status of an in-progress or completed call.
// This endpoint is free — no x402 payment is required.
func (c *VoiceClient) GetCallStatus(ctx context.Context, callID string) (*CallStatusResponse, error) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return nil, fmt.Errorf("callID is required")
	}

	endpoint := "/v1/voice/call/" + urlQueryEscape(callID)
	respBytes, err := c.doGet(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	var status CallStatusResponse
	if err := json.Unmarshal(respBytes, &status); err != nil {
		return nil, fmt.Errorf("failed to decode call status: %w", err)
	}

	// Stash any unmapped fields in Extra so callers can read them without losing data.
	var raw map[string]interface{}
	if err := json.Unmarshal(respBytes, &raw); err == nil {
		known := map[string]struct{}{
			"call_id": {}, "status": {}, "to": {}, "from": {},
			"started_at": {}, "ended_at": {}, "call_length": {},
			"recording_url": {}, "concatenated_transcript": {},
			"transcripts": {}, "ended_reason": {},
		}
		extra := make(map[string]interface{})
		for k, v := range raw {
			if _, isKnown := known[k]; !isKnown {
				extra[k] = v
			}
		}
		if len(extra) > 0 {
			status.Extra = extra
		}
	}

	return &status, nil
}
