package blockrun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	// DefaultVideoTimeout is the default per-HTTP-call timeout (submit or a
	// single poll). The overall generate budget is videoPollBudget.
	DefaultVideoTimeout = 300 * time.Second

	// videoPollInterval is the wait between poll attempts.
	videoPollInterval = 5 * time.Second
	// videoPollBudget is the overall wall-clock budget for submit + polling.
	// If upstream runs past this, Generate returns without charging.
	videoPollBudget = 300 * time.Second
	// videoMaxTimeoutSeconds is the signed-auth window floor — bumped above the
	// server default so the PAYMENT-SIGNATURE stays valid across the poll window.
	videoMaxTimeoutSeconds = 600
)

// VideoClient is the BlockRun Video Generation client.
//
// Generates short AI videos via x402 micropayments on Base chain. The
// gateway is async (POST submit returns 202 { id, poll_url }); this client
// signs once, submits, then polls poll_url with the same wallet's
// PAYMENT-SIGNATURE until the job completes. Settlement happens on the first
// completed poll, so a caller who gives up is never charged. Supports xAI
// Grok Imagine Video and
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
	// pollInterval is the wait between poll attempts. Defaults to
	// videoPollInterval; overridable (mainly for tests).
	pollInterval time.Duration
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

	client := &VideoClient{baseClient: bc, pollInterval: videoPollInterval}

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

	return c.submitVideoAndPoll(ctx, "/v1/videos/generations", body)
}

