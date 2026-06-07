package blockrun

// Tests for the Exa / DefiLlama / 0x DEX / Modal passthrough methods.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newPassthroughServer records the last request path+query and JSON body.
func newPassthroughServer(t *testing.T) (*httptest.Server, *string, *map[string]any) {
	t.Helper()
	var lastURL string
	var lastBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastURL = r.URL.String()
		lastBody = nil
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&lastBody)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	return server, &lastURL, &lastBody
}

func newPassthroughClient(t *testing.T, serverURL string) *LLMClient {
	t.Helper()
	c, err := NewLLMClient(testPrivateKey, WithAPIURL(serverURL))
	if err != nil {
		t.Fatalf("NewLLMClient: %v", err)
	}
	return c
}

// ── Exa ──────────────────────────────────────────────────────────────────

func TestExaSearch(t *testing.T) {
	server, lastURL, lastBody := newPassthroughServer(t)
	defer server.Close()
	c := newPassthroughClient(t, server.URL)

	if _, err := c.ExaSearch(context.Background(), "latest AI papers", map[string]any{"numResults": 5}); err != nil {
		t.Fatalf("ExaSearch: %v", err)
	}
	if !strings.HasSuffix(*lastURL, "/v1/exa/search") {
		t.Errorf("path = %s", *lastURL)
	}
	if (*lastBody)["query"] != "latest AI papers" || (*lastBody)["numResults"] != float64(5) {
		t.Errorf("body = %+v", *lastBody)
	}
}

func TestExaValidation(t *testing.T) {
	c := newPassthroughClient(t, "https://example.invalid")
	ctx := context.Background()
	if _, err := c.ExaSearch(ctx, " ", nil); err == nil {
		t.Error("expected error for blank query")
	}
	if _, err := c.ExaContents(ctx, nil, nil); err == nil {
		t.Error("expected error for empty urls")
	}
	if _, err := c.Exa(ctx, "", nil); err == nil {
		t.Error("expected error for blank path")
	}
}

// ── DefiLlama ────────────────────────────────────────────────────────────

func TestDefiConveniences(t *testing.T) {
	server, lastURL, _ := newPassthroughServer(t)
	defer server.Close()
	c := newPassthroughClient(t, server.URL)
	ctx := context.Background()

	if _, err := c.DefiProtocols(ctx); err != nil {
		t.Fatalf("DefiProtocols: %v", err)
	}
	if !strings.HasSuffix(*lastURL, "/v1/defillama/protocols") {
		t.Errorf("path = %s", *lastURL)
	}

	if _, err := c.DefiProtocol(ctx, "aave"); err != nil {
		t.Fatalf("DefiProtocol: %v", err)
	}
	if !strings.HasSuffix(*lastURL, "/v1/defillama/protocol/aave") {
		t.Errorf("path = %s", *lastURL)
	}

	if _, err := c.DefiPrices(ctx, []string{"coingecko:bitcoin", "base:0xabc"}); err != nil {
		t.Fatalf("DefiPrices: %v", err)
	}
	if !strings.Contains(*lastURL, "/v1/defillama/prices/coingecko:bitcoin,base:0xabc") {
		t.Errorf("path = %s", *lastURL)
	}
}

func TestDefiYieldsPassesParams(t *testing.T) {
	server, lastURL, _ := newPassthroughServer(t)
	defer server.Close()
	c := newPassthroughClient(t, server.URL)

	if _, err := c.DefiYields(context.Background(), map[string]string{"chain": "Base"}); err != nil {
		t.Fatalf("DefiYields: %v", err)
	}
	if !strings.Contains(*lastURL, "/v1/defillama/yields") || !strings.Contains(*lastURL, "chain=Base") {
		t.Errorf("path = %s", *lastURL)
	}
}

func TestDefiWrapsArrayResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"aave"},{"name":"uniswap"}]`))
	}))
	defer server.Close()
	c := newPassthroughClient(t, server.URL)

	out, err := c.DefiProtocols(context.Background())
	if err != nil {
		t.Fatalf("DefiProtocols: %v", err)
	}
	arr, ok := out["data"].([]any)
	if !ok || len(arr) != 2 {
		t.Errorf("expected wrapped array, got %+v", out)
	}
}

func TestDefiValidation(t *testing.T) {
	c := newPassthroughClient(t, "https://example.invalid")
	ctx := context.Background()
	if _, err := c.Defi(ctx, "", nil); err == nil {
		t.Error("expected error for blank path")
	}
	if _, err := c.DefiProtocol(ctx, " "); err == nil {
		t.Error("expected error for blank slug")
	}
	if _, err := c.DefiPrices(ctx, nil); err == nil {
		t.Error("expected error for empty coins")
	}
}

// ── 0x DEX ───────────────────────────────────────────────────────────────

func TestDexQuote(t *testing.T) {
	server, lastURL, _ := newPassthroughServer(t)
	defer server.Close()
	c := newPassthroughClient(t, server.URL)

	if _, err := c.DexQuote(context.Background(), map[string]string{
		"chainId": "8453", "sellToken": "0xa", "buyToken": "0xb", "sellAmount": "1000",
	}); err != nil {
		t.Fatalf("DexQuote: %v", err)
	}
	if !strings.Contains(*lastURL, "/v1/zerox/quote") || !strings.Contains(*lastURL, "chainId=8453") {
		t.Errorf("path = %s", *lastURL)
	}
}

func TestDexGaslessSubmitPosts(t *testing.T) {
	server, lastURL, lastBody := newPassthroughServer(t)
	defer server.Close()
	c := newPassthroughClient(t, server.URL)

	if _, err := c.DexGaslessSubmit(context.Background(), map[string]any{
		"trade": map[string]any{"signature": "0xsig"},
	}); err != nil {
		t.Fatalf("DexGaslessSubmit: %v", err)
	}
	if !strings.HasSuffix(*lastURL, "/v1/zerox/gasless/submit") {
		t.Errorf("path = %s", *lastURL)
	}
	trade, _ := (*lastBody)["trade"].(map[string]any)
	if trade["signature"] != "0xsig" {
		t.Errorf("body = %+v", *lastBody)
	}
}

func TestDexGaslessStatusPath(t *testing.T) {
	server, lastURL, _ := newPassthroughServer(t)
	defer server.Close()
	c := newPassthroughClient(t, server.URL)

	if _, err := c.DexGaslessStatus(context.Background(), "0xtradehash"); err != nil {
		t.Fatalf("DexGaslessStatus: %v", err)
	}
	if !strings.Contains(*lastURL, "/v1/zerox/gasless/status/0xtradehash") {
		t.Errorf("path = %s", *lastURL)
	}
}

func TestDexValidation(t *testing.T) {
	c := newPassthroughClient(t, "https://example.invalid")
	ctx := context.Background()
	if _, err := c.Dex(ctx, "", nil); err == nil {
		t.Error("expected error for blank path")
	}
	if _, err := c.DexGaslessSubmit(ctx, nil); err == nil {
		t.Error("expected error for empty submit body")
	}
	if _, err := c.DexGaslessStatus(ctx, " "); err == nil {
		t.Error("expected error for blank trade hash")
	}
}

// ── Modal ────────────────────────────────────────────────────────────────

func TestModalLifecycle(t *testing.T) {
	server, lastURL, lastBody := newPassthroughServer(t)
	defer server.Close()
	c := newPassthroughClient(t, server.URL)
	ctx := context.Background()

	if _, err := c.ModalSandboxCreate(ctx, map[string]any{"image": "python:3.11"}); err != nil {
		t.Fatalf("ModalSandboxCreate: %v", err)
	}
	if !strings.HasSuffix(*lastURL, "/v1/modal/sandbox/create") || (*lastBody)["image"] != "python:3.11" {
		t.Errorf("create: url=%s body=%+v", *lastURL, *lastBody)
	}

	if _, err := c.ModalSandboxExec(ctx, "sb_123", []string{"python", "-c", "print(1)"}); err != nil {
		t.Fatalf("ModalSandboxExec: %v", err)
	}
	if !strings.HasSuffix(*lastURL, "/v1/modal/sandbox/exec") || (*lastBody)["sandbox_id"] != "sb_123" {
		t.Errorf("exec: url=%s body=%+v", *lastURL, *lastBody)
	}

	if _, err := c.ModalSandboxStatus(ctx, "sb_123"); err != nil {
		t.Fatalf("ModalSandboxStatus: %v", err)
	}
	if !strings.HasSuffix(*lastURL, "/v1/modal/sandbox/status") {
		t.Errorf("status: url=%s", *lastURL)
	}

	if _, err := c.ModalSandboxTerminate(ctx, "sb_123"); err != nil {
		t.Fatalf("ModalSandboxTerminate: %v", err)
	}
	if !strings.HasSuffix(*lastURL, "/v1/modal/sandbox/terminate") {
		t.Errorf("terminate: url=%s", *lastURL)
	}
}

func TestModalValidation(t *testing.T) {
	c := newPassthroughClient(t, "https://example.invalid")
	ctx := context.Background()
	if _, err := c.Modal(ctx, "", nil); err == nil {
		t.Error("expected error for blank path")
	}
	if _, err := c.ModalSandboxExec(ctx, "", []string{"ls"}); err == nil {
		t.Error("expected error for blank sandbox id")
	}
	if _, err := c.ModalSandboxExec(ctx, "sb_1", nil); err == nil {
		t.Error("expected error for empty command")
	}
}
