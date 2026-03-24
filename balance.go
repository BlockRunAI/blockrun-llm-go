package blockrun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

var (
	baseMainnetRPCs = []string{
		"https://base.publicnode.com",
		"https://mainnet.base.org",
		"https://base.meowrpc.com",
	}
	baseSepoliaRPCs = []string{
		"https://sepolia.base.org",
		"https://base-sepolia-rpc.publicnode.com",
	}

	// USDCBaseTestnet is the USDC contract address on Base Sepolia testnet.
	USDCBaseTestnet = "0x036CbD53842c5426634e7929541eC2318f3dCF7e"
)

// rpcRequest is the JSON-RPC request payload.
type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

// rpcResponse is the JSON-RPC response payload.
type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Result  string      `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

// rpcError is the JSON-RPC error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// GetBalance queries the USDC balance on Base mainnet for the client's wallet address.
func (c *LLMClient) GetBalance(ctx context.Context) (float64, error) {
	return getUSDCBalance(ctx, c.address, USDCBaseContract, baseMainnetRPCs)
}

// GetBalanceTestnet queries the USDC balance on Base Sepolia testnet for the client's wallet address.
func (c *LLMClient) GetBalanceTestnet(ctx context.Context) (float64, error) {
	return getUSDCBalance(ctx, c.address, USDCBaseTestnet, baseSepoliaRPCs)
}

// getUSDCBalance queries the USDC balance for an address using the balanceOf selector.
// It tries each RPC endpoint in order and returns the first successful result.
func getUSDCBalance(ctx context.Context, address string, usdcContract string, rpcs []string) (float64, error) {
	// balanceOf(address) selector = 0x70a08231
	// Pad address to 32 bytes (remove 0x prefix, left-pad with zeros)
	addr := strings.TrimPrefix(strings.ToLower(address), "0x")
	data := "0x70a08231" + fmt.Sprintf("%064s", addr)

	callObj := map[string]string{
		"to":   usdcContract,
		"data": data,
	}

	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  "eth_call",
		Params:  []interface{}{callObj, "latest"},
		ID:      1,
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal RPC request: %w", err)
	}

	var lastErr error
	for _, rpcURL := range rpcs {
		result, err := callRPC(ctx, rpcURL, payload)
		if err != nil {
			lastErr = err
			continue
		}
		return parseUSDCBalance(result)
	}

	if lastErr != nil {
		return 0, fmt.Errorf("all RPC endpoints failed, last error: %w", lastErr)
	}
	return 0, fmt.Errorf("no RPC endpoints configured")
}

// callRPC sends a JSON-RPC request to the given URL and returns the result string.
func callRPC(ctx context.Context, rpcURL string, payload []byte) (string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("RPC request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("RPC returned status %d: %s", resp.StatusCode, string(body))
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return "", fmt.Errorf("failed to parse RPC response: %w", err)
	}

	if rpcResp.Error != nil {
		return "", fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// parseUSDCBalance converts a hex-encoded balance string to a float64 USDC amount.
// USDC has 6 decimal places.
func parseUSDCBalance(hexResult string) (float64, error) {
	hexResult = strings.TrimPrefix(hexResult, "0x")
	if hexResult == "" || hexResult == "0" {
		return 0, nil
	}

	balance := new(big.Int)
	_, ok := balance.SetString(hexResult, 16)
	if !ok {
		return 0, fmt.Errorf("failed to parse hex balance: %s", hexResult)
	}

	// Convert from 6 decimal places to float64
	// balance / 1_000_000
	balanceFloat := new(big.Float).SetInt(balance)
	divisor := new(big.Float).SetInt64(1_000_000)
	result, _ := new(big.Float).Quo(balanceFloat, divisor).Float64()

	return result, nil
}
