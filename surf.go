package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SurfClient is the BlockRun Surf client — asksurf.ai crypto-data gateway via
// x402 micropayments.
//
// Surf is a single backend partner exposing ~83 crypto-intelligence endpoints
// (exchange data, on-chain SQL, prediction markets, wallet/social analytics, …).
//
// Pricing is tiered:
//
//	Tier 1  $0.001  market data, lists, single-token reads
//	Tier 2  $0.005  AI-derived intelligence (rankings, trends, search)
//	Tier 3  $0.020  heavy LLM reports + on-chain SQL/structured queries
//
// SECURITY: Your private key is used ONLY for local EIP-712 signing.
// The key NEVER leaves your machine — only signatures are transmitted.
type SurfClient struct {
	*baseClient
}

// SurfClientOption configures a SurfClient.
type SurfClientOption func(*SurfClient)

// WithSurfAPIURL sets a custom API URL for the surf client.
func WithSurfAPIURL(url string) SurfClientOption {
	return func(c *SurfClient) {
		c.apiURL = strings.TrimSuffix(url, "/")
	}
}

// WithSurfTimeout sets the HTTP timeout for the surf client.
func WithSurfTimeout(timeout time.Duration) SurfClientOption {
	return func(c *SurfClient) {
		c.httpClient.Timeout = timeout
	}
}

// WithSurfHTTPClient sets a custom HTTP client for the surf client.
func WithSurfHTTPClient(client *http.Client) SurfClientOption {
	return func(c *SurfClient) {
		c.httpClient = client
	}
}

// NewSurfClient creates a new BlockRun Surf client.
//
// If privateKey is empty, it will be read from the BLOCKRUN_WALLET_KEY
// or BASE_CHAIN_WALLET_KEY environment variable.
func NewSurfClient(privateKey string, opts ...SurfClientOption) (*SurfClient, error) {
	bc, err := newBaseClient(privateKey, "", DefaultTimeout)
	if err != nil {
		return nil, err
	}

	client := &SurfClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	bc.checkEnvAPIURL()
	return client, nil
}

// SurfTierPrices mirrors src/lib/surf.ts SURF_TIER_*_PRICE on the backend.
var SurfTierPrices = map[int]float64{
	1: 0.001,
	2: 0.005,
	3: 0.020,
}

// SurfEndpoint is a single discovery entry in the Surf catalog.
type SurfEndpoint struct {
	Path           string   `json:"path"`
	Method         string   `json:"method"`
	Tier           int      `json:"tier"`
	PriceUSD       float64  `json:"price_usd"`
	RequiredParams []string `json:"required_params"`
}

type surfCatalogEntry struct {
	method   string
	tier     int
	required []string
}

