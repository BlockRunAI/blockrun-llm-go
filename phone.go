package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// PhoneClient is the BlockRun Phone client — Twilio-backed phone lookup +
// number provisioning via x402 micropayments.
//
// Endpoints (all under /v1/phone/...):
//
//	POST /lookup            $0.01    Carrier + line type lookup
//	POST /lookup/fraud      $0.05    Carrier + SIM-swap / call-forwarding fraud signals
//	POST /numbers/buy       $5.00    Provision a US/CA number (30-day lease, bound to wallet)
//	POST /numbers/renew     $5.00    Extend an existing number by 30 days
//	POST /numbers/list      $0.001   List the wallet's active numbers
//	POST /numbers/release   free     Release a provisioned number (still flows through x402
//	                                 so the backend can verify wallet ownership)
//
// After buying a number, use it as the From caller-ID in VoiceClient.Call().
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine — only signatures are transmitted.
type PhoneClient struct {
	*baseClient
}

// PhoneClientOption configures a PhoneClient.
type PhoneClientOption func(*PhoneClient)

// WithPhoneAPIURL sets a custom API URL for the phone client.
func WithPhoneAPIURL(url string) PhoneClientOption {
	return func(c *PhoneClient) {
		c.apiURL = strings.TrimSuffix(url, "/")
	}
}

// WithPhoneTimeout sets the HTTP timeout for the phone client.
func WithPhoneTimeout(timeout time.Duration) PhoneClientOption {
	return func(c *PhoneClient) {
		c.httpClient.Timeout = timeout
	}
}

// WithPhoneHTTPClient sets a custom HTTP client for the phone client.
func WithPhoneHTTPClient(client *http.Client) PhoneClientOption {
	return func(c *PhoneClient) {
		c.httpClient = client
	}
}

// PhonePrices mirrors src/lib/twilio.ts PHONE_PRICES on the backend
// (settled USDC amount per endpoint).
var PhonePrices = map[string]float64{
	"lookup":          0.01,
	"lookup/fraud":    0.05,
	"numbers/buy":     5.00,
	"numbers/renew":   5.00,
	"numbers/list":    0.001,
	"numbers/release": 0.0,
}

// NewPhoneClient creates a new BlockRun Phone client.
//
// If privateKey is empty, it will be read from the BLOCKRUN_WALLET_KEY
// or BASE_CHAIN_WALLET_KEY environment variable.
func NewPhoneClient(privateKey string, opts ...PhoneClientOption) (*PhoneClient, error) {
	bc, err := newBaseClient(privateKey, "", DefaultTimeout)
	if err != nil {
		return nil, err
	}

	client := &PhoneClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	bc.checkEnvAPIURL()
	return client, nil
}

// PhoneLookupResponse is the Twilio Lookup payload. The shape varies by request
// type (basic vs fraud), so fields are loosely typed; access raw values via Extra.
type PhoneLookupResponse struct {
	PhoneNumber  string                 `json:"phone_number,omitempty"`
	CallerName   map[string]any         `json:"caller_name,omitempty"`
	Carrier      map[string]any         `json:"carrier,omitempty"`
	CountryCode  string                 `json:"country_code,omitempty"`
	NationalFmt  string                 `json:"national_format,omitempty"`
	LineType     map[string]any         `json:"line_type_intelligence,omitempty"`
	SimSwap      map[string]any         `json:"sim_swap,omitempty"`
	CallFwd      map[string]any         `json:"call_forwarding,omitempty"`
	TxHash       string                 `json:"txHash,omitempty"`
	Extra        map[string]any         `json:"-"`
}

// NumberBuyResponse is returned by BuyNumber once a number has been provisioned.
type NumberBuyResponse struct {
	PhoneNumber string `json:"phone_number"`
	ExpiresAt   string `json:"expires_at"`
	Chain       string `json:"chain,omitempty"`
	Message     string `json:"message,omitempty"`
	TxHash      string `json:"txHash,omitempty"`
}

// NumberRenewResponse is returned by RenewNumber.
type NumberRenewResponse struct {
	PhoneNumber string `json:"phone_number"`
	ExpiresAt   string `json:"expires_at"`
	Chain       string `json:"chain,omitempty"`
	Message     string `json:"message,omitempty"`
	TxHash      string `json:"txHash,omitempty"`
}

