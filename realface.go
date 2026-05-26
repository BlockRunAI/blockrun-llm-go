package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// groupIDRe matches upstream RealFace group ids, e.g. "legacy_rf_8137".
var groupIDRe = regexp.MustCompile(`^legacy_rf_\d+$`)

// RealFaceClient enrolls a real person's face via x402 micropayments.
//
// A RealFace registers a *real person's* likeness as a face/character
// reference asset. Unlike a Virtual Portrait (AI-generated character, see
// PortraitClient), RealFace proves the enroller is the same person in the
// photo via a brief on-phone liveness check (nod + blink, ~1 minute).
// No KYC — no government ID, no account login, just the liveness step.
// After enrollment ($0.01 USDC, one-time) you get back a "ta_xxxxxxxx" asset
// id that can be passed as VideoGenerateOptions.RealFaceAssetID to
// VideoClient.Generate on Seedance 2.0 / 2.0-fast to keep the same person
// across multiple videos.
//
// The flow is three steps:
//
//  1. Init           — FREE. Returns a group id + an H5 link the real person
//     scans on their phone.
//  2. (phone liveness) — The rights-holder opens the H5 link, allows camera,
//     nods + blinks. ~60 seconds.
//  3. Enroll         — $0.01 USDC. Uploads the face photo, matches it against
//     the live capture, returns the "ta_" asset.
//
// Use Status (or the WaitForActive helper) between steps 2 and 3 to detect
// when the person has finished the phone check.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine — only signatures are transmitted.
type RealFaceClient struct {
	*baseClient
}

// RealFaceClientOption configures a RealFaceClient.
type RealFaceClientOption func(*RealFaceClient)

// WithRealFaceAPIURL sets a custom API URL for the realface client.
func WithRealFaceAPIURL(url string) RealFaceClientOption {
	return func(c *RealFaceClient) {
		c.apiURL = strings.TrimSuffix(url, "/")
	}
}

// WithRealFaceTimeout sets the HTTP timeout for the realface client.
func WithRealFaceTimeout(timeout time.Duration) RealFaceClientOption {
	return func(c *RealFaceClient) {
		c.httpClient.Timeout = timeout
	}
}

// WithRealFaceHTTPClient sets a custom HTTP client for the realface client.
func WithRealFaceHTTPClient(client *http.Client) RealFaceClientOption {
	return func(c *RealFaceClient) {
		c.httpClient = client
	}
}

// NewRealFaceClient creates a new BlockRun RealFace client.
//
// If privateKey is empty, it will be read from the BLOCKRUN_WALLET_KEY
// or BASE_CHAIN_WALLET_KEY environment variable.
func NewRealFaceClient(privateKey string, opts ...RealFaceClientOption) (*RealFaceClient, error) {
	bc, err := newBaseClient(privateKey, "", DefaultTimeout)
	if err != nil {
		return nil, err
	}

	client := &RealFaceClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	bc.checkEnvAPIURL()

	return client, nil
}

// RealFaceInit is the response from POST /v1/realface/init (free, rate-limited).
type RealFaceInit struct {
	Object           string         `json:"object,omitempty"`
	GroupID          string         `json:"group_id"` // legacy_rf_xxxx — pass to Status/Enroll
	H5Link           string         `json:"h5_link"`  // URL the real person scans for liveness
	Status           string         `json:"status,omitempty"`
	ExpiresInSeconds int            `json:"expires_in_seconds,omitempty"`
	NextSteps        map[string]any `json:"next_steps,omitempty"`
	Refreshed        bool           `json:"refreshed,omitempty"`
}

// RealFaceStatus is the response from GET /v1/realface/status?groupId=…
// (free, rate-limited).
type RealFaceStatus struct {
	Object          string `json:"object,omitempty"`
	GroupID         string `json:"group_id"`
	Status          string `json:"status"` // pending_validation | active | …
	AssetCount      int    `json:"asset_count,omitempty"`
	ReadyToFinalize bool   `json:"ready_to_finalize"` // true once status == "active"
}

// RealFaceEnrollment is the response from POST /v1/realface/enroll ($0.01 USDC).
type RealFaceEnrollment struct {
	Object          string              `json:"object,omitempty"`
	AssetID         string              `json:"asset_id"` // ta_xxxxxxxx — pass as RealFaceAssetID
	GroupID         string              `json:"group_id,omitempty"`
	ByteplusAssetID string              `json:"byteplus_asset_id,omitempty"`
	Name            string              `json:"name"`
	ImageURL        string              `json:"image_url"`
	CreatedAt       string              `json:"created_at,omitempty"`
	Usage           *PortraitUsage      `json:"usage,omitempty"`
	Price           map[string]any      `json:"price,omitempty"`
	Settlement      *PortraitSettlement `json:"settlement,omitempty"`
}

// RealFaceListItem is one row in the wallet RealFace list. Upstream uses
// camelCase here, so the JSON tags match for transparent ingestion.
type RealFaceListItem struct {
	AssetID          string `json:"assetId"`
	GroupID          string `json:"groupId,omitempty"`
	Name             string `json:"name,omitempty"`
	ImageURL         string `json:"imageUrl,omitempty"`
	CreatedAt        string `json:"createdAt,omitempty"`
	EnrollmentTxHash string `json:"enrollmentTxHash,omitempty"`
	ByteplusAssetID  string `json:"byteplusAssetId,omitempty"`
}

// RealFaceList is the response from GET /v1/wallet/<address>/realfaces.
type RealFaceList struct {
	Wallet    string             `json:"wallet"`
	RealFaces []RealFaceListItem `json:"realfaces"`
	Count     int                `json:"count,omitempty"`
}

