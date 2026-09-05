package blockrun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"strconv"
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
	JSONRPC string    `json:"jsonrpc"`
	ID      int       `json:"id"`
	Result  string    `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

// rpcError is the JSON-RPC error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// GetBalance queries the client's USDC balance on the chain it pays from:
// Base mainnet for a Base client, Solana mainnet for a Solana one.
//
// It used to read the Base USDC contract unconditionally, so a Solana client
// fed its bs58 pubkey into an eth_call against public Base RPCs — which either
// errors or answers about an address that is not the caller's.
func (c *LLMClient) GetBalance(ctx context.Context) (float64, error) {
	// An API-key client has no address to read a chain balance for. Returning
	// 0 would be the worst answer available: it is indistinguishable from an
	// empty wallet, and an agent that gates on it would stop calling an account
	// with plenty of credit.
	if c.isAPIKey() {
		return 0, walletOnly("GetBalance")
	}
	if c.isSolana() {
		return getSolanaUSDCBalance(ctx, c.solanaRPCURL, c.address)
	}
	return getUSDCBalance(ctx, c.address, USDCBaseContract, baseMainnetRPCs)
}

// GetBalanceTestnet queries the USDC balance on Base Sepolia testnet.
//
// Base only: there is no configured Solana devnet USDC mint, so a Solana client
// gets an explicit error rather than a reading from the wrong chain.
func (c *LLMClient) GetBalanceTestnet(ctx context.Context) (float64, error) {
	if c.isAPIKey() {
		return 0, walletOnly("GetBalanceTestnet")
	}
	if c.isSolana() {
		return 0, &ValidationError{
			Field:   "chain",
			Message: "GetBalanceTestnet reads Base Sepolia; this client pays on Solana and no devnet USDC mint is configured. Use GetBalance for Solana mainnet.",
		}
	}
	return getUSDCBalance(ctx, c.address, USDCBaseTestnet, baseSepoliaRPCs)
}

// getSolanaUSDCBalance sums the client's USDC across every SPL token account
// the owner holds for the mint.
//
// One owner can hold more than one token account for a single mint (the
// associated account plus any auxiliary ones), so reading only the first
// under-reports the balance. Amounts are summed in raw base units and scaled
// once, rather than converted per account, to keep the rounding to a single
// step. Decimals come from the RPC rather than being assumed: they are fixed
// per mint, so the first account's value describes them all.
//
// A wallet that has never held USDC has no token account at all. That is a zero
// balance, not an error.
func getSolanaUSDCBalance(ctx context.Context, rpcURL, owner string) (float64, error) {
	if rpcURL == "" {
		rpcURL = DefaultSolanaRPCURL
	}

	var res struct {
		Value []struct {
			Account struct {
				Data struct {
					Parsed struct {
						Info struct {
							TokenAmount struct {
								Amount   string `json:"amount"`
								Decimals int    `json:"decimals"`
							} `json:"tokenAmount"`
						} `json:"info"`
					} `json:"parsed"`
				} `json:"data"`
			} `json:"account"`
		} `json:"value"`
	}

	params := []any{
		owner,
		map[string]string{"mint": USDCSolanaMainnet},
		map[string]string{"encoding": "jsonParsed", "commitment": "confirmed"},
	}
	if err := solanaRPCCallContext(ctx, rpcURL, "getTokenAccountsByOwner", params, &res); err != nil {
		return 0, fmt.Errorf("failed to read Solana USDC balance: %w", err)
	}

	var rawTotal int64
	decimals := int(usdcSolanaDecimals)
	for _, acc := range res.Value {
		amount := acc.Account.Data.Parsed.Info.TokenAmount
		if amount.Amount == "" {
			continue
		}
		raw, err := strconv.ParseInt(amount.Amount, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("unparseable token amount %q from RPC: %w", amount.Amount, err)
		}
		rawTotal += raw
		if amount.Decimals > 0 {
			decimals = amount.Decimals
		}
	}

	return float64(rawTotal) / math.Pow10(decimals), nil
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
