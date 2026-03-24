package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetupAgentWalletFromEnv(t *testing.T) {
	t.Setenv("BASE_CHAIN_WALLET_KEY", testPrivateKey)
	t.Setenv("BLOCKRUN_WALLET_KEY", "")

	client, err := SetupAgentWallet()
	if err != nil {
		t.Fatalf("Failed to setup agent wallet: %v", err)
	}

	if client.GetWalletAddress() != testWalletAddress {
		t.Errorf("Expected wallet address %s, got %s", testWalletAddress, client.GetWalletAddress())
	}
}

func TestSetupAgentWalletWithOptions(t *testing.T) {
	t.Setenv("BLOCKRUN_WALLET_KEY", testPrivateKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	client, err := SetupAgentWallet(WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to setup agent wallet with options: %v", err)
	}

	if client.GetWalletAddress() != testWalletAddress {
		t.Errorf("Expected wallet address %s, got %s", testWalletAddress, client.GetWalletAddress())
	}
}

func TestStatus(t *testing.T) {
	// Create a mock RPC server that returns a balance
	rpcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a hex-encoded balance (1.5 USDC = 1500000 = 0x16e360)
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  "0x000000000000000000000000000000000000000000000000000000000016e360",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer rpcServer.Close()

	// Override RPC endpoints for testing
	origRPCs := baseMainnetRPCs
	baseMainnetRPCs = []string{rpcServer.URL}
	defer func() { baseMainnetRPCs = origRPCs }()

	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	address, balance, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}

	if address != testWalletAddress {
		t.Errorf("Expected address %s, got %s", testWalletAddress, address)
	}

	if balance != 1.5 {
		t.Errorf("Expected balance 1.5, got %f", balance)
	}
}
