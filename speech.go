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
	// DefaultSpeechModel is the default text-to-speech model.
	DefaultSpeechModel = "elevenlabs/flash-v2.5"

	// DefaultSoundFxModel is the default sound-effect model.
	DefaultSoundFxModel = "elevenlabs/sound-effects"

	// Available speech models (pass as SpeechGenerateOptions.Model):
	//   elevenlabs/flash-v2.5        $0.05/1k chars, ~75ms latency, 32 languages (default)
	//   elevenlabs/turbo-v2.5        $0.05/1k chars, ~250ms latency, 32 languages
	//   elevenlabs/multilingual-v2   $0.10/1k chars, long-form narration, 29 languages
	//   elevenlabs/v3                $0.10/1k chars, max expressiveness, 70+ languages
	//
	// TTS price = (characters / 1000) x model rate, minimum $0.001/request.
	// Sound effects are flat $0.05/generation (up to 22s).

	// DefaultSpeechTimeout is the default timeout for speech synthesis
	// (synchronous, <1s for Flash; allow headroom for long-form input).
	DefaultSpeechTimeout = 120 * time.Second
)

// SpeechVoiceAliases are the friendly voice aliases accepted by
// /v1/audio/speech. Raw ElevenLabs voice_ids pass through unchanged.
// Mirrors the backend VOICE_ALIASES.
var SpeechVoiceAliases = []string{
	"sarah",   // Mature, reassuring, confident (default)
	"george",  // Warm, captivating storyteller
	"laura",   // Enthusiast, quirky
	"charlie", // Deep, confident, energetic
	"river",   // Relaxed, neutral, informative
	"roger",   // Laid-back, casual, resonant
	"callum",  // Husky trickster
	"harry",   // Fierce warrior
}

// SpeechClient is the BlockRun Voice client (ElevenLabs text-to-speech and
// sound effects) with automatic x402 micropayments on Base chain.
//
// TTS pricing scales with input characters; sound effects are flat
// $0.05/generation.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine - only signatures are transmitted.
type SpeechClient struct {
	*baseClient
}

// SpeechClientOption configures a SpeechClient.
type SpeechClientOption func(*SpeechClient)

// WithSpeechAPIURL sets a custom API URL for the speech client.
func WithSpeechAPIURL(url string) SpeechClientOption {
	return func(c *SpeechClient) {
		c.apiURL = strings.TrimSuffix(url, "/")
	}
}

// WithSpeechTimeout sets the HTTP timeout for the speech client.
func WithSpeechTimeout(timeout time.Duration) SpeechClientOption {
	return func(c *SpeechClient) {
		c.httpClient.Timeout = timeout
	}
}

// WithSpeechHTTPClient sets a custom HTTP client for the speech client.
func WithSpeechHTTPClient(client *http.Client) SpeechClientOption {
	return func(c *SpeechClient) {
		c.httpClient = client
	}
}

// NewSpeechClient creates a new BlockRun Speech client.
//
// If privateKey is empty, it will be read from the BLOCKRUN_WALLET_KEY
// or BASE_CHAIN_WALLET_KEY environment variable.
func NewSpeechClient(privateKey string, opts ...SpeechClientOption) (*SpeechClient, error) {
	bc, err := newBaseClient(privateKey, "", DefaultSpeechTimeout)
	if err != nil {
		return nil, err
	}

	client := &SpeechClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	bc.checkEnvAPIURL()

	return client, nil
}

// SpeechAudio represents a single synthesized audio clip.
type SpeechAudio struct {
	// URL is the hosted audio URL.
	URL string `json:"url"`
	// Format is the audio format (mp3, opus, pcm, wav).
	Format string `json:"format,omitempty"`
	// Characters is the billed input character count (TTS only).
	Characters int `json:"characters,omitempty"`
	// Credits is the upstream character cost, when reported.
	Credits float64 `json:"credits,omitempty"`
}

// SpeechResponse represents the API response for speech synthesis or
// sound-effect generation.
type SpeechResponse struct {
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Data    []SpeechAudio `json:"data"`
	TxHash  string        `json:"txHash,omitempty"`
}

