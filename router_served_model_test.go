package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// servedModelServer answers /v1/chat/completions with bodyModel in the JSON
// body (omitted when empty) and the given headers.
func servedModelServer(t *testing.T, bodyModel string, headers map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		resp := map[string]any{
			"id":      "test",
			"object":  "chat.completion",
			"created": 1700000000,
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": "OK"},
				"finish_reason": "stop",
			}},
		}
		if bodyModel != "" {
			resp["model"] = bodyModel
		}
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestSmartChatReportsServedModel(t *testing.T) {
	const prompt = "hello"
	decision, err := routePrompt(prompt, RoutingFree)
	if err != nil {
		t.Fatal(err)
	}
	requested := decision.Model
	const served = "nvidia/llama-3.2-11b-vision"
	if requested == served {
		t.Fatalf("test needs a free primary other than %s for %q", served, prompt)
	}

	tests := []struct {
		name      string
		bodyModel string
		headers   map[string]string
		want      string
	}{
		{name: "no reroute", bodyModel: requested, want: requested},
		{
			name:      "body model differs",
			bodyModel: served,
			headers:   map[string]string{"X-Health-Reroute": "true", "X-Original-Model": requested, "X-Served-Model": served},
			want:      served,
		},
		{
			name:    "header only, health reroute",
			headers: map[string]string{"X-Health-Reroute": "true", "X-Original-Model": requested, "X-Served-Model": served},
			want:    served,
		},
		{
			name:    "header only, cascade fallback",
			headers: map[string]string{"X-Original-Model": requested, "X-Fallback-Model": served},
			want:    served,
		},
		{name: "no body model, no headers", want: requested},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := servedModelServer(t, tt.bodyModel, tt.headers)
			defer server.Close()
			client, err := NewLLMClient(routerTestKey, WithAPIURL(server.URL))
			if err != nil {
				t.Fatalf("NewLLMClient: %v", err)
			}
			resp, err := client.SmartChat(context.Background(), prompt, &SmartChatOptions{RoutingProfile: RoutingFree})
			if err != nil {
				t.Fatalf("SmartChat: %v", err)
			}
			if resp.Model != tt.want {
				t.Errorf("Model = %q, want %q", resp.Model, tt.want)
			}
			if resp.RequestedModel != requested {
				t.Errorf("RequestedModel = %q, want %q", resp.RequestedModel, requested)
			}
			if resp.Routing.Model != requested {
				t.Errorf("Routing.Model = %q, want %q", resp.Routing.Model, requested)
			}
			if resp.Response != "OK" {
				t.Errorf("Response = %q, want OK", resp.Response)
			}
		})
	}
}

// The JSON shape is additive: existing keys keep their names.
func TestSmartChatResponseJSONKeys(t *testing.T) {
	raw, err := json.Marshal(SmartChatResponse{Response: "r", Model: "served", RequestedModel: "req"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"response", "model", "requested_model", "routing"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing JSON key %q in %s", k, raw)
		}
	}
	if got["model"] != "served" || got["requested_model"] != "req" {
		t.Errorf("unexpected values: %s", raw)
	}
}
