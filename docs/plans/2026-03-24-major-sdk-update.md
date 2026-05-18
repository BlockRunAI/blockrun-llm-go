# BlockRun Go SDK Major Update — Feature Parity with Python/TypeScript SDKs

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Bring the Go SDK to full feature parity with the Python and TypeScript SDKs — adding X/Twitter data, Search, Prediction Markets, Smart Routing, caching, cost logging, balance checking, context.Context support, and tool/function calling.

**Architecture:** Refactor the duplicated payment logic in LLMClient/ImageClient into a shared `baseClient` struct (embedded composition). All new endpoints build on this shared base. Add `context.Context` as first parameter to all public methods. New features are each in their own file following existing patterns.

**Tech Stack:** Go 1.22, go-ethereum (existing), stdlib net/http, encoding/json, crypto/sha256, os

---

## Task 1: Refactor — Extract shared `baseClient` from LLMClient/ImageClient

The current LLMClient and ImageClient duplicate ~200 lines of identical payment/retry logic. Extract into a shared `baseClient` with a generic `doRequest` method that returns raw `[]byte`.

**Files:**
- Create: `base_client.go`
- Modify: `client.go`
- Modify: `image.go`
- Test: `client_test.go` (all existing tests must still pass)

**Step 1: Create `base_client.go`**

```go
// base_client.go
package blockrun

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

// baseClient contains shared logic for all BlockRun API clients.
type baseClient struct {
	privateKey      *ecdsa.PrivateKey
	address         string
	apiURL          string
	httpClient      *http.Client

	mu              sync.Mutex
	sessionTotalUSD float64
	sessionCalls    int
}

// newBaseClient creates a baseClient from a private key hex string.
func newBaseClient(privateKey string, apiURL string, timeout time.Duration) (*baseClient, error) {
	key := privateKey
	if key == "" {
		key = os.Getenv("BLOCKRUN_WALLET_KEY")
	}
	if key == "" {
		key = os.Getenv("BASE_CHAIN_WALLET_KEY")
	}
	if key == "" {
		return nil, &ValidationError{
			Field:   "privateKey",
			Message: "Private key required. Pass privateKey parameter or set BASE_CHAIN_WALLET_KEY environment variable. NOTE: Your key never leaves your machine - only signatures are sent.",
		}
	}

	key = strings.TrimPrefix(key, "0x")
	ecdsaKey, err := crypto.HexToECDSA(key)
	if err != nil {
		return nil, &ValidationError{
			Field:   "privateKey",
			Message: fmt.Sprintf("Invalid private key format: %v", err),
		}
	}

	address := crypto.PubkeyToAddress(ecdsaKey.PublicKey).Hex()

	bc := &baseClient{
		privateKey: ecdsaKey,
		address:    address,
		apiURL:     apiURL,
		httpClient: &http.Client{Timeout: timeout},
	}

	if envURL := os.Getenv("BLOCKRUN_API_URL"); envURL != "" && bc.apiURL == DefaultAPIURL {
		bc.apiURL = strings.TrimSuffix(envURL, "/")
	}

	return bc, nil
}

// doRequest makes a POST request with x402 payment handling.
// Returns the raw response body bytes on success.
func (bc *baseClient) doRequest(ctx context.Context, endpoint string, body map[string]any) ([]byte, error) {
	url := bc.apiURL + endpoint

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := bc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPaymentRequired {
		return bc.handlePaymentAndRetry(ctx, url, jsonBody, resp)
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("API error: %s", string(bodyBytes)),
		}
	}

	return io.ReadAll(resp.Body)
}

// doGet makes a GET request with x402 payment handling.
func (bc *baseClient) doGet(ctx context.Context, endpoint string) ([]byte, error) {
	url := bc.apiURL + endpoint

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := bc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPaymentRequired {
		return bc.handlePaymentAndRetry(ctx, url, nil, resp)
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("API error: %s", string(bodyBytes)),
		}
	}

	return io.ReadAll(resp.Body)
}

// handlePaymentAndRetry handles 402 responses by signing and retrying.
func (bc *baseClient) handlePaymentAndRetry(ctx context.Context, url string, body []byte, resp *http.Response) ([]byte, error) {
	paymentHeader := resp.Header.Get("payment-required")
	if paymentHeader == "" {
		var respBody map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&respBody); err == nil {
			if _, ok := respBody["x402"]; ok {
				jsonBytes, _ := json.Marshal(respBody)
				paymentHeader = string(jsonBytes)
			}
		}
	}

	if paymentHeader == "" {
		return nil, &PaymentError{Message: "402 response but no payment requirements found"}
	}

	paymentReq, err := ParsePaymentRequired(paymentHeader)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to parse payment requirements: %v", err)}
	}

	paymentOption, err := ExtractPaymentDetails(paymentReq)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to extract payment details: %v", err)}
	}

	resourceURL := paymentReq.Resource.URL
	if resourceURL == "" {
		resourceURL = url
	}

	paymentPayload, err := CreatePaymentPayload(
		bc.privateKey,
		paymentOption.PayTo,
		paymentOption.Amount,
		paymentOption.Network,
		resourceURL,
		paymentReq.Resource.Description,
		paymentOption.MaxTimeoutSeconds,
		paymentOption.Extra,
		paymentReq.Extensions,
	)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to create payment: %v", err)}
	}

	// Build retry request — POST if body present, GET otherwise
	method := "GET"
	var bodyReader io.Reader
	if body != nil {
		method = "POST"
		bodyReader = bytes.NewReader(body)
	}

	retryReq, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create retry request: %w", err)
	}
	if body != nil {
		retryReq.Header.Set("Content-Type", "application/json")
	}
	retryReq.Header.Set("PAYMENT-SIGNATURE", paymentPayload)

	retryResp, err := bc.httpClient.Do(retryReq)
	if err != nil {
		return nil, fmt.Errorf("retry request failed: %w", err)
	}
	defer retryResp.Body.Close()

	if retryResp.StatusCode == http.StatusPaymentRequired {
		return nil, &PaymentError{Message: "Payment was rejected. Check your wallet balance."}
	}

	if retryResp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(retryResp.Body)
		return nil, &APIError{
			StatusCode: retryResp.StatusCode,
			Message:    fmt.Sprintf("API error after payment: %s", string(bodyBytes)),
		}
	}

	// Track spending
	bc.mu.Lock()
	bc.sessionCalls++
	if amountStr := paymentOption.Amount; amountStr != "" {
		var amountMicro float64
		if _, err := fmt.Sscanf(amountStr, "%f", &amountMicro); err == nil {
			bc.sessionTotalUSD += amountMicro / 1_000_000
		}
	}
	bc.mu.Unlock()

	return io.ReadAll(retryResp.Body)
}

// GetWalletAddress returns the wallet address used for payments.
func (bc *baseClient) GetWalletAddress() string {
	return bc.address
}

// GetSpending returns session spending information.
func (bc *baseClient) GetSpending() Spending {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return Spending{
		TotalUSD: bc.sessionTotalUSD,
		Calls:    bc.sessionCalls,
	}
}
```

