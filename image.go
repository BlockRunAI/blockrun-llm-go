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
	// DefaultImageModel is the default image generation model.
	DefaultImageModel = "google/nano-banana"

	// DefaultImageSize is the default image size.
	DefaultImageSize = "1024x1024"

	// DefaultImageTimeout is the default timeout for image generation (images take longer).
	DefaultImageTimeout = 120 * time.Second
)

// ImageClient is the BlockRun Image Generation client.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine - only signatures are transmitted.
type ImageClient struct {
	*baseClient
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

	client := &ImageClient{baseClient: bc}

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
	URL           string `json:"url"`
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

	// Make request with payment handling
	respBytes, err := c.doRequest(ctx, "/v1/images/generations", body)
	if err != nil {
		return nil, err
	}

	var imageResp ImageResponse
	if err := json.Unmarshal(respBytes, &imageResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
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
