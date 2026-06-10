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
	// DefaultImageModel is the default image generation model.
	DefaultImageModel = "google/nano-banana"

	// Available image models (pass as ImageGenerateOptions.Model):
	//   openai/dall-e-3          $0.04-0.08/image
	//   openai/gpt-image-1       $0.02-0.04/image  (also supports edit)
	//   openai/gpt-image-2       $0.06-0.12/image  (ChatGPT Images 2.0, also supports edit)
	//   google/nano-banana       $0.05/image
	//   google/nano-banana-pro   $0.10-0.15/image
	//   black-forest/flux-1.1-pro $0.04/image
	//   xai/grok-imagine-image     $0.02/image
	//   xai/grok-imagine-image-pro $0.07/image
	//   zai/cogview-4            $0.015/image

	// DefaultImageSize is the default image size.
	DefaultImageSize = "1024x1024"

	// DefaultImageTimeout is the default timeout for image generation (images take longer).
	DefaultImageTimeout = 120 * time.Second

	// imagePollInterval is the wait between poll attempts on the async path.
	imagePollInterval = 3 * time.Second
	// imagePollBudget is the overall wall-clock budget for submit + polling on
	// the async path. If upstream runs past this, the call returns an error
	// without charging.
	imagePollBudget = 300 * time.Second
	// imageMaxTimeoutSeconds is the signed-auth window floor — bumped above
	// the server default so the PAYMENT-SIGNATURE stays valid across the poll
	// window of a slow generation.
	imageMaxTimeoutSeconds = 600
)

// ImageClient is the BlockRun Image Generation client.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine - only signatures are transmitted.
type ImageClient struct {
	*baseClient
	// pollInterval is the wait between poll attempts on the async path.
	// Defaults to imagePollInterval; overridable (mainly for tests).
	pollInterval time.Duration
}

// ImageClientOption is a function that configures an ImageClient.
type ImageClientOption func(*ImageClient)

// WithImageAPIURL sets a custom API URL for the image client.
func WithImageAPIURL(url string) ImageClientOption {
	return func(c *ImageClient) {
		c.apiURL = strings.TrimSuffix(url, "/")
	}
}

// WithImageTimeout sets the HTTP timeout for the image client.
func WithImageTimeout(timeout time.Duration) ImageClientOption {
	return func(c *ImageClient) {
		c.httpClient.Timeout = timeout
	}
}

// WithImageHTTPClient sets a custom HTTP client for the image client.
func WithImageHTTPClient(client *http.Client) ImageClientOption {
	return func(c *ImageClient) {
		c.httpClient = client
	}
}

// NewImageClient creates a new BlockRun Image client.
//
// If privateKey is empty, it will be read from the BLOCKRUN_WALLET_KEY
// or BASE_CHAIN_WALLET_KEY environment variable.
func NewImageClient(privateKey string, opts ...ImageClientOption) (*ImageClient, error) {
	bc, err := newBaseClient(privateKey, "", DefaultImageTimeout)
	if err != nil {
		return nil, err
	}

	client := &ImageClient{baseClient: bc, pollInterval: imagePollInterval}

	// Apply options
	for _, opt := range opts {
		opt(client)
	}

	// Check for custom API URL in environment (after options so user-set URLs win)
	bc.checkEnvAPIURL()

	return client, nil
}

// ImageGenerateOptions contains optional parameters for image generation.
type ImageGenerateOptions struct {
	Model   string `json:"model,omitempty"`
	Size    string `json:"size,omitempty"`
	N       int    `json:"n,omitempty"`
	Quality string `json:"quality,omitempty"`
}