**Step 2: Refactor `client.go` to embed `baseClient`**

Replace the duplicated fields and methods in LLMClient. The LLMClient struct becomes:

```go
type LLMClient struct {
	*baseClient
}

func NewLLMClient(privateKey string, opts ...ClientOption) (*LLMClient, error) {
	bc, err := newBaseClient(privateKey, DefaultAPIURL, DefaultTimeout)
	if err != nil {
		return nil, err
	}
	client := &LLMClient{baseClient: bc}
	for _, opt := range opts {
		opt(client)
	}
	return client, nil
}
```

ClientOption changes to `func(*LLMClient)` operating on `client.baseClient` fields. All methods (Chat, ChatCompletion, ListModels, etc.) now take `ctx context.Context` as first param and use `bc.doRequest(ctx, ...)`.

**Step 3: Refactor `image.go` to embed `baseClient`**

Same pattern — ImageClient embeds `*baseClient`, removes duplicated payment logic.

**Step 4: Run existing tests**

Run: `cd /Users/vickyfu/Documents/blockrun-web/blockrun-llm-go && go test ./... -v`
Expected: All existing tests pass (update test calls to pass `context.Background()` where needed)

**Step 5: Commit**

```bash
git add base_client.go client.go image.go client_test.go image_test.go
git commit -m "refactor: extract shared baseClient, add context.Context to all methods"
```

---

## Task 2: Add `context.Context` to all public methods

Every public method that does I/O gets `ctx context.Context` as first parameter. This is idiomatic Go and enables cancellation, deadlines, and tracing.

**Files:**
- Modify: `client.go` — Chat, ChatWithSystem, ChatCompletion, ListModels, ListImageModels, ListAllModels
- Modify: `image.go` — Generate, ListImageModels
- Modify: `examples/basic/main.go` — update to use `context.Background()`
- Test: `client_test.go`, `image_test.go` — update all calls

**(This is done as part of Task 1 above — they are one atomic refactor.)**

---

## Task 3: X/Twitter Data Endpoints

Add all 15 X/Twitter endpoints matching the Python SDK.

**Files:**
- Create: `x_twitter.go`
- Create: `x_twitter_types.go`
- Create: `x_twitter_test.go`

**Step 1: Create `x_twitter_types.go`**

```go
package blockrun

// XUser represents an X/Twitter user profile.
type XUser struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Username        string `json:"username"`
	Description     string `json:"description,omitempty"`
	FollowersCount  int    `json:"followers_count,omitempty"`
	FollowingCount  int    `json:"following_count,omitempty"`
	TweetCount      int    `json:"tweet_count,omitempty"`
	Verified        bool   `json:"verified,omitempty"`
	ProfileImageURL string `json:"profile_image_url,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
}

