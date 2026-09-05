package blockrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const testAPIKey = "brk_live_unit_test"

func TestConfiguredEmptyAPIKeyNeverSelectsWallet(t *testing.T) {
	for _, key := range []string{"", "   ", "bad"} {
		t.Run(fmt.Sprintf("key=%q", key), func(t *testing.T) {
			t.Setenv("BLOCKRUN_API_KEY", key)
			t.Setenv("BLOCKRUN_WALLET_KEY", testPrivateKey)
			t.Setenv("SOLANA_WALLET_KEY", testSolanaKey(t))
			constructors := []func() (*LLMClient, error){
				func() (*LLMClient, error) { return NewLLMClient("") },
				func() (*LLMClient, error) { return NewLLMClientSolana("", "") },
				func() (*LLMClient, error) { return SetupAgentClient("base") },
			}
			for i, construct := range constructors {
				client, err := construct()
				var validation *ValidationError
				if client != nil || !errors.As(err, &validation) || validation.Field != "apiKey" {
					t.Errorf("entry %d selected a wallet or returned the wrong error: %v", i, err)
				}
			}
			// An explicit wallet remains a deliberate wallet selection, even if the API env is invalid.
			for _, construct := range []func() (*LLMClient, error){
				func() (*LLMClient, error) { return NewLLMClient(testPrivateKey) },
				func() (*LLMClient, error) { return NewLLMClientSolana(testSolanaKey(t), "") },
			} {
				client, err := construct()
				if err != nil || client.AuthMode() != "wallet" {
					t.Fatalf("explicit wallet lost precedence: %v", err)
				}
			}
		})
	}
}

func TestUnsettingAPIKeyRestoresWalletWithoutChangingExistingClient(t *testing.T) {
	t.Setenv("BLOCKRUN_API_KEY", testAPIKey)
	t.Setenv("BLOCKRUN_WALLET_KEY", testPrivateKey)
	account, err := NewLLMClient("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("BLOCKRUN_API_KEY"); err != nil {
		t.Fatal(err)
	}
	wallet, err := NewLLMClient("")
	if err != nil {
		t.Fatal(err)
	}
	if wallet.AuthMode() != "wallet" || wallet.GetWalletAddress() != testWalletAddress || account.AuthMode() != "api-key" {
		t.Fatal("reconstructing a wallet changed the account client or wallet identity")
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
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
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
	t.Setenv("BLOCKRUN_API_BASE_URL", "https://changed.example")
	for _, client := range []*LLMClient{account, wallet, account} {
		if reply, err := client.Chat(context.Background(), "openai/gpt-5.2", "hi"); err != nil || reply != "ok" {
			t.Fatalf("call failed: %q %v", reply, err)
		}
	}
	if shared.Transport != originalTransport || wallet.GetWalletAddress() != testWalletAddress {
		t.Fatal("account setup mutated caller transport or wallet identity")
	}
}

type accountMode interface {
	AuthMode() string
	GetWalletAddress() string
}

func TestAccountConstructorsNoWallet(t *testing.T) {
	t.Setenv("BLOCKRUN_API_KEY", testAPIKey)
	t.Setenv("BLOCKRUN_WALLET_KEY", "invalid-wallet-must-not-be-loaded")
	t.Setenv("SOLANA_WALLET_KEY", "invalid-wallet-must-not-be-loaded")
	constructors := []func() (accountMode, error){
		func() (accountMode, error) { return NewLLMClientWithAPIKey("") },
		func() (accountMode, error) { return NewAnthropicClientWithAPIKey("") },
		func() (accountMode, error) { return NewImageClientWithAPIKey("") },
		func() (accountMode, error) { return NewVideoClientWithAPIKey("") },
		func() (accountMode, error) { return NewMusicClientWithAPIKey("") },
		func() (accountMode, error) { return NewSpeechClientWithAPIKey("") },
		func() (accountMode, error) { return NewVoiceClientWithAPIKey("") },
		func() (accountMode, error) { return NewPhoneClientWithAPIKey("") },
		func() (accountMode, error) { return NewPortraitClientWithAPIKey("") },
		func() (accountMode, error) { return NewRealFaceClientWithAPIKey("") },
		func() (accountMode, error) { return NewSurfClientWithAPIKey("") },
		func() (accountMode, error) { return NewRPCClientWithAPIKey("") },
		func() (accountMode, error) { return NewLLMClient("") },
		func() (accountMode, error) { return NewLLMClientSolana("", "") },
		func() (accountMode, error) { return SetupAgentClient("") },
	}
	for i, newClient := range constructors {
		c, err := newClient()
		if err != nil {
			t.Fatalf("constructor %d: %v", i, err)
		}
		if c.AuthMode() != "api-key" || c.GetWalletAddress() != "" {
			t.Fatal("wallet initialized")
		}
	}
}

func TestAccountHTTPAndQuota(t *testing.T) {
	for _, status := range []int{200, 401, 402, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			count := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				if r.Header.Get("Authorization") != "Bearer "+testAPIKey || r.Header.Get("Payment-Signature") != "" {
					t.Error("incorrect authentication")
				}
				if r.URL.Path != "/v1/chat/completions" {
					t.Error(r.URL.Path)
				}
				w.Header().Set("Retry-After", "10")
				w.Header().Set("Payment-Required", "never-sign")
				w.WriteHeader(status)
				if status == 200 {
					fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
				} else {
					fmt.Fprintf(w, `{"error":{"code":"quota","message":%q}}`, testAPIKey)
				}
			}))
			defer server.Close()
			client, err := NewLLMClientWithAPIKey(testAPIKey, WithAPIURL(server.URL+"/v1/"))
			if err != nil {
				t.Fatal(err)
			}
			reply, err := client.Chat(context.Background(), "openai/gpt-5.2", "hi")
			if status == 200 {
				if err != nil || reply != "ok" {
					t.Fatalf("%s %v", reply, err)
				}
			} else {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.StatusCode != status || apiErr.RetryAfter != "10" {
					t.Fatalf("%v", err)
				}
				if strings.Contains(fmt.Sprint(apiErr.Body), testAPIKey) {
					t.Fatal("key leaked")
				}
			}
			if count != 1 {
				t.Fatal("replayed request")
			}
		})
	}
}

