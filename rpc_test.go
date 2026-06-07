package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewRPCClient_RequiresKey(t *testing.T) {
	t.Setenv("BLOCKRUN_WALLET_KEY", "")
	t.Setenv("BASE_CHAIN_WALLET_KEY", "")

	if _, err := NewRPCClient(""); err == nil {
		t.Fatal("expected error when no private key is configured")
	}
}

func TestNewRPCClient_EnvKey(t *testing.T) {
	t.Setenv("BLOCKRUN_WALLET_KEY", testPrivateKey)

	c, err := NewRPCClient("")
	if err != nil {
		t.Fatalf("NewRPCClient: %v", err)
	}
	if c.GetWalletAddress() != testWalletAddress {
		t.Errorf("address mismatch: got %s", c.GetWalletAddress())
	}
}

func TestRPCNetworkRegistry_MirrorsBackend(t *testing.T) {
	// 40 curated chains: 29 EVM + 11 non-EVM (backend src/lib/tatum.ts).
	if len(RPCSupportedNetworks) != 40 {
		t.Errorf("expected 40 curated networks, got %d", len(RPCSupportedNetworks))
	}
	seen := map[string]bool{}
	for _, n := range RPCSupportedNetworks {
		if seen[n] {
			t.Errorf("duplicate network %q", n)
		}
		seen[n] = true
	}
	for _, must := range []string{"ethereum", "base", "solana", "bitcoin", "ripple", "sui"} {
		if !seen[must] {
			t.Errorf("missing network %q", must)
		}
	}
	for alias, canonical := range RPCNetworkAliases {
		if !seen[canonical] {
			t.Errorf("alias %q resolves to uncurated network %q", alias, canonical)
		}
	}
	if RPCNetworkAliases["xrpl"] != "ripple" || RPCNetworkAliases["sol"] != "solana" {
		t.Error("expected xrpl->ripple and sol->solana aliases")
	}
	if RPCPriceUSD != 0.002 {
		t.Errorf("expected RPCPriceUSD 0.002, got %v", RPCPriceUSD)
	}
}

func TestRPCClient_CallValidation(t *testing.T) {
	c, err := NewRPCClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewRPCClient: %v", err)
	}

	if _, err := c.Call(context.Background(), "Bad Network!", "eth_blockNumber", nil); err == nil {
		t.Error("expected error for malformed network")
	}
	if _, err := c.Call(context.Background(), "ethereum", "  ", nil); err == nil {
		t.Error("expected error for blank method")
	}
}

func TestRPCClient_Call(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/rpc/ethereum") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Network", "ethereum")
		w.Header().Set("X-Cache", "HIT")
		w.Header().Set("X-Payment-Receipt", "0xdeadbeef")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  "0x1499f7c",
		})
	}))
	defer server.Close()

	c, err := NewRPCClient(testPrivateKey, WithRPCAPIURL(server.URL))
	if err != nil {
		t.Fatalf("NewRPCClient: %v", err)
	}

	resp, err := c.Call(context.Background(), "ethereum", "eth_getBalance", []any{"0xabc", "latest"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if sawBody["jsonrpc"] != "2.0" || sawBody["method"] != "eth_getBalance" {
		t.Errorf("unexpected body: %+v", sawBody)
	}
	params, ok := sawBody["params"].([]any)
	if !ok || len(params) != 2 || params[0] != "0xabc" {
		t.Errorf("unexpected params: %+v", sawBody["params"])
	}

	var result string
	if err := json.Unmarshal(resp.Result, &result); err != nil || result != "0x1499f7c" {
		t.Errorf("unexpected result: %s (err %v)", resp.Result, err)
	}
	if resp.Network != "ethereum" {
		t.Errorf("expected network ethereum, got %q", resp.Network)
	}
	if !resp.CacheHit {
		t.Error("expected CacheHit true")
	}
	if resp.TxHash != "0xdeadbeef" {
		t.Errorf("expected tx hash, got %q", resp.TxHash)
	}
}

func TestRPCClient_CallOmitsNilParams(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sawBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": "0x1"})
	}))
	defer server.Close()

	c, err := NewRPCClient(testPrivateKey, WithRPCAPIURL(server.URL))
	if err != nil {
		t.Fatalf("NewRPCClient: %v", err)
	}
	if _, err := c.Call(context.Background(), "base", "eth_blockNumber", nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if _, present := sawBody["params"]; present {
		t.Errorf("params should be omitted when nil, got %+v", sawBody["params"])
	}
}

func TestRPCClient_CallSurfacesJSONRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error":   map[string]any{"code": -32601, "message": "no method"},
		})
	}))
	defer server.Close()

	c, err := NewRPCClient(testPrivateKey, WithRPCAPIURL(server.URL))
	if err != nil {
		t.Fatalf("NewRPCClient: %v", err)
	}
	resp, err := c.Call(context.Background(), "ethereum", "eth_bogus", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != -32601 || resp.Error.Message != "no method" {
		t.Errorf("expected JSON-RPC error to pass through, got %+v", resp.Error)
	}
}

func TestRPCClient_Batch(t *testing.T) {
	var sawBody []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode batch body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Network", "polygon")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"jsonrpc": "2.0", "id": 1, "result": "0x10"},
			{"jsonrpc": "2.0", "id": 7, "result": "0x3b9aca00"},
		})
	}))
	defer server.Close()

	c, err := NewRPCClient(testPrivateKey, WithRPCAPIURL(server.URL))
	if err != nil {
		t.Fatalf("NewRPCClient: %v", err)
	}

	out, err := c.Batch(context.Background(), "polygon", []RPCBatchRequest{
		{Method: "eth_blockNumber"},
		{Method: "eth_gasPrice", ID: 7},
	})
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}

	if len(sawBody) != 2 {
		t.Fatalf("expected 2 batch entries, got %d", len(sawBody))
	}
	if sawBody[0]["method"] != "eth_blockNumber" || sawBody[0]["id"] != float64(1) {
		t.Errorf("entry 0 wrong: %+v", sawBody[0])
	}
	if sawBody[1]["method"] != "eth_gasPrice" || sawBody[1]["id"] != float64(7) {
		t.Errorf("entry 1 wrong: %+v", sawBody[1])
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(out))
	}
	if out[0].Network != "polygon" || out[1].Network != "polygon" {
		t.Error("expected gateway network metadata on every batch element")
	}
}

func TestRPCClient_BatchValidation(t *testing.T) {
	c, err := NewRPCClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewRPCClient: %v", err)
	}

	if _, err := c.Batch(context.Background(), "ethereum", nil); err == nil {
		t.Error("expected error for empty batch")
	}
	if _, err := c.Batch(context.Background(), "ethereum", []RPCBatchRequest{{Method: " "}}); err == nil {
		t.Error("expected error for blank method in batch")
	}
}