// XTweet represents an X/Twitter tweet.
type XTweet struct {
	ID        string         `json:"id"`
	Text      string         `json:"text"`
	AuthorID  string         `json:"author_id,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
	Metrics   map[string]int `json:"public_metrics,omitempty"`
}

// XUserLookupResponse is the response from XUserLookup.
type XUserLookupResponse struct {
	Users          []XUser  `json:"users"`
	NotFound       []string `json:"not_found,omitempty"`
	TotalRequested int      `json:"total_requested"`
	TotalFound     int      `json:"total_found"`
}

// XFollowersResponse is the response from XFollowers/XFollowings.
type XFollowersResponse struct {
	Followers    []XUser `json:"followers"`
	HasNextPage  bool    `json:"has_next_page"`
	NextCursor   string  `json:"next_cursor,omitempty"`
	TotalReturned int    `json:"total_returned"`
	Username     string  `json:"username"`
}

// XFollowingsResponse is the response from XFollowings.
type XFollowingsResponse struct {
	Followings   []XUser `json:"followings"`
	HasNextPage  bool    `json:"has_next_page"`
	NextCursor   string  `json:"next_cursor,omitempty"`
	TotalReturned int    `json:"total_returned"`
	Username     string  `json:"username"`
}

// XUserInfoResponse is the response from XUserInfo.
type XUserInfoResponse struct {
	Data     map[string]any `json:"data"`
	Username string         `json:"username"`
}

// XVerifiedFollowersResponse is the response from XVerifiedFollowers.
type XVerifiedFollowersResponse struct {
	Followers     []XUser `json:"followers"`
	HasNextPage   bool    `json:"has_next_page"`
	NextCursor    string  `json:"next_cursor,omitempty"`
	TotalReturned int     `json:"total_returned"`
}

// XTweetsResponse is the response from XUserTweets/XUserMentions.
type XTweetsResponse struct {
	Tweets        []XTweet `json:"tweets"`
	HasNextPage   bool     `json:"has_next_page"`
	NextCursor    string   `json:"next_cursor,omitempty"`
	TotalReturned int      `json:"total_returned"`
}

// XTweetLookupResponse is the response from XTweetLookup.
type XTweetLookupResponse struct {
	Tweets         []XTweet `json:"tweets"`
	NotFound       []string `json:"not_found,omitempty"`
	TotalRequested int      `json:"total_requested"`
	TotalFound     int      `json:"total_found"`
}

// XTweetRepliesResponse is the response from XTweetReplies.
type XTweetRepliesResponse struct {
	Replies       []XTweet `json:"replies"`
	HasNextPage   bool     `json:"has_next_page"`
	NextCursor    string   `json:"next_cursor,omitempty"`
	TotalReturned int      `json:"total_returned"`
}

// XTweetThreadResponse is the response from XTweetThread.
type XTweetThreadResponse struct {
	Tweets        []XTweet `json:"tweets"`
	HasNextPage   bool     `json:"has_next_page"`
	NextCursor    string   `json:"next_cursor,omitempty"`
	TotalReturned int      `json:"total_returned"`
}

// XSearchResponse is the response from XSearch.
type XSearchResponse struct {
	Tweets        []XTweet `json:"tweets"`
	HasNextPage   bool     `json:"has_next_page"`
	NextCursor    string   `json:"next_cursor,omitempty"`
	TotalReturned int      `json:"total_returned"`
}

// XTrendingResponse is the response from XTrending.
type XTrendingResponse struct {
	Data map[string]any `json:"data"`
}

// XArticlesRisingResponse is the response from XArticlesRising.
type XArticlesRisingResponse struct {
	Data map[string]any `json:"data"`
}

// XAuthorAnalyticsResponse is the response from XAuthorAnalytics.
type XAuthorAnalyticsResponse struct {
	Data   map[string]any `json:"data"`
	Handle string         `json:"handle"`
}

// XCompareAuthorsResponse is the response from XCompareAuthors.
type XCompareAuthorsResponse struct {
	Data map[string]any `json:"data"`
}
```

**Step 2: Create `x_twitter.go`**

```go
package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
)

// XUserLookup looks up X/Twitter user profiles by username.
// Pricing: $0.002 per user (min $0.02).
func (c *LLMClient) XUserLookup(ctx context.Context, usernames []string) (*XUserLookupResponse, error) {
	if len(usernames) == 0 {
		return nil, &ValidationError{Field: "usernames", Message: "At least one username is required"}
	}
	body := map[string]any{"usernames": usernames}
	data, err := c.doRequest(ctx, "/v1/x/users/lookup", body)
	if err != nil {
		return nil, err
	}
	var resp XUserLookupResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// XFollowers gets the followers of an X/Twitter user.
// Pricing: $0.05 per page (~200 accounts).
func (c *LLMClient) XFollowers(ctx context.Context, username string, cursor string) (*XFollowersResponse, error) {
	if username == "" {
		return nil, &ValidationError{Field: "username", Message: "Username is required"}
	}
	body := map[string]any{"username": username}
	if cursor != "" {
		body["cursor"] = cursor
	}
	data, err := c.doRequest(ctx, "/v1/x/users/followers", body)
	if err != nil {
		return nil, err
	}
	var resp XFollowersResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// XFollowings gets the accounts an X/Twitter user is following.
// Pricing: $0.05 per page (~200 accounts).
func (c *LLMClient) XFollowings(ctx context.Context, username string, cursor string) (*XFollowingsResponse, error) {
	if username == "" {
		return nil, &ValidationError{Field: "username", Message: "Username is required"}
	}
	body := map[string]any{"username": username}
	if cursor != "" {
		body["cursor"] = cursor
	}
	data, err := c.doRequest(ctx, "/v1/x/users/followings", body)
	if err != nil {
		return nil, err
	}
	var resp XFollowingsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// XUserInfo gets detailed profile information for an X/Twitter user.
// Pricing: $0.002 per request.
func (c *LLMClient) XUserInfo(ctx context.Context, username string) (*XUserInfoResponse, error) {
	if username == "" {
		return nil, &ValidationError{Field: "username", Message: "Username is required"}
	}
	body := map[string]any{"username": username}
	data, err := c.doRequest(ctx, "/v1/x/users/info", body)
	if err != nil {
		return nil, err
	}
	var resp XUserInfoResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// XVerifiedFollowers gets verified followers of an X/Twitter user.
// Pricing: $0.048 per page.
func (c *LLMClient) XVerifiedFollowers(ctx context.Context, userID string, cursor string) (*XVerifiedFollowersResponse, error) {
	if userID == "" {
		return nil, &ValidationError{Field: "userID", Message: "User ID is required"}
	}
	body := map[string]any{"userId": userID}
	if cursor != "" {
		body["cursor"] = cursor
	}
	data, err := c.doRequest(ctx, "/v1/x/users/verified-followers", body)
	if err != nil {
		return nil, err
	}
	var resp XVerifiedFollowersResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// XUserTweets gets tweets posted by an X/Twitter user.
// Pricing: $0.032 per page.
func (c *LLMClient) XUserTweets(ctx context.Context, username string, includeReplies bool, cursor string) (*XTweetsResponse, error) {
	if username == "" {
		return nil, &ValidationError{Field: "username", Message: "Username is required"}
	}
	body := map[string]any{"username": username, "includeReplies": includeReplies}
	if cursor != "" {
		body["cursor"] = cursor
	}
	data, err := c.doRequest(ctx, "/v1/x/users/tweets", body)
	if err != nil {
		return nil, err
	}
	var resp XTweetsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// XUserMentions gets mentions of an X/Twitter user.
// Pricing: $0.032 per page.
func (c *LLMClient) XUserMentions(ctx context.Context, username string, sinceTime, untilTime, cursor string) (*XTweetsResponse, error) {
	if username == "" {
		return nil, &ValidationError{Field: "username", Message: "Username is required"}
	}
	body := map[string]any{"username": username}
	if sinceTime != "" {
		body["sinceTime"] = sinceTime
	}
	if untilTime != "" {
		body["untilTime"] = untilTime
	}
	if cursor != "" {
		body["cursor"] = cursor
	}
	data, err := c.doRequest(ctx, "/v1/x/users/mentions", body)
	if err != nil {
		return nil, err
	}
	var resp XTweetsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// XTweetLookup looks up tweets by their IDs.
// Pricing: $0.16 per batch (max 200 IDs).
func (c *LLMClient) XTweetLookup(ctx context.Context, tweetIDs []string) (*XTweetLookupResponse, error) {
	if len(tweetIDs) == 0 {
		return nil, &ValidationError{Field: "tweetIDs", Message: "At least one tweet ID is required"}
	}
	body := map[string]any{"tweet_ids": tweetIDs}
	data, err := c.doRequest(ctx, "/v1/x/tweets/lookup", body)
	if err != nil {
		return nil, err
	}
	var resp XTweetLookupResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// XTweetReplies gets replies to a specific tweet.
// Pricing: $0.032 per page.
func (c *LLMClient) XTweetReplies(ctx context.Context, tweetID string, queryType string, cursor string) (*XTweetRepliesResponse, error) {
	if tweetID == "" {
		return nil, &ValidationError{Field: "tweetID", Message: "Tweet ID is required"}
	}
	body := map[string]any{"tweetId": tweetID}
	if queryType != "" {
		body["queryType"] = queryType
	}
	if cursor != "" {
		body["cursor"] = cursor
	}
	data, err := c.doRequest(ctx, "/v1/x/tweets/replies", body)
	if err != nil {
		return nil, err
	}
	var resp XTweetRepliesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// XTweetThread gets the full conversation thread for a tweet.
// Pricing: $0.032 per page.
func (c *LLMClient) XTweetThread(ctx context.Context, tweetID string, cursor string) (*XTweetThreadResponse, error) {
	if tweetID == "" {
		return nil, &ValidationError{Field: "tweetID", Message: "Tweet ID is required"}
	}
	body := map[string]any{"tweetId": tweetID}
	if cursor != "" {
		body["cursor"] = cursor
	}
	data, err := c.doRequest(ctx, "/v1/x/tweets/thread", body)
	if err != nil {
		return nil, err
	}
	var resp XTweetThreadResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// XSearch searches X/Twitter tweets.
// Pricing: $0.032 per page.
func (c *LLMClient) XSearch(ctx context.Context, query string, queryType string, cursor string) (*XSearchResponse, error) {
	if query == "" {
		return nil, &ValidationError{Field: "query", Message: "Search query is required"}
	}
	body := map[string]any{"query": query}
	if queryType != "" {
		body["queryType"] = queryType
	}
	if cursor != "" {
		body["cursor"] = cursor
	}
	data, err := c.doRequest(ctx, "/v1/x/search", body)
	if err != nil {
		return nil, err
	}
	var resp XSearchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// XTrending gets current trending topics on X/Twitter.
// Pricing: $0.002 per request.
func (c *LLMClient) XTrending(ctx context.Context) (*XTrendingResponse, error) {
	data, err := c.doRequest(ctx, "/v1/x/trending", map[string]any{})
	if err != nil {
		return nil, err
	}
	var resp XTrendingResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// XArticlesRising gets rising/viral articles on X/Twitter.
// Pricing: $0.05 per request.
func (c *LLMClient) XArticlesRising(ctx context.Context) (*XArticlesRisingResponse, error) {
	data, err := c.doRequest(ctx, "/v1/x/articles/rising", map[string]any{})
	if err != nil {
		return nil, err
	}
	var resp XArticlesRisingResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// XAuthorAnalytics gets analytics for an X/Twitter author.
// Pricing: $0.02 per request.
func (c *LLMClient) XAuthorAnalytics(ctx context.Context, handle string) (*XAuthorAnalyticsResponse, error) {
	if handle == "" {
		return nil, &ValidationError{Field: "handle", Message: "Handle is required"}
	}
	body := map[string]any{"handle": handle}
	data, err := c.doRequest(ctx, "/v1/x/authors", body)
	if err != nil {
		return nil, err
	}
	var resp XAuthorAnalyticsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// XCompareAuthors compares two X/Twitter authors.
// Pricing: $0.05 per request.
func (c *LLMClient) XCompareAuthors(ctx context.Context, handle1, handle2 string) (*XCompareAuthorsResponse, error) {
	if handle1 == "" || handle2 == "" {
		return nil, &ValidationError{Field: "handles", Message: "Both handles are required"}
	}
	body := map[string]any{"handle1": handle1, "handle2": handle2}
	data, err := c.doRequest(ctx, "/v1/x/compare", body)
	if err != nil {
		return nil, err
	}
	var resp XCompareAuthorsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}
```

**Step 3: Create `x_twitter_test.go` with mock server tests**

Test each endpoint with httptest mock server returning appropriate JSON.

**Step 4: Run tests**

Run: `go test ./... -v -run TestX`
Expected: All X/Twitter tests pass

**Step 5: Commit**

```bash
git add x_twitter.go x_twitter_types.go x_twitter_test.go
git commit -m "feat: add X/Twitter data endpoints (15 methods)"
```

---

## Task 4: Search Endpoints

**Files:**
- Create: `search.go`
- Create: `search_test.go`

**Step 1: Add types to `types.go` or inline**

```go
// SearchOptions contains options for standalone search.
type SearchOptions struct {
	Sources    []string `json:"sources,omitempty"`    // "web", "x", "news"
	MaxResults int      `json:"max_results,omitempty"`
	FromDate   string   `json:"from_date,omitempty"`  // YYYY-MM-DD
	ToDate     string   `json:"to_date,omitempty"`    // YYYY-MM-DD
}

// SearchResult is the response from a standalone search.
type SearchResult struct {
	Query       string   `json:"query"`
	Summary     string   `json:"summary"`
	Citations   []string `json:"citations,omitempty"`
	SourcesUsed int      `json:"sources_used"`
	Model       string   `json:"model,omitempty"`
}
```

**Step 2: Create `search.go`**

```go
package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
)

// Search performs a standalone web/X/news search.
// Pricing: ~$0.025 per source used.
func (c *LLMClient) Search(ctx context.Context, query string, opts *SearchOptions) (*SearchResult, error) {
	if query == "" {
		return nil, &ValidationError{Field: "query", Message: "Search query is required"}
	}
	body := map[string]any{"query": query}
	if opts != nil {
		if len(opts.Sources) > 0 {
			body["sources"] = opts.Sources
		}
		if opts.MaxResults > 0 {
			body["max_results"] = opts.MaxResults
		}
		if opts.FromDate != "" {
			body["from_date"] = opts.FromDate
		}
		if opts.ToDate != "" {
			body["to_date"] = opts.ToDate
		}
	}
	data, err := c.doRequest(ctx, "/v1/search", body)
	if err != nil {
		return nil, err
	}
	var resp SearchResult
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}
```

**Step 3: Test and commit**

```bash
git add search.go search_test.go
git commit -m "feat: add standalone Search endpoint"
```

---

## Task 5: Prediction Market Endpoints

**Files:**
- Create: `prediction_market.go`
- Create: `prediction_market_test.go`

**Step 1: Create `prediction_market.go`**

```go
package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// PM makes a GET request to prediction market endpoints.
// Path examples: "polymarket/events", "kalshi/markets/KXBTC-25MAR14"
// Pricing: $0.001 per request.
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
	data, err := c.doGet(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return resp, nil
}

// PMQuery makes a POST request to prediction market query endpoints.
// Pricing: $0.005 per request.
func (c *LLMClient) PMQuery(ctx context.Context, path string, query any) (map[string]any, error) {
	if path == "" {
		return nil, &ValidationError{Field: "path", Message: "Path is required"}
	}
	body := map[string]any{"query": query}
	data, err := c.doRequest(ctx, "/v1/pm/"+path, body)
	if err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return resp, nil
}
```

**Step 2: Test and commit**

```bash
git add prediction_market.go prediction_market_test.go
git commit -m "feat: add Prediction Market endpoints (Polymarket, Kalshi)"
```

---

## Task 6: Tool/Function Calling Support

Add OpenAI-compatible function calling to ChatCompletion.

**Files:**
- Modify: `types.go` — add Tool, ToolChoice, ToolCall types
- Modify: `client.go` — handle tools in ChatCompletion

**Step 1: Add types**

```go
// Tool represents a function tool for chat completion.
type Tool struct {
	Type     string       `json:"type"` // "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a callable function.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"` // JSON Schema
}

