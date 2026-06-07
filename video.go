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

	// Available video models (pass as VideoGenerateOptions.Model):
	//   xai/grok-imagine-video       $0.05/sec, 8s default
	//   bytedance/seedance-1.5-pro   $0.03/sec, 5s default up to 10s, 720p
	//   bytedance/seedance-2.0-fast  $0.15/sec, ~60-80s gen, sweet spot
	//   bytedance/seedance-2.0       $0.30/sec, 720p Pro
	// The client passes Model through; no enum gating.

	// DefaultVideoTimeout is the default timeout for video generation
	// (video gen + polling can take up to 3 minutes).
	DefaultVideoTimeout = 300 * time.Second
)

// VideoClient is the BlockRun Video Generation client.
//
// Generates short AI videos via x402 micropayments on Base chain. The
// client blocks until the video is ready because the gateway handles
// async polling internally. Supports xAI Grok Imagine Video and
// ByteDance Seedance (1.5-pro / 2.0-fast / 2.0).
//
// Seedance 2.0 fast/pro additionally accept a RealFaceAssetID — a
// "ta_xxxxxx" face/character asset for identity consistency across
// multiple videos. The asset can be either a Virtual Portrait
// (AI-generated character, enroll via PortraitClient) or a RealFace
// (a real person's likeness, enroll via RealFaceClient). Both flows
// return the same "ta_" id and cost $0.01 USDC. seedance-1.5-pro does
// NOT support these assets, and RealFaceAssetID is mutually exclusive
// with ImageURL.
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
	// Mutually exclusive with RealFaceAssetID.
	ImageURL string `json:"image_url,omitempty"`
	// RealFaceAssetID is a "ta_xxxxxx" face/character asset for identity
	// consistency — either a Virtual Portrait (via PortraitClient) or a
	// RealFace (via RealFaceClient). Seedance 2.0 fast/pro only. Mutually
	// exclusive with ImageURL.
	RealFaceAssetID string `json:"real_face_asset_id,omitempty"`
	// LastFrameURL enables first-and-last-frame interpolation: a second image
	// that seeds the FINAL frame so the model tweens from ImageURL →
	// LastFrameURL. Requires ImageURL (the first frame) and a Seedance model
	// (bytedance/seedance-1.5-pro, seedance-2.0, or seedance-2.0-fast).
	// Priced identically to image-to-video. Mutually exclusive with
	// RealFaceAssetID.
	LastFrameURL string `json:"last_frame_url,omitempty"`
	// ReferenceImageURLs is the omni / multi-reference input: up to 9
	// reference image URLs for character/style consistency (Seedance 2.0
	// only). Cite them as "image 1", "image 2" in the prompt. Mutually
	// exclusive with ImageURL / LastFrameURL / RealFaceAssetID.
	ReferenceImageURLs []string `json:"reference_image_urls,omitempty"`
	// DurationSeconds is the duration to bill for (defaults to model's default duration).
	DurationSeconds int `json:"duration_seconds,omitempty"`
	// AspectRatio overrides the output aspect ratio — "adaptive" / "16:9" /
	// "9:16" / "1:1" / "4:3" / "3:4" / "21:9" / "9:21". Seedance only; Grok
	// ignores it.
	AspectRatio string `json:"aspect_ratio,omitempty"`
	// Resolution overrides the output resolution — "360p" / "480p" / "720p" /
	// "1080p" / "4K". Seedance defaults to "720p"; Grok ignores it.
	Resolution string `json:"resolution,omitempty"`
	// GenerateAudio toggles synced audio in the output. Seedance defaults to
	// true for text-to-video and false for image- or face-conditioned
	// generation; Grok ignores it. Use a pointer so the field can be left
	// unset (nil) to defer to the model default.
	GenerateAudio *bool `json:"generate_audio,omitempty"`
	// Seed is a deterministic generation seed (Seedance only). Use a pointer
	// so 0 is a valid explicit seed.
	Seed *int `json:"seed,omitempty"`
	// Watermark embeds the upstream watermark on the output (Seedance only).
	Watermark *bool `json:"watermark,omitempty"`
	// ReturnLastFrame also returns the final frame as an image alongside the
	// clip — useful for chaining (Seedance only).
	ReturnLastFrame bool `json:"return_last_frame,omitempty"`
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
	ID                     string  `json:"id"`
	Name                   string  `json:"name"`
	Provider               string  `json:"provider"`
	Description            string  `json:"description"`
	PricePerSecond         float64 `json:"pricePerSecond"`
	DefaultDurationSeconds int     `json:"defaultDurationSeconds"`
	MaxDurationSeconds     int     `json:"maxDurationSeconds"`
	SupportsImageInput     bool    `json:"supportsImageInput"`
	Available              bool    `json:"available"`
}

