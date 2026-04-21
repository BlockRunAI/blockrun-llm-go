package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// PricePoint is a realtime quote returned by the Pyth-backed price endpoints.
type PricePoint struct {
	Symbol      string  `json:"symbol"`
	Price       float64 `json:"price"`
	PublishTime int64   `json:"publishTime,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	FeedID      string  `json:"feedId,omitempty"`
	Timestamp   string  `json:"timestamp,omitempty"`
	AssetType   string  `json:"assetType,omitempty"`
	Category    string  `json:"category,omitempty"`
	Source      string  `json:"source,omitempty"`
	Free        bool    `json:"free,omitempty"`
}

// PriceBar is a single OHLC bar in a historical series.
type PriceBar struct {
	T int64   `json:"t,omitempty"` // bar open (unix seconds)
	O float64 `json:"o,omitempty"`
	H float64 `json:"h,omitempty"`
	L float64 `json:"l,omitempty"`
	C float64 `json:"c,omitempty"`
	V float64 `json:"v,omitempty"`
}

// PriceHistoryResponse is returned by the history endpoints.
type PriceHistoryResponse struct {
	Symbol     string     `json:"symbol"`
	Resolution string     `json:"resolution,omitempty"`
	From       int64      `json:"from,omitempty"`
	To         int64      `json:"to,omitempty"`
	Bars       []PriceBar `json:"bars"`
	Source     string     `json:"source,omitempty"`
	Category   string     `json:"category,omitempty"`
}

// SymbolListResponse is returned by the list-discovery endpoints. The backend
// sometimes returns a bare array and sometimes an object; ListSymbols
// normalises both into this struct.
type SymbolListResponse struct {
	Symbols []map[string]any `json:"symbols"`
	Count   int              `json:"count,omitempty"`
}

// PriceCategory selects the upstream market category.
type PriceCategory string

const (
	// CategoryCrypto — free across price, history and list.
	CategoryCrypto PriceCategory = "crypto"
	// CategoryFX — free across price, history and list.
	CategoryFX PriceCategory = "fx"
	// CategoryCommodity — free across price, history and list (XAU, XAG, XPT, WTI, …).
	CategoryCommodity PriceCategory = "commodity"
	// CategoryUSStock — legacy alias for stocks/us (paid price+history, free list).
	CategoryUSStock PriceCategory = "usstock"
	// CategoryStocks — global equities; requires Market (us/hk/jp/kr/gb/de/fr/nl/ie/lu/cn/ca).
	CategoryStocks PriceCategory = "stocks"
)

// PriceOptions are optional flags shared by Price and History.
type PriceOptions struct {
	// Market is required when Category is CategoryStocks. One of us, hk, jp,
	// kr, gb, de, fr, nl, ie, lu, cn, ca.
	Market string
	// Session is an optional US-equity hint: "pre", "post", or "on". Ignored
	// for non-equity categories.
	Session string
}

// HistoryOptions extends PriceOptions with the bar window + resolution.
type HistoryOptions struct {
	PriceOptions
	// Resolution — TradingView convention. Valid: 1, 5, 15, 60, 240, D, W, M.
	// Defaults to "D" when empty.
	Resolution string
	From       int64 // unix seconds (required)
	To         int64 // unix seconds (defaults to now)
}

// ListOptions filters the symbol list endpoint.
type ListOptions struct {
	PriceOptions
	Query string // free-text filter (maps to ?q=)
	Limit int    // max 2000; defaults to 100 when 0
}

// Price fetches a realtime quote for symbol in category.
//
// For CategoryStocks, opts.Market is required.
// The client transparently handles x402 payment for paid categories.
func (c *LLMClient) Price(ctx context.Context, category PriceCategory, symbol string, opts *PriceOptions) (*PricePoint, error) {
	if symbol == "" {
		return nil, &ValidationError{Field: "symbol", Message: "symbol is required"}
	}
	endpoint, err := categoryPath(category, optsMarket(opts), "price", symbol)
	if err != nil {
		return nil, err
	}
	query := map[string]string{}
	if opts != nil && opts.Session != "" {
		query["session"] = opts.Session
	}
	respBytes, err := c.doGetWithPayment(ctx, endpoint, query)
	if err != nil {
		return nil, err
	}
	var pp PricePoint
	if err := json.Unmarshal(respBytes, &pp); err != nil {
		return nil, fmt.Errorf("failed to decode price response: %w", err)
	}
	return &pp, nil
}

// History fetches OHLC bars between From and To (unix seconds).
func (c *LLMClient) History(ctx context.Context, category PriceCategory, symbol string, opts *HistoryOptions) (*PriceHistoryResponse, error) {
	if symbol == "" {
		return nil, &ValidationError{Field: "symbol", Message: "symbol is required"}
	}
	if opts == nil || opts.From <= 0 {
		return nil, &ValidationError{Field: "From", Message: "history requires From (unix seconds)"}
	}
	endpoint, err := categoryPath(category, opts.Market, "history", symbol)
	if err != nil {
		return nil, err
	}
	query := map[string]string{
		"from": strconv.FormatInt(opts.From, 10),
	}
	if opts.To > 0 {
		query["to"] = strconv.FormatInt(opts.To, 10)
	}
	resolution := opts.Resolution
	if resolution == "" {
		resolution = "D"
	}
	query["resolution"] = resolution
	if opts.Session != "" {
		query["session"] = opts.Session
	}
	respBytes, err := c.doGetWithPayment(ctx, endpoint, query)
	if err != nil {
		return nil, err
	}
	var hr PriceHistoryResponse
	if err := json.Unmarshal(respBytes, &hr); err != nil {
		return nil, fmt.Errorf("failed to decode history response: %w", err)
	}
	return &hr, nil
}

// ListSymbols returns available tickers in a category. Always free — the
// endpoint never gates on x402.
func (c *LLMClient) ListSymbols(ctx context.Context, category PriceCategory, opts *ListOptions) (*SymbolListResponse, error) {
	market := ""
	if opts != nil {
		market = opts.Market
	}
	endpoint, err := categoryPath(category, market, "list", "")
	if err != nil {
		return nil, err
	}
	query := map[string]string{}
	if opts != nil {
		if opts.Query != "" {
			query["q"] = opts.Query
		}
		limit := opts.Limit
		if limit <= 0 {
			limit = 100
		}
		query["limit"] = strconv.Itoa(limit)
	} else {
		query["limit"] = "100"
	}

	respBytes, err := c.doGetWithPayment(ctx, endpoint, query)
	if err != nil {
		return nil, err
	}
	// The backend returns either a bare array or an object. Try object first.
	var asObject SymbolListResponse
	if err := json.Unmarshal(respBytes, &asObject); err == nil && asObject.Symbols != nil {
		return &asObject, nil
	}
	var asArray []map[string]any
	if err := json.Unmarshal(respBytes, &asArray); err != nil {
		return nil, fmt.Errorf("failed to decode symbol list: %w", err)
	}
	return &SymbolListResponse{Symbols: asArray, Count: len(asArray)}, nil
}

func optsMarket(opts *PriceOptions) string {
	if opts == nil {
		return ""
	}
	return opts.Market
}

func categoryPath(category PriceCategory, market, kind, symbol string) (string, error) {
	var base string
	switch category {
	case CategoryStocks:
		if market == "" {
			return "", &ValidationError{Field: "Market", Message: "Market is required for CategoryStocks"}
		}
		base = "/v1/stocks/" + market
	case CategoryCrypto, CategoryFX, CategoryCommodity, CategoryUSStock:
		base = "/v1/" + string(category)
	default:
		return "", &ValidationError{Field: "category", Message: fmt.Sprintf("unknown category: %q", string(category))}
	}
	if symbol == "" {
		return base + "/" + kind, nil
	}
	return base + "/" + kind + "/" + strings.ToUpper(symbol), nil
}