// ToolCall represents a tool call in an assistant message.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction contains the function name and arguments.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}
```

Update `ChatCompletionOptions`:
```go
type ChatCompletionOptions struct {
	MaxTokens        int               `json:"max_tokens,omitempty"`
	Temperature      float64           `json:"temperature,omitempty"`
	TopP             float64           `json:"top_p,omitempty"`
	Search           bool              `json:"-"`
	SearchParameters *SearchParameters `json:"search_parameters,omitempty"`
	Tools            []Tool            `json:"tools,omitempty"`
	ToolChoice       any               `json:"tool_choice,omitempty"` // string or {"type":"function","function":{"name":"..."}}
}
```

Update `ChatMessage`:
```go
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}
```

Update `Choice`:
```go
type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}
```

**Step 2: Update ChatCompletion to pass tools/tool_choice in body**

In `client.go` ChatCompletion, add:
```go
if opts.Tools != nil {
	body["tools"] = opts.Tools
}
if opts.ToolChoice != nil {
	body["tool_choice"] = opts.ToolChoice
}
```

**Step 3: Test and commit**

```bash
git add types.go client.go client_test.go
git commit -m "feat: add tool/function calling support in ChatCompletion"
```

---

## Task 7: Response Cache with TTL

**Files:**
- Create: `cache.go`
- Create: `cache_test.go`

**Step 1: Create `cache.go`**

```go
package blockrun

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Default TTL values by endpoint pattern.
var defaultTTL = map[string]time.Duration{
	"/v1/x/":      1 * time.Hour,
	"/v1/pm/":     30 * time.Minute,
	"/v1/search":  15 * time.Minute,
	"/v1/chat/":   0,
	"/v1/images/": 0,
}

