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
	//   xai/grok-imagine-video       8s default
	//   xai/grok-imagine-video-1.5
	//   bytedance/seedance-1.5-pro   5s default up to 10s, 720p
	//   bytedance/seedance-2.0-mini
	//   bytedance/seedance-2.0-fast  ~60-80s gen, sweet spot
	//   bytedance/seedance-2.0       720p Pro
	//   bytedance/seedance-2.5
	// Live prices: https://blockrun.ai/models
	// The client passes Model through; per-model checks cover only the
	// Seedance fields (validateVideoOptions).

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
	// ReferenceImageURLs is the omni / multi-reference input for
	// character/style consistency: up to 9 images on Seedance 2.0 / 2.0-fast /
	// 2.0-mini, up to 30 on 2.5. Cite them as "image 1", "image 2" in the
	// prompt. Mutually exclusive with ImageURL / LastFrameURL / RealFaceAssetID.
	ReferenceImageURLs []string `json:"reference_image_urls,omitempty"`
	// ReferenceVideos and ReferenceAudios are reference clips, up to 3 of each,
	// on Seedance 2.0 / 2.0-fast / 2.0-mini / 2.5. Audio needs a reference image
	// or video in the same request. Each clip bills at the model's reference
	// ceiling whatever its real length (15.2s on the 2.0 family, 30.2s on 2.5),
	// because the gateway only sees URLs: check the quote before relying on them.
	ReferenceVideos []VideoReferenceClip `json:"reference_videos,omitempty"`
	ReferenceAudios []VideoReferenceClip `json:"reference_audios,omitempty"`
	// BitrateMode is "standard" or "high" (Seedance 2.0 / 2.0-fast / 2.0-mini / 2.5).
	BitrateMode string `json:"bitrate_mode,omitempty"`
	// OutputFormat is "mp4" or "mov" (Seedance 2.5; every other model returns MP4).
	OutputFormat string `json:"output_format,omitempty"`
	// CameraFixed pins the camera (Seedance 1.5 Pro).
	CameraFixed *bool `json:"camera_fixed,omitempty"`
	// SafetyIdentifier is a stable end-user id for the provider's safety
	// systems (any Seedance model).
	SafetyIdentifier string `json:"safety_identifier,omitempty"`
	// InputType asserts the mode you intend: "text", "image",
	// "first_last_frame" or "reference". The gateway refuses a request whose
	// fields imply another mode instead of generating the wrong thing.
	InputType string `json:"input_type,omitempty"`
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
	// true for every input (text, image, first/last frame, reference media,
	// RealFace); set false for a silent clip. Grok ignores it. Use a pointer
	// so the field can be left unset (nil) to take the default.
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