func TestAccountMediaPolling(t *testing.T) {
	for _, kind := range []string{"image", "video", "music"} {
		t.Run(kind, func(t *testing.T) {
			count := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				if r.Header.Get("Authorization") != "Bearer "+testAPIKey {
					t.Error("missing account key")
				}
				if count == 1 {
					w.WriteHeader(202)
					fmt.Fprint(w, `{"status":"queued","poll_url":"/api/v1/jobs/1"}`)
					return
				}
				if r.Method != "GET" || r.URL.Path != "/v1/jobs/1" {
					t.Errorf("bad poll: %s %s", r.Method, r.URL.Path)
				}
				fmt.Fprint(w, `{"status":"completed","created":1,"data":[{"url":"https://example.com/result"}]}`)
			}))
			defer server.Close()
			ctx := context.Background()
			switch kind {
			case "image":
				c, e := NewImageClientWithAPIKey(testAPIKey, WithImageAPIURL(server.URL))
				if e != nil {
					t.Fatal(e)
				}
				c.pollInterval = time.Millisecond
				_, e = c.Generate(ctx, "cat", nil)
				if e != nil {
					t.Fatal(e)
				}
			case "video":
				c, e := NewVideoClientWithAPIKey(testAPIKey, WithVideoAPIURL(server.URL))
				if e != nil {
					t.Fatal(e)
				}
				c.pollInterval = time.Millisecond
				_, e = c.Generate(ctx, "cat", nil)
				if e != nil {
					t.Fatal(e)
				}
			case "music":
				c, e := NewMusicClientWithAPIKey(testAPIKey, WithMusicAPIURL(server.URL))
				if e != nil {
					t.Fatal(e)
				}
				_, e = c.Generate(ctx, "song", nil)
				if e != nil {
					t.Fatal(e)
				}
			}
			if count != 2 {
				t.Fatalf("requests: %d", count)
			}
		})
	}
}