// CacheEntry represents a cached API response.
type CacheEntry struct {
	CachedAt float64 `json:"cached_at"`
	Endpoint string  `json:"endpoint"`
	Response json.RawMessage `json:"response"`
}

// Cache provides local response caching with TTL.
type Cache struct {
	mu  sync.RWMutex
	dir string
}

// NewCache creates a new cache in ~/.blockrun/cache/.
func NewCache() *Cache {
	dir := filepath.Join(os.Getenv("HOME"), ".blockrun", "cache")
	os.MkdirAll(dir, 0755)
	return &Cache{dir: dir}
}

// cacheKey generates a SHA256 key from endpoint + body.
func cacheKey(endpoint string, body map[string]any) string {
	data, _ := json.Marshal(map[string]any{"endpoint": endpoint, "body": body})
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)[:16]
}

// ttlFor returns the TTL for the given endpoint.
func ttlFor(endpoint string) time.Duration {
	for prefix, ttl := range defaultTTL {
		if strings.HasPrefix(endpoint, prefix) {
			return ttl
		}
	}
	return 0
}

// Get returns a cached response if it exists and is not expired.
func (c *Cache) Get(endpoint string, body map[string]any) (json.RawMessage, bool) {
	ttl := ttlFor(endpoint)
	if ttl == 0 {
		return nil, false
	}

	key := cacheKey(endpoint, body)
	path := filepath.Join(c.dir, key+".json")

	c.mu.RLock()
	defer c.mu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}

	age := time.Since(time.Unix(int64(entry.CachedAt), 0))
	if age > ttl {
		return nil, false
	}

	return entry.Response, true
}

// Set stores a response in the cache.
func (c *Cache) Set(endpoint string, body map[string]any, response json.RawMessage) {
	ttl := ttlFor(endpoint)
	if ttl == 0 {
		return
	}

	key := cacheKey(endpoint, body)
	path := filepath.Join(c.dir, key+".json")

	entry := CacheEntry{
		CachedAt: float64(time.Now().Unix()),
		Endpoint: endpoint,
		Response: response,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	os.WriteFile(path, data, 0644)
}

// Clear removes all cached responses.
func (c *Cache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		os.Remove(filepath.Join(c.dir, e.Name()))
	}
	return nil
}
```

**Step 2: Integrate cache into `baseClient`**

Add `cache *Cache` field to `baseClient`. In `doRequest`, check cache before making the request. After successful response, store in cache. Add `WithCache(enabled bool)` option.

**Step 3: Test and commit**

```bash
git add cache.go cache_test.go base_client.go
git commit -m "feat: add response caching with per-endpoint TTL"
```

---

## Task 8: Cost Logging

**Files:**
- Create: `cost_log.go`
- Create: `cost_log_test.go`

**Step 1: Create `cost_log.go`**

```go
package blockrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CostLogEntry represents a single cost log entry.
type CostLogEntry struct {
	Timestamp float64 `json:"ts"`
	Endpoint  string  `json:"endpoint"`
	CostUSD   float64 `json:"cost_usd"`
}

// CostSummary represents a summary of all logged costs.
type CostSummary struct {
	TotalUSD   float64            `json:"total_usd"`
	Calls      int                `json:"calls"`
	ByEndpoint map[string]float64 `json:"by_endpoint"`
}

// CostLog provides persistent cost logging to JSONL.
type CostLog struct {
	mu   sync.Mutex
	path string
}

// NewCostLog creates a new cost log at ~/.blockrun/cost_log.jsonl.
func NewCostLog() *CostLog {
	dir := filepath.Join(os.Getenv("HOME"), ".blockrun")
	os.MkdirAll(dir, 0755)
	return &CostLog{path: filepath.Join(dir, "cost_log.jsonl")}
}

// Append logs a cost entry.
func (cl *CostLog) Append(endpoint string, costUSD float64) {
	entry := CostLogEntry{
		Timestamp: float64(time.Now().UnixMilli()) / 1000.0,
		Endpoint:  endpoint,
		CostUSD:   costUSD,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	data = append(data, '\n')

	cl.mu.Lock()
	defer cl.mu.Unlock()

	f, err := os.OpenFile(cl.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(data)
}

// Summary reads the log and returns a cost summary.
func (cl *CostLog) Summary() (*CostSummary, error) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	data, err := os.ReadFile(cl.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &CostSummary{ByEndpoint: map[string]float64{}}, nil
		}
		return nil, err
	}

	summary := &CostSummary{ByEndpoint: map[string]float64{}}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	for dec.More() {
		var entry CostLogEntry
		if err := dec.Decode(&entry); err != nil {
			continue
		}
		summary.TotalUSD += entry.CostUSD
		summary.Calls++
		summary.ByEndpoint[entry.Endpoint] += entry.CostUSD
	}
	return summary, nil
}
```

(Note: add `"strings"` to imports in the actual implementation.)

**Step 2: Integrate into `baseClient` — log after each paid request**

**Step 3: Test and commit**

```bash
git add cost_log.go cost_log_test.go base_client.go
git commit -m "feat: add persistent cost logging to JSONL"
```

---

## Task 9: Balance Checking

**Files:**
- Create: `balance.go`
- Create: `balance_test.go`

**Step 1: Create `balance.go`**

```go
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

