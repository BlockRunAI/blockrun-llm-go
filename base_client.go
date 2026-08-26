package blockrun

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

// baseClient contains the shared fields and methods for all BlockRun clients.
// It handles HTTP requests, x402 payment negotiation, and spending tracking.
//
// The default chain is Base (EIP-712 signing with privateKey). When chain is
// "solana" the client pays USDC on Solana instead: privateKey is nil, solanaKey
// holds the bs58 signing key, and address is the bs58 public key.
type baseClient struct {
	privateKey      *ecdsa.PrivateKey
	address         string
	apiURL          string
	httpClient      *http.Client
	cache           *Cache
	mu              sync.Mutex
	sessionTotalUSD float64
	sessionCalls    int
	costLog         *CostLog

	// chain is "base" (default) or "solana".
	chain string
	// solanaKey is the bs58 Solana signing key (only set when chain == "solana").
	solanaKey string
	// solanaRPCURL fetches blockhash + mint info while signing (chain == "solana").
	solanaRPCURL string
}

// chainSolana and chainBase identify the two payment chains this SDK signs for.
const (
	chainSolana = "solana"
	chainBase   = "base"
)

// isSolana reports whether this client pays on Solana.
func (bc *baseClient) isSolana() bool { return bc.chain == chainSolana }

// resolvedChain names the chain this client signs on. chain is left empty by
// newBaseClient, so Base is the default.
func (bc *baseClient) resolvedChain() string {
	if bc.chain == "" {
		return chainBase
	}
	return bc.chain
}

// extractPaymentDetails picks the option from a 402 that this client can
// actually sign. Selecting purely on the server's ordering hands a Solana
// client a Base option whenever a gateway lists Base first, and vice versa.
func (bc *baseClient) extractPaymentDetails(req *PaymentRequirement) (*PaymentOption, error) {
	return ExtractPaymentDetailsForChain(req, bc.resolvedChain())
}

// hasWallet reports whether a signing key is configured for the resolved chain.
// Base signs with privateKey (secp256k1); Solana signs with solanaKey (ed25519)
// and leaves privateKey nil, so a privateKey-only check silently locks Solana
// clients out of every paid endpoint.
func (bc *baseClient) hasWallet() bool {
	if bc.isSolana() {
		return bc.solanaKey != ""
	}
	return bc.privateKey != nil
}

// newBaseClient creates a new baseClient with the given private key, API URL, and timeout.
//
// If privateKey is empty, it checks BLOCKRUN_WALLET_KEY then BASE_CHAIN_WALLET_KEY env vars.
// If apiURL is empty, DefaultAPIURL is used; BLOCKRUN_API_URL env var can override.
func newBaseClient(privateKey, apiURL string, timeout time.Duration) (*baseClient, error) {
	// Get private key from param or environment
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
			Message: "Private key required. Pass privateKey parameter or set BLOCKRUN_WALLET_KEY environment variable. NOTE: Your key never leaves your machine - only signatures are sent.",
		}
	}

	// Parse private key
	key = strings.TrimPrefix(key, "0x")
	ecdsaKey, err := crypto.HexToECDSA(key)
	if err != nil {
		return nil, &ValidationError{
			Field:   "privateKey",
			Message: fmt.Sprintf("Invalid private key format: %v", err),
		}
	}

	// Get wallet address
	address := crypto.PubkeyToAddress(ecdsaKey.PublicKey).Hex()

	// Determine API URL
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}

	bc := &baseClient{
		privateKey: ecdsaKey,
		address:    address,
		apiURL:     apiURL,
		httpClient: &http.Client{Timeout: timeout},
		costLog:    NewCostLog(),
	}

	return bc, nil
}

