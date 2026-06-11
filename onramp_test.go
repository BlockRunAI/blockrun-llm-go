package blockrun

// Tests for the Coinbase Onramp client.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testOnrampAddress = "0x1111111111111111111111111111111111111111"

func TestOnramp(t *testing.T) {
	var lastPath string
	var lastBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&lastBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"url": "https://pay.coinbase.com/buy/select-asset?sessionToken=abc&defaultAsset=USDC&defaultNetwork=base",
		})
	}))
	defer server.Close()

	c := newPassthroughClient(t, server.URL)
	res, err := c.Onramp(context.Background(), testOnrampAddress)
	if err != nil {
		t.Fatalf("Onramp: %v", err)
	}
	if !strings.HasSuffix(lastPath, "/v1/onramp/token") {
		t.Errorf("path = %s", lastPath)
	}
	if lastBody["address"] != testOnrampAddress || lastBody["network"] != "base" || lastBody["asset"] != "USDC" {
		t.Errorf("body = %+v", lastBody)
	}
	if !strings.HasPrefix(res.URL, "https://pay.coinbase.com/") {
		t.Errorf("url = %s", res.URL)
	}
}

func TestOnrampValidation(t *testing.T) {
	c := newPassthroughClient(t, "https://example.invalid")
	ctx := context.Background()

	if _, err := c.Onramp(ctx, ""); err == nil {
		t.Error("expected error for blank address")
	}
	if _, err := c.Onramp(ctx, "not-an-address"); err == nil {
		t.Error("expected error for malformed address")
	}
	if _, err := c.Onramp(ctx, "0x123"); err == nil {
		t.Error("expected error for too-short address")
	}
}

func TestOnrampRejectsNonCoinbaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://evil.example/phish"})
	}))
	defer server.Close()

	c := newPassthroughClient(t, server.URL)
	if _, err := c.Onramp(context.Background(), testOnrampAddress); err == nil {
		t.Error("expected error for non-Coinbase url")
	}
}
