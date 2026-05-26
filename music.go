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
	// DefaultMusicModel is the default music generation model.
	DefaultMusicModel = "minimax/music-2.5+"

	// Available music models (pass as MusicGenerateOptions.Model):
	//   minimax/music-2.5+   $0.1575/track, ~3 min, vocals or instrumental
	//   minimax/music-2.5    $0.1575/track

	// DefaultMusicTimeout is the default timeout for music generation
	// (music gen takes 1-3 minutes).
	DefaultMusicTimeout = 210 * time.Second
)

// MusicClient is the BlockRun Music Generation client.
//
// Generates full-length (~3 minute) music tracks using MiniMax Music 2.5+
// with automatic x402 micropayments on Base chain. Pricing: $0.1575/track.
//
// Generated URLs expire in ~24h — download immediately if you need to keep
// the track.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine - only signatures are transmitted.
type MusicClient struct {
	*baseClient
}

// MusicClientOption configures a MusicClient.
type MusicClientOption func(*MusicClient)

// WithMusicAPIURL sets a custom API URL for the music client.
func WithMusicAPIURL(url string) MusicClientOption {
	return func(c *MusicClient) {
		c.apiURL = strings.TrimSuffix(url, "/")
	}
}

// WithMusicTimeout sets the HTTP timeout for the music client.
func WithMusicTimeout(timeout time.Duration) MusicClientOption {
	return func(c *MusicClient) {
		c.httpClient.Timeout = timeout
	}
}

// WithMusicHTTPClient sets a custom HTTP client for the music client.
func WithMusicHTTPClient(client *http.Client) MusicClientOption {
	return func(c *MusicClient) {
		c.httpClient = client
	}
}

// NewMusicClient creates a new BlockRun Music client.
//
// If privateKey is empty, it will be read from the BLOCKRUN_WALLET_KEY
// or BASE_CHAIN_WALLET_KEY environment variable.
func NewMusicClient(privateKey string, opts ...MusicClientOption) (*MusicClient, error) {
	bc, err := newBaseClient(privateKey, "", DefaultMusicTimeout)
	if err != nil {
		return nil, err
	}

	client := &MusicClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	bc.checkEnvAPIURL()

	return client, nil
}

// AudioTrack represents a single generated audio track.
type AudioTrack struct {
	// URL is the CDN URL for the track — download within ~24h.
	URL string `json:"url"`
	// DurationSeconds is the length of the generated track.
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	// Lyrics is the generated/echoed lyric text, when applicable.
	Lyrics string `json:"lyrics,omitempty"`
}

// MusicResponse represents the API response for music generation.
type MusicResponse struct {
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Data    []AudioTrack `json:"data"`
	TxHash  string       `json:"txHash,omitempty"`
}

// AudioModel represents an available audio/music model from the API.
type AudioModel struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Provider           string  `json:"provider"`
	Description        string  `json:"description"`
	PricePerTrack      float64 `json:"price_per_track"`
	MaxDurationSeconds int     `json:"max_duration_seconds"`
}

// MusicGenerateOptions contains optional parameters for music generation.
type MusicGenerateOptions struct {
	// Model is the music model ID (default: "minimax/music-2.5+").
	Model string
	// Instrumental generates a track without vocals. Defaults to true.
	// Use a pointer so the field can be left unset (nil → true).
	Instrumental *bool
	// Lyrics are custom lyrics. Cannot be combined with Instrumental=true.
	Lyrics string
}

// Generate generates a music track from a text prompt.
//
// Takes 1-3 minutes. Returns a CDN URL valid for ~24h. By default the track
// is instrumental; pass Instrumental=false with Lyrics for a vocal track.
func (c *MusicClient) Generate(ctx context.Context, prompt string, opts *MusicGenerateOptions) (*MusicResponse, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, &ValidationError{Field: "prompt", Message: "prompt is required"}
	}

	// Default: instrumental track.
	instrumental := true
	model := DefaultMusicModel
	lyrics := ""

	if opts != nil {
		if opts.Model != "" {
			model = opts.Model
		}
		if opts.Instrumental != nil {
			instrumental = *opts.Instrumental
		}
		lyrics = strings.TrimSpace(opts.Lyrics)
	}

	if instrumental && lyrics != "" {
		return nil, &ValidationError{
			Field:   "Lyrics",
			Message: "cannot specify Lyrics when Instrumental is true",
		}
	}

	body := map[string]any{
		"model":        model,
		"prompt":       prompt,
		"instrumental": instrumental,
	}
	if lyrics != "" {
		body["lyrics"] = lyrics
	}

	respBytes, err := c.doRequest(ctx, "/v1/audio/generations", body)
	if err != nil {
		return nil, err
	}

	var musicResp MusicResponse
	if err := json.Unmarshal(respBytes, &musicResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &musicResp, nil
}