// surfCatalog mirrors src/lib/surf.ts SURF_ENDPOINTS on the backend. Keep
// this in sync when backend endpoints change — used for discovery, parameter
// validation, and auto GET/POST routing in SurfClient.Call().
var surfCatalog = []SurfEndpoint{
	// exchange
	{"exchange/markets", "GET", 1, SurfTierPrices[1], nil},
	{"exchange/price", "GET", 1, SurfTierPrices[1], []string{"pair"}},
	{"exchange/perp", "GET", 1, SurfTierPrices[1], []string{"pair"}},
	{"exchange/depth", "GET", 2, SurfTierPrices[2], []string{"pair"}},
	{"exchange/klines", "GET", 2, SurfTierPrices[2], []string{"pair"}},
	{"exchange/funding-history", "GET", 2, SurfTierPrices[2], []string{"pair"}},
	{"exchange/long-short-ratio", "GET", 2, SurfTierPrices[2], []string{"pair"}},
	// fund
	{"fund/detail", "GET", 1, SurfTierPrices[1], nil},
	{"fund/portfolio", "GET", 1, SurfTierPrices[1], nil},
	{"fund/ranking", "GET", 1, SurfTierPrices[1], []string{"metric"}},
	// market
	{"market/ranking", "GET", 1, SurfTierPrices[1], nil},
	{"market/fear-greed", "GET", 1, SurfTierPrices[1], nil},
	{"market/futures", "GET", 1, SurfTierPrices[1], nil},
	{"market/price", "GET", 1, SurfTierPrices[1], []string{"symbol"}},
	{"market/etf", "GET", 1, SurfTierPrices[1], []string{"symbol"}},
	{"market/options", "GET", 1, SurfTierPrices[1], []string{"symbol"}},
	{"market/liquidation/exchange-list", "GET", 2, SurfTierPrices[2], nil},
	{"market/liquidation/order", "GET", 2, SurfTierPrices[2], nil},
	{"market/liquidation/chart", "GET", 2, SurfTierPrices[2], []string{"symbol"}},
	{"market/onchain-indicator", "GET", 2, SurfTierPrices[2], []string{"symbol", "metric"}},
	{"market/price-indicator", "GET", 2, SurfTierPrices[2], []string{"indicator", "symbol"}},
	// news
	{"news/feed", "GET", 1, SurfTierPrices[1], nil},
	{"news/detail", "GET", 1, SurfTierPrices[1], []string{"id"}},
	// onchain
	{"onchain/bridge/ranking", "GET", 1, SurfTierPrices[1], nil},
	{"onchain/yield/ranking", "GET", 1, SurfTierPrices[1], nil},
	{"onchain/gas-price", "GET", 1, SurfTierPrices[1], []string{"chain"}},
	{"onchain/tx", "GET", 1, SurfTierPrices[1], []string{"hash", "chain"}},
	{"onchain/schema", "GET", 3, SurfTierPrices[3], nil},
	{"onchain/query", "POST", 3, SurfTierPrices[3], nil},
	{"onchain/sql", "POST", 3, SurfTierPrices[3], nil},
	// prediction-market
	{"prediction-market/category-metrics", "GET", 1, SurfTierPrices[1], nil},
	{"prediction-market/polymarket/ranking", "GET", 1, SurfTierPrices[1], nil},
	{"prediction-market/polymarket/trades", "GET", 1, SurfTierPrices[1], nil},
	{"prediction-market/polymarket/markets", "GET", 1, SurfTierPrices[1], []string{"market_slug"}},
	{"prediction-market/polymarket/events", "GET", 1, SurfTierPrices[1], []string{"event_slug"}},
	{"prediction-market/polymarket/prices", "GET", 1, SurfTierPrices[1], []string{"condition_id"}},
	{"prediction-market/polymarket/volumes", "GET", 1, SurfTierPrices[1], []string{"condition_id"}},
	{"prediction-market/polymarket/open-interest", "GET", 1, SurfTierPrices[1], []string{"condition_id"}},
	{"prediction-market/polymarket/positions", "GET", 2, SurfTierPrices[2], []string{"address"}},
	{"prediction-market/polymarket/activity", "GET", 2, SurfTierPrices[2], []string{"address"}},
	{"prediction-market/kalshi/ranking", "GET", 1, SurfTierPrices[1], nil},
	{"prediction-market/kalshi/markets", "GET", 1, SurfTierPrices[1], []string{"market_ticker"}},
	{"prediction-market/kalshi/events", "GET", 1, SurfTierPrices[1], []string{"event_ticker"}},
	{"prediction-market/kalshi/prices", "GET", 1, SurfTierPrices[1], []string{"ticker"}},
	{"prediction-market/kalshi/trades", "GET", 1, SurfTierPrices[1], []string{"ticker"}},
	{"prediction-market/kalshi/volumes", "GET", 1, SurfTierPrices[1], []string{"ticker"}},
	{"prediction-market/kalshi/open-interest", "GET", 1, SurfTierPrices[1], []string{"ticker"}},
	// project
	{"project/detail", "GET", 1, SurfTierPrices[1], nil},
	{"project/defi/metrics", "GET", 1, SurfTierPrices[1], []string{"metric"}},
	{"project/defi/ranking", "GET", 1, SurfTierPrices[1], []string{"metric"}},
	// search
	{"search/airdrop", "GET", 2, SurfTierPrices[2], nil},
	{"search/events", "GET", 2, SurfTierPrices[2], nil},
	{"search/kalshi", "GET", 2, SurfTierPrices[2], nil},
	{"search/polymarket", "GET", 2, SurfTierPrices[2], nil},
	{"search/web", "GET", 2, SurfTierPrices[2], []string{"q"}},
	{"search/project", "GET", 2, SurfTierPrices[2], []string{"q"}},
	{"search/news", "GET", 2, SurfTierPrices[2], []string{"q"}},
	{"search/wallet", "GET", 2, SurfTierPrices[2], []string{"q"}},
	{"search/fund", "GET", 2, SurfTierPrices[2], []string{"q"}},
	{"search/social/people", "GET", 2, SurfTierPrices[2], []string{"q"}},
	{"search/social/posts", "GET", 2, SurfTierPrices[2], []string{"q"}},
	// social
	{"social/detail", "GET", 2, SurfTierPrices[2], nil},
	{"social/ranking", "GET", 2, SurfTierPrices[2], nil},
	{"social/smart-followers/history", "GET", 2, SurfTierPrices[2], nil},
	{"social/mindshare", "GET", 2, SurfTierPrices[2], []string{"q", "interval"}},
	{"social/tweets", "GET", 1, SurfTierPrices[1], []string{"ids"}},
	{"social/tweet/replies", "GET", 1, SurfTierPrices[1], []string{"tweet_id"}},
	{"social/user", "GET", 1, SurfTierPrices[1], []string{"handle"}},
	{"social/user/followers", "GET", 1, SurfTierPrices[1], []string{"handle"}},
	{"social/user/following", "GET", 1, SurfTierPrices[1], []string{"handle"}},
	{"social/user/posts", "GET", 1, SurfTierPrices[1], []string{"handle"}},
	{"social/user/replies", "GET", 1, SurfTierPrices[1], []string{"handle"}},
	// token
	{"token/tokenomics", "GET", 1, SurfTierPrices[1], nil},
	{"token/dex-trades", "GET", 2, SurfTierPrices[2], []string{"address"}},
	{"token/holders", "GET", 2, SurfTierPrices[2], []string{"address", "chain"}},
	{"token/transfers", "GET", 2, SurfTierPrices[2], []string{"address", "chain"}},
	// wallet
	{"wallet/detail", "GET", 2, SurfTierPrices[2], []string{"address"}},
	{"wallet/history", "GET", 2, SurfTierPrices[2], []string{"address"}},
	{"wallet/net-worth", "GET", 2, SurfTierPrices[2], []string{"address"}},
	{"wallet/transfers", "GET", 2, SurfTierPrices[2], []string{"address"}},
	{"wallet/protocols", "GET", 2, SurfTierPrices[2], []string{"address"}},
	{"wallet/labels/batch", "GET", 2, SurfTierPrices[2], []string{"addresses"}},
	// web
	{"web/fetch", "GET", 2, SurfTierPrices[2], []string{"url"}},
}

