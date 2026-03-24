package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchWithOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/search" {
			t.Errorf("Expected path /v1/search, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}

		if body["query"] != "latest AI news" {
			t.Errorf("Expected query 'latest AI news', got %v", body["query"])
		}
		if body["max_results"] != float64(5) {
			t.Errorf("Expected max_results 5, got %v", body["max_results"])
		}
		sources, ok := body["sources"].([]any)
		if !ok || len(sources) != 2 {
			t.Errorf("Expected 2 sources, got %v", body["sources"])
		}
		if body["from_date"] != "2026-01-01" {
			t.Errorf("Expected from_date '2026-01-01', got %v", body["from_date"])
		}
		if body["to_date"] != "2026-03-24" {
			t.Errorf("Expected to_date '2026-03-24', got %v", body["to_date"])
		}

		resp := SearchResult{
			Query:       "latest AI news",
			Summary:     "Here are the latest AI developments...",
			Citations:   []string{"https://example.com/article1", "https://example.com/article2"},
			SourcesUsed: 3,
			Model:       "blockrun/search-v1",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	result, err := client.Search(context.Background(), "latest AI news", &SearchOptions{
		Sources:    []string{"web", "news"},
		MaxResults: 5,
		FromDate:   "2026-01-01",
		ToDate:     "2026-03-24",
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if result.Query != "latest AI news" {
		t.Errorf("Expected query 'latest AI news', got %s", result.Query)
	}
	if result.Summary != "Here are the latest AI developments..." {
		t.Errorf("Unexpected summary: %s", result.Summary)
	}
	if len(result.Citations) != 2 {
		t.Errorf("Expected 2 citations, got %d", len(result.Citations))
	}
	if result.SourcesUsed != 3 {
		t.Errorf("Expected 3 sources used, got %d", result.SourcesUsed)
	}
	if result.Model != "blockrun/search-v1" {
		t.Errorf("Expected model 'blockrun/search-v1', got %s", result.Model)
	}
}

func TestSearchWithoutOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}

		if body["query"] != "what is x402" {
			t.Errorf("Expected query 'what is x402', got %v", body["query"])
		}
		// Ensure no optional fields are present
		if _, ok := body["sources"]; ok {
			t.Error("Expected no sources field when opts is nil")
		}
		if _, ok := body["max_results"]; ok {
			t.Error("Expected no max_results field when opts is nil")
		}

		resp := SearchResult{
			Query:       "what is x402",
			Summary:     "x402 is a payment protocol...",
			SourcesUsed: 2,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	result, err := client.Search(context.Background(), "what is x402", nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if result.Query != "what is x402" {
		t.Errorf("Expected query 'what is x402', got %s", result.Query)
	}
	if result.SourcesUsed != 2 {
		t.Errorf("Expected 2 sources used, got %d", result.SourcesUsed)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.Search(context.Background(), "", nil)
	if err == nil {
		t.Fatal("Expected error for empty query, got nil")
	}

	valErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("Expected ValidationError, got %T: %v", err, err)
	}
	if valErr.Field != "query" {
		t.Errorf("Expected field 'query', got %s", valErr.Field)
	}
}