// newSolanaBaseClient creates a baseClient that pays USDC on Solana.
//
// If solanaKey is empty it is loaded via LoadSolanaWallet (SOLANA_WALLET_KEY →
// ~/.*/solana-wallet.json → ~/.blockrun/.solana-session). If apiURL is empty,
// DefaultSolanaAPIURL is used; BLOCKRUN_SOLANA_API_URL can override. If rpcURL is
// empty, DefaultSolanaRPCURL is used; SOLANA_RPC_URL can override.
func newSolanaBaseClient(solanaKey, apiURL, rpcURL string, timeout time.Duration) (*baseClient, error) {
	key := solanaKey
	if key == "" {
		loaded, err := LoadSolanaWallet()
		if err != nil {
			return nil, &ValidationError{Field: "privateKey", Message: fmt.Sprintf("Failed to load Solana wallet: %v", err)}
		}
		key = loaded
	}
	if key == "" {
		return nil, &ValidationError{
			Field:   "privateKey",
			Message: "Solana private key required. Pass privateKey, set SOLANA_WALLET_KEY, or create ~/.blockrun/.solana-session. NOTE: Your key never leaves your machine - only signatures are sent.",
		}
	}

	address, err := GetSolanaPublicKey(key)
	if err != nil {
		return nil, &ValidationError{Field: "privateKey", Message: fmt.Sprintf("Invalid Solana private key: %v", err)}
	}

	if apiURL == "" {
		apiURL = DefaultSolanaAPIURL
	}
	if rpcURL == "" {
		rpcURL = strings.TrimSpace(os.Getenv("SOLANA_RPC_URL"))
	}
	if rpcURL == "" {
		rpcURL = DefaultSolanaRPCURL
	}

	return &baseClient{
		address:      address,
		apiURL:       strings.TrimSuffix(apiURL, "/"),
		httpClient:   &http.Client{Timeout: timeout},
		costLog:      NewCostLog(),
		chain:        chainSolana,
		solanaKey:    key,
		solanaRPCURL: rpcURL,
	}, nil
}

// checkEnvAPIURL overrides apiURL with the chain's API-URL env var if still at
// the chain default. Called after options are applied so user-set URLs win.
func (bc *baseClient) checkEnvAPIURL() {
	if bc.isSolana() {
		if envURL := os.Getenv("BLOCKRUN_SOLANA_API_URL"); envURL != "" && bc.apiURL == DefaultSolanaAPIURL {
			bc.apiURL = strings.TrimSuffix(envURL, "/")
		}
		return
	}
	if envURL := os.Getenv("BLOCKRUN_API_URL"); envURL != "" && bc.apiURL == DefaultAPIURL {
		bc.apiURL = strings.TrimSuffix(envURL, "/")
	}
}

// pollPaymentPayload returns the PAYMENT-SIGNATURE to attach to an async poll.
//
// Base reuses the submit-time signature: its EIP-712 authorization window
// (validBefore = now + maxTimeout, floored at the client's poll budget) covers
// the whole poll loop, and the gateway binds by wallet, not signature equality.
//
// Solana MUST re-sign periodically: a Solana transaction is pinned to a recent
// blockhash that expires within ~1-2 minutes, so the submit-time transaction
// would fail simulation by the time a slow job completes. It re-signs (fresh
// blockhash) once the current signature is older than solanaPollResignInterval,
// well inside the blockhash lifetime; the gateway settles the transaction
// attached to the poll that first observes completion, so that settling
// transaction is always fresh. On a re-sign error the current payload is kept so
// the poll still surfaces a meaningful gateway error rather than a local one.
//
// Returns the payload to use and the (possibly updated) last-signed time.
func (bc *baseClient) pollPaymentPayload(current string, lastSigned time.Time, option *PaymentOption, resourceURL, description string, extensions map[string]any) (string, time.Time) {
	if !bc.isSolana() || time.Since(lastSigned) < solanaPollResignInterval {
		return current, lastSigned
	}
	// allowServerBlockhash=false: the 402's extra.recentBlockhash is pinned to
	// submit time, so reusing it here would re-sign with the same expiring hash
	// and defeat the re-sign. Force a fresh one.
	fresh, err := bc.signPayment(option, resourceURL, description, extensions, false)
	if err != nil {
		return current, lastSigned
	}
	return fresh, time.Now()
}

// createPaymentPayload signs an x402 payment for the resolved chain. Base uses
// EIP-712 (secp256k1); Solana uses the SVM exact scheme (ed25519). This is the
// single signing entry point shared by every payment retry path.
func (bc *baseClient) createPaymentPayload(option *PaymentOption, resourceURL, description string, extensions map[string]any) (string, error) {
	return bc.signPayment(option, resourceURL, description, extensions, true)
}

// signPayment is createPaymentPayload with control over the Solana
// server-provided-blockhash fast path (see createSolanaPaymentPayload).
// allowServerBlockhash is ignored on Base.
func (bc *baseClient) signPayment(option *PaymentOption, resourceURL, description string, extensions map[string]any, allowServerBlockhash bool) (string, error) {
	// Every payment path funnels through here, so this is the one place the
	// no-wallet invariant cannot be bypassed. Without it a keyless Base client
	// reaches CreatePaymentPayload and nil-derefs on privateKey.PublicKey,
	// panicking the caller's goroutine instead of returning an error. The
	// exported constructors always set a key, so this is defence in depth —
	// but the guard belongs at the choke point, not at one of six call sites.
	if !bc.hasWallet() {
		return "", &PaymentError{Message: "endpoint returned 402 but no wallet is configured"}
	}
	if bc.isSolana() {
		return createSolanaPaymentPayload(bc.solanaKey, option, resourceURL, description, extensions, bc.solanaRPCURL, allowServerBlockhash)
	}
	return CreatePaymentPayload(
		bc.privateKey,
		option.PayTo,
		option.Amount,
		option.Network,
		resourceURL,
		description,
		option.MaxTimeoutSeconds,
		option.Extra,
		extensions,
	)
}