// Generate generates a video clip from a text prompt (or text + image / face asset).
//
// Blocks until the video is ready (30-120s typical).
func (c *VideoClient) Generate(ctx context.Context, prompt string, opts *VideoGenerateOptions) (*VideoResponse, error) {
	body := map[string]any{
		"prompt": prompt,
		"model":  DefaultVideoModel,
	}

	if opts != nil {
		if opts.ImageURL != "" && opts.RealFaceAssetID != "" {
			return nil, &ValidationError{
				Field:   "RealFaceAssetID",
				Message: "ImageURL and RealFaceAssetID are mutually exclusive; pass at most one",
			}
		}
		if opts.RealFaceAssetID != "" && !strings.HasPrefix(opts.RealFaceAssetID, "ta_") {
			return nil, &ValidationError{
				Field:   "RealFaceAssetID",
				Message: "RealFaceAssetID must start with 'ta_' (a Virtual Portrait or RealFace asset id, e.g. 'ta_abc123xyz' — enroll via PortraitClient or RealFaceClient)",
			}
		}
		if opts.LastFrameURL != "" && opts.ImageURL == "" {
			return nil, &ValidationError{
				Field:   "LastFrameURL",
				Message: "LastFrameURL requires ImageURL: ImageURL seeds the FIRST frame and LastFrameURL the FINAL frame — send both",
			}
		}
		if opts.LastFrameURL != "" && opts.RealFaceAssetID != "" {
			return nil, &ValidationError{
				Field:   "LastFrameURL",
				Message: "LastFrameURL and RealFaceAssetID are mutually exclusive; first-and-last-frame uses ImageURL + LastFrameURL",
			}
		}
		if len(opts.ReferenceImageURLs) > 0 {
			if opts.ImageURL != "" || opts.LastFrameURL != "" || opts.RealFaceAssetID != "" {
				return nil, &ValidationError{
					Field:   "ReferenceImageURLs",
					Message: "ReferenceImageURLs is mutually exclusive with ImageURL, LastFrameURL, and RealFaceAssetID",
				}
			}
			if len(opts.ReferenceImageURLs) > 9 {
				return nil, &ValidationError{
					Field:   "ReferenceImageURLs",
					Message: "ReferenceImageURLs accepts at most 9 images",
				}
			}
		}
		if opts.Model != "" {
			body["model"] = opts.Model
		}
		if opts.ImageURL != "" {
			body["image_url"] = opts.ImageURL
		}
		if opts.LastFrameURL != "" {
			body["last_frame_url"] = opts.LastFrameURL
		}
		if len(opts.ReferenceImageURLs) > 0 {
			body["reference_image_urls"] = opts.ReferenceImageURLs
		}
		if opts.RealFaceAssetID != "" {
			body["real_face_asset_id"] = opts.RealFaceAssetID
		}
		if opts.DurationSeconds > 0 {
			body["duration_seconds"] = opts.DurationSeconds
		}
		if opts.AspectRatio != "" {
			body["aspect_ratio"] = opts.AspectRatio
		}
		if opts.Resolution != "" {
			body["resolution"] = opts.Resolution
		}
		if opts.GenerateAudio != nil {
			body["generate_audio"] = *opts.GenerateAudio
		}
		if opts.Seed != nil {
			body["seed"] = *opts.Seed
		}
		if opts.Watermark != nil {
			body["watermark"] = *opts.Watermark
		}
		if opts.ReturnLastFrame {
			body["return_last_frame"] = true
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
