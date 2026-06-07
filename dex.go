package blockrun

// 0x DEX — /v1/zerox/{path}.
//
// Swap + Gasless quote APIs, powered by 0x. FREE — no x402 payment; BlockRun
// takes an on-chain affiliate fee on executed swaps instead. Methods live on
// *LLMClient.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Dex queries the 0x Swap / Gasless APIs via GET (generic escape hatch).
//
// path is one of: "price", "quote", "gasless/price", "gasless/quote",
// "gasless/status/{tradeHash}", "gasless/approval-tokens", "gasless/chains",
// "swap/chains". For the one POST endpoint use DexGaslessSubmit.
// params carry the swap query (chainId, sellToken, buyToken, sellAmount,
// taker, ...).
func (c *LLMClient) Dex(ctx context.Context, path string, params map[string]string) (map[string]any, error) {
	if strings.TrimSpace(path) == "" {
		return nil, &ValidationError{Field: "path", Message: "Path is required"}
	}

	respBytes, err := c.doGetWithPayment(ctx, "/v1/zerox/"+path, params)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

// DexPrice returns an indicative Permit2 swap price — no commitment (free).
func (c *LLMClient) DexPrice(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.Dex(ctx, "price", params)
}

// DexQuote returns a firm Permit2 swap quote with permit2.eip712 + tx data (free).
func (c *LLMClient) DexQuote(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.Dex(ctx, "quote", params)
}

// DexGaslessPrice returns a gasless indicative price quote (free).
func (c *LLMClient) DexGaslessPrice(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.Dex(ctx, "gasless/price", params)
}

// DexGaslessQuote returns a gasless firm quote — trade.eip712 to sign (free).
func (c *LLMClient) DexGaslessQuote(ctx context.Context, params map[string]string) (map[string]any, error) {
	return c.Dex(ctx, "gasless/quote", params)
}

// DexGaslessSubmit submits a signed gasless trade; the 0x relayer pays gas (free).
//
// body carries the signed trade (+ optional approval) eip712 payloads.
func (c *LLMClient) DexGaslessSubmit(ctx context.Context, body map[string]any) (map[string]any, error) {
	if len(body) == 0 {
		return nil, &ValidationError{Field: "body", Message: "Signed trade body is required"}
	}

	respBytes, err := c.doRequest(ctx, "/v1/zerox/gasless/submit", body)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

// DexGaslessStatus polls a gasless trade's status by tradeHash (free).
func (c *LLMClient) DexGaslessStatus(ctx context.Context, tradeHash string) (map[string]any, error) {
	if strings.TrimSpace(tradeHash) == "" {
		return nil, &ValidationError{Field: "tradeHash", Message: "Trade hash is required"}
	}
	return c.Dex(ctx, "gasless/status/"+tradeHash, nil)
}

// DexChains lists chains where the Swap API is supported (free).
func (c *LLMClient) DexChains(ctx context.Context) (map[string]any, error) {
	return c.Dex(ctx, "swap/chains", nil)
}

// DexGaslessChains lists chains where the Gasless API is supported (free).
func (c *LLMClient) DexGaslessChains(ctx context.Context) (map[string]any, error) {
	return c.Dex(ctx, "gasless/chains", nil)
}