// GetWalletAddress returns the wallet address being used for payments.
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

// doRequest makes a POST request to the given endpoint with automatic x402
// payment handling. It returns the raw response bytes for the caller to unmarshal.
func (bc *baseClient) doRequest(ctx context.Context, endpoint string, body map[string]any) ([]byte, error) {
	data, _, err := bc.doRequestHeaders(ctx, endpoint, body)
	return data, err
}

// doRequestHeaders is doRequest plus the final HTTP response headers, for
// endpoints that surface gateway metadata in headers (e.g. /v1/rpc/{network}
// returns X-Network / X-Cache / X-Payment-Receipt). Headers are nil when the
// response was served from the local cache.
func (bc *baseClient) doRequestHeaders(ctx context.Context, endpoint string, body map[string]any) ([]byte, http.Header, error) {
	// Check cache before making request
	if bc.cache != nil {
		if cached, ok := bc.cache.Get(endpoint, body); ok {
			return cached, nil, nil
		}
	}

	url := bc.apiURL + endpoint

	// Encode body
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encode request body: %w", err)
	}

	// First attempt (will likely return 402)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := bc.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle 402 Payment Required
	if resp.StatusCode == http.StatusPaymentRequired {
		return bc.handlePaymentAndRetryHeaders(ctx, url, jsonBody, resp)
	}

	// Handle other errors
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("API error: %s", string(bodyBytes)),
		}
	}

	// Read successful response
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	// Store in cache
	if bc.cache != nil {
		bc.cache.Set(endpoint, body, data)
	}

	return data, resp.Header, nil
}

// doGet makes a GET request to the given endpoint and returns raw response bytes.
func (bc *baseClient) doGet(ctx context.Context, endpoint string) ([]byte, error) {
	// Check cache before making request
	if bc.cache != nil {
		if cached, ok := bc.cache.Get(endpoint, nil); ok {
			return cached, nil
		}
	}

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

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("API error: %s", string(bodyBytes)),
		}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Store in cache
	if bc.cache != nil {
		bc.cache.Set(endpoint, nil, data)
	}

	return data, nil
}

// cacheQueryBody reshapes a GET's query map into the map[string]any the cache
// keys on. Without it every query on one endpoint would share a cache entry and
// serve another query's response.
func cacheQueryBody(query map[string]string) map[string]any {
	if len(query) == 0 {
		return nil
	}
	body := make(map[string]any, len(query))
	for k, v := range query {
		body[k] = v
	}
	return body
}

// doGetWithPayment issues a GET, and if it comes back 402, signs the payment
// and retries. This is used for Pyth-backed market-data endpoints where the
// same path may be free (crypto/fx/commodity) or paid (stocks/usstock).
//
// The cache is consulted on the way in and populated on the way out, on both
// the free and the paid branch. It matters more here than on doGet: a miss on
// this path costs real USDC, not just latency.
func (bc *baseClient) doGetWithPayment(ctx context.Context, endpoint string, query map[string]string) ([]byte, error) {
	cacheBody := cacheQueryBody(query)
	if bc.cache != nil {
		if cached, ok := bc.cache.Get(endpoint, cacheBody); ok {
			return cached, nil
		}
	}

	url, err := buildRequestURL(bc.apiURL, endpoint, query)
	if err != nil {
		return nil, err
	}

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
		// Kept alongside the signPayment guard on purpose: bailing here skips
		// parsing the 402 body, so a keyless client gets "no wallet is
		// configured" rather than whatever parse error a malformed 402 would
		// produce first. signPayment holds the invariant; this holds the
		// message.
		if !bc.hasWallet() {
			return nil, &PaymentError{Message: "endpoint returned 402 but no wallet is configured"}
		}
		data, err := bc.handleGetPaymentAndRetry(ctx, url, resp)
		if err != nil {
			return nil, err
		}
		if bc.cache != nil {
			bc.cache.Set(endpoint, cacheBody, data)
		}
		return data, nil
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("API error: %s", string(bodyBytes)),
		}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if bc.cache != nil {
		bc.cache.Set(endpoint, cacheBody, data)
	}
	return data, nil
}

