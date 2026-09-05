package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const testAPIKey = "brk_live_TESTKEYTESTKEYTESTKEYTESTKEY"

// TestMain clears BLOCKRUN_API_KEY for the whole package.
//
// Without it, every test that asserts "no credential configured" fails on the
// machine of anyone who actually has a key exported — which is every developer
// working on this feature. Tests that want the variable set it with t.Setenv,
// which overrides this.
func TestMain(m *testing.M) {
	os.Unsetenv(EnvAPIKey)
	os.Unsetenv(EnvAPIKeyURL)
	os.Exit(m.Run())
}

func TestIsAPIKey(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"brk_live_abc", true},
		{"  brk_test_abc  ", true},
		{testPrivateKey, false},
		{"", false},
		{"sk-abc", false},
	} {
		if got := IsAPIKey(tc.in); got != tc.want {
			t.Errorf("IsAPIKey(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// The precedence rule in one place, because it decides whether a call spends
// credit or on-chain USDC.
func TestResolveAPIKeyPrecedence(t *testing.T) {
	t.Run("explicit key wins over everything", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "brk_live_fromenv")
		if got, err := resolveAPIKey(testAPIKey); err != nil || got != testAPIKey {
			t.Errorf("got %q, want the explicit key", got)
		}
	})

	t.Run("explicit wallet key opts out of the env key", func(t *testing.T) {
		t.Setenv(EnvAPIKey, testAPIKey)
		if got, err := resolveAPIKey(testPrivateKey); err != nil || got != "" {
			t.Errorf("got %q, want no API key: an explicit wallet key chooses the x402 rail", got)
		}
	})

	t.Run("env key beats the wallet env vars", func(t *testing.T) {
		t.Setenv(EnvAPIKey, testAPIKey)
		t.Setenv("BLOCKRUN_WALLET_KEY", testPrivateKey)
		if got, err := resolveAPIKey(""); err != nil || got != testAPIKey {
			t.Errorf("got %q, want the env API key", got)
		}
	})

	t.Run("no key anywhere", func(t *testing.T) {
		if got, err := resolveAPIKey(""); err != nil || got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("a non-brk env value is not a key", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "not-a-key")
		if got, err := resolveAPIKey(""); err == nil || got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestNewLLMClientWithAPIKey(t *testing.T) {
	client, err := NewLLMClient(testAPIKey)
	if err != nil {
		t.Fatalf("NewLLMClient with an API key: %v", err)
	}
	if client.PaymentMode() != PaymentModeAPIKey {
		t.Errorf("PaymentMode = %q, want %q", client.PaymentMode(), PaymentModeAPIKey)
	}
	if client.apiURL != DefaultAPIKeyURL {
		t.Errorf("apiURL = %q, want %q", client.apiURL, DefaultAPIKeyURL)
	}
	if client.GetWalletAddress() != "" {
		t.Errorf("GetWalletAddress = %q, want empty on the account rail", client.GetWalletAddress())
	}
	if client.hasWallet() {
		t.Error("hasWallet = true on the account rail")
	}
}

// A wallet client must be exactly what it was before this feature existed.
func TestWalletClientUnaffectedByAPIKeySupport(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewLLMClient: %v", err)
	}
	if client.PaymentMode() != PaymentModeWallet {
		t.Errorf("PaymentMode = %q, want %q", client.PaymentMode(), PaymentModeWallet)
	}
	if client.apiURL != DefaultAPIURL {
		t.Errorf("apiURL = %q, want %q", client.apiURL, DefaultAPIURL)
	}
	if client.GetWalletAddress() != testWalletAddress {
		t.Errorf("GetWalletAddress = %q, want %q", client.GetWalletAddress(), testWalletAddress)
	}
	if !client.hasWallet() {
		t.Error("hasWallet = false for a wallet client")
	}
}

func TestAPIKeyFromEnvBeatsWalletEnv(t *testing.T) {
	t.Setenv(EnvAPIKey, testAPIKey)
	t.Setenv("BLOCKRUN_WALLET_KEY", testPrivateKey)

	client, err := NewLLMClient("")
	if err != nil {
		t.Fatalf("NewLLMClient: %v", err)
	}
	if client.PaymentMode() != PaymentModeAPIKey {
		t.Fatalf("PaymentMode = %q, want %q", client.PaymentMode(), PaymentModeAPIKey)
	}
}

// The Solana constructors take the key too: on the account rail there is no
// transfer to sign, so the chain stops being a question.
func TestSolanaConstructorAcceptsAPIKey(t *testing.T) {
	client, err := NewLLMClientSolana(testAPIKey, "")
	if err != nil {
		t.Fatalf("NewLLMClientSolana with an API key: %v", err)
	}
	if client.PaymentMode() != PaymentModeAPIKey {
		t.Errorf("PaymentMode = %q, want %q", client.PaymentMode(), PaymentModeAPIKey)
	}
	if client.isSolana() {
		t.Error("isSolana = true: an API-key client signs on no chain")
	}
	if client.apiURL != DefaultAPIKeyURL {
		t.Errorf("apiURL = %q, want %q", client.apiURL, DefaultAPIKeyURL)
	}
}

// BLOCKRUN_API_URL names the x402 gateway. Following it would send the key to a
// host configured for a different rail.
func TestX402EnvURLDoesNotRetargetAPIKeyClient(t *testing.T) {
	t.Setenv("BLOCKRUN_API_URL", "https://private-x402.example.com/api")

	client, err := NewLLMClient(testAPIKey)
	if err != nil {
		t.Fatalf("NewLLMClient: %v", err)
	}
	if client.apiURL != DefaultAPIKeyURL {
		t.Errorf("apiURL = %q, want %q", client.apiURL, DefaultAPIKeyURL)
	}
}

func TestAPIKeyURLEnvOverride(t *testing.T) {
	t.Setenv(EnvAPIKeyURL, "https://api.staging.example.com/")

	client, err := NewLLMClient(testAPIKey)
	if err != nil {
		t.Fatalf("NewLLMClient: %v", err)
	}
	if client.apiURL != "https://api.staging.example.com" {
		t.Errorf("apiURL = %q, want the override without its trailing slash", client.apiURL)
	}
}

// The whole point of the rail: one request, key attached, no 402 round trip.
func TestAPIKeyChatSendsBearerAndSkipsThe402Dance(t *testing.T) {
	var seenAuth, seenPath string
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		seenAuth = r.Header.Get("Authorization")
		seenPath = r.URL.Path
		if r.Header.Get("PAYMENT-SIGNATURE") != "" {
			t.Error("account rail sent a PAYMENT-SIGNATURE")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": "4"}},
			},
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testAPIKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("NewLLMClient: %v", err)
	}

	got, err := client.Chat(context.Background(), "openai/gpt-4o", "2+2?")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got != "4" {
		t.Errorf("Chat = %q, want %q", got, "4")
	}
	if requests != 1 {
		t.Errorf("made %d requests, want 1", requests)
	}
	if seenAuth != "Bearer "+testAPIKey {
		t.Errorf("Authorization = %q, want the bearer key", seenAuth)
	}
	// No "/api" was inserted: the endpoint constants are already /v1/...
	if seenPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", seenPath)
	}
}

// A 402 here is "out of credit", not "sign this". The old code path would have
// answered with "no wallet is configured", which sends the reader looking for a
// wallet problem they do not have.
func TestAPIKey402IsACreditRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"type": "insufficient_quota",
				"code": "BALANCE_EXHAUSTED",
			},
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testAPIKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("NewLLMClient: %v", err)
	}

	_, err = client.Chat(context.Background(), "openai/gpt-4o", "hi")
	if err == nil {
		t.Fatal("expected an error on a 402")
	}
	pe, ok := err.(*PaymentError)
	if !ok {
		t.Fatalf("error type = %T, want *PaymentError", err)
	}
	if strings.Contains(pe.Message, "no wallet is configured") {
		t.Errorf("message blames a wallet: %q", pe.Message)
	}
	if !strings.Contains(pe.Message, "user.blockrun.ai") {
		t.Errorf("message does not say where to top up: %q", pe.Message)
	}
	if !strings.Contains(pe.Message, "BALANCE_EXHAUSTED") {
		t.Errorf("message drops the gateway's own reason: %q", pe.Message)
	}
}

