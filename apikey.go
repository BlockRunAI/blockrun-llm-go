package blockrun

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// The API-key rail.
//
// BlockRun sells the same catalogue through two front doors. The x402 rail
// (blockrun.ai / sol.blockrun.ai) authenticates a caller by wallet signature
// and settles USDC on-chain per request. The account rail
// (api.blockrun.ai) authenticates a caller by API key and draws down prepaid
// credit held against the account at user.blockrun.ai.
//
// The two are the same backend and the same response shapes, which is what
// makes one client able to serve both: the only differences are the host, the
// header that authenticates, and the fact that a 402 on the account rail means
// "out of credit" rather than "sign this".
//
// A key is never a wallet. In API-key mode there is no private key, no address,
// and nothing signed locally — so the wallet-only helpers (GetBalance, Onramp,
// GetWalletAddress) report that plainly instead of returning a zero that looks
// like an answer.

const (
	// DefaultAPIKeyURL is the account-rail gateway. Unlike DefaultAPIURL it
	// carries no "/api" suffix: api.blockrun.ai serves /v1/... at the root and
	// answers /api/v1/... with a wrong_host error.
	DefaultAPIKeyURL = "https://api.blockrun.ai"

	// APIKeyPrefix is the prefix every BlockRun API key carries. It is what
	// lets one credential parameter accept either a wallet key or an API key:
	// a hex private key can never start with "brk_".
	APIKeyPrefix = "brk_"

	// EnvAPIKey holds a BlockRun API key. Setting it puts every client in this
	// process on the account rail, including the Solana constructors — the key
	// is the payment method, so the chain stops being a question.
	EnvAPIKey = "BLOCKRUN_API_KEY"

	// EnvAPIKeyURL overrides the account-rail host. Deliberately not
	// BLOCKRUN_API_URL: that one names the x402 gateway, and a developer who
	// has it pointed at a private x402 deployment must not have an API-key
	// client silently follow it there.
	EnvAPIKeyURL = "BLOCKRUN_API_KEY_URL"
)

// PaymentMode names which rail a client pays on.
type PaymentMode string

const (
	// PaymentModeWallet signs x402 payments locally and settles USDC on-chain.
	PaymentModeWallet PaymentMode = "wallet"
	// PaymentModeAPIKey authenticates with an API key and draws prepaid credit.
	PaymentModeAPIKey PaymentMode = "apikey"
)

// IsAPIKey reports whether a credential string is a BlockRun API key rather
// than a wallet private key.
func IsAPIKey(credential string) bool {
	// The prefix alone is not a key. A truncated secret ("brk_") would
	// otherwise select the account rail and fail at request time with a 401,
	// which is the opposite of this rail's promise that a bad credential fails
	// where you set it rather than where you use it.
	return len(strings.TrimSpace(credential)) > len(APIKeyPrefix) &&
		strings.HasPrefix(strings.TrimSpace(credential), APIKeyPrefix)
}

// resolveAPIKey decides whether a constructor call is an API-key call.
//
// Precedence, and the reason for it: an explicit argument beats everything,
// because the caller wrote it at the call site. Then BLOCKRUN_API_KEY beats the
// wallet variables, because it is the new variable — a developer who has not
// set it keeps the wallet behaviour they already had, and one who has set it
// meant to, even if an old BLOCKRUN_WALLET_KEY is still sitting in their
// profile. PaymentMode() exists so that decision is never invisible.
func resolveAPIKey(credential string) (string, error) {
	if IsAPIKey(credential) {
		return strings.TrimSpace(credential), nil
	}
	// Explicit wallet selection takes precedence, even over an invalid env key.
	if strings.TrimSpace(credential) != "" {
		return "", nil
	}
	// Blank is unset, not invalid. `BLOCKRUN_API_KEY=` in a .env file, a bare
	// `docker -e BLOCKRUN_API_KEY`, and an unpopulated `${{ secrets.X }}` all
	// arrive as the empty string, and every one of them means "I am not on the
	// account rail" — erroring there breaks wallet users who never opted in,
	// on upgrade, in CI. Keying on os.LookupEnv instead of the value made all
	// three a hard failure.
	//
	// A non-blank value that is not a key is a different thing: someone typed a
	// credential and got it wrong, and silently spending USDC instead of credit
	// is the wrong way to tell them.
	env := strings.TrimSpace(os.Getenv(EnvAPIKey))
	if env == "" {
		return "", nil
	}
	if !IsAPIKey(env) {
		return "", &ValidationError{Field: EnvAPIKey, Message: "Invalid configured API key: expected a key starting with \"brk_\". Correct it, clear it, or explicitly pass a wallet key."}
	}
	return env, nil
}

