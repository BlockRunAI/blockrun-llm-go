package blockrun

// Multi-chain JSON-RPC client — POST /v1/rpc/{network}.
//
// One endpoint, 40+ chains: Ethereum, Base, Solana, Polygon, BSC, Arbitrum,
// Optimism, Avalanche, Bitcoin, Sui, and more (powered by Tatum's RPC
// gateway). Standard JSON-RPC 2.0 passthrough — no API key, pay-per-call in
// USDC. Flat $0.002 per call; a JSON-RPC batch charges per element.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine - only signatures are transmitted.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	// RPCPriceUSD is the flat price per JSON-RPC call (batch = N x this).
	// Informational only — the actual quote always comes from the 402
	// challenge.
	RPCPriceUSD = 0.002

	// DefaultRPCTimeout is the default HTTP timeout for RPC calls
	// (the upstream gateway timeout is 20s).
	DefaultRPCTimeout = 60 * time.Second
)

// RPCSupportedNetworks are the curated chains accepted by /v1/rpc/{network}.
// Mirrors the backend chain registry (verified live 2026-06-07). EVM chains
// use eth_* JSON-RPC; non-EVM (Solana / UTXO / NEAR / Sui / XRP Ledger /
// Polkadot) speak their own JSON-RPC dialect. Unknown but well-formed slugs
// fall through server-side to a generic "{slug}-mainnet" gateway attempt, so
// new Tatum chains work without an SDK update.
var RPCSupportedNetworks = []string{
	// EVM
	"ethereum",
	"base",
	"arbitrum",
	"arbitrum-nova",
	"optimism",
	"polygon",
	"bsc",
	"avalanche",
	"fantom",
	"cronos",
	"celo",
	"gnosis",
	"zksync",
	"berachain",
	"unichain",
	"monad",
	"chiliz",
	"moonbeam",
	"aurora",
	"flare",
	"oasis",
	"kaia",
	"sonic",
	"xdc",
	"abstract",
	"hyperevm",
	"plume",
	"ronin",
	"rootstock",
	// Non-EVM (JSON-RPC-compatible)
	"solana",
	"bitcoin",
	"litecoin",
	"dogecoin",
	"bitcoin-cash",
	"near",
	"sui",
	"ripple",
	"polkadot",
	"kusama",
	"zcash",
}

// RPCNetworkAliases are common short names the gateway also accepts
// (resolved server-side).
var RPCNetworkAliases = map[string]string{
	"eth":                 "ethereum",
	"arb":                 "arbitrum",
	"arbitrum-one":        "arbitrum",
	"arb-one":             "arbitrum",
	"arb-nova":            "arbitrum-nova",
	"op":                  "optimism",
	"matic":               "polygon",
	"pol":                 "polygon",
	"bnb":                 "bsc",
	"binance":             "bsc",
	"binance-smart-chain": "bsc",
	"avax":                "avalanche",
	"ftm":                 "fantom",
	"bera":                "berachain",
	"klaytn":              "kaia",
	"chz":                 "chiliz",
	"hyperliquid":         "hyperevm",
	"rsk":                 "rootstock",
	"sol":                 "solana",
	"btc":                 "bitcoin",
	"ltc":                 "litecoin",
	"doge":                "dogecoin",
	"bch":                 "bitcoin-cash",
	"xrp":                 "ripple",
	"xrpl":                "ripple",
	"dot":                 "polkadot",
	"zec":                 "zcash",
}

// rpcNetworkPattern matches well-formed network slugs (mirrors the backend).
var rpcNetworkPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)

// RPCClient is the BlockRun multi-chain RPC client with automatic x402
// micropayments on Base chain.
//
// Standard JSON-RPC 2.0 access to 40+ chains through BlockRun's Tatum
// gateway. Flat $0.002 per call; a JSON-RPC batch charges per element.
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine - only signatures are transmitted.
type RPCClient struct {
	*baseClient
}

// RPCClientOption configures an RPCClient.
type RPCClientOption func(*RPCClient)

// WithRPCAPIURL sets a custom API URL for the RPC client.
func WithRPCAPIURL(url string) RPCClientOption {
	return func(c *RPCClient) {
		c.apiURL = strings.TrimSuffix(url, "/")
	}
}

// WithRPCTimeout sets the HTTP timeout for the RPC client.
func WithRPCTimeout(timeout time.Duration) RPCClientOption {
	return func(c *RPCClient) {
		c.httpClient.Timeout = timeout
	}
}

// WithRPCHTTPClient sets a custom HTTP client for the RPC client.
func WithRPCHTTPClient(client *http.Client) RPCClientOption {
	return func(c *RPCClient) {
		c.httpClient = client
	}
}

