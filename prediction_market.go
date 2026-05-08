package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// PM fetches prediction market data via GET request.
//
// path identifies the prediction market resource (e.g., "polymarket/events").
// params are optional query parameters appended to the URL.
func (c *LLMClient) PM(ctx context.Context, path string, params map[string]string) (map[string]any, error) {
	if path == "" {
		return nil, &ValidationError{Field: "path", Message: "Path is required"}
	}

	endpoint := "/v1/pm/" + path

	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		endpoint += "?" + q.Encode()
	}

	respBytes, err := c.doGet(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

// PMQuery sends a prediction market query via POST request.
//
// path identifies the prediction market resource (e.g., "polymarket/wallet/identities").
// query is marshaled directly as the JSON request body.
func (c *LLMClient) PMQuery(ctx context.Context, path string, query any) (map[string]any, error) {
	if path == "" {
		return nil, &ValidationError{Field: "path", Message: "Path is required"}
	}

	// Marshal the query into a map[string]any for doRequest
	queryBytes, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to encode query: %w", err)
	}

	var body map[string]any
	if err := json.Unmarshal(queryBytes, &body); err != nil {
		return nil, fmt.Errorf("failed to decode query as object: %w", err)
	}

	endpoint := "/v1/pm/" + path

	respBytes, err := c.doRequest(ctx, endpoint, body)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Predexon v2 convenience helpers — thin wrappers over PM() / PMQuery().
// All v2 endpoints are live in production as of 2026-05-07.
// Tier 1 = $0.001/call, Tier 2 = $0.005/call.
// ---------------------------------------------------------------------------

// PMMarkets lists canonical cross-venue markets (Predexon v2). Tier 1.
// Filter with venue, status, category, league, event_id, pagination_key.
func (c *LLMClient) PMMarkets(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.PM(ctx, "markets", params)
}

// PMListings lists venue-native executable listings flattened across canonical
// markets (Predexon v2). Tier 1.
func (c *LLMClient) PMListings(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.PM(ctx, "markets/listings", params)
}

// PMOutcome resolves a canonical Predexon outcome ID to its market context and
// venue listings. Tier 1.
func (c *LLMClient) PMOutcome(ctx context.Context, predexonID string) (map[string]any, error) {
	if predexonID == "" {
		return nil, &ValidationError{Field: "predexonID", Message: "predexon ID is required"}
	}
	return c.PM(ctx, "outcomes/"+predexonID, nil)
}

// PMPolymarketMarketsKeyset lists Polymarket markets with cursor-based keyset
// pagination (use pagination_key in params). Tier 1.
func (c *LLMClient) PMPolymarketMarketsKeyset(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.PM(ctx, "polymarket/markets/keyset", params)
}

// PMPolymarketEventsKeyset lists Polymarket events with cursor-based keyset
// pagination (use pagination_key in params). Tier 1.
func (c *LLMClient) PMPolymarketEventsKeyset(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.PM(ctx, "polymarket/events/keyset", params)
}

// PMSportsCategories lists available sports categories. Tier 1.
func (c *LLMClient) PMSportsCategories(ctx context.Context) (map[string]any, error) {
	return c.PM(ctx, "sports/categories", nil)
}

// PMSportsMarkets lists sports markets grouped by game. Filter with league,
// sport_type, status, venue. Tier 1.
func (c *LLMClient) PMSportsMarkets(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.PM(ctx, "sports/markets", params)
}

// PMWalletIdentity fetches identity + profile metadata (ENS, Twitter,
// portfolio, etc.) for one wallet. Tier 2.
func (c *LLMClient) PMWalletIdentity(ctx context.Context, wallet string) (map[string]any, error) {
	if wallet == "" {
		return nil, &ValidationError{Field: "wallet", Message: "wallet is required"}
	}
	return c.PM(ctx, "polymarket/wallet/identity/"+wallet, nil)
}

// PMWalletIdentities does a bulk identity lookup for up to 200 wallet
// addresses (POST). Tier 2.
func (c *LLMClient) PMWalletIdentities(ctx context.Context, addresses []string) (map[string]any, error) {
	if len(addresses) == 0 {
		return nil, &ValidationError{Field: "addresses", Message: "at least one address is required"}
	}
	if len(addresses) > 200 {
		return nil, &ValidationError{Field: "addresses", Message: "max 200 addresses per request"}
	}
	return c.PMQuery(ctx, "polymarket/wallet/identities", map[string]any{"addresses": addresses})
}

// PMWalletCluster discovers wallets connected to a seed address via on-chain
// transfers and identity proofs. Tier 2.
func (c *LLMClient) PMWalletCluster(ctx context.Context, address string) (map[string]any, error) {
	if address == "" {
		return nil, &ValidationError{Field: "address", Message: "address is required"}
	}
	return c.PM(ctx, "polymarket/wallet/"+address+"/cluster", nil)
}