func TestAccountRawStreamAndOrigin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "data: [DONE]\n\n") }))
	defer server.Close()
	c, e := NewLLMClientWithAPIKey(testAPIKey, WithAPIURL(server.URL))
	if e != nil {
		t.Fatal(e)
	}
	resp, e := c.APIRequest(context.Background(), "POST", "/v1/responses", map[string]any{"stream": true})
	if e != nil {
		t.Fatal(e)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(data), "[DONE]") {
		t.Fatal("missing SSE")
	}
	for _, path := range []string{"https://evil.example/job", "//evil.example/job", "http://u:p@" + strings.TrimPrefix(server.URL, "http://") + "/job"} {
		if _, err := c.APIRequest(context.Background(), "GET", path, nil); err == nil {
			t.Fatal("allowed cross-origin request")
		}
	}
	if _, err := c.GetBalance(context.Background()); err == nil {
		t.Fatal("account balance read as wallet")
	}
	if _, err := c.GetCostSummary(); err == nil {
		t.Fatal("account usage read as wallet")
	}
	if c.GetSpending().AuthMode != "api-key" {
		t.Fatal("missing spending scope")
	}
}

func TestPreferredWalletChain(t *testing.T) {
	for _, tt := range []struct {
		sol, base bool
		want      string
	}{{false, false, "solana"}, {false, true, "base"}, {true, false, "solana"}, {true, true, "solana"}} {
		if got := preferredWalletChain(tt.sol, tt.base); got != tt.want {
			t.Fatalf("%s != %s", got, tt.want)
		}
	}
}

func TestAccountStreamRedirectAndValidation(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/target", 302)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	c, err := NewLLMClientWithAPIKey(testAPIKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := c.ChatCompletionStream(context.Background(), "openai/gpt-5.2", []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := stream.Next()
	stream.Close()
	if err != nil || chunk.Choices[0].Delta.Content != "ok" {
		t.Fatalf("%v %v", chunk, err)
	}
	_, err = c.APIRequest(context.Background(), "GET", "/redirect", nil)
	var ae *APIError
	if !errors.As(err, &ae) || ae.StatusCode != 302 || count != 2 {
		t.Fatalf("redirect followed: %d %v", count, err)
	}
	for _, endpoint := range []string{"http://example.com", "https://u:p@example.com", "https://example.com?q=1"} {
		if _, err := NewLLMClientWithAPIKey(testAPIKey, WithAPIURL(endpoint)); err == nil {
			t.Fatalf("invalid endpoint accepted: %s", endpoint)
		}
	}
	t.Setenv("BLOCKRUN_API_KEY", testAPIKey)
	wallet, err := NewLLMClient("0x" + strings.Repeat("01", 32))
	if err != nil || wallet.AuthMode() != "wallet" {
		t.Fatal("explicit wallet lost precedence")
	}
	if _, err := NewLLMClientWithAPIKey("bad"); err == nil {
		t.Fatal("invalid key accepted")
	}
}

func TestAccountPollingRecoversExistingJob(t *testing.T) {
	for _, status := range []int{502, 503, 504, 522, 524} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			count := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				if r.Method != http.MethodGet || r.URL.RequestURI() != "/v1/jobs/existing?token=signed" {
					t.Errorf("wrong poll: %s %s", r.Method, r.URL)
				}
				if count == 1 {
					w.WriteHeader(status)
					fmt.Fprint(w, `{"error":{"message":"temporary gateway failure"}}`)
					return
				}
				fmt.Fprint(w, `{"status":"completed","id":"existing"}`)
			}))
			defer server.Close()
			client, err := NewLLMClientWithAPIKey(testAPIKey, WithAPIURL(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			data, err := client.pollAccount(context.Background(), []byte(`{"status":"queued","poll_url":"/v1/jobs/existing?token=signed"}`), 202, time.Second, time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "completed") || count != 2 {
				t.Fatalf("bad recovery: %s / %d", data, count)
			}
		})
	}
}