// NewRPCClient creates a new BlockRun multi-chain RPC client.
//
// If privateKey is empty, it will be read from the BLOCKRUN_WALLET_KEY
// or BASE_CHAIN_WALLET_KEY environment variable.
func NewRPCClient(privateKey string, opts ...RPCClientOption) (*RPCClient, error) {
	bc, err := newBaseClient(privateKey, "", DefaultRPCTimeout)
	if err != nil {
		return nil, err
	}

	client := &RPCClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	bc.checkEnvAPIURL()

	return client, nil
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// RPCResponse is the response from a multi-chain JSON-RPC call.
//
// Standard JSON-RPC 2.0 envelope plus BlockRun gateway metadata pulled from
// response headers (X-Network / X-Cache / X-Payment-Receipt). Result is the
// raw JSON result — unmarshal it into the shape the method returns.
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`

	// Network is the canonical network key, e.g. "ethereum" (from X-Network).
	Network string `json:"-"`
	// CacheHit reports whether the gateway served the response from its
	// method-aware cache (from X-Cache).
	CacheHit bool `json:"-"`
	// TxHash is the x402 settlement transaction hash (single calls).
	TxHash string `json:"-"`
}

// RPCBatchRequest is a single request in a JSON-RPC batch. JSONRPC and a
// missing ID are filled in automatically.
type RPCBatchRequest struct {
	Method  string `json:"method"`
	Params  []any  `json:"params,omitempty"`
	ID      any    `json:"id,omitempty"`
	JSONRPC string `json:"jsonrpc,omitempty"`
}

// Call makes a single JSON-RPC 2.0 call. Flat $0.002.
//
// network is a chain name (e.g. "ethereum", "base", "solana") or a common
// alias ("eth", "sol", "matic", ...) — see RPCSupportedNetworks /
// RPCNetworkAliases. method is the chain RPC method, e.g. "eth_blockNumber",
// "eth_call", "eth_getBalance" (EVM) or "getSlot", "getAccountInfo" (Solana).
// params is the method-specific params array (nil to omit).
func (c *RPCClient) Call(ctx context.Context, network, method string, params []any) (*RPCResponse, error) {
	if err := validateRPCNetwork(network); err != nil {
		return nil, err
	}
	if strings.TrimSpace(method) == "" {
		return nil, &ValidationError{Field: "method", Message: "method is required"}
	}

	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		body["params"] = params
	}

	respBytes, headers, err := c.doRequestHeaders(ctx, "/v1/rpc/"+network, body)
	if err != nil {
		return nil, err
	}

	var rpcResp RPCResponse
	if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	applyRPCHeaders(&rpcResp, headers)

	return &rpcResp, nil
}

// Batch makes a JSON-RPC 2.0 batch call. Priced per element ($0.002 x N).
//
// Each request needs a Method; JSONRPC and missing IDs are filled in
// automatically. Responses are returned in upstream order.
func (c *RPCClient) Batch(ctx context.Context, network string, requests []RPCBatchRequest) ([]RPCResponse, error) {
	if err := validateRPCNetwork(network); err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return nil, &ValidationError{Field: "requests", Message: "batch requires at least one request"}
	}

	batch := make([]map[string]any, 0, len(requests))
	for i, req := range requests {
		if strings.TrimSpace(req.Method) == "" {
			return nil, &ValidationError{
				Field:   "requests",
				Message: fmt.Sprintf("batch request %d is missing method", i),
			}
		}
		entry := map[string]any{
			"jsonrpc": "2.0",
			"id":      i + 1,
			"method":  req.Method,
		}
		if req.JSONRPC != "" {
			entry["jsonrpc"] = req.JSONRPC
		}
		if req.ID != nil {
			entry["id"] = req.ID
		}
		if req.Params != nil {
			entry["params"] = req.Params
		}
		batch = append(batch, entry)
	}

	// The endpoint takes a raw JSON-RPC array body, so we bypass the
	// map-based doRequest and post the encoded slice directly.
	respBytes, headers, err := c.doRawRequestHeaders(ctx, "/v1/rpc/"+network, batch)
	if err != nil {
		return nil, err
	}

	var rpcResps []RPCResponse
	if err := json.Unmarshal(respBytes, &rpcResps); err != nil {
		// Upstream collapsed the batch (shouldn't happen) — try a single
		// envelope before giving up.
		var single RPCResponse
		if errSingle := json.Unmarshal(respBytes, &single); errSingle != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		rpcResps = []RPCResponse{single}
	}
	for i := range rpcResps {
		applyRPCHeaders(&rpcResps[i], headers)
	}

	return rpcResps, nil
}

// doRawRequestHeaders posts an arbitrary JSON value (e.g. a JSON-RPC batch
// array) with automatic x402 payment handling and returns the body plus the
// final response headers. Mirrors baseClient.doRequestHeaders, which only
// accepts map bodies.
func (c *RPCClient) doRawRequestHeaders(ctx context.Context, endpoint string, body any) ([]byte, http.Header, error) {
	url := c.apiURL + endpoint

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encode request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPaymentRequired {
		return c.handlePaymentAndRetryHeaders(ctx, url, jsonBody, resp)
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("API error: %s", string(bodyBytes)),
		}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	return data, resp.Header, nil
}

func validateRPCNetwork(network string) error {
	if !rpcNetworkPattern.MatchString(network) {
		return &ValidationError{
			Field:   "network",
			Message: fmt.Sprintf("malformed network %q (lowercase letters, digits and dashes only)", network),
		}
	}
	return nil
}

func applyRPCHeaders(resp *RPCResponse, headers http.Header) {
	if headers == nil {
		return
	}
	resp.Network = headers.Get("X-Network")
	resp.CacheHit = strings.EqualFold(headers.Get("X-Cache"), "HIT")
	resp.TxHash = headers.Get("X-Payment-Receipt")
}
