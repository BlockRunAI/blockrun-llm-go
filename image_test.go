package blockrun

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewImageClient(t *testing.T) {
	client, err := NewImageClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create image client: %v", err)
	}

	if client.GetWalletAddress() != testWalletAddress {
		t.Errorf("Expected wallet address %s, got %s", testWalletAddress, client.GetWalletAddress())
	}
}

func TestNewImageClientEmptyKey(t *testing.T) {
	// Clear env vars for test
	t.Setenv("BLOCKRUN_WALLET_KEY", "")
	t.Setenv("BASE_CHAIN_WALLET_KEY", "")

	_, err := NewImageClient("")
	if err == nil {
		t.Error("Expected error for empty key, got nil")
	}
}

func TestImageClientListModels(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/models" {
			t.Errorf("Expected path /v1/images/models, got %s", r.URL.Path)
		}

		models := struct {
			Data []ImageModel `json:"data"`
		}{
			Data: []ImageModel{
				{ID: "google/nano-banana", Name: "Nano Banana", Provider: "google", PricePerImage: 0.01, Available: true},
				{ID: "openai/dall-e-3", Name: "DALL-E 3", Provider: "openai", PricePerImage: 0.04, Available: true},
			},
		}

		json.NewEncoder(w).Encode(models)
	}))
	defer server.Close()

	client, err := NewImageClient(testPrivateKey, WithImageAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create image client: %v", err)
	}

	models, err := client.ListImageModels()
	if err != nil {
		t.Fatalf("Failed to list image models: %v", err)
	}

	if len(models) != 2 {
		t.Errorf("Expected 2 image models, got %d", len(models))
	}

	if models[0].ID != "google/nano-banana" {
		t.Errorf("Expected first model google/nano-banana, got %s", models[0].ID)
	}
}

func TestImageClientGetSpending(t *testing.T) {
	client, err := NewImageClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create image client: %v", err)
	}

	// Initial spending should be zero
	spending := client.GetSpending()
	if spending.TotalUSD != 0 {
		t.Errorf("Expected initial TotalUSD 0, got %f", spending.TotalUSD)
	}
	if spending.Calls != 0 {
		t.Errorf("Expected initial Calls 0, got %d", spending.Calls)
	}
}
