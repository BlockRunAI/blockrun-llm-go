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
	// DefaultVideoModel is the default video generation model.
	DefaultVideoModel = "xai/grok-imagine-video"

	// DefaultVideoTimeout is the default timeout for video generation
	// (video gen + polling can take up to 3 minutes).
	DefaultVideoTimeout = 300 * time.Second
)

// VideoClient is the BlockRun Video Generation client.
//
// Generates short AI videos using xAI's Grok Imagine Video via x402
// micropayments on Base chain. The client blocks until the video is
// ready because the gateway handles async polling internally.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine - only signatures are transmitted.
type VideoClient struct {
	*baseClient
}

// VideoClientOption is a function that configures a VideoClient.
type VideoClientOption func(*VideoClient)

// WithVideoAPIURL sets a custom API URL for the video client.
func WithVideoAPIURL(url string) VideoClientOption {
	return func(c *VideoClient) {
		c.apiURL = strings.TrimSuffix(url, "/")
	}
}

// WithVideoTimeout sets the HTTP timeout for the video client.
func WithVideoTimeout(timeout time.Duration) VideoClientOption {
	return func(c *VideoClient) {
		c.httpClient.Timeout = timeout
	}
}

// WithVideoHTTPClient sets a custom HTTP client for the video client.
func WithVideoHTTPClient(client *http.Client) VideoClientOption {
	return func(c *VideoClient) {
		c.httpClient = client
	}
}

// NewVideoClient creates a new BlockRun Video client.
//
// If privateKey is empty, it will be read from the BLOCKRUN_WALLET_KEY
// or BASE_CHAIN_WALLET_KEY environment variable.
func NewVideoClient(privateKey string, opts ...VideoClientOption) (*VideoClient, error) {
	bc, err := newBaseClient(privateKey, "", DefaultVideoTimeout)
	if err != nil {
		return nil, err
	}

	client := &VideoClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	bc.checkEnvAPIURL()

	return client, nil
}

// VideoGenerateOptions contains optional parameters for video generation.
type VideoGenerateOptions struct {
	// Model is the video model ID (default: "xai/grok-imagine-video").
	Model string `json:"model,omitempty"`
	// ImageURL is an optional seed image URL for image-to-video.
	ImageURL string `json:"image_url,omitempty"`
	// DurationSeconds is the duration to bill for (defaults to model's default duration).
	DurationSeconds int `json:"duration_seconds,omitempty"`
}

// VideoClip represents a single generated video clip.
type VideoClip struct {
	// URL is the permanent blockrun-hosted URL
	// (falls back to upstream URL if backup fails).
	URL string `json:"url"`
	// SourceURL is the original upstream URL (e.g. vidgen.x.ai).
	SourceURL string `json:"source_url,omitempty"`
	// DurationSeconds is the duration of the generated video.
	DurationSeconds int `json:"duration_seconds,omitempty"`
	// RequestID is the upstream provider's request id (xAI).
	RequestID string `json:"request_id,omitempty"`
	// BackedUp is true when the gateway mirrored the video to its GCS bucket.
	BackedUp bool `json:"backed_up,omitempty"`
}

// VideoResponse represents the API response for video generation.
type VideoResponse struct {
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Data    []VideoClip `json:"data"`
	TxHash  string      `json:"txHash,omitempty"`
}

// VideoModel represents an available video model from the API.
type VideoModel struct {
	ID                      string  `json:"id"`
	Name                    string  `json:"name"`
	Provider                string  `json:"provider"`
	Description             string  `json:"description"`
	PricePerSecond          float64 `json:"pricePerSecond"`
	DefaultDurationSeconds  int     `json:"defaultDurationSeconds"`
	MaxDurationSeconds      int     `json:"maxDurationSeconds"`
	SupportsImageInput      bool    `json:"supportsImageInput"`
	Available               bool    `json:"available"`
}

// Generate generates a video clip from a text prompt (or text + image).
//
// Blocks until the video is ready (30-120s typical).
func (c *VideoClient) Generate(ctx context.Context, prompt string, opts *VideoGenerateOptions) (*VideoResponse, error) {
	body := map[string]any{
		"prompt": prompt,
		"model":  DefaultVideoModel,
	}

	if opts != nil {
		if opts.Model != "" {
			body["model"] = opts.Model
		}
		if opts.ImageURL != "" {
			body["image_url"] = opts.ImageURL
		}
		if opts.DurationSeconds > 0 {
			body["duration_seconds"] = opts.DurationSeconds
		}
	}

	respBytes, err := c.doRequest(ctx, "/v1/videos/generations", body)
	if err != nil {
		return nil, err
	}

	var videoResp VideoResponse
	if err := json.Unmarshal(respBytes, &videoResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &videoResp, nil
}