// OwnedNumber describes a single wallet-owned phone number.
type OwnedNumber struct {
	PhoneNumber string `json:"phone_number"`
	Chain       string `json:"chain,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	Active      bool   `json:"active,omitempty"`
}

// NumberListResponse is returned by ListNumbers.
type NumberListResponse struct {
	Numbers []OwnedNumber `json:"numbers"`
	Count   int           `json:"count"`
	TxHash  string        `json:"txHash,omitempty"`
}

// NumberReleaseResponse is returned by ReleaseNumber.
type NumberReleaseResponse struct {
	Released    bool   `json:"released"`
	PhoneNumber string `json:"phone_number"`
	TxHash      string `json:"txHash,omitempty"`
}

// Lookup performs a carrier + line-type lookup. Costs ~$0.01.
//
// phoneNumber must be in E.164 format (e.g. "+14155552671").
func (c *PhoneClient) Lookup(ctx context.Context, phoneNumber string) (*PhoneLookupResponse, error) {
	if err := validateE164(phoneNumber); err != nil {
		return nil, err
	}
	respBytes, err := c.doRequest(ctx, "/v1/phone/lookup", map[string]any{
		"phoneNumber": strings.TrimSpace(phoneNumber),
	})
	if err != nil {
		return nil, err
	}
	return unmarshalLookup(respBytes)
}

// LookupFraud performs a carrier lookup with fraud signals (SIM swap, call
// forwarding). Costs ~$0.05.
func (c *PhoneClient) LookupFraud(ctx context.Context, phoneNumber string) (*PhoneLookupResponse, error) {
	if err := validateE164(phoneNumber); err != nil {
		return nil, err
	}
	respBytes, err := c.doRequest(ctx, "/v1/phone/lookup/fraud", map[string]any{
		"phoneNumber": strings.TrimSpace(phoneNumber),
	})
	if err != nil {
		return nil, err
	}
	return unmarshalLookup(respBytes)
}

// BuyNumberOptions are parameters for BuyNumber.
type BuyNumberOptions struct {
	// Country is the ISO country code. "US" or "CA" (default "US").
	Country string
	// AreaCode is an optional 3-digit area-code hint. The backend falls back
	// to any number in the country if the area code can't be matched.
	AreaCode string
}

// BuyNumber provisions a dedicated phone number for 30 days. Costs $5.00.
//
// Note: payment is settled only after Twilio confirms the purchase, so failed
// purchases do NOT charge your wallet.
func (c *PhoneClient) BuyNumber(ctx context.Context, opts BuyNumberOptions) (*NumberBuyResponse, error) {
	country := opts.Country
	if country == "" {
		country = "US"
	}
	if country != "US" && country != "CA" {
		return nil, &ValidationError{Field: "Country", Message: "Country must be 'US' or 'CA'"}
	}

	body := map[string]any{"country": country}
	if opts.AreaCode != "" {
		if len(opts.AreaCode) != 3 || !isAllDigits(opts.AreaCode) {
			return nil, &ValidationError{
				Field:   "AreaCode",
				Message: "AreaCode must be a 3-digit string, e.g. '415'",
			}
		}
		body["areaCode"] = opts.AreaCode
	}

	respBytes, err := c.doRequest(ctx, "/v1/phone/numbers/buy", body)
	if err != nil {
		return nil, err
	}

	var result NumberBuyResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// RenewNumber extends an existing provisioned number by 30 days. Costs $5.00.
//
// Returns APIError 403 if the wallet doesn't own this number or it has expired.
func (c *PhoneClient) RenewNumber(ctx context.Context, phoneNumber string) (*NumberRenewResponse, error) {
	if err := validateE164(phoneNumber); err != nil {
		return nil, err
	}
	respBytes, err := c.doRequest(ctx, "/v1/phone/numbers/renew", map[string]any{
		"phoneNumber": strings.TrimSpace(phoneNumber),
	})
	if err != nil {
		return nil, err
	}

	var result NumberRenewResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// ListNumbers lists the wallet's active phone numbers. Costs ~$0.001.
func (c *PhoneClient) ListNumbers(ctx context.Context) (*NumberListResponse, error) {
	respBytes, err := c.doRequest(ctx, "/v1/phone/numbers/list", map[string]any{})
	if err != nil {
		return nil, err
	}

	var result NumberListResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// ReleaseNumber returns a provisioned number to the Twilio pool. Free, but the
// request still flows through x402 so the backend can verify ownership.
func (c *PhoneClient) ReleaseNumber(ctx context.Context, phoneNumber string) (*NumberReleaseResponse, error) {
	if err := validateE164(phoneNumber); err != nil {
		return nil, err
	}
	respBytes, err := c.doRequest(ctx, "/v1/phone/numbers/release", map[string]any{
		"phoneNumber": strings.TrimSpace(phoneNumber),
	})
	if err != nil {
		return nil, err
	}

	var result NumberReleaseResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// ---------------------------------------------------------------- internals

func validateE164(value string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return &ValidationError{
			Field:   "phoneNumber",
			Message: "phoneNumber is required (E.164 format, e.g. '+14155552671')",
		}
	}
	if !strings.HasPrefix(v, "+") || len(v) < 8 || len(v) > 16 || !isAllDigits(v[1:]) {
		return &ValidationError{
			Field:   "phoneNumber",
			Message: fmt.Sprintf("phoneNumber must be E.164 (e.g. '+14155552671'), got %q", value),
		}
	}
	return nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func unmarshalLookup(respBytes []byte) (*PhoneLookupResponse, error) {
	var result PhoneLookupResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(respBytes, &raw); err == nil {
		known := map[string]struct{}{
			"phone_number":           {},
			"caller_name":            {},
			"carrier":                {},
			"country_code":           {},
			"national_format":        {},
			"line_type_intelligence": {},
			"sim_swap":               {},
			"call_forwarding":        {},
			"txHash":                 {},
		}
		extra := make(map[string]any)
		for k, v := range raw {
			if _, ok := known[k]; !ok {
				extra[k] = v
			}
		}
		if len(extra) > 0 {
			result.Extra = extra
		}
	}
	return &result, nil
}