// GenerateFromContent generates a video from a standard Seedance content[] body.
//
// It targets the gateway's POST /v1/videos endpoint, which accepts the
// mainstream multimodal content array (text + a single reference image) used by
// other Seedance APIs — so a caller already holding a content[]-shaped request
// can submit it unchanged. The gateway validates unsupported inputs BEFORE
// charging, then delegates to the same x402 pipeline as Generate.
//
// Most SDK users should prefer Generate (structured options like ImageURL /
// LastFrameURL); this exists for migrating existing content[] payloads with no
// reshaping. Image inputs belong inside the content items, not in opts — only
// Model and the scalar render fields (Resolution, DurationSeconds, AspectRatio,
// GenerateAudio, Seed, Watermark) are forwarded from opts.
func (c *VideoClient) GenerateFromContent(ctx context.Context, content []map[string]any, opts *VideoGenerateOptions) (*VideoResponse, error) {
	if len(content) == 0 {
		return nil, &ValidationError{
			Field:   "content",
			Message: "content must be a non-empty list of Seedance content items",
		}
	}

	body := map[string]any{
		"content": content,
	}

	if opts != nil {
		if opts.Model != "" {
			body["model"] = opts.Model
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
	}

	return c.submitVideoAndPoll(ctx, "/v1/videos", body)
}

// submitVideoAndPoll runs the async video pipeline shared by Generate and
// GenerateFromContent: POST submit (402 -> sign -> 202 { id, poll_url }), then
// GET-poll poll_url with the same wallet's PAYMENT-SIGNATURE until the job
// reaches "completed". The gateway settles only on the first completed poll, so
// upstream failure or a caller giving up costs nothing.
func (c *VideoClient) submitVideoAndPoll(ctx context.Context, submitPath string, body map[string]any) (*VideoResponse, error) {
	submitURL := c.apiURL + submitPath

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request body: %w", err)
	}

	// Step 1: unauth POST -> 402 with payment requirements.
	req1, err := http.NewRequestWithContext(ctx, "POST", submitURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := c.httpClient.Do(req1)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	paymentHeader := resp1.Header.Get("payment-required")
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()

	if resp1.StatusCode != http.StatusPaymentRequired {
		return nil, &APIError{
			StatusCode: resp1.StatusCode,
			Message:    fmt.Sprintf("expected 402 on video submit, got %d: %s", resp1.StatusCode, string(body1)),
		}
	}
	if paymentHeader == "" {
		return nil, &PaymentError{Message: "402 response but no payment requirements found"}
	}

	// Step 2: sign the payment authorization.
	paymentReq, err := ParsePaymentRequired(paymentHeader)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to parse payment requirements: %v", err)}
	}
	paymentOption, err := ExtractPaymentDetails(paymentReq)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to extract payment details: %v", err)}
	}
	resourceURL := paymentReq.Resource.URL
	if resourceURL == "" {
		resourceURL = submitURL
	}
	if paymentOption.MaxTimeoutSeconds < videoMaxTimeoutSeconds {
		// Floor the validity window so the same signature survives the poll window
		// (Base/EIP-712 only; the Solana path re-derives validity from the blockhash).
		paymentOption.MaxTimeoutSeconds = videoMaxTimeoutSeconds
	}
	paymentPayload, err := c.createPaymentPayload(paymentOption, resourceURL, paymentReq.Resource.Description, paymentReq.Extensions)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to create payment: %v", err)}
	}

	// Step 3: submit with payment -> 202 { id, poll_url }.
	req2, err := http.NewRequestWithContext(ctx, "POST", submitURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create submit request: %w", err)
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("PAYMENT-SIGNATURE", paymentPayload)
	resp2, err := c.httpClient.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("submit request failed: %w", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()

	if resp2.StatusCode == http.StatusPaymentRequired {
		return nil, &PaymentError{Message: "Payment was rejected. Check your wallet balance."}
	}
	if resp2.StatusCode != http.StatusOK && resp2.StatusCode != http.StatusAccepted {
		return nil, &APIError{
			StatusCode: resp2.StatusCode,
			Message:    fmt.Sprintf("Submit failed: %s", string(body2)),
		}
	}

	var submitData struct {
		ID      string `json:"id"`
		PollURL string `json:"poll_url"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(body2, &submitData); err != nil {
		return nil, fmt.Errorf("failed to decode submit response: %w", err)
	}
	if submitData.ID == "" || submitData.PollURL == "" {
		return nil, &APIError{
			StatusCode: resp2.StatusCode,
			Message:    fmt.Sprintf("submit response missing id/poll_url: %s", string(body2)),
		}
	}

	pollURL := c.absoluteURL(submitData.PollURL)

	// Step 4: poll until completed. Base reuses the submit signature; Solana
	// re-signs with a fresh blockhash once the current one nears expiry.
	deadline := time.Now().Add(videoPollBudget)
	lastStatus := submitData.Status
	if lastStatus == "" {
		lastStatus = "queued"
	}
	pollSig := paymentPayload
	lastSigned := time.Now()

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.pollInterval):
		}

		pollReq, err := http.NewRequestWithContext(ctx, "GET", pollURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create poll request: %w", err)
		}
		pollSig, lastSigned = c.pollPaymentPayload(pollSig, lastSigned, paymentOption, resourceURL, paymentReq.Resource.Description, paymentReq.Extensions)
		pollReq.Header.Set("PAYMENT-SIGNATURE", pollSig)
		pollResp, err := c.httpClient.Do(pollReq)
		if err != nil {
			return nil, fmt.Errorf("poll request failed: %w", err)
		}
		pollBytes, _ := io.ReadAll(pollResp.Body)
		pollResp.Body.Close()

		var pollData map[string]any
		_ = json.Unmarshal(pollBytes, &pollData)
		if s, ok := pollData["status"].(string); ok && s != "" {
			lastStatus = s
		}

		if pollResp.StatusCode == http.StatusAccepted && (lastStatus == "queued" || lastStatus == "in_progress") {
			continue
		}
		if lastStatus == "failed" {
			return nil, &APIError{
				StatusCode: pollResp.StatusCode,
				Message:    fmt.Sprintf("Upstream generation failed: %s", string(pollBytes)),
			}
		}
		// Terminal success is keyed on status, NOT the HTTP code — the gateway
		// settles on-chain the moment a poll reports "completed", so coupling
		// success to a literal 200 would spin to the deadline (and return a
		// "not charged" error) on a completed-but-non-200 poll, even though the
		// caller was already charged. Record the cost as soon as completion is
		// observed (the charge is irreversible at that point), then decode.
		if lastStatus == "completed" {
			c.recordVideoCost(paymentOption.Amount, submitPath)
			var videoResp VideoResponse
			if err := json.Unmarshal(pollBytes, &videoResp); err != nil {
				return nil, fmt.Errorf("failed to decode response: %w", err)
			}
			if tx := pollResp.Header.Get("x-payment-receipt"); tx != "" {
				videoResp.TxHash = tx
			}
			return &videoResp, nil
		}
		// 504 on a poll = transient upstream hiccup; keep polling. Any other
		// non-2xx is a hard failure.
		if pollResp.StatusCode != http.StatusOK &&
			pollResp.StatusCode != http.StatusAccepted &&
			pollResp.StatusCode != http.StatusGatewayTimeout {
			return nil, &APIError{
				StatusCode: pollResp.StatusCode,
				Message:    fmt.Sprintf("Poll failed: %s", string(pollBytes)),
			}
		}
	}

	return nil, &APIError{
		StatusCode: http.StatusGatewayTimeout,
		Message: fmt.Sprintf(
			"Video generation did not complete within %.0fs (last status: %s). No payment was taken.",
			videoPollBudget.Seconds(), lastStatus,
		),
	}
}

// absoluteURL resolves a server-supplied relative poll_url against the API host.
func (c *VideoClient) absoluteURL(u string) string {
	return c.resolvePollURL(u)
}

// recordVideoCost tracks spending for a completed video job, mirroring the
// accounting baseClient does for synchronous paid calls.
func (c *VideoClient) recordVideoCost(amount, submitPath string) {
	c.recordSettledCost(amount, submitPath)
}
