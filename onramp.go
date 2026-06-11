package blockrun

// Coinbase Onramp — POST /v1/onramp/token.
//
// Exchanges your Base wallet address for a one-time Coinbase Onramp URL.
// The gateway holds the CDP API key, signs the JWT, and mints a single-use
// `sessionToken` (Coinbase requires Secure Init since 2025-07-31 — plain
// appId URLs are deprecated). The returned URL is one-time and expires in
// ~5 minutes, so mint it at click time and never cache it.
//
// Funding your own wallet is free: the endpoint settles $0 and uses your
// x402 signature purely to authenticate the wallet — the funding address
// must match the signer, so you can only mint a link for the wallet you
// control. Base / USDC only.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// evmAddressRegex validates a 0x-prefixed 40-hex-char EVM address.
var evmAddressRegex = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

// onrampURLPrefix is the only host a valid Onramp link may use.
const onrampURLPrefix = "https://pay.coinbase.com/"

// OnrampResult is the response from Onramp.
type OnrampResult struct {
	// URL is a one-time https://pay.coinbase.com/... link prefilled for the
	// wallet. It expires in ~5 minutes — open it immediately, never cache it.
	URL string `json:"url"`
}

// Onramp mints a one-time Coinbase Onramp link that funds address with Base
// USDC via card or bank (60+ fiat currencies). Free — the x402 signature only
// authenticates the wallet, so address must match the signing wallet.
//
// Base / USDC only. Returns an error if address is malformed, the gateway is
// unreachable or unconfigured, or no valid onramp URL is returned.
func (c *LLMClient) Onramp(ctx context.Context, address string) (*OnrampResult, error) {
	address = strings.TrimSpace(address)
	if !evmAddressRegex.MatchString(address) {
		return nil, &ValidationError{
			Field:   "address",
			Message: "A valid Base (EVM) address (0x + 40 hex) is required. Onramp supports Base USDC only.",
		}
	}

	body := map[string]any{
		"address": address,
		"network": "base",
		"asset":   "USDC",
	}

	respBytes, err := c.doRequest(ctx, "/v1/onramp/token", body)
	if err != nil {
		return nil, err
	}

	var result OnrampResult
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if !strings.HasPrefix(result.URL, onrampURLPrefix) {
		return nil, fmt.Errorf("gateway returned no onramp url")
	}
	return &result, nil
}