// Base chain RPC endpoints for balance checking.
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

	// USDCBaseTestnet is the USDC contract on Base Sepolia.
	USDCBaseTestnet = "0x036CbD53842c5426634e7929541eC2318f3dCF7e"
)

// GetBalance returns the USDC balance for the client's wallet on Base.
func (c *LLMClient) GetBalance(ctx context.Context) (float64, error) {
	return getUSDCBalance(ctx, c.address, false)
}

// GetBalanceTestnet returns the USDC balance on Base Sepolia testnet.
func (c *LLMClient) GetBalanceTestnet(ctx context.Context) (float64, error) {
	return getUSDCBalance(ctx, c.address, true)
}

// getUSDCBalance queries the USDC balanceOf for an address.
func getUSDCBalance(ctx context.Context, address string, testnet bool) (float64, error) {
	rpcs := baseMainnetRPCs
	usdcContract := USDCBaseContract
	if testnet {
		rpcs = baseSepoliaRPCs
		usdcContract = USDCBaseTestnet
	}

	// balanceOf(address) selector = 0x70a08231
	addr := strings.TrimPrefix(strings.ToLower(address), "0x")
	callData := "0x70a08231" + fmt.Sprintf("%064s", addr)

	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "eth_call",
		"params": []any{
			map[string]string{
				"to":   usdcContract,
				"data": callData,
			},
			"latest",
		},
		"id": 1,
	}

	body, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 10 * time.Second}

	var lastErr error
	for _, rpc := range rpcs {
		req, err := http.NewRequestWithContext(ctx, "POST", rpc, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}

		var result struct {
			Result string `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			lastErr = err
			continue
		}

		if result.Error != nil {
			lastErr = fmt.Errorf("RPC error: %s", result.Error.Message)
			continue
		}

		// Parse hex result to big.Int then convert to float64 (6 decimals)
		hexStr := strings.TrimPrefix(result.Result, "0x")
		if hexStr == "" || hexStr == "0" {
			return 0, nil
		}
		balance, ok := new(big.Int).SetString(hexStr, 16)
		if !ok {
			lastErr = fmt.Errorf("failed to parse balance: %s", result.Result)
			continue
		}

		// USDC has 6 decimals
		balanceFloat := new(big.Float).SetInt(balance)
		divisor := new(big.Float).SetInt64(1_000_000)
		result64, _ := new(big.Float).Quo(balanceFloat, divisor).Float64()
		return result64, nil
	}

	return 0, fmt.Errorf("all RPC endpoints failed: %w", lastErr)
}
```

**Step 2: Test and commit**

```bash
git add balance.go balance_test.go
git commit -m "feat: add USDC balance checking via Base chain RPC"
```

---

## Task 10: Smart Routing (ClawRouter)

**Files:**
- Create: `router.go`
- Create: `router_test.go`

**Step 1: Create `router.go`**

Implement the 14-dimension scoring algorithm locally (no API call — all <1ms local computation). Routing profiles: free, eco, auto, premium. Returns SmartChatResponse with routing decision.

```go
package blockrun

import (
	"context"
	"math"
	"regexp"
	"strings"
)

// RoutingProfile determines model selection strategy.
type RoutingProfile string

const (
	RoutingFree    RoutingProfile = "free"
	RoutingEco     RoutingProfile = "eco"
	RoutingAuto    RoutingProfile = "auto"
	RoutingPremium RoutingProfile = "premium"
)

// RoutingTier represents the complexity tier.
type RoutingTier string

const (
	TierSimple    RoutingTier = "SIMPLE"
	TierMedium    RoutingTier = "MEDIUM"
	TierComplex   RoutingTier = "COMPLEX"
	TierReasoning RoutingTier = "REASONING"
)

// RoutingDecision contains the model selection decision.
type RoutingDecision struct {
	Model        string  `json:"model"`
	Tier         string  `json:"tier"`
	Confidence   float64 `json:"confidence"`
	Method       string  `json:"method"`
	Reasoning    string  `json:"reasoning"`
	CostEstimate float64 `json:"cost_estimate"`
	BaselineCost float64 `json:"baseline_cost"`
	Savings      float64 `json:"savings"`
}

// SmartChatResponse contains the chat response plus routing info.
type SmartChatResponse struct {
	Response string          `json:"response"`
	Model    string          `json:"model"`
	Routing  RoutingDecision `json:"routing"`
}

// SmartChatOptions contains options for smart chat.
type SmartChatOptions struct {
	System         string
	MaxTokens      int
	Temperature    float64
	RoutingProfile RoutingProfile
}

// Model routing tables by profile and tier.
var routingTable = map[RoutingProfile]map[RoutingTier]string{
	RoutingFree: {
		TierSimple:    "nvidia/gpt-oss-120b",
		TierMedium:    "nvidia/gpt-oss-120b",
		TierComplex:   "nvidia/gpt-oss-120b",
		TierReasoning: "nvidia/gpt-oss-120b",
	},
	RoutingEco: {
		TierSimple:    "nvidia/kimi-k2.5",
		TierMedium:    "deepseek/deepseek-chat",
		TierComplex:   "google/gemini-2.5-pro",
		TierReasoning: "deepseek/deepseek-reasoner",
	},
	RoutingAuto: {
		TierSimple:    "nvidia/kimi-k2.5",
		TierMedium:    "google/gemini-2.5-flash",
		TierComplex:   "google/gemini-3.1-pro",
		TierReasoning: "deepseek/deepseek-reasoner",
	},
	RoutingPremium: {
		TierSimple:    "google/gemini-2.5-flash",
		TierMedium:    "openai/gpt-5.4",
		TierComplex:   "anthropic/claude-opus-4.5",
		TierReasoning: "openai/o3",
	},
}

// Scoring patterns
var (
	codePatterns      = regexp.MustCompile(`(?i)(code|function|class|def |import |package |func |struct |interface |```|algorithm|implement|debug|refactor|syntax)`)
	reasoningPatterns = regexp.MustCompile(`(?i)(prove|theorem|derive|logical|deduc|infer|step.by.step|think.*through|reason|analyz|evaluat|compar.*contrast)`)
	technicalPatterns = regexp.MustCompile(`(?i)(API|database|SQL|HTTP|TCP|DNS|cache|server|deploy|docker|kubernetes|cloud|AWS|GCP|architecture)`)
	creativePatterns  = regexp.MustCompile(`(?i)(story|poem|creative|imagin|write.*about|fiction|narrative|metaphor)`)
	simplePatterns    = regexp.MustCompile(`(?i)(^(what|who|when|where|how|why|is|are|was|were|do|does|did|can|could|will|would|should)\s.{0,50}$|define|meaning of|translate|convert|calculate \d)`)
	multiStepPatterns = regexp.MustCompile(`(?i)(first.*then|step \d|multiple|several|list.*all|compare.*and|pros.*cons|advantages.*disadvantages)`)
	agenticPatterns   = regexp.MustCompile(`(?i)(agent|tool|browse|search.*web|look.*up|find.*information|execute|automate|workflow)`)
)

// scorePrompt scores a prompt on 14 dimensions, returns a complexity score.
func scorePrompt(prompt string) (float64, RoutingTier) {
	tokenEstimate := float64(len(strings.Fields(prompt)))

	scores := map[string]float64{
		"tokens":     math.Min(tokenEstimate/500.0, 1.0) * 0.08,
		"code":       countMatches(codePatterns, prompt) * 0.15,
		"reasoning":  countMatches(reasoningPatterns, prompt) * 0.18,
		"technical":  countMatches(technicalPatterns, prompt) * 0.10,
		"creative":   countMatches(creativePatterns, prompt) * 0.05,
		"simple":     -countMatches(simplePatterns, prompt) * 0.02,
		"multiStep":  countMatches(multiStepPatterns, prompt) * 0.12,
		"agenticTask": countMatches(agenticPatterns, prompt) * 0.04,
	}

	total := 0.0
	for _, v := range scores {
		total += v
	}

	var tier RoutingTier
	switch {
	case total < 0.0:
		tier = TierSimple
	case total < 0.3:
		tier = TierMedium
	case total < 0.5:
		tier = TierComplex
	default:
		tier = TierReasoning
	}

	return total, tier
}

func countMatches(re *regexp.Regexp, s string) float64 {
	matches := re.FindAllString(s, -1)
	if len(matches) > 0 {
		return math.Min(float64(len(matches))/3.0, 1.0)
	}
	return 0
}

// SmartChat automatically routes to the best model based on prompt analysis.
func (c *LLMClient) SmartChat(ctx context.Context, prompt string, opts *SmartChatOptions) (*SmartChatResponse, error) {
	if prompt == "" {
		return nil, &ValidationError{Field: "prompt", Message: "Prompt is required"}
	}

	profile := RoutingAuto
	if opts != nil && opts.RoutingProfile != "" {
		profile = opts.RoutingProfile
	}

	score, tier := scorePrompt(prompt)

	table, ok := routingTable[profile]
	if !ok {
		table = routingTable[RoutingAuto]
	}
	model := table[tier]

	confidence := 0.7 + math.Min(math.Abs(score)*0.3, 0.25)

	// Build chat options
	var chatOpts *ChatCompletionOptions
	if opts != nil && (opts.MaxTokens > 0 || opts.Temperature > 0) {
		chatOpts = &ChatCompletionOptions{
			MaxTokens:   opts.MaxTokens,
			Temperature: opts.Temperature,
		}
	}

	// Make the chat call
	var response string
	var err error
	if opts != nil && opts.System != "" {
		response, err = c.ChatWithSystem(ctx, model, prompt, opts.System)
	} else {
		response, err = c.Chat(ctx, model, prompt)
	}
	if err != nil {
		return nil, err
	}
	_ = chatOpts // used when we extend to pass full options

	return &SmartChatResponse{
		Response: response,
		Model:    model,
		Routing: RoutingDecision{
			Model:      model,
			Tier:       string(tier),
			Confidence: confidence,
			Method:     "rules",
			Reasoning:  fmt.Sprintf("Scored %.3f -> %s tier, profile=%s", score, tier, profile),
		},
	}, nil
}
```

(Note: add `"fmt"` to imports.)

**Step 2: Test routing logic with unit tests**

Test that different prompts route to different tiers: simple questions -> SIMPLE, code prompts -> MEDIUM/COMPLEX, reasoning -> REASONING.

**Step 3: Commit**

```bash
git add router.go router_test.go
git commit -m "feat: add SmartChat with ClawRouter (14-dimension local scoring)"
```

---

## Task 11: Streaming Support (SSE)

**Files:**
- Create: `stream.go`
- Create: `stream_test.go`

**Step 1: Create `stream.go`**

```go
package blockrun

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ChatCompletionChunk represents a single SSE chunk during streaming.
type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
}