// handleGetPaymentAndRetry mirrors handlePaymentAndRetry for GET requests
// (no body to re-send; PAYMENT-SIGNATURE rides on a second GET to the same URL).
func (bc *baseClient) handleGetPaymentAndRetry(ctx context.Context, url string, resp *http.Response) ([]byte, error) {
	body, _ := io.ReadAll(resp.Body)
	paymentHeader := paymentRequirementsFrom(resp.Header.Get("payment-required"), body)
	if paymentHeader == "" {
		return nil, &PaymentError{Message: "402 response but no payment requirements found"}
	}

	paymentReq, err := ParsePaymentRequired(paymentHeader)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to parse payment requirements: %v", err)}
	}
	paymentOption, err := bc.extractPaymentDetails(paymentReq)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to extract payment details: %v", err)}
	}

	resourceURL := paymentReq.Resource.URL
	if resourceURL == "" {
		resourceURL = url
	}

	paymentPayload, err := bc.createPaymentPayload(paymentOption, resourceURL, paymentReq.Resource.Description, paymentReq.Extensions)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to create payment: %v", err)}
	}

	retryReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create retry request: %w", err)
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

	respBytes, err := io.ReadAll(retryResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	bc.mu.Lock()
	bc.sessionCalls++
	var costUSD float64
	if amountStr := paymentOption.Amount; amountStr != "" {
		var amountMicro float64
		if _, err := fmt.Sscanf(amountStr, "%f", &amountMicro); err == nil {
			costUSD = amountMicro / 1_000_000
			bc.sessionTotalUSD += costUSD
		}
	}
	bc.mu.Unlock()

	if bc.costLog != nil && costUSD > 0 {
		endpoint := strings.TrimPrefix(url, bc.apiURL)
		if idx := strings.Index(endpoint, "?"); idx != -1 {
			endpoint = endpoint[:idx]
		}
		bc.costLog.Append(endpoint, costUSD)
	}

	return respBytes, nil
}

// buildRequestURL joins endpoint onto apiURL and appends query.
//
// Every part that a caller can influence is escaped by net/url: the endpoint is
// set as url.URL.Path, so it is escaped as a path (slashes kept, "?" and "#"
// escaped) and cannot rewrite the request target; url.Values.Encode escapes
// query keys as well as values, so a key cannot forge extra parameters. Encode
// also sorts by key, which makes the request string deterministic — a bare map
// range used to leak Go's iteration order into every URL.
func buildRequestURL(apiURL, endpoint string, query map[string]string) (string, error) {
	u, err := url.Parse(apiURL)
	if err != nil {
		return "", fmt.Errorf("invalid API URL %q: %w", apiURL, err)
	}

	u.Path = strings.TrimSuffix(u.Path, "/") + "/" + strings.TrimPrefix(endpoint, "/")
	// Path is authoritative from here; a stale RawPath would be re-used verbatim.
	u.RawPath = ""

	if len(query) > 0 {
		values := make(url.Values, len(query))
		for k, v := range query {
			values.Set(k, v)
		}
		u.RawQuery = values.Encode()
	}

	return u.String(), nil
}

// paymentRequirementsFrom returns the x402 payment-requirements payload of a
// 402 response: the payment-required header when present, else the JSON body.
//
// The body fallback exists for gateways that answer with requirements inline
// rather than in a header. Two body shapes are recognised — the requirement
// nested under "x402", and a bare requirement with a top-level "accepts" — and
// each is returned as the JSON that ParsePaymentRequired should see. Every
// payment path shares this one copy: the previous per-path copies disagreed on
// whether to marshal respBody["x402"] or the whole body, so at most one of them
// could have been right.
//
// The return value is "" when the response carries no requirements at all.
func paymentRequirementsFrom(header string, body []byte) string {
	if header != "" {
		return header
	}
	var respBody map[string]any
	if err := json.Unmarshal(body, &respBody); err != nil {
		return ""
	}
	if nested, ok := respBody["x402"]; ok {
		if jsonBytes, err := json.Marshal(nested); err == nil {
			return string(jsonBytes)
		}
		return ""
	}
	if _, ok := respBody["accepts"]; ok {
		if jsonBytes, err := json.Marshal(respBody); err == nil {
			return string(jsonBytes)
		}
	}
	return ""
}

// handlePaymentAndRetry handles a 402 response by signing a payment and retrying.
func (bc *baseClient) handlePaymentAndRetry(ctx context.Context, url string, body []byte, resp *http.Response) ([]byte, error) {
	data, _, err := bc.handlePaymentAndRetryHeaders(ctx, url, body, resp)
	return data, err
}

