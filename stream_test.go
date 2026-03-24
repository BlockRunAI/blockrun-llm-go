package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatCompletionStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("Expected path /v1/chat/completions, got %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Expected Accept: text/event-stream, got %s", r.Header.Get("Accept"))
		}

		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		if reqBody["stream"] != true {
			t.Errorf("Expected stream=true in request body, got %v", reqBody["stream"])
		}

		// Write SSE response
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support Flusher")
		}

		chunks := []string{
			`data: {"id":"1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}`,
			``,
			`data: {"id":"1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":" world"}}]}`,
			``,
			`data: [DONE]`,
		}

		for _, chunk := range chunks {
			fmt.Fprintf(w, "%s\n", chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	messages := []ChatMessage{
		{Role: "user", Content: "Say hello"},
	}

	stream, err := client.ChatCompletionStream(context.Background(), "gpt-4o", messages, nil)
	if err != nil {
		t.Fatalf("Failed to create stream: %v", err)
	}
	defer stream.Close()

	// Read first chunk
	chunk1, err := stream.Next()
	if err != nil {
		t.Fatalf("Failed to read first chunk: %v", err)
	}
	if chunk1 == nil {
		t.Fatal("Expected first chunk, got nil")
	}
	if chunk1.ID != "1" {
		t.Errorf("Expected chunk ID '1', got '%s'", chunk1.ID)
	}
	if len(chunk1.Choices) != 1 {
		t.Fatalf("Expected 1 choice, got %d", len(chunk1.Choices))
	}
	if chunk1.Choices[0].Delta.Role != "assistant" {
		t.Errorf("Expected role 'assistant', got '%s'", chunk1.Choices[0].Delta.Role)
	}
	if chunk1.Choices[0].Delta.Content != "Hello" {
		t.Errorf("Expected content 'Hello', got '%s'", chunk1.Choices[0].Delta.Content)
	}

	// Read second chunk
	chunk2, err := stream.Next()
	if err != nil {
		t.Fatalf("Failed to read second chunk: %v", err)
	}
	if chunk2 == nil {
		t.Fatal("Expected second chunk, got nil")
	}
	if chunk2.Choices[0].Delta.Content != " world" {
		t.Errorf("Expected content ' world', got '%s'", chunk2.Choices[0].Delta.Content)
	}

	// Read DONE
	chunk3, err := stream.Next()
	if err != nil {
		t.Fatalf("Unexpected error after DONE: %v", err)
	}
	if chunk3 != nil {
		t.Error("Expected nil chunk after DONE")
	}

	// Subsequent calls should also return nil, nil
	chunk4, err := stream.Next()
	if err != nil {
		t.Fatalf("Unexpected error on post-DONE call: %v", err)
	}
	if chunk4 != nil {
		t.Error("Expected nil chunk on post-DONE call")
	}
}

func TestChatCompletionStreamClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support Flusher")
		}

		fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi\"}}]}\n\n")
		flusher.Flush()

		fmt.Fprintf(w, "data: [DONE]\n")
		flusher.Flush()
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	messages := []ChatMessage{
		{Role: "user", Content: "Hi"},
	}

	stream, err := client.ChatCompletionStream(context.Background(), "gpt-4o", messages, nil)
	if err != nil {
		t.Fatalf("Failed to create stream: %v", err)
	}

	// Close without reading all chunks
	if err := stream.Close(); err != nil {
		t.Fatalf("Failed to close stream: %v", err)
	}

	// After close, Next should return nil, nil (done is set)
	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Unexpected error after close: %v", err)
	}
	if chunk != nil {
		t.Error("Expected nil chunk after close")
	}
}

func TestChatCompletionStreamValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Empty model
	_, err = client.ChatCompletionStream(context.Background(), "", []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("Expected error for empty model")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Errorf("Expected ValidationError, got %T", err)
	}

	// Empty messages
	_, err = client.ChatCompletionStream(context.Background(), "gpt-4o", nil, nil)
	if err == nil {
		t.Error("Expected error for nil messages")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Errorf("Expected ValidationError, got %T", err)
	}

	// Empty messages slice
	_, err = client.ChatCompletionStream(context.Background(), "gpt-4o", []ChatMessage{}, nil)
	if err == nil {
		t.Error("Expected error for empty messages")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestChatCompletionStreamWithFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support Flusher")
		}

		fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Done\"}, \"finish_reason\":\"stop\"}]}\n\n")
		flusher.Flush()

		fmt.Fprintf(w, "data: [DONE]\n")
		flusher.Flush()
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	stream, err := client.ChatCompletionStream(context.Background(), "gpt-4o", []ChatMessage{{Role: "user", Content: "test"}}, nil)
	if err != nil {
		t.Fatalf("Failed to create stream: %v", err)
	}
	defer stream.Close()

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Failed to read chunk: %v", err)
	}

	if chunk.Choices[0].FinishReason != "stop" {
		t.Errorf("Expected finish_reason 'stop', got '%s'", chunk.Choices[0].FinishReason)
	}
}

func TestChatCompletionStreamCollectContent(t *testing.T) {
	// Test collecting full content from a stream
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support Flusher")
		}

		parts := []string{"The ", "quick ", "brown ", "fox"}
		for _, part := range parts {
			fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"%s\"}}]}\n\n", part)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n")
		flusher.Flush()
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	stream, err := client.ChatCompletionStream(context.Background(), "gpt-4o", []ChatMessage{{Role: "user", Content: "test"}}, nil)
	if err != nil {
		t.Fatalf("Failed to create stream: %v", err)
	}
	defer stream.Close()

	var fullContent string
	for {
		chunk, err := stream.Next()
		if err != nil {
			t.Fatalf("Error reading stream: %v", err)
		}
		if chunk == nil {
			break
		}
		if len(chunk.Choices) > 0 {
			fullContent += chunk.Choices[0].Delta.Content
		}
	}

	expected := "The quick brown fox"
	if fullContent != expected {
		t.Errorf("Expected collected content '%s', got '%s'", expected, fullContent)
	}
}