// ChunkChoice is a choice within a streaming chunk.
type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason string     `json:"finish_reason,omitempty"`
}

// ChunkDelta contains the incremental content in a stream chunk.
type ChunkDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// Stream represents an active SSE stream that yields chunks.
type Stream struct {
	scanner *bufio.Scanner
	body    io.ReadCloser
	done    bool
}

// Next returns the next chunk from the stream.
// Returns nil, nil when the stream is complete.
func (s *Stream) Next() (*ChatCompletionChunk, error) {
	if s.done {
		return nil, nil
	}

	for s.scanner.Scan() {
		line := s.scanner.Text()

		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			s.done = true
			return nil, nil
		}

		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, fmt.Errorf("failed to decode chunk: %w", err)
		}

		return &chunk, nil
	}

	if err := s.scanner.Err(); err != nil {
		return nil, err
	}

	s.done = true
	return nil, nil
}

// Close closes the underlying stream.
func (s *Stream) Close() error {
	s.done = true
	return s.body.Close()
}

// ChatCompletionStream starts a streaming chat completion.
func (c *LLMClient) ChatCompletionStream(ctx context.Context, model string, messages []ChatMessage, opts *ChatCompletionOptions) (*Stream, error) {
	if model == "" {
		return nil, &ValidationError{Field: "model", Message: "Model is required"}
	}
	if len(messages) == 0 {
		return nil, &ValidationError{Field: "messages", Message: "At least one message is required"}
	}

	body := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   true,
	}

	maxTokens := DefaultMaxTokens
	if opts != nil {
		if opts.MaxTokens > 0 {
			maxTokens = opts.MaxTokens
		}
		if opts.Temperature > 0 {
			body["temperature"] = opts.Temperature
		}
		if opts.TopP > 0 {
			body["top_p"] = opts.TopP
		}
		if opts.Tools != nil {
			body["tools"] = opts.Tools
		}
		if opts.ToolChoice != nil {
			body["tool_choice"] = opts.ToolChoice
		}
	}
	body["max_tokens"] = maxTokens

	// For streaming, we need to handle the 402 flow first, then stream the response
	// Use a special path that returns the raw http.Response
	url := c.apiURL + "/v1/chat/completions"

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request body: %w", err)
	}

	// First attempt
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// Handle 402 — sign and retry
	if resp.StatusCode == http.StatusPaymentRequired {
		resp.Body.Close()
		paymentSig, err := c.signPaymentFromResponse(resp)
		if err != nil {
			return nil, err
		}

		retryReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create retry request: %w", err)
		}
		retryReq.Header.Set("Content-Type", "application/json")
		retryReq.Header.Set("Accept", "text/event-stream")
		retryReq.Header.Set("PAYMENT-SIGNATURE", paymentSig)

		resp, err = c.httpClient.Do(retryReq)
		if err != nil {
			return nil, fmt.Errorf("retry request failed: %w", err)
		}
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("API error: %s", string(bodyBytes)),
		}
	}

	return &Stream{
		scanner: bufio.NewScanner(resp.Body),
		body:    resp.Body,
	}, nil
}
```

(Note: imports `"bytes"`, `"io"`, `"net/http"` needed. Also needs a `signPaymentFromResponse` helper method on baseClient.)

**Step 2: Test and commit**

```bash
git add stream.go stream_test.go
git commit -m "feat: add SSE streaming for chat completions"
```

---

## Task 12: Agent Wallet Setup

**Files:**
- Create: `setup.go`
- Create: `setup_test.go`

**Step 1: Create `setup.go`**

```go
package blockrun

