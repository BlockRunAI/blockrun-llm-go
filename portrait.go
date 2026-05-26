package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// maxPortraitNameLen mirrors the upstream limit; enforced locally to fail fast.
const maxPortraitNameLen = 64

// PortraitClient enrolls Virtual Portraits via x402 micropayments.
//
// A Virtual Portrait is an AI-generated character image registered as a
// face/character reference asset. After enrollment ($0.01 USDC, one-time, no
// KYC) you get back a "ta_xxxxxxxx" asset id that can be passed as
// VideoGenerateOptions.RealFaceAssetID to VideoClient.Generate on Seedance 2.0
// or 2.0-fast to keep the same character across multiple videos.
//
// For a real person's likeness, use RealFaceClient instead — it enrolls a real
// face for $0.01 via a brief on-phone liveness check (no KYC) and yields a
// "ta_" id usable the same way. Virtual Portraits are for AI-generated
// personas, mascots, avatars, and virtual spokespeople.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine — only signatures are transmitted.
type PortraitClient struct {
	*baseClient
}

// PortraitClientOption configures a PortraitClient.
type PortraitClientOption func(*PortraitClient)

// WithPortraitAPIURL sets a custom API URL for the portrait client.
func WithPortraitAPIURL(url string) PortraitClientOption {
	return func(c *PortraitClient) {
		c.apiURL = strings.TrimSuffix(url, "/")
	}
}

// WithPortraitTimeout sets the HTTP timeout for the portrait client.
func WithPortraitTimeout(timeout time.Duration) PortraitClientOption {
	return func(c *PortraitClient) {
		c.httpClient.Timeout = timeout
	}
}

// WithPortraitHTTPClient sets a custom HTTP client for the portrait client.
func WithPortraitHTTPClient(client *http.Client) PortraitClientOption {
	return func(c *PortraitClient) {
		c.httpClient = client
	}
}

// NewPortraitClient creates a new BlockRun Portrait client.
//
// If privateKey is empty, it will be read from the BLOCKRUN_WALLET_KEY
// or BASE_CHAIN_WALLET_KEY environment variable.
func NewPortraitClient(privateKey string, opts ...PortraitClientOption) (*PortraitClient, error) {
	bc, err := newBaseClient(privateKey, "", DefaultTimeout)
	if err != nil {
		return nil, err
	}

	client := &PortraitClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	bc.checkEnvAPIURL()

	return client, nil
}

// PortraitUsage describes how an enrolled portrait can be used.
type PortraitUsage struct {
	CompatibleModels []string `json:"compatible_models,omitempty"`
	HowToUse         string   `json:"how_to_use,omitempty"`
}

// PortraitSettlement is the on-chain settlement of the enrollment payment.
type PortraitSettlement struct {
	Success bool   `json:"success"`
	TxHash  string `json:"tx_hash,omitempty"`
	Network string `json:"network,omitempty"`
}

// PortraitEnrollment is the response from POST /v1/portrait/enroll.
type PortraitEnrollment struct {
	Object     string              `json:"object,omitempty"`
	AssetID    string              `json:"asset_id"` // ta_xxxxxxxx — pass as RealFaceAssetID
	GroupID    string              `json:"group_id,omitempty"`
	Name       string              `json:"name"`
	ImageURL   string              `json:"image_url"`
	CreatedAt  string              `json:"created_at,omitempty"`
	Usage      *PortraitUsage      `json:"usage,omitempty"`
	Price      map[string]any      `json:"price,omitempty"`
	Settlement *PortraitSettlement `json:"settlement,omitempty"`
}

// PortraitListItem is one row in the wallet portrait list. Upstream uses
// camelCase here, so the JSON tags match for transparent ingestion.
type PortraitListItem struct {
	AssetID          string `json:"assetId"`
	GroupID          string `json:"groupId,omitempty"`
	Name             string `json:"name,omitempty"`
	ImageURL         string `json:"imageUrl,omitempty"`
	CreatedAt        string `json:"createdAt,omitempty"`
	EnrollmentTxHash string `json:"enrollmentTxHash,omitempty"`
}

// PortraitList is the response from GET /v1/wallet/<address>/portraits.
type PortraitList struct {
	Wallet    string             `json:"wallet"`
	Portraits []PortraitListItem `json:"portraits"`
	Count     int                `json:"count,omitempty"`
}

// Enroll enrolls a Virtual Portrait. Costs $0.01 USDC on Base, one-time.
//
// name is the display name (1-64 chars). imageURL must be a public http(s)
// URL pointing to a JPG/PNG/WEBP image (max 10 MB), server-side fetched at
// enrollment time. The endpoint settles only AFTER the portrait is
// successfully registered upstream, so a failed enrollment (content filter,
// network error → HTTP 502) is returned as an APIError with no charge.
func (c *PortraitClient) Enroll(ctx context.Context, name, imageURL string) (*PortraitEnrollment, error) {
	if err := validatePortraitName(name); err != nil {
		return nil, err
	}
	if err := validateImageURL(imageURL); err != nil {
		return nil, err
	}

	body := map[string]any{
		"name":      name,
		"image_url": imageURL,
	}

	respBytes, err := c.doRequest(ctx, "/v1/portrait/enroll", body)
	if err != nil {
		return nil, err
	}

	var result PortraitEnrollment
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// ListPortraits lists portraits enrolled by a wallet. Free, but rate-limited
// to ~20 requests/hour/IP.
//
// If walletAddress is empty, the client's own address is used.
func (c *PortraitClient) ListPortraits(ctx context.Context, walletAddress string) (*PortraitList, error) {
	addr := walletAddress
	if addr == "" {
		addr = c.address
	}
	respBytes, err := c.doGet(ctx, "/v1/wallet/"+addr+"/portraits")
	if err != nil {
		return nil, err
	}

	var result PortraitList
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// ---------------------------------------------------------------- internals

func validatePortraitName(name string) error {
	if strings.TrimSpace(name) == "" {
		return &ValidationError{Field: "name", Message: "name is required (1-64 chars)"}
	}
	if len(name) > maxPortraitNameLen {
		return &ValidationError{
			Field:   "name",
			Message: fmt.Sprintf("name must be %d chars or fewer (got %d)", maxPortraitNameLen, len(name)),
		}
	}
	return nil
}

func validateImageURL(imageURL string) error {
	lower := strings.ToLower(imageURL)
	if imageURL == "" || (!strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "http://")) {
		return &ValidationError{Field: "image_url", Message: "image_url must be an http(s) URL"}
	}
	return nil
}