// poll_url is minted by the x402 gateway relative to ITS host, so it arrives as
// "/api/v1/...". api.blockrun.ai serves that route at "/v1/..." and answers
// "/api/v1/..." with wrong_host — so an unstripped prefix is an async job that
// polls a 404 until the budget runs out.
func TestResolvePollURLStripsAPIPrefixOnAccountRail(t *testing.T) {
	apiKeyClient, err := NewLLMClient(testAPIKey)
	if err != nil {
		t.Fatalf("NewLLMClient: %v", err)
	}
	got, err := apiKeyClient.resolvePollURL("/api/v1/images/generations/job_1")
	want := DefaultAPIKeyURL + "/v1/images/generations/job_1"
	if err != nil || got != want {
		t.Errorf("account rail: got %q, want %q", got, want)
	}

	walletClient, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewLLMClient: %v", err)
	}
	got, err = walletClient.resolvePollURL("/api/v1/images/generations/job_1")
	want = "https://blockrun.ai/api/v1/images/generations/job_1"
	if err != nil || got != want {
		t.Errorf("wallet rail: got %q, want %q", got, want)
	}

	if _, err := apiKeyClient.resolvePollURL("https://elsewhere.example/x"); err == nil {
		t.Error("foreign polling origin was accepted")
	}
}

