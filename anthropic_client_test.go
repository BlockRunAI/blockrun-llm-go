package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewAnthropicClient(t *testing.T) {
	client, err := NewAnthropicClient("0x" + "a" + string(make([]byte, 63)))
	_ = client
	// Should fail with invalid key, not a missing-key error
	if err == nil {
		t.Error("expected error for invalid key")
	}
}

func TestAnthropicMessagesCreate(t *testing.T) {
	// Mock server returning a valid AnthropicResponse
	mockResp := AnthropicResponse{
		ID:   "msg_test123",
		Type: "message",
		Role: "assistant",
		Content: []AnthropicContentBlock{
			{Type: "text", Text: "Hello! How can I help?"},
		},
		Model:      "claude-sonnet-4-6",
		StopReason: "end_turn",
		Usage:      AnthropicUsage{InputTokens: 10, OutputTokens: 8},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockResp)
	}))
	defer server.Close()

	client, err := NewAnthropicClient(testPrivateKey, WithAnthropicAPIURL(server.URL))
	if err != nil {
		t.Fatalf("NewAnthropicClient: %v", err)
	}

	resp, err := client.Messages.Create(context.Background(), AnthropicCreateParams{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 1024,
		Messages: []AnthropicMessage{
			{Role: "user", Content: "Hello!"},
		},
	})
	if err != nil {
		t.Fatalf("Messages.Create: %v", err)
	}

	if resp.ID != "msg_test123" {
		t.Errorf("expected ID msg_test123, got %s", resp.ID)
	}
	if resp.Text() != "Hello! How can I help?" {
		t.Errorf("unexpected text: %s", resp.Text())
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("expected stop_reason end_turn, got %s", resp.StopReason)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", resp.Usage.InputTokens)
	}
}

func TestAnthropicMessagesCreateWithSystem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["system"] != "You are a helpful assistant." {
			t.Errorf("expected system prompt, got %v", body["system"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AnthropicResponse{
			ID:         "msg_sys",
			Type:       "message",
			Role:       "assistant",
			Content:    []AnthropicContentBlock{{Type: "text", Text: "Sure!"}},
			Model:      "claude-sonnet-4-6",
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	client, err := NewAnthropicClient(testPrivateKey, WithAnthropicAPIURL(server.URL))
	if err != nil {
		t.Fatalf("NewAnthropicClient: %v", err)
	}

	resp, err := client.Messages.Create(context.Background(), AnthropicCreateParams{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 512,
		System:    "You are a helpful assistant.",
		Messages:  []AnthropicMessage{{Role: "user", Content: "Help?"}},
	})
	if err != nil {
		t.Fatalf("Messages.Create: %v", err)
	}
	if resp.Text() != "Sure!" {
		t.Errorf("unexpected text: %s", resp.Text())
	}
}

func TestAnthropicValidation(t *testing.T) {
	client, err := NewAnthropicClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewAnthropicClient: %v", err)
	}

	_, err = client.Messages.Create(context.Background(), AnthropicCreateParams{
		MaxTokens: 1024,
		Messages:  []AnthropicMessage{{Role: "user", Content: "Hello"}},
	})
	if err == nil {
		t.Error("expected error for missing model")
	}

	_, err = client.Messages.Create(context.Background(), AnthropicCreateParams{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 1024,
	})
	if err == nil {
		t.Error("expected error for empty messages")
	}

	_, err = client.Messages.Create(context.Background(), AnthropicCreateParams{
		Model:    "claude-sonnet-4-6",
		Messages: []AnthropicMessage{{Role: "user", Content: "Hello"}},
	})
	if err == nil {
		t.Error("expected error for zero max_tokens")
	}
}

func TestAnthropicResponseText(t *testing.T) {
	resp := &AnthropicResponse{
		Content: []AnthropicContentBlock{
			{Type: "text", Text: "Hello "},
			{Type: "tool_use", ID: "tool1", Name: "search"},
			{Type: "text", Text: "world"},
		},
	}
	if got := resp.Text(); got != "Hello world" {
		t.Errorf("Text() = %q, want %q", got, "Hello world")
	}
}