// VideoReferenceClip is one reference video or audio clip.
type VideoReferenceClip struct {
	URL string `json:"url"`
	// Role is "reference", or empty.
	Role string `json:"role,omitempty"`
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
	// LastFrameURL is the final frame as an image, when ReturnLastFrame was set
	// and the upstream returned one.
	LastFrameURL string `json:"last_frame_url,omitempty"`
	// LastFrameBackedUp is true when the gateway mirrored that frame.
	LastFrameBackedUp bool `json:"last_frame_backed_up,omitempty"`
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
		if err := validateVideoOptions(opts); err != nil {
			return nil, err
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
		if len(opts.ReferenceVideos) > 0 {
			body["reference_videos"] = opts.ReferenceVideos
		}
		if len(opts.ReferenceAudios) > 0 {
			body["reference_audios"] = opts.ReferenceAudios
		}
		if opts.BitrateMode != "" {
			body["bitrate_mode"] = opts.BitrateMode
		}
		if opts.OutputFormat != "" {
			body["output_format"] = opts.OutputFormat
		}
		if opts.CameraFixed != nil {
			body["camera_fixed"] = *opts.CameraFixed
		}
		if opts.SafetyIdentifier != "" {
			body["safety_identifier"] = opts.SafetyIdentifier
		}
		if opts.InputType != "" {
			body["input_type"] = opts.InputType
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
// poll until the job reaches "completed": with the same PAYMENT-SIGNATURE on Base,
// with an ownership proof and one payment on completion on Solana. The gateway
// settles only on completion, so upstream failure or a caller giving up costs
// nothing.
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
	c.applyAuth(req1)
	resp1, err := c.httpClient.Do(req1)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()

	// Account rail: the API key IS the payment, so the submit answers with the
	// async envelope directly and there is no 402 to answer. A 402 here means
	// the account is out of credit.
	if c.isAPIKey() {
		if resp1.StatusCode == http.StatusPaymentRequired {
			return nil, apiKeyPaymentError(body1)
		}
		if resp1.StatusCode != http.StatusOK && resp1.StatusCode != http.StatusAccepted {
			return nil, &APIError{
				StatusCode: resp1.StatusCode,
				Message:    fmt.Sprintf("Submit failed: %s", string(body1)),
			}
		}
		return c.pollVideoJob(ctx, submitPath, body1, resp1.StatusCode, nil)
	}

	if resp1.StatusCode != http.StatusPaymentRequired {
		return nil, &APIError{
			StatusCode: resp1.StatusCode,
			Message:    fmt.Sprintf("expected 402 on video submit, got %d: %s", resp1.StatusCode, string(body1)),
		}
	}
	paymentHeader := paymentRequirementsFrom(resp1.Header.Get("payment-required"), body1)
	if paymentHeader == "" {
		return nil, &PaymentError{Message: "402 response but no payment requirements found"}
	}

	// Step 2: sign the payment authorization.
	paymentReq, err := ParsePaymentRequired(paymentHeader)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to parse payment requirements: %v", err)}
	}
	paymentOption, err := c.extractPaymentDetails(paymentReq)
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

	return c.pollVideoJob(ctx, submitPath, body2, resp2.StatusCode, &imagePayment{
		option:      paymentOption,
		payload:     paymentPayload,
		resourceURL: resourceURL,
		description: paymentReq.Resource.Description,
		extensions:  paymentReq.Extensions,
	})
}

// pollVideoJob follows an async video job to a terminal state. `pay` is nil on
// the account rail.
func (c *VideoClient) pollVideoJob(
	ctx context.Context,
	submitPath string,
	submitBody []byte,
	submitStatus int,
	pay *imagePayment,
) (*VideoResponse, error) {
	// Solana polls with an ownership proof and pays once, on completion.
	if pay != nil && c.isSolana() {
		return c.followSolanaVideoJob(ctx, submitPath, submitBody, submitStatus, pay)
	}

	body, header, err := c.pollAsyncJob(ctx, asyncJob{
		label:        "Video generation",
		endpoint:     submitPath,
		submitBody:   submitBody,
		submitStatus: submitStatus,
		pay:          pay,
		budget:       videoPollBudget,
		interval:     c.pollInterval,
	})
	if err != nil {
		return nil, err
	}
	return decodeVideoResponse(body, header)
}

// decodeVideoResponse unmarshals a completed video poll and attaches the
// settlement receipt from the X-Payment-Receipt header when present.
func decodeVideoResponse(body []byte, header http.Header) (*VideoResponse, error) {
	var videoResp VideoResponse
	if err := json.Unmarshal(body, &videoResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if tx := header.Get("x-payment-receipt"); tx != "" {
		videoResp.TxHash = tx
	}
	return &videoResp, nil
}

// Seedance capabilities per model, mirroring the gateway registry. Everything
// here the gateway would also refuse; on the account rail nothing else runs
// before the key is billed.
const (
	seedance25    = "bytedance/seedance-2.5"
	seedance15Pro = "bytedance/seedance-1.5-pro"
	// seedanceMaxReferenceClips is the per-type clip ceiling.
	seedanceMaxReferenceClips = 3
)

// seedanceReferenceImageLimit is also the set of models with reference media and
// bitrate_mode: the Seedance 2.x family.
var seedanceReferenceImageLimit = map[string]int{
	"bytedance/seedance-2.0":      9,
	"bytedance/seedance-2.0-fast": 9,
	"bytedance/seedance-2.0-mini": 9,
	seedance25:                    30,
}

// validateVideoOptions refuses a request the gateway would refuse, before
// anything is sent or signed.
func validateVideoOptions(opts *VideoGenerateOptions) error {
	model := opts.Model
	if model == "" {
		model = DefaultVideoModel
	}
	fail := func(field, format string, args ...any) error {
		return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
	}
	limit, seedance2x := seedanceReferenceImageLimit[model]

	hasClips := len(opts.ReferenceVideos) > 0 || len(opts.ReferenceAudios) > 0
	if len(opts.ReferenceImageURLs) > 0 || hasClips {
		if opts.ImageURL != "" || opts.LastFrameURL != "" || opts.RealFaceAssetID != "" {
			return fail("ReferenceImageURLs", "reference inputs are mutually exclusive with ImageURL, LastFrameURL, and RealFaceAssetID")
		}
		if !seedance2x {
			return fail("Model", "model %s does not accept reference media; use a Seedance 2.x model", model)
		}
	}
	if len(opts.ReferenceImageURLs) > limit && seedance2x {
		return fail("ReferenceImageURLs", "ReferenceImageURLs accepts at most %d images on %s", limit, model)
	}
	for _, list := range []struct {
		field string
		clips []VideoReferenceClip
	}{{"ReferenceVideos", opts.ReferenceVideos}, {"ReferenceAudios", opts.ReferenceAudios}} {
		if len(list.clips) > seedanceMaxReferenceClips {
			return fail(list.field, "%s accepts at most %d clips", list.field, seedanceMaxReferenceClips)
		}
		for _, clip := range list.clips {
			if !strings.HasPrefix(clip.URL, "https://") && !strings.HasPrefix(clip.URL, "http://") {
				return fail(list.field, "%s must be http(s) URLs", list.field)
			}
			if clip.Role != "" && clip.Role != "reference" {
				return fail(list.field, "%s role must be \"reference\" or empty", list.field)
			}
		}
	}
	if len(opts.ReferenceAudios) > 0 && len(opts.ReferenceImageURLs) == 0 && len(opts.ReferenceVideos) == 0 {
		return fail("ReferenceAudios", "ReferenceAudios requires a reference image or video in the same request")
	}
	if opts.BitrateMode != "" {
		if opts.BitrateMode != "standard" && opts.BitrateMode != "high" {
			return fail("BitrateMode", "BitrateMode must be standard or high")
		}
		if !seedance2x {
			return fail("BitrateMode", "BitrateMode requires a Seedance 2.x model; got %s", model)
		}
	}
	if opts.OutputFormat != "" {
		if opts.OutputFormat != "mp4" && opts.OutputFormat != "mov" {
			return fail("OutputFormat", "OutputFormat must be mp4 or mov")
		}
		if model != seedance25 {
			return fail("OutputFormat", "OutputFormat requires %s; every other model returns MP4", seedance25)
		}
	}
	if opts.CameraFixed != nil && model != seedance15Pro {
		return fail("CameraFixed", "CameraFixed requires %s; got %s", seedance15Pro, model)
	}
	if opts.SafetyIdentifier != "" && !strings.HasPrefix(model, "bytedance/seedance-") {
		return fail("SafetyIdentifier", "SafetyIdentifier requires a Seedance model; got %s", model)
	}
	switch opts.InputType {
	case "", "text", "image", "first_last_frame", "reference":
	default:
		return fail("InputType", "InputType must be text, image, first_last_frame or reference")
	}
	return nil
}