var surfCatalogByPath = func() map[string]surfCatalogEntry {
	m := make(map[string]surfCatalogEntry, len(surfCatalog))
	for _, e := range surfCatalog {
		m[e.Path] = surfCatalogEntry{method: e.Method, tier: e.Tier, required: e.RequiredParams}
	}
	return m
}()

// SurfEndpoints returns the full Surf endpoint catalog with method, tier and price.
func SurfEndpoints() []SurfEndpoint {
	out := make([]SurfEndpoint, len(surfCatalog))
	copy(out, surfCatalog)
	return out
}

// SurfEndpointInfo returns catalog info for one path, or nil if unknown.
func SurfEndpointInfo(path string) *SurfEndpoint {
	entry, ok := surfCatalogByPath[path]
	if !ok {
		return nil
	}
	return &SurfEndpoint{
		Path:           path,
		Method:         entry.method,
		Tier:           entry.tier,
		PriceUSD:       SurfTierPrices[entry.tier],
		RequiredParams: entry.required,
	}
}

// SurfPrice returns the settled USDC price for a Surf endpoint.
func SurfPrice(path string) (float64, error) {
	info := SurfEndpointInfo(path)
	if info == nil {
		return 0, fmt.Errorf("unknown Surf endpoint: %q", path)
	}
	return info.PriceUSD, nil
}

