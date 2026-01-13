package blockrun

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Test wallet for testing purposes only - never use in production
const testPrivateKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
const testWalletAddress = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"

func TestNewLLMClient(t *testing.T) {
	// Test with valid private key
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if client.GetWalletAddress() != testWalletAddress {
		t.Errorf("Expected wallet address %s, got %s", testWalletAddress, client.GetWalletAddress())
	}
}

func TestNewLLMClientWithoutPrefix(t *testing.T) {
	// Test without 0x prefix
	key := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	client, err := NewLLMClient(key)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if client.GetWalletAddress() != testWalletAddress {
		t.Errorf("Expected wallet address %s, got %s", testWalletAddress, client.GetWalletAddress())
	}
}

func TestNewLLMClientInvalidKey(t *testing.T) {
	// Test with invalid key
	_, err := NewLLMClient("invalid-key")
	if err == nil {
		t.Error("Expected error for invalid key, got nil")
	}
}

func TestNewLLMClientEmptyKey(t *testing.T) {
	// Clear env var for test
	t.Setenv("BASE_CHAIN_WALLET_KEY", "")

	_, err := NewLLMClient("")
	if err == nil {
		t.Error("Expected error for empty key, got nil")
	}
}

func TestCreatePaymentPayload(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	payload, err := CreatePaymentPayload(
		client.privateKey,
		"0x1234567890123456789012345678901234567890",
		"1000",
		"eip155:8453",
		"https://blockrun.ai/api/v1/chat/completions",
		"Test payment",
		300,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to create payment payload: %v", err)
	}

	// Decode and verify structure
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("Failed to decode payload: %v", err)
	}

	var pp PaymentPayload
	if err := json.Unmarshal(decoded, &pp); err != nil {
		t.Fatalf("Failed to unmarshal payload: %v", err)
	}

	if pp.X402Version != 2 {
		t.Errorf("Expected x402Version 2, got %d", pp.X402Version)
	}

	if pp.Accepted.Amount != "1000" {
		t.Errorf("Expected amount 1000, got %s", pp.Accepted.Amount)
	}

	if pp.Payload.Authorization.From != testWalletAddress {
		t.Errorf("Expected from %s, got %s", testWalletAddress, pp.Payload.Authorization.From)
	}
}

func TestParsePaymentRequired(t *testing.T) {
	// Create a sample payment requirement
	req := PaymentRequirement{
		X402Version: 2,
		Accepts: []PaymentOption{
			{
				Scheme:            "exact",
				Network:           "eip155:8453",
				Amount:            "1000",
				Asset:             USDCBase,
				PayTo:             "0x1234567890123456789012345678901234567890",
				MaxTimeoutSeconds: 300,
			},
		},
		Resource: ResourceInfo{
			URL:         "https://blockrun.ai/api/v1/chat/completions",
			Description: "Test resource",
			MimeType:    "application/json",
		},
	}

	// Encode it
	jsonData, _ := json.Marshal(req)
	encoded := base64.StdEncoding.EncodeToString(jsonData)

	// Parse it back
	parsed, err := ParsePaymentRequired(encoded)
	if err != nil {
		t.Fatalf("Failed to parse payment required: %v", err)
	}

	if parsed.X402Version != 2 {
		t.Errorf("Expected x402Version 2, got %d", parsed.X402Version)
	}

	if len(parsed.Accepts) != 1 {
		t.Errorf("Expected 1 accept option, got %d", len(parsed.Accepts))
	}

	if parsed.Accepts[0].Amount != "1000" {
		t.Errorf("Expected amount 1000, got %s", parsed.Accepts[0].Amount)
	}
}

func TestListModels(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("Expected path /v1/models, got %s", r.URL.Path)
		}

		models := struct {
			Data []Model `json:"data"`
		}{
			Data: []Model{
				{ID: "openai/gpt-4o", Name: "GPT-4o", Provider: "openai", InputPrice: 2.5, OutputPrice: 10.0},
				{ID: "anthropic/claude-sonnet-4", Name: "Claude Sonnet 4", Provider: "anthropic", InputPrice: 3.0, OutputPrice: 15.0},
			},
		}

		json.NewEncoder(w).Encode(models)
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	models, err := client.ListModels()
	if err != nil {
		t.Fatalf("Failed to list models: %v", err)
	}

	if len(models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(models))
	}

	if models[0].ID != "openai/gpt-4o" {
		t.Errorf("Expected first model openai/gpt-4o, got %s", models[0].ID)
	}
}

func TestValidation(t *testing.T) {
	// Test private key validation
	if err := ValidatePrivateKey(""); err == nil {
		t.Error("Expected error for empty private key")
	}

	if err := ValidatePrivateKey(testPrivateKey); err != nil {
		t.Errorf("Unexpected error for valid private key: %v", err)
	}

	// Test model validation
	if err := ValidateModel(""); err == nil {
		t.Error("Expected error for empty model")
	}

	if err := ValidateModel("openai/gpt-4o"); err != nil {
		t.Errorf("Unexpected error for valid model: %v", err)
	}

	// Test temperature validation
	if err := ValidateTemperature(-0.5); err == nil {
		t.Error("Expected error for negative temperature")
	}

	if err := ValidateTemperature(0.7); err != nil {
		t.Errorf("Unexpected error for valid temperature: %v", err)
	}
}

func TestGetSpending(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
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

func TestListImageModels(t *testing.T) {
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

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
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

func TestListAllModels(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			models := struct {
				Data []Model `json:"data"`
			}{
				Data: []Model{
					{ID: "openai/gpt-4o", Name: "GPT-4o", Provider: "openai", InputPrice: 2.5, OutputPrice: 10.0},
				},
			}
			json.NewEncoder(w).Encode(models)
		case "/v1/images/models":
			models := struct {
				Data []ImageModel `json:"data"`
			}{
				Data: []ImageModel{
					{ID: "google/nano-banana", Name: "Nano Banana", Provider: "google", PricePerImage: 0.01, Available: true},
				},
			}
			json.NewEncoder(w).Encode(models)
		default:
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	models, err := client.ListAllModels()
	if err != nil {
		t.Fatalf("Failed to list all models: %v", err)
	}

	if len(models) != 2 {
		t.Errorf("Expected 2 total models, got %d", len(models))
	}

	// Check that we have both types
	foundLLM := false
	foundImage := false
	for _, m := range models {
		if m.Type == "llm" {
			foundLLM = true
			if m.ID != "openai/gpt-4o" {
				t.Errorf("Expected LLM model openai/gpt-4o, got %s", m.ID)
			}
		}
		if m.Type == "image" {
			foundImage = true
			if m.ID != "google/nano-banana" {
				t.Errorf("Expected image model google/nano-banana, got %s", m.ID)
			}
		}
	}

	if !foundLLM {
		t.Error("Expected to find LLM model")
	}
	if !foundImage {
		t.Error("Expected to find image model")
	}
}
