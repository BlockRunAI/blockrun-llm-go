package blockrun

import (
	"context"
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
	t.Setenv("BLOCKRUN_WALLET_KEY", "")
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

	models, err := client.ListModels(context.Background())
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

	models, err := client.ListImageModels(context.Background())
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

	models, err := client.ListAllModels(context.Background())
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

func TestChatCompletionWithTools(t *testing.T) {
	// Create a mock server that verifies tools are sent and returns tool_calls
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("Expected path /v1/chat/completions, got %s", r.URL.Path)
		}

		// Decode request body and verify tools/tool_choice are present
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}

		// Verify tools are in the request
		tools, ok := reqBody["tools"]
		if !ok {
			t.Fatal("Expected 'tools' in request body")
		}
		toolsList, ok := tools.([]any)
		if !ok || len(toolsList) != 1 {
			t.Fatalf("Expected 1 tool, got %v", tools)
		}

		// Verify tool_choice is in the request
		toolChoice, ok := reqBody["tool_choice"]
		if !ok {
			t.Fatal("Expected 'tool_choice' in request body")
		}
		if toolChoice != "auto" {
			t.Errorf("Expected tool_choice 'auto', got %v", toolChoice)
		}

		// Return a response with tool_calls
		resp := ChatResponse{
			ID:      "chatcmpl-123",
			Object:  "chat.completion",
			Created: 1700000000,
			Model:   "openai/gpt-4o",
			Choices: []Choice{
				{
					Index: 0,
					Message: ChatMessage{
						Role: "assistant",
						ToolCalls: []ToolCall{
							{
								ID:   "call_abc123",
								Type: "function",
								Function: ToolCallFunction{
									Name:      "get_weather",
									Arguments: `{"location":"San Francisco","unit":"celsius"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
			Usage: Usage{
				PromptTokens:     50,
				CompletionTokens: 20,
				TotalTokens:      70,
			},
		}

		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	messages := []ChatMessage{
		{Role: "user", Content: "What's the weather in San Francisco?"},
	}

	opts := &ChatCompletionOptions{
		Tools: []Tool{
			{
				Type: "function",
				Function: ToolFunction{
					Name:        "get_weather",
					Description: "Get current weather for a location",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"location": map[string]any{
								"type":        "string",
								"description": "City name",
							},
							"unit": map[string]any{
								"type": "string",
								"enum": []string{"celsius", "fahrenheit"},
							},
						},
						"required": []string{"location"},
					},
				},
			},
		},
		ToolChoice: "auto",
	}

	resp, err := client.ChatCompletion(context.Background(), "openai/gpt-4o", messages, opts)
	if err != nil {
		t.Fatalf("Failed to call ChatCompletion with tools: %v", err)
	}

	if len(resp.Choices) != 1 {
		t.Fatalf("Expected 1 choice, got %d", len(resp.Choices))
	}

	choice := resp.Choices[0]

	// Verify tool_calls are deserialized
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(choice.Message.ToolCalls))
	}

	tc := choice.Message.ToolCalls[0]
	if tc.ID != "call_abc123" {
		t.Errorf("Expected tool call ID 'call_abc123', got '%s'", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("Expected tool call type 'function', got '%s'", tc.Type)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("Expected function name 'get_weather', got '%s'", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"location":"San Francisco","unit":"celsius"}` {
		t.Errorf("Unexpected arguments: %s", tc.Function.Arguments)
	}

	if choice.FinishReason != "tool_calls" {
		t.Errorf("Expected finish_reason 'tool_calls', got '%s'", choice.FinishReason)
	}
}