import (
	"context"
	"fmt"
)

// SetupAgentWallet creates or loads a wallet and returns a configured LLMClient.
// If the wallet is new, prints funding instructions.
func SetupAgentWallet(opts ...ClientOption) (*LLMClient, error) {
	wallet, err := GetOrCreateWallet()
	if err != nil {
		return nil, fmt.Errorf("failed to setup wallet: %w", err)
	}

	client, err := NewLLMClient(wallet.PrivateKey, opts...)
	if err != nil {
		return nil, err
	}

	if wallet.IsNew {
		fmt.Print(FormatWalletCreatedMessage(wallet.Address))
	}

	return client, nil
}

// Status returns wallet address and USDC balance.
func (c *LLMClient) Status(ctx context.Context) (address string, balance float64, err error) {
	address = c.GetWalletAddress()
	balance, err = c.GetBalance(ctx)
	return
}
```

**Step 2: Test and commit**

```bash
git add setup.go setup_test.go
git commit -m "feat: add SetupAgentWallet and Status utilities"
```

---

## Task 13: Wallet Scanning (multi-provider)

**Files:**
- Modify: `wallet.go` — add ScanWallets

**Step 1: Add ScanWallets**

```go
// ScanWallets looks for wallet files from various providers.
// Searches ~/.<provider>/wallet.json and similar patterns.
func ScanWallets() []WalletInfo {
	home := os.Getenv("HOME")
	providers := []struct {
		dir  string
		file string
	}{
		{".blockrun", ".session"},
		{".blockrun", "wallet.key"},
		{".agentcash", "wallet.json"},
		{".replit", "wallet.json"},
	}

	var wallets []WalletInfo
	for _, p := range providers {
		path := filepath.Join(home, p.dir, p.file)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		key := strings.TrimSpace(string(data))
		if key == "" {
			continue
		}
		// Try to parse as JSON (some providers store {"privateKey": "..."})
		var jsonWallet struct {
			PrivateKey string `json:"privateKey"`
		}
		if err := json.Unmarshal(data, &jsonWallet); err == nil && jsonWallet.PrivateKey != "" {
			key = jsonWallet.PrivateKey
		}

		addr, err := GetAddressFromKey(key)
		if err != nil {
			continue
		}
		wallets = append(wallets, WalletInfo{
			PrivateKey: key,
			Address:    addr,
		})
	}
	return wallets
}
```

**Step 2: Test and commit**

```bash
git add wallet.go wallet_test.go
git commit -m "feat: add ScanWallets for multi-provider wallet discovery"
```

---

## Task 14: Update Examples and README

**Files:**
- Modify: `examples/basic/main.go` — add context.Context, show new features
- Modify: `README.md` — document all new features

**Step 1: Update example to showcase new features**

Add examples for:
- context.Context usage
- X/Twitter data
- Search
- Smart routing
- Streaming
- Balance checking
- Tool calling

**Step 2: Update README**

Add sections for all new features with code examples.

**Step 3: Commit**

```bash
git add examples/ README.md
git commit -m "docs: update examples and README for v2.0 features"
```

---

## Task 15: Final — Run all tests, verify compilation

**Step 1: Run full test suite**

Run: `cd /Users/vickyfu/Documents/blockrun-web/blockrun-llm-go && go test ./... -v`
Expected: All tests pass, no compilation errors

**Step 2: Run go vet and check for issues**

Run: `go vet ./...`
Expected: No issues

**Step 3: Final commit if any fixes needed**

```bash
git add -A
git commit -m "chore: fix any remaining issues from v2.0 update"
```

---

## Summary of New Files

| File | Purpose | Lines (est.) |
|------|---------|--------------|
| `base_client.go` | Shared payment/retry logic | ~200 |
| `x_twitter.go` | 15 X/Twitter endpoints | ~300 |
| `x_twitter_types.go` | X/Twitter response types | ~120 |
| `search.go` | Standalone search | ~50 |
| `prediction_market.go` | PM + PMQuery | ~70 |
| `router.go` | SmartChat with ClawRouter | ~200 |
| `stream.go` | SSE streaming | ~150 |
| `cache.go` | Response caching with TTL | ~130 |
| `cost_log.go` | JSONL cost logging | ~100 |
| `balance.go` | USDC balance checking | ~100 |
| `setup.go` | Agent wallet setup | ~30 |
| Tests (6 files) | Unit tests for all above | ~600 |

**Total new code: ~2,050 lines**
**Modified files: client.go, image.go, types.go, wallet.go, examples/, README.md**