// Init starts (or refreshes) a RealFace enrollment. Free, but rate-limited to
// ~10 calls/hour/IP (each call creates an upstream session).
//
// name is the display name for the asset group (1-64 chars). If groupID is
// non-empty, the h5_link is refreshed for that existing group instead of
// creating a new one — use this when the original 120s H5 session expired
// before the person finished scanning.
func (c *RealFaceClient) Init(ctx context.Context, name, groupID string) (*RealFaceInit, error) {
	if err := validatePortraitName(name); err != nil {
		return nil, err
	}
	if groupID != "" && !groupIDRe.MatchString(groupID) {
		return nil, &ValidationError{Field: "groupID", Message: "groupID must look like 'legacy_rf_<digits>'"}
	}

	body := map[string]any{"name": name}
	if groupID != "" {
		body["groupId"] = groupID
	}

	respBytes, err := c.doRequest(ctx, "/v1/realface/init", body)
	if err != nil {
		return nil, err
	}

	var result RealFaceInit
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// Status polls the state of a RealFace asset group. Free, but rate-limited.
//
// ReadyToFinalize is true once the real person has completed the phone
// liveness check (status == "active").
func (c *RealFaceClient) Status(ctx context.Context, groupID string) (*RealFaceStatus, error) {
	if groupID == "" || !groupIDRe.MatchString(groupID) {
		return nil, &ValidationError{Field: "groupID", Message: "groupID must look like 'legacy_rf_<digits>'"}
	}

	endpoint := "/v1/realface/status?groupId=" + urlQueryEscape(groupID)
	respBytes, err := c.doGet(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	var result RealFaceStatus
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// WaitForActiveOptions configures WaitForActive.
type WaitForActiveOptions struct {
	// Timeout is the overall budget before giving up (default 180s; the H5
	// session itself expires ~120s after each init/refresh).
	Timeout time.Duration
	// PollInterval is the gap between status checks (default 4s; keep >=3s to
	// respect rate limits).
	PollInterval time.Duration
}

// WaitForActive blocks until the group is active (the real person finished the
// phone liveness check), then returns its status. Convenience wrapper around
// repeated Status polling. Respects ctx cancellation.
//
// Returns an APIError with StatusCode 504 if the group is not active within
// the timeout (the person may not have finished the phone liveness check;
// call Init with the same groupID to refresh an expired h5_link).
func (c *RealFaceClient) WaitForActive(ctx context.Context, groupID string, opts *WaitForActiveOptions) (*RealFaceStatus, error) {
	if groupID == "" || !groupIDRe.MatchString(groupID) {
		return nil, &ValidationError{Field: "groupID", Message: "groupID must look like 'legacy_rf_<digits>'"}
	}

	timeout := 180 * time.Second
	pollInterval := 4 * time.Second
	if opts != nil {
		if opts.Timeout > 0 {
			timeout = opts.Timeout
		}
		if opts.PollInterval > 0 {
			pollInterval = opts.PollInterval
		}
	}

	deadline := time.Now().Add(timeout)
	for {
		state, err := c.Status(ctx, groupID)
		if err != nil {
			return nil, err
		}
		if state.ReadyToFinalize {
			return state, nil
		}
		if time.Now().Add(pollInterval).After(deadline) {
			return nil, &APIError{
				StatusCode: http.StatusGatewayTimeout,
				Message: fmt.Sprintf(
					"RealFace group %s not active after %.0fs (last status: %q). "+
						"The person may not have finished the phone liveness check; "+
						"call Init(groupID) to refresh an expired h5_link.",
					groupID, timeout.Seconds(), state.Status,
				),
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// Enroll finalizes a RealFace enrollment. Costs $0.01 USDC on Base, one-time.
//
// Requires the real person to have already completed the phone liveness check
// (group status == "active"; use WaitForActive to block on it). name is the
// display name (1-64 chars), imageURL a public http(s) URL to a JPG/PNG/WEBP
// photo of the same person (max 10 MB), and groupID the "legacy_rf_…" id from
// Init.
//
// The endpoint settles only AFTER the asset is successfully matched and
// registered upstream. Failures are returned as an APIError with no charge:
// 425 = group not active yet (do the phone check first), 422 = face did not
// match the live capture (try a clearer photo), 502 = upstream failure.
func (c *RealFaceClient) Enroll(ctx context.Context, name, imageURL, groupID string) (*RealFaceEnrollment, error) {
	if err := validatePortraitName(name); err != nil {
		return nil, err
	}
	if err := validateImageURL(imageURL); err != nil {
		return nil, err
	}
	if groupID == "" || !groupIDRe.MatchString(groupID) {
		return nil, &ValidationError{Field: "groupID", Message: "groupID must look like 'legacy_rf_<digits>'"}
	}

	body := map[string]any{
		"name":      name,
		"image_url": imageURL,
		"group_id":  groupID,
	}

	respBytes, err := c.doRequest(ctx, "/v1/realface/enroll", body)
	if err != nil {
		return nil, err
	}

	var result RealFaceEnrollment
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// ListRealFaces lists RealFaces enrolled by a wallet. Free, but rate-limited
// to ~20 requests/hour/IP.
//
// If walletAddress is empty, the client's own address is used.
func (c *RealFaceClient) ListRealFaces(ctx context.Context, walletAddress string) (*RealFaceList, error) {
	addr := walletAddress
	if addr == "" {
		addr = c.address
	}
	respBytes, err := c.doGet(ctx, "/v1/wallet/"+addr+"/realfaces")
	if err != nil {
		return nil, err
	}

	var result RealFaceList
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}
