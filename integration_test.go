// +build integration

package blockrun

import (
	"os"
	"testing"
)

// TestRealAPI tests against the real BlockRun API.
// Run with: go test -tags=integration -v
func TestRealAPI(t *testing.T) {
	privateKey := os.Getenv("BASE_CHAIN_WALLET_KEY")
	if privateKey == "" {
		t.Skip("BASE_CHAIN_WALLET_KEY not set, skipping integration test")
	}

	client, err := NewLLMClient(privateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	t.Logf("Wallet address: %s", client.GetWalletAddress())

	// Test 1: List models (no payment required)
	t.Run("ListModels", func(t *testing.T) {
		models, err := client.ListModels()
		if err != nil {
			t.Fatalf("ListModels failed: %v", err)
		}
		t.Logf("Found %d models", len(models))
		if len(models) == 0 {
			t.Error("Expected at least one model")
		}
	})

	// Test 2: Simple chat (requires payment)
	t.Run("Chat", func(t *testing.T) {
		response, err := client.Chat("openai/gpt-4o-mini", "What is 2+2? Reply with just the number.")
		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}
		t.Logf("Response: %s", response)
		if response == "" {
			t.Error("Expected non-empty response")
		}
	})
}
