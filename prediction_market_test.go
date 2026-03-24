package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPMGetWithoutParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/pm/polymarket/events" {
			t.Errorf("Expected path /v1/pm/polymarket/events, got %s", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("Expected no query params, got %s", r.URL.RawQuery)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"events": []any{"event1", "event2"},
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	result, err := client.PM(context.Background(), "polymarket/events", nil)
	if err != nil {
		t.Fatalf("PM failed: %v", err)
	}
	events, ok := result["events"].([]any)
	if !ok || len(events) != 2 {
		t.Errorf("Expected 2 events, got %v", result["events"])
	}
}

func TestPMGetWithParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/pm/polymarket/search" {
			t.Errorf("Expected path /v1/pm/polymarket/search, got %s", r.URL.Path)
		}
		q := r.URL.Query().Get("q")
		if q != "bitcoin" {
			t.Errorf("Expected query param q=bitcoin, got q=%s", q)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"results": []any{"market1"},
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	result, err := client.PM(context.Background(), "polymarket/search", map[string]string{"q": "bitcoin"})
	if err != nil {
		t.Fatalf("PM failed: %v", err)
	}
	results, ok := result["results"].([]any)
	if !ok || len(results) != 1 {
		t.Errorf("Expected 1 result, got %v", result["results"])
	}
}

func TestPMQueryPost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/pm/kalshi/query" {
			t.Errorf("Expected path /v1/pm/kalshi/query, got %s", r.URL.Path)
		}

		body := readRequestBody(t, r)
		if body["market"] != "presidential-election" {
			t.Errorf("Expected market presidential-election, got %v", body["market"])
		}
		if body["limit"] != float64(10) {
			t.Errorf("Expected limit 10, got %v", body["limit"])
		}

		json.NewEncoder(w).Encode(map[string]any{
			"markets": []any{"market1", "market2"},
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	query := map[string]any{
		"market": "presidential-election",
		"limit":  10,
	}
	result, err := client.PMQuery(context.Background(), "kalshi/query", query)
	if err != nil {
		t.Fatalf("PMQuery failed: %v", err)
	}
	markets, ok := result["markets"].([]any)
	if !ok || len(markets) != 2 {
		t.Errorf("Expected 2 markets, got %v", result["markets"])
	}
}

func TestPMEmptyPathValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.PM(context.Background(), "", nil)
	if err == nil {
		t.Error("Expected validation error for empty path")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestPMQueryEmptyPathValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.PMQuery(context.Background(), "", map[string]any{"q": "test"})
	if err == nil {
		t.Error("Expected validation error for empty path")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}
