package blockrun

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func decodeServiceCodeInfo(t *testing.T, payload string) map[string]any {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	var pp PaymentPayload
	if err := json.Unmarshal(decoded, &pp); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	bc, ok := pp.Extensions["builder-code"].(map[string]any)
	if !ok {
		t.Fatalf("missing builder-code extension: %v", pp.Extensions)
	}
	info, ok := bc["info"].(map[string]any)
	if !ok {
		t.Fatalf("missing builder-code.info: %v", bc)
	}
	return info
}

func assertServiceCode(t *testing.T, info map[string]any) {
	t.Helper()
	s, ok := info["s"].([]any)
	if !ok || len(s) != 1 || s[0] != "blockrun" {
		t.Errorf("expected service code [blockrun], got %v", info["s"])
	}
}

func TestCreatePaymentPayloadAttachesServiceCode(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
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
		t.Fatalf("failed to create payment payload: %v", err)
	}

	assertServiceCode(t, decodeServiceCodeInfo(t, payload))
}

func TestCreatePaymentPayloadPreservesEchoedAppCode(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	extensions := map[string]any{
		"builder-code": map[string]any{
			"info": map[string]any{"a": "blockrun"},
		},
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
		extensions,
	)
	if err != nil {
		t.Fatalf("failed to create payment payload: %v", err)
	}

	info := decodeServiceCodeInfo(t, payload)
	assertServiceCode(t, info)
	if info["a"] != "blockrun" {
		t.Errorf("expected echoed app code blockrun, got %v", info["a"])
	}
}
