package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const routerTestKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

// --- scorePrompt / tier tests ---

func TestScorePromptSimple(t *testing.T) {
	prompts := []string{
		"hello",
		"hi",
		"thanks",
		"yes",
	}
	for _, p := range prompts {
		score, _ := scorePrompt(p)
		tier := tierFromScore(score)
		// Simple greetings should score very low → SIMPLE or MEDIUM at most
		if tier != TierSimple && tier != TierMedium {
			t.Errorf("prompt %q: expected SIMPLE or MEDIUM, got %s (score=%.2f)", p, tier, score)
		}
	}
}

func TestScorePromptCodeRelated(t *testing.T) {
	prompts := []string{
		"Write a function to sort a list in Python:\n```python\ndef sort_list(items):\n    return sorted(items)\n```",
		"func main() { fmt.Println(\"hello\") }",
		"import os\ndef read_file(path):\n    return open(path).read()",
	}
	for _, p := range prompts {
		score, _ := scorePrompt(p)
		tier := tierFromScore(score)
		if tier == TierSimple {
			t.Errorf("code prompt %q: expected MEDIUM or higher, got SIMPLE (score=%.2f)", p, score)
		}
	}
}

func TestScorePromptComplexReasoning(t *testing.T) {
	prompt := "Explain step-by-step how a distributed database achieves consistency. " +
		"Compare the CAP theorem trade-offs, analyze the algorithm used by Raft consensus, " +
		"and evaluate whether microservice architectures benefit from eventual consistency."
	score, _ := scorePrompt(prompt)
	tier := tierFromScore(score)
	if tier != TierComplex && tier != TierReasoning {
		t.Errorf("complex prompt: expected COMPLEX or REASONING, got %s (score=%.2f)", tier, score)
	}
}

func TestScorePromptDeterministic(t *testing.T) {
	prompt := "Explain how kubernetes networking works with microservice architecture"
	s1, r1 := scorePrompt(prompt)
	s2, r2 := scorePrompt(prompt)
	if s1 != s2 {
		t.Errorf("scorePrompt not deterministic: %.4f != %.4f", s1, s2)
	}
	if r1 != r2 {
		t.Errorf("reasoning not deterministic: %q != %q", r1, r2)
	}
}

// --- Routing profile tests ---

func TestRoutingProfileSelectsDifferentModels(t *testing.T) {
	prompt := "Explain the CAP theorem and analyze distributed consensus algorithms step-by-step"

	profiles := []RoutingProfile{RoutingFree, RoutingEco, RoutingAuto, RoutingPremium}
	models := make(map[string]bool)

	for _, p := range profiles {
		decision, err := routePrompt(prompt, p)
		if err != nil {
			t.Fatalf("routePrompt(%s) error: %v", p, err)
		}
		models[decision.Model] = true
	}

	// Free uses one model for everything; the other profiles should differ
	if len(models) < 2 {
		t.Errorf("expected at least 2 distinct models across profiles, got %d", len(models))
	}
}

func TestRoutingTableCoverage(t *testing.T) {
	profiles := []RoutingProfile{RoutingFree, RoutingEco, RoutingAuto, RoutingPremium}
	tiers := []RoutingTier{TierSimple, TierMedium, TierComplex, TierReasoning}

	for _, p := range profiles {
		for _, tier := range tiers {
			model, ok := routingTable[p][tier]
			if !ok || model == "" {
				t.Errorf("missing model for profile=%s tier=%s", p, tier)
			}
		}
	}
}

func TestRoutePromptUnknownProfile(t *testing.T) {
	_, err := routePrompt("hello", RoutingProfile("unknown"))
	if err == nil {
		t.Error("expected error for unknown profile")
	}
}

func TestTierFromScore(t *testing.T) {
	tests := []struct {
		score float64
		tier  RoutingTier
	}{
		{-0.1, TierSimple},
		{-0.02, TierSimple},
		{0.0, TierMedium},
		{0.15, TierMedium},
		{0.29, TierMedium},
		{0.3, TierComplex},
		{0.45, TierComplex},
		{0.5, TierReasoning},
		{0.8, TierReasoning},
	}
	for _, tt := range tests {
		got := tierFromScore(tt.score)
		if got != tt.tier {
			t.Errorf("tierFromScore(%.2f) = %s, want %s", tt.score, got, tt.tier)
		}
	}
}

func TestRoutingDecisionFields(t *testing.T) {
	decision, err := routePrompt("hello", RoutingAuto)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Method != "claw_router_v1" {
		t.Errorf("expected method claw_router_v1, got %s", decision.Method)
	}
	if decision.Model == "" {
		t.Error("expected non-empty model")
	}
	if decision.Tier == "" {
		t.Error("expected non-empty tier")
	}
}

// --- SmartChat integration test with mock server ---

func TestSmartChatWithMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", 404)
			return
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		model, _ := body["model"].(string)

		resp := ChatResponse{
			ID:      "test-123",
			Object:  "chat.completion",
			Created: 1700000000,
			Model:   model,
			Choices: []Choice{
				{
					Index:        0,
					Message:      ChatMessage{Role: "assistant", Content: "Mock response from " + model},
					FinishReason: "stop",
				},
			},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewLLMClient(routerTestKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("NewLLMClient: %v", err)
	}

	t.Run("simple prompt", func(t *testing.T) {
		resp, err := client.SmartChat(context.Background(), "hello", nil)
		if err != nil {
			t.Fatalf("SmartChat: %v", err)
		}
		if resp.Response == "" {
			t.Error("expected non-empty response")
		}
		if resp.Model == "" {
			t.Error("expected non-empty model")
		}
		if resp.Routing.Method != "claw_router_v1" {
			t.Errorf("expected method claw_router_v1, got %s", resp.Routing.Method)
		}
	})

	t.Run("with system prompt", func(t *testing.T) {
		opts := &SmartChatOptions{
			System:         "You are a helpful assistant.",
			RoutingProfile: RoutingPremium,
		}
		resp, err := client.SmartChat(context.Background(), "hello", opts)
		if err != nil {
			t.Fatalf("SmartChat: %v", err)
		}
		if resp.Response == "" {
			t.Error("expected non-empty response")
		}
	})

	t.Run("complex prompt routes to higher tier", func(t *testing.T) {
		prompt := "Explain step-by-step how distributed database consensus algorithms work, " +
			"analyze the trade-offs, and compare Raft vs Paxos for microservice architectures"
		resp, err := client.SmartChat(context.Background(), prompt, &SmartChatOptions{
			RoutingProfile: RoutingPremium,
		})
		if err != nil {
			t.Fatalf("SmartChat: %v", err)
		}
		tier := resp.Routing.Tier
		if tier != string(TierComplex) && tier != string(TierReasoning) {
			t.Errorf("expected COMPLEX or REASONING tier, got %s", tier)
		}
	})

	t.Run("empty prompt returns error", func(t *testing.T) {
		_, err := client.SmartChat(context.Background(), "", nil)
		if err == nil {
			t.Error("expected error for empty prompt")
		}
	})
}