// The async submit answers 202 on the FIRST request here, with no 402 in
// between — the shape the wallet path never sees.
func TestAPIKeyImageAsyncPollsWithoutSigning(t *testing.T) {
	polls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testAPIKey {
			t.Errorf("%s %s: Authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		if r.Header.Get("PAYMENT-SIGNATURE") != "" {
			t.Errorf("%s %s carried a PAYMENT-SIGNATURE", r.Method, r.URL.Path)
		}

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/generations":
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]any{
				"id":     "img_1",
				"status": "queued",
				// Minted by the gateway, so it carries the /api prefix.
				"poll_url": "/api/v1/images/generations/img_1",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/images/generations/img_1":
			polls++
			if polls < 2 {
				w.WriteHeader(http.StatusAccepted)
				json.NewEncoder(w).Encode(map[string]any{"status": "in_progress"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"status":  "completed",
				"created": 1,
				"data":    []map[string]string{{"url": "https://cdn.example/i.png"}},
				"price":   map[string]string{"amount": "0.021000", "currency": "USD"},
				"payment": map[string]string{"status": "settled", "network": "credit"},
			})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewImageClient(testAPIKey, WithImageAPIURL(server.URL))
	if err != nil {
		t.Fatalf("NewImageClient: %v", err)
	}
	client.pollInterval = time.Millisecond

	resp, err := client.Generate(context.Background(), "a red cube", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].URL != "https://cdn.example/i.png" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if polls != 2 {
		t.Errorf("polled %d times, want 2", polls)
	}

	// The completed payload carried a price, so the session accounting saw it.
	if spend := client.GetSpending(); spend.Calls != 1 || spend.TotalUSD != 0.021 {
		t.Errorf("GetSpending = %+v, want 1 call at $0.021", spend)
	}
}

// A settled price in the response body is the only cost figure this rail
// publishes, so it is the only one the SDK books.
func TestRecordAPIKeyCost(t *testing.T) {
	client, err := NewLLMClient(testAPIKey)
	if err != nil {
		t.Fatalf("NewLLMClient: %v", err)
	}

	client.recordAPIKeyCost([]byte(`{"price":{"amount":"0.002000","currency":"USD"}}`), "/v1/search")
	client.recordAPIKeyCost([]byte(`{"choices":[]}`), "/v1/chat/completions") // no price published
	client.recordAPIKeyCost([]byte(`not json at all`), "/v1/rpc/ethereum")

	spend := client.GetSpending()
	if spend.Calls != 3 {
		t.Errorf("Calls = %d, want 3: every billable request counts", spend.Calls)
	}
	if spend.TotalUSD != 0.002 {
		t.Errorf("TotalUSD = %v, want 0.002: only the priced response contributes", spend.TotalUSD)
	}
}

func TestRecordAPIKeyCostIsNoOpOnWalletRail(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewLLMClient: %v", err)
	}
	client.recordAPIKeyCost([]byte(`{"price":{"amount":"9.99"}}`), "/v1/search")
	if spend := client.GetSpending(); spend.Calls != 0 || spend.TotalUSD != 0 {
		t.Errorf("GetSpending = %+v, want zero: the wallet rail books from the signed amount", spend)
	}
}

// Returning 0 from GetBalance would be indistinguishable from an empty wallet,
// and an agent that gates on it would stop calling a well-funded account.
func TestWalletOnlyHelpersRefuseOnAccountRail(t *testing.T) {
	client, err := NewLLMClient(testAPIKey)
	if err != nil {
		t.Fatalf("NewLLMClient: %v", err)
	}
	ctx := context.Background()

	if _, err := client.GetBalance(ctx); err == nil {
		t.Error("GetBalance returned no error on the account rail")
	} else if !strings.Contains(err.Error(), "user.blockrun.ai") {
		t.Errorf("GetBalance error does not point at the dashboard: %v", err)
	}

	if _, err := client.GetBalanceTestnet(ctx); err == nil {
		t.Error("GetBalanceTestnet returned no error on the account rail")
	}

	if _, err := client.Onramp(ctx, testWalletAddress); err == nil {
		t.Error("Onramp returned no error on the account rail")
	}
}

// A skill or agent calls this unconditionally; with a key configured it must
// not mint a keyfile for a wallet that will never sign anything.
func TestSetupAgentWalletUsesAPIKeyWithoutMintingAWallet(t *testing.T) {
	t.Setenv(EnvAPIKey, testAPIKey)

	client, err := SetupAgentWallet()
	if err != nil {
		t.Fatalf("SetupAgentWallet: %v", err)
	}
	if client.PaymentMode() != PaymentModeAPIKey {
		t.Errorf("PaymentMode = %q, want %q", client.PaymentMode(), PaymentModeAPIKey)
	}
	if client.GetWalletAddress() != "" {
		t.Errorf("SetupAgentWallet minted an address on the account rail: %q", client.GetWalletAddress())
	}
}

// Free models are free on this rail too, and a paid GET still authenticates.
func TestAPIKeyPaidGETCarriesTheKey(t *testing.T) {
	var seenAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]any{
			"price": map[string]string{"amount": "0.001000"},
			"data":  []string{"ok"},
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testAPIKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("NewLLMClient: %v", err)
	}

	if _, err := client.doGetWithPayment(context.Background(), "/v1/market/crypto/price/BTC", nil); err != nil {
		t.Fatalf("doGetWithPayment: %v", err)
	}
	if seenAuth != "Bearer "+testAPIKey {
		t.Errorf("Authorization = %q, want the bearer key", seenAuth)
	}
	if spend := client.GetSpending(); spend.TotalUSD != 0.001 {
		t.Errorf("TotalUSD = %v, want 0.001", spend.TotalUSD)
	}
}