// Get fetches a /v1/surf/{path} GET endpoint with optional query params.
//
// Param values are stringified via fmt.Sprint. For list values (e.g. ids,
// addresses) pass a comma-joined string per the backend convention.
func (c *SurfClient) Get(ctx context.Context, path string, params map[string]any) (map[string]any, error) {
	if err := validateSurfPath(path, "GET", params); err != nil {
		return nil, err
	}
	respBytes, err := c.doGetWithPayment(ctx, "/v1/surf/"+path, stringifyParams(params))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(respBytes, &out); err != nil {
		return nil, fmt.Errorf("failed to decode surf response: %w", err)
	}
	return out, nil
}

// Post sends a JSON body to a /v1/surf/{path} POST endpoint.
func (c *SurfClient) Post(ctx context.Context, path string, body map[string]any) (map[string]any, error) {
	if err := validateSurfPath(path, "POST", body); err != nil {
		return nil, err
	}
	if body == nil {
		body = map[string]any{}
	}
	respBytes, err := c.doRequest(ctx, "/v1/surf/"+path, body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(respBytes, &out); err != nil {
		return nil, fmt.Errorf("failed to decode surf response: %w", err)
	}
	return out, nil
}

// SurfCallOptions bundles the optional params/body for Call.
type SurfCallOptions struct {
	// Params are merged into the query string for GET endpoints, or into
	// the JSON body for POST endpoints.
	Params map[string]any
	// Body is merged into the JSON body for POST endpoints (wins over Params).
	// Ignored for GET endpoints.
	Body map[string]any
}

// Call auto-routes to Get or Post based on the catalog.
//
// For GET endpoints, Params and Body are merged into query parameters
// (Body wins on conflict). For POST endpoints they are merged into the JSON
// body (Body wins on conflict).
func (c *SurfClient) Call(ctx context.Context, path string, opts SurfCallOptions) (map[string]any, error) {
	info := SurfEndpointInfo(path)
	if info == nil {
		return nil, fmt.Errorf("unknown Surf endpoint: %q (try SurfEndpoints() to list available paths)", path)
	}
	merged := mergeMaps(opts.Params, opts.Body)
	if info.Method == "GET" {
		return c.Get(ctx, path, merged)
	}
	return c.Post(ctx, path, merged)
}

// ---------------------------------------------------------------- internals

func validateSurfPath(path, method string, supplied map[string]any) error {
	info := SurfEndpointInfo(path)
	if info == nil {
		// Allow forward-compat with newer backend endpoints — the server
		// will reject anything truly unknown.
		return nil
	}
	if info.Method != method {
		return &ValidationError{
			Field:   "path",
			Message: fmt.Sprintf("Surf endpoint %q requires method %s, got %s", path, info.Method, method),
		}
	}
	var missing []string
	for _, p := range info.RequiredParams {
		if _, ok := supplied[p]; !ok {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return &ValidationError{
			Field:   "params",
			Message: fmt.Sprintf("Surf endpoint %q is missing required params: %v", path, missing),
		}
	}
	return nil
}

func stringifyParams(params map[string]any) map[string]string {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]string, len(params))
	for k, v := range params {
		switch tv := v.(type) {
		case nil:
			continue
		case string:
			out[k] = tv
		case []string:
			out[k] = strings.Join(tv, ",")
		default:
			out[k] = fmt.Sprint(tv)
		}
	}
	return out
}

func mergeMaps(a, b map[string]any) map[string]any {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