// handlePaymentAndRetryHeaders is handlePaymentAndRetry plus the retry
// response headers (settlement receipt, gateway metadata).
func (bc *baseClient) handlePaymentAndRetryHeaders(ctx context.Context, url string, body []byte, resp *http.Response) ([]byte, http.Header, error) {
	// Payment requirements: the header if the gateway sent one, else the body.
	respBody, _ := io.ReadAll(resp.Body)
	paymentHeader := paymentRequirementsFrom(resp.Header.Get("payment-required"), respBody)
	if paymentHeader == "" {
		return nil, nil, &PaymentError{Message: "402 response but no payment requirements found"}
	}

	// Parse payment requirements
	paymentReq, err := ParsePaymentRequired(paymentHeader)
	if err != nil {
		return nil, nil, &PaymentError{Message: fmt.Sprintf("Failed to parse payment requirements: %v", err)}
	}

	// Extract the option this client's chain can sign
	paymentOption, err := bc.extractPaymentDetails(paymentReq)
	if err != nil {
		return nil, nil, &PaymentError{Message: fmt.Sprintf("Failed to extract payment details: %v", err)}
	}

	// Determine resource URL
	resourceURL := paymentReq.Resource.URL
	if resourceURL == "" {
		resourceURL = url
	}

	// Create signed payment payload
	paymentPayload, err := bc.createPaymentPayload(paymentOption, resourceURL, paymentReq.Resource.Description, paymentReq.Extensions)
	if err != nil {
		return nil, nil, &PaymentError{Message: fmt.Sprintf("Failed to create payment: %v", err)}
	}

	// Retry with payment signature
	retryReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create retry request: %w", err)
	}
	retryReq.Header.Set("Content-Type", "application/json")
	retryReq.Header.Set("PAYMENT-SIGNATURE", paymentPayload)

	retryResp, err := bc.httpClient.Do(retryReq)
	if err != nil {
		return nil, nil, fmt.Errorf("retry request failed: %w", err)
	}
	defer retryResp.Body.Close()

	// Check for payment rejection
	if retryResp.StatusCode == http.StatusPaymentRequired {
		return nil, nil, &PaymentError{Message: "Payment was rejected. Check your wallet balance."}
	}

	// Handle other errors
	if retryResp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(retryResp.Body)
		return nil, nil, &APIError{
			StatusCode: retryResp.StatusCode,
			Message:    fmt.Sprintf("API error after payment: %s", string(bodyBytes)),
		}
	}

	// Read successful response
	respBytes, err := io.ReadAll(retryResp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Track spending - convert amount from micro-USDC to USD
	bc.mu.Lock()
	bc.sessionCalls++
	var costUSD float64
	if amountStr := paymentOption.Amount; amountStr != "" {
		var amountMicro float64
		if _, err := fmt.Sscanf(amountStr, "%f", &amountMicro); err == nil {
			costUSD = amountMicro / 1_000_000
			bc.sessionTotalUSD += costUSD
		}
	}
	bc.mu.Unlock()

	// Log cost to persistent JSONL file
	if bc.costLog != nil && costUSD > 0 {
		endpoint := strings.TrimPrefix(url, bc.apiURL)
		bc.costLog.Append(endpoint, costUSD)
	}

	return respBytes, retryResp.Header, nil
}

// recordSettledCost tracks spending for a paid call whose settlement happens
// outside handlePaymentAndRetryHeaders — the async submit→poll flows (video,
// slow-path images), which charge only once a poll observes "completed". It
// mirrors the session accounting and JSONL cost log of the synchronous path.
func (bc *baseClient) recordSettledCost(amount, endpoint string) {
	var costUSD float64
	if amount != "" {
		var amountMicro float64
		if _, err := fmt.Sscanf(amount, "%f", &amountMicro); err == nil {
			costUSD = amountMicro / 1_000_000
		}
	}

	bc.mu.Lock()
	bc.sessionCalls++
	if costUSD > 0 {
		bc.sessionTotalUSD += costUSD
	}
	bc.mu.Unlock()

	if bc.costLog != nil && costUSD > 0 {
		bc.costLog.Append(endpoint, costUSD)
	}
}

// resolvePollURL resolves a server-supplied relative poll_url against the API
// host. poll_url comes back as "/api/v1/...": apiURL already ends in "/api",
// so strip that once to avoid "/api/api/...".
func (bc *baseClient) resolvePollURL(u string) string {
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	base := bc.apiURL
	if strings.HasSuffix(base, "/api") {
		base = strings.TrimSuffix(base, "/api")
	}
	return base + u
}