// newAPIKeyBaseClient builds a baseClient on the account rail. It cannot fail:
// there is no key to parse and no address to derive.
func newAPIKeyBaseClient(apiKey string, timeout time.Duration) *baseClient {
	apiURL := DefaultAPIKeyURL
	if env := strings.TrimSpace(os.Getenv(EnvAPIKeyURL)); env != "" {
		apiURL = strings.TrimSuffix(env, "/")
	}
	return &baseClient{
		apiKey:     apiKey,
		apiURL:     apiURL,
		httpClient: &http.Client{Timeout: timeout},
		costLog:    NewCostLog(),
	}
}

// isAPIKey reports whether this client pays from prepaid credit.
func (bc *baseClient) isAPIKey() bool { return bc.apiKey != "" }

// PaymentMode reports which rail this client pays on. Worth calling once at
// startup when both a key and a wallet are configured in the environment — it
// is the difference between spending credit and spending USDC.
func (bc *baseClient) PaymentMode() PaymentMode {
	if bc.isAPIKey() {
		return PaymentModeAPIKey
	}
	return PaymentModeWallet
}

// applyAuth attaches the API key to a gateway request. It is a no-op on the
// x402 rail, so every request site can call it unconditionally — which is the
// point: a site that forgets is a request that 401s, and there are fourteen of
// them.
//
// Authorization: Bearer is the OpenAI-SDK shape; the gateway also accepts
// x-api-key for Anthropic-shaped clients. One is sent, not both, so a proxy
// that logs headers records the key once.
func (bc *baseClient) applyAuth(req *http.Request) {
	if !bc.isAPIKey() {
		return
	}
	req.Header.Set("Authorization", "Bearer "+bc.apiKey)
}

// apiKeyPaymentError explains a 402 that arrived on the account rail.
//
// On the x402 rail a 402 is the normal opening move of a conversation. On this
// one it is a refusal: the account is out of credit, suspended, or past its
// limit. Signing is not the answer and there is nothing to sign with, so the
// error says what to do instead rather than letting the caller fall into the
// wallet path and get "no wallet is configured" for a problem that has nothing
// to do with wallets.
func apiKeyPaymentError(body []byte) error {
	detail := strings.TrimSpace(string(body))
	if len(detail) > 400 {
		detail = detail[:400] + "…"
	}
	msg := "402 from api.blockrun.ai: this account has no credit left for that call. " +
		"Top up at https://user.blockrun.ai/dashboard/credits, or call one of the free models, " +
		"which need no credit."
	if detail != "" {
		msg = fmt.Sprintf("%s Gateway said: %s", msg, detail)
	}
	return &PaymentError{Message: msg}
}

// walletOnly is the error every wallet-only helper returns in API-key mode.
// Naming the helper matters: "no wallet" alone leaves the caller guessing which
// of GetBalance / Onramp / GetWalletAddress they should not have called.
func walletOnly(helper string) error {
	return &ValidationError{
		Field: "paymentMode",
		Message: fmt.Sprintf(
			"%s is wallet-only and this client authenticates with a BlockRun API key. "+
				"Credit balance, usage and top-ups live at https://user.blockrun.ai/dashboard. "+
				"Construct the client with a wallet private key (or unset %s) to use %s.",
			helper, EnvAPIKey, helper,
		),
	}
}

// recordAPIKeyCost books what the account rail says a call cost.
//
// On the x402 rail the price is known before the request: it is the amount
// signed into the 402. On this rail there is no 402, so the only figure the
// client can honestly report is the one the gateway puts in the response —
// `price.amount`, alongside a settled `payment` block. The image and video
// families return it, receipt and all.
//
// Plenty do not — chat, /v1/messages, /v1/search, the Surf and DefiLlama
// families — because they are billed post-hoc from usage at whatever per-model
// rate the account contracted, and the SDK is not the place to re-derive a
// price sheet: two implementations of one pricing rule drift, and a drifted
// number in a cost log is worse than a blank one. Those count as a call and
// contribute $0, which is why GetSpending documents its total as a floor and
// names the dashboard as the authority.
//
// A no-op on the wallet rail, where handlePaymentAndRetryHeaders already did
// this accounting from the signed amount.
func (bc *baseClient) recordAPIKeyCost(body []byte, endpoint string) {
	if !bc.isAPIKey() {
		return
	}

	var settled struct {
		Price struct {
			Amount string `json:"amount"`
		} `json:"price"`
	}
	// A non-JSON or differently shaped body is not an error here: plenty of
	// endpoints answer with a bare array (DefiLlama) or a JSON-RPC envelope.
	// It just means there is no price to book.
	_ = json.Unmarshal(body, &settled)

	var costUSD float64
	if settled.Price.Amount != "" {
		if v, err := strconv.ParseFloat(settled.Price.Amount, 64); err == nil && v > 0 {
			costUSD = v
		}
	}

	bc.mu.Lock()
	bc.sessionCalls++
	bc.sessionTotalUSD += costUSD
	bc.mu.Unlock()

	if bc.costLog != nil && costUSD > 0 {
		bc.costLog.Append(endpoint, costUSD)
	}
}