// ImageData represents a single generated image.
type ImageData struct {
	URL string `json:"url"`
	// SourceURL is the original upstream URL (e.g. imgen.x.ai). Omitted for data URIs.
	SourceURL string `json:"source_url,omitempty"`
	// BackedUp is true when the gateway mirrored the image to its GCS bucket.
	BackedUp      bool   `json:"backed_up,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
}

// ImageResponse represents the API response for image generation.
type ImageResponse struct {
	Created int64       `json:"created"`
	Data    []ImageData `json:"data"`
	// TxHash is the on-chain USDC settlement transaction for this call (from
	// the gateway's X-Payment-Receipt header), when available.
	TxHash string `json:"tx_hash,omitempty"`
}

// ImageModel represents an available image model from the API.
type ImageModel struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Provider        string   `json:"provider"`
	Description     string   `json:"description"`
	PricePerImage   float64  `json:"pricePerImage"`
	SupportedSizes  []string `json:"supportedSizes,omitempty"`
	MaxPromptLength int      `json:"maxPromptLength,omitempty"`
	Available       bool     `json:"available"`
}

// Generate generates an image from a text prompt.
func (c *ImageClient) Generate(ctx context.Context, prompt string, opts *ImageGenerateOptions) (*ImageResponse, error) {
	// Build request body
	body := map[string]any{
		"prompt": prompt,
		"model":  DefaultImageModel,
		"size":   DefaultImageSize,
		"n":      1,
	}

	if opts != nil {
		if opts.Model != "" {
			body["model"] = opts.Model
		}
		if opts.Size != "" {
			body["size"] = opts.Size
		}
		if opts.N > 0 {
			body["n"] = opts.N
		}
		if opts.Quality != "" {
			body["quality"] = opts.Quality
		}
	}

	return c.submitImageAndMaybePoll(ctx, "/v1/images/generations", body)
}

// DefaultImageEditModel is the default model for image editing / fusion.
const DefaultImageEditModel = "openai/gpt-image-2"

// ImageEditOptions contains optional parameters for image editing.
type ImageEditOptions struct {
	// Model is the edit-capable model ID. Defaults to DefaultImageEditModel.
	// Edit-supported: openai/gpt-image-1, openai/gpt-image-2,
	// google/nano-banana, google/nano-banana-pro.
	Model string `json:"model,omitempty"`
	// Mask is an optional base64 data URI marking the region to edit.
	// Cannot be combined with multiple source images.
	Mask string `json:"mask,omitempty"`
	Size string `json:"size,omitempty"`
	N    int    `json:"n,omitempty"`
}

// Edit edits or fuses images using img2img.
//
// Pass one source image for a standard edit, or multiple (up to the
// provider's limit, typically 4) to fuse them — e.g. a reference photo
// plus a brand logo. Each image must be a base64 data URI (data:image/...).
//
// Example (single):
//
//	resp, err := client.Edit(ctx, "make the sky purple",
//		[]string{"data:image/png;base64,..."}, nil)
//
// Example (fusion):
//
//	resp, err := client.Edit(ctx, "place the logo on the shirt",
//		[]string{photo, logo}, &blockrun.ImageEditOptions{Model: "google/nano-banana"})
func (c *ImageClient) Edit(ctx context.Context, prompt string, images []string, opts *ImageEditOptions) (*ImageResponse, error) {
	if len(images) == 0 {
		return nil, fmt.Errorf("at least one source image is required")
	}

	body := map[string]any{
		"prompt": prompt,
		"model":  DefaultImageEditModel,
		"size":   DefaultImageSize,
		"n":      1,
	}

	// Single image is sent as a string (OpenAI-compatible); multiple images
	// are sent as an array for fusion. The gateway accepts both.
	if len(images) == 1 {
		body["image"] = images[0]
	} else {
		body["image"] = images
	}

	if opts != nil {
		if opts.Model != "" {
			body["model"] = opts.Model
		}
		if opts.Size != "" {
			body["size"] = opts.Size
		}
		if opts.N > 0 {
			body["n"] = opts.N
		}
		if opts.Mask != "" {
			body["mask"] = opts.Mask
		}
	}

	return c.submitImageAndMaybePoll(ctx, "/v1/images/image2image", body)
}

// submitImageAndMaybePoll runs the gateway's hybrid image pipeline shared by
// Generate and Edit. Fast models complete inline: POST (402 → sign → retry)
// returns 200 with image data and payment settled in the same call. Slow
// models return 202 { id, poll_url } instead; the gateway settles USDC only
// on the first poll that observes status=completed, so an upstream failure or
// a caller giving up costs nothing. This client GET-polls poll_url with the
// same wallet's PAYMENT-SIGNATURE until the job reaches a terminal state,
// then returns the same ImageResponse shape as the fast path — callers never
// see the async envelope.
func (c *ImageClient) submitImageAndMaybePoll(ctx context.Context, endpoint string, body map[string]any) (*ImageResponse, error) {
	submitURL := c.apiURL + endpoint

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request body: %w", err)
	}

	// Step 1: unauth POST → 402 with payment requirements.
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

	// Free/proxy path: the server answered without requiring payment.
	if resp1.StatusCode == http.StatusOK {
		return decodeImageResponse(body1, resp1.Header)
	}
	if resp1.StatusCode != http.StatusPaymentRequired {
		return nil, &APIError{
			StatusCode: resp1.StatusCode,
			Message:    fmt.Sprintf("API error: %s", string(body1)),
		}
	}
	if paymentHeader == "" {
		return nil, &PaymentError{Message: "402 response but no payment requirements found"}
	}

	// Step 2: sign the payment authorization. Floor the validity window at
	// imageMaxTimeoutSeconds so the same signature survives the poll window.
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
	maxTimeout := paymentOption.MaxTimeoutSeconds
	if maxTimeout < imageMaxTimeoutSeconds {
		maxTimeout = imageMaxTimeoutSeconds
	}
	paymentPayload, err := CreatePaymentPayload(
		c.privateKey,
		paymentOption.PayTo,
		paymentOption.Amount,
		paymentOption.Network,
		resourceURL,
		paymentReq.Resource.Description,
		maxTimeout,
		paymentOption.Extra,
		paymentReq.Extensions,
	)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to create payment: %v", err)}
	}

	// Step 3: retry with payment → 200 image data (fast path) or
	// 202 { id, poll_url } (slow path).
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

	switch resp2.StatusCode {
	case http.StatusPaymentRequired:
		return nil, &PaymentError{Message: "Payment was rejected. Check your wallet balance."}
	case http.StatusOK:
		// Fast path: generated and settled inline.
		c.recordSettledCost(paymentOption.Amount, endpoint)
		return decodeImageResponse(body2, resp2.Header)
	case http.StatusAccepted:
		// Slow path: async envelope — fall through to the poll loop below.
	default:
		return nil, &APIError{
			StatusCode: resp2.StatusCode,
			Message:    fmt.Sprintf("API error after payment: %s", string(body2)),
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

	pollURL := c.resolvePollURL(submitData.PollURL)

	// Step 4: poll with the same PAYMENT-SIGNATURE until terminal. The
	// gateway enforces wallet binding (not signature equality), so reusing
	// the submit signature is valid and settles exactly once, on the first
	// poll that observes "completed".
	deadline := time.Now().Add(imagePollBudget)
	lastStatus := submitData.Status
	if lastStatus == "" {
		lastStatus = "queued"
	}

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
		pollReq.Header.Set("PAYMENT-SIGNATURE", paymentPayload)
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
				Message:    fmt.Sprintf("Upstream generation failed (no payment was taken): %s", string(pollBytes)),
			}
		}
		// Terminal success is keyed on status, NOT the HTTP code — the
		// gateway settles on-chain the moment a poll reports "completed", so
		// the charge is irreversible at that point. Record the cost as soon
		// as completion is observed, then decode.
		if lastStatus == "completed" {
			c.recordSettledCost(paymentOption.Amount, endpoint)
			return decodeImageResponse(pollBytes, pollResp.Header)
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
			"Image generation did not complete within %.0fs (last status: %s). No payment was taken.",
			imagePollBudget.Seconds(), lastStatus,
		),
	}
}

// decodeImageResponse unmarshals a gateway image payload (the synchronous
// shape or a completed poll, both carry data: [...]) and attaches the
// settlement receipt from the X-Payment-Receipt header when present.
func decodeImageResponse(body []byte, header http.Header) (*ImageResponse, error) {
	var imageResp ImageResponse
	if err := json.Unmarshal(body, &imageResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if header != nil {
		if tx := header.Get("x-payment-receipt"); tx != "" {
			imageResp.TxHash = tx
		}
	}
	return &imageResp, nil
}

// ListImageModels returns the list of available image models with pricing.
func (c *ImageClient) ListImageModels(ctx context.Context) ([]ImageModel, error) {
	respBytes, err := c.doGet(ctx, "/v1/images/models")
	if err != nil {
		return nil, fmt.Errorf("failed to list image models: %w", err)
	}

	var result struct {
		Data []ImageModel `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode image models response: %w", err)
	}

	return result.Data, nil
}