func TestInvalidAPIKeyEnvDoesNotSelectWallet(t *testing.T) {
	for _, value := range []string{"", "   ", "not-a-key"} {
		t.Run("invalid="+value, func(t *testing.T) {
			t.Setenv(EnvAPIKey, value)
			t.Setenv("BLOCKRUN_WALLET_KEY", testPrivateKey)
			if _, err := NewLLMClient(""); err == nil {
				t.Fatal("expected invalid configured API key error")
			}
			client, err := NewLLMClient(testPrivateKey)
			if err != nil || client.PaymentMode() != PaymentModeWallet {
				t.Fatal("explicit wallet must still work")
			}
		})
	}
}

func TestAPIKeyRotationDoesNotChangeExistingClient(t *testing.T) {
	t.Setenv(EnvAPIKey, testAPIKey)
	first, err := NewLLMClient("")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvAPIKey, "brk_test_second")
	second, err := NewLLMClient("")
	if err != nil {
		t.Fatal(err)
	}
	if first.apiKey != testAPIKey || second.apiKey != "brk_test_second" {
		t.Fatal("client credentials crossed")
	}
}

func TestForeignPollURLCannotReceiveAccountCredential(t *testing.T) {
	// Use the public image API and a second server that must receive no key.
	seen := 0
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen++; w.WriteHeader(500) }))
	defer foreign.Close()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(map[string]string{"id": "test", "poll_url": foreign.URL + "/job"})
	}))
	defer gateway.Close()
	client, err := NewImageClient(testAPIKey, WithImageAPIURL(gateway.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Generate(context.Background(), "test", &ImageGenerateOptions{Model: "openai/gpt-image-1"})
	if err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("expected origin error, got %v", err)
	}
	if seen != 0 {
		t.Fatal("account credential reached foreign poll server")
	}
}

func TestAccountAndWalletCanShareHTTPClientWithoutSharingAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/account/v1/chat/completions":
			if r.Header.Get("Authorization") != "Bearer "+testAPIKey || r.Header.Get("Payment-Signature") != "" {
				t.Error("account authentication changed or included a wallet proof")
			}
		case "/wallet/v1/chat/completions":
			if r.Header.Get("Authorization") != "" {
				t.Error("account key crossed into wallet request")
			}
		default:
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()
	shared := server.Client()
	originalTransport := shared.Transport
	t.Setenv("BLOCKRUN_API_KEY", testAPIKey)
	account, err := NewLLMClient("", WithAPIURL(server.URL+"/account"), WithHTTPClient(shared))
	if err != nil {
		t.Fatal(err)
	}
	wallet, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL+"/wallet"), WithHTTPClient(shared))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BLOCKRUN_API_KEY", "brk_live_changed_account")
	t.Setenv("BLOCKRUN_API_KEY_URL", "https://changed.example")
	for _, client := range []*LLMClient{account, wallet, account} {
		if reply, err := client.Chat(context.Background(), "openai/gpt-5.2", "hi"); err != nil || reply != "ok" {
			t.Fatalf("call failed: %q %v", reply, err)
		}
	}
	if shared.Transport != originalTransport || wallet.GetWalletAddress() != testWalletAddress {
		t.Fatal("account setup mutated caller transport or wallet identity")
	}
}
