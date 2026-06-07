package blockrun

// DefiLlama — GET /v1/defillama/{path}.
//
// DeFi protocols, TVL, yields, and token prices, powered by DefiLlama.
// $0.005/call (prices/{coins} $0.001/call). Methods live on *LLMClient and
// pay automatically via x402.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Defi queries DefiLlama DeFi data (generic escape hatch).
//
// path is one of: "protocols", "protocol/{slug}", "chains", "yields",
// "prices/{coins}" (coins comma-separated, e.g. "coingecko:bitcoin").
// params are optional query parameters passed through to DefiLlama.
func (c *LLMClient) Defi(ctx context.Context, path string, params map[string]string) (map[string]any, error) {
	if strings.TrimSpace(path) == "" {
		return nil, &ValidationError{Field: "path", Message: "Path is required"}
	}

	respBytes, err := c.doGetWithPayment(ctx, "/v1/defillama/"+path, params)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(respBytes, &result); err != nil {
		// Some DefiLlama endpoints return a JSON array — wrap it.
		var arr []any
		if errArr := json.Unmarshal(respBytes, &arr); errArr == nil {
			return map[string]any{"data": arr}, nil
		}
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

// DefiProtocols lists all DeFi protocols with TVL ($0.005/call).
func (c *LLMClient) DefiProtocols(ctx context.Context) (map[string]any, error) {
	return c.Defi(ctx, "protocols", nil)
}

// DefiProtocol returns one protocol's details + historical TVL ($0.005/call).
func (c *LLMClient) DefiProtocol(ctx context.Context, slug string) (map[string]any, error) {
	if strings.TrimSpace(slug) == "" {
		return nil, &ValidationError{Field: "slug", Message: "Slug is required"}
	}
	return c.Defi(ctx, "protocol/"+slug, nil)
}

// DefiChains returns the current TVL of every chain ($0.005/call).
func (c *LLMClient) DefiChains(ctx context.Context) (map[string]any, error) {
	return c.Defi(ctx, "chains", nil)
}

// DefiYields lists yield pools with APY/TVL ($0.005/call).
func (c *LLMClient) DefiYields(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.Defi(ctx, "yields", params)
}

// DefiPrices looks up token prices ($0.001/call).
//
// coins are ids like "coingecko:bitcoin" or "{chain}:{address}".
func (c *LLMClient) DefiPrices(ctx context.Context, coins []string) (map[string]any, error) {
	if len(coins) == 0 {
		return nil, &ValidationError{Field: "coins", Message: "At least one coin id is required"}
	}
	return c.Defi(ctx, "prices/"+strings.Join(coins, ","), nil)
}
