package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewPhoneClient_RequiresKey(t *testing.T) {
	t.Setenv("BLOCKRUN_WALLET_KEY", "")
	t.Setenv("BASE_CHAIN_WALLET_KEY", "")

	if _, err := NewPhoneClient(""); err == nil {
		t.Fatal("expected error when no private key is configured")
	}
}

func TestNewPhoneClient_EnvKey(t *testing.T) {
	t.Setenv("BLOCKRUN_WALLET_KEY", testPrivateKey)

	c, err := NewPhoneClient("")
	if err != nil {
		t.Fatalf("NewPhoneClient: %v", err)
	}
	if c.GetWalletAddress() != testWalletAddress {
		t.Errorf("address mismatch: got %s", c.GetWalletAddress())
	}
}

func TestPhoneClient_E164Validation(t *testing.T) {
	c, err := NewPhoneClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewPhoneClient: %v", err)
	}

	cases := []string{"", "4155552671", "+abc", "+1", "+155501234567812345"}
	for _, bad := range cases {
		if _, err := c.Lookup(context.Background(), bad); err == nil {
			t.Errorf("Lookup(%q) — expected validation error, got nil", bad)
		}
	}
}

func TestPhoneClient_Lookup(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/phone/lookup") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"phone_number": "+14155552671",
			"country_code": "US",
			"carrier":      map[string]any{"name": "T-Mobile USA, Inc.", "type": "mobile"},
			"some_extra":   "preserved",
		})
	}))
	defer server.Close()

	c, err := NewPhoneClient(testPrivateKey, WithPhoneAPIURL(server.URL))
	if err != nil {
		t.Fatalf("NewPhoneClient: %v", err)
	}

	out, err := c.Lookup(context.Background(), "+14155552671")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if out.PhoneNumber != "+14155552671" {
		t.Errorf("phone_number = %q", out.PhoneNumber)
	}
	if got, _ := sawBody["phoneNumber"].(string); got != "+14155552671" {
		t.Errorf("request body phoneNumber = %v", sawBody["phoneNumber"])
	}
	if out.Extra["some_extra"] != "preserved" {
		t.Errorf("expected unknown fields in Extra, got %+v", out.Extra)
	}
}

func TestPhoneClient_BuyNumberValidation(t *testing.T) {
	c, err := NewPhoneClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewPhoneClient: %v", err)
	}

	if _, err := c.BuyNumber(context.Background(), BuyNumberOptions{Country: "FR"}); err == nil {
		t.Error("expected error for non-US/CA country")
	}
	if _, err := c.BuyNumber(context.Background(), BuyNumberOptions{Country: "US", AreaCode: "41"}); err == nil {
		t.Error("expected error for 2-digit area code")
	}
	if _, err := c.BuyNumber(context.Background(), BuyNumberOptions{Country: "US", AreaCode: "abc"}); err == nil {
		t.Error("expected error for non-digit area code")
	}
}

func TestPhoneClient_BuyNumber_HappyPath(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/phone/numbers/buy") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"phone_number": "+14155550199",
			"expires_at":   "2026-06-17T00:00:00Z",
			"chain":        "base",
			"message":      "ok",
		})
	}))
	defer server.Close()

	c, _ := NewPhoneClient(testPrivateKey, WithPhoneAPIURL(server.URL))
	got, err := c.BuyNumber(context.Background(), BuyNumberOptions{Country: "US", AreaCode: "415"})
	if err != nil {
		t.Fatalf("BuyNumber: %v", err)
	}
	if got.PhoneNumber != "+14155550199" {
		t.Errorf("phone_number = %q", got.PhoneNumber)
	}
	if got.ExpiresAt == "" {
		t.Error("expected expires_at to be set")
	}
	if sawBody["country"] != "US" || sawBody["areaCode"] != "415" {
		t.Errorf("body mismatch: %+v", sawBody)
	}
}

func TestPhoneClient_ListNumbers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"numbers": []map[string]any{
				{"phone_number": "+14155550199", "chain": "base", "expires_at": "2026-06-17T00:00:00Z", "active": true},
			},
			"count": 1,
		})
	}))
	defer server.Close()

	c, _ := NewPhoneClient(testPrivateKey, WithPhoneAPIURL(server.URL))
	out, err := c.ListNumbers(context.Background())
	if err != nil {
		t.Fatalf("ListNumbers: %v", err)
	}
	if out.Count != 1 || len(out.Numbers) != 1 {
		t.Fatalf("unexpected list: %+v", out)
	}
	if out.Numbers[0].PhoneNumber != "+14155550199" {
		t.Errorf("phone_number = %q", out.Numbers[0].PhoneNumber)
	}
}