// VoiceInfo represents a voice entry returned by GET /v1/audio/voices.
type VoiceInfo struct {
	VoiceID    string            `json:"voice_id"`
	Name       string            `json:"name,omitempty"`
	Alias      string            `json:"alias,omitempty"`
	Category   string            `json:"category,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	PreviewURL string            `json:"preview_url,omitempty"`
}

// SpeechGenerateOptions contains optional parameters for speech synthesis.
type SpeechGenerateOptions struct {
	// Model is the speech model ID (default: "elevenlabs/flash-v2.5").
	Model string
	// Voice is a friendly alias (see SpeechVoiceAliases) or a raw
	// ElevenLabs voice_id (default: "sarah").
	Voice string
	// ResponseFormat is the audio format: mp3 (default), opus, pcm, wav.
	ResponseFormat string
	// Speed is the playback speed 0.7-1.2. Use a pointer so the field
	// can be left unset (nil → model default).
	Speed *float64
}

// SoundEffectOptions contains optional parameters for sound-effect generation.
type SoundEffectOptions struct {
	// Model is the sound-effect model ID (default: "elevenlabs/sound-effects").
	Model string
	// DurationSeconds is the target duration 0.5-22s (nil → auto).
	DurationSeconds *float64
	// PromptInfluence is 0-1; higher follows the prompt more literally.
	PromptInfluence *float64
	// ResponseFormat is the audio format: mp3 (default), opus, pcm, wav.
	ResponseFormat string
}

// Generate synthesizes speech from text (OpenAI-compatible TTS).
//
// Price scales with character count: (chars / 1000) x model rate, minimum
// $0.001/request. Synthesis is synchronous. Per-model character caps apply
// (flash/turbo 40k, multilingual-v2 10k, v3 5k).
func (c *SpeechClient) Generate(ctx context.Context, input string, opts *SpeechGenerateOptions) (*SpeechResponse, error) {
	if strings.TrimSpace(input) == "" {
		return nil, &ValidationError{Field: "input", Message: "input is required"}
	}

	model := DefaultSpeechModel
	body := map[string]any{
		"input": input,
	}

	if opts != nil {
		if opts.Model != "" {
			model = opts.Model
		}
		if opts.Voice != "" {
			body["voice"] = opts.Voice
		}
		if opts.ResponseFormat != "" {
			body["response_format"] = opts.ResponseFormat
		}
		if opts.Speed != nil {
			body["speed"] = *opts.Speed
		}
	}
	body["model"] = model

	respBytes, err := c.doRequest(ctx, "/v1/audio/speech", body)
	if err != nil {
		return nil, err
	}

	var speechResp SpeechResponse
	if err := json.Unmarshal(respBytes, &speechResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &speechResp, nil
}

// SoundEffect generates a cinematic sound effect from a text prompt.
//
// Flat $0.05/generation, up to 22 seconds of audio. Text is capped at
// 1000 characters server-side.
func (c *SpeechClient) SoundEffect(ctx context.Context, text string, opts *SoundEffectOptions) (*SpeechResponse, error) {
	if strings.TrimSpace(text) == "" {
		return nil, &ValidationError{Field: "text", Message: "text is required"}
	}

	model := DefaultSoundFxModel
	body := map[string]any{
		"text": text,
	}

	if opts != nil {
		if opts.Model != "" {
			model = opts.Model
		}
		if opts.DurationSeconds != nil {
			body["duration_seconds"] = *opts.DurationSeconds
		}
		if opts.PromptInfluence != nil {
			body["prompt_influence"] = *opts.PromptInfluence
		}
		if opts.ResponseFormat != "" {
			body["response_format"] = opts.ResponseFormat
		}
	}
	body["model"] = model

	respBytes, err := c.doRequest(ctx, "/v1/audio/sound-effects", body)
	if err != nil {
		return nil, err
	}

	var speechResp SpeechResponse
	if err := json.Unmarshal(respBytes, &speechResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &speechResp, nil
}

// ListVoices lists available voices for TTS (free, rate-limited
// 60 req/min/IP).
//
// Pass a voice's Alias (if present) or VoiceID as
// SpeechGenerateOptions.Voice on Generate.
func (c *SpeechClient) ListVoices(ctx context.Context) ([]VoiceInfo, error) {
	respBytes, err := c.doGet(ctx, "/v1/audio/voices")
	if err != nil {
		return nil, err
	}

	var payload struct {
		Data []VoiceInfo `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &payload); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return payload.Data, nil
}
