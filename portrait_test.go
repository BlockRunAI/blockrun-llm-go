package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewPortraitClient_RequiresKey(t *testing.T) {
	t.Setenv("BLOCKRUN_WALLET_KEY", "")
	t.Setenv("BASE_CHAIN_WALLET_KEY", "")

	if _, err := NewPortraitClient(""); err == nil {
		t.Fatal("expected error when no private key is configured")
	}
}

func TestPortraitClient_EnrollValidation(t *testing.T) {
	c, err := NewPortraitClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewPortraitClient: %v", err)
	}

	if _, err := c.Enroll(context.Background(), "", "https://example.com/a.jpg"); err == nil {
		t.Error("expected error for empty name")
	}
	if _, err := c.Enroll(context.Background(), strings.Repeat("x", 65), "https://example.com/a.jpg"); err == nil {
		t.Error("expected error for over-long name")
	}
	if _, err := c.Enroll(context.Background(), "Spokesperson", "ftp://example.com/a.jpg"); err == nil {
		t.Error("expected error for non-http image url")
	}
}

func TestPortraitClient_Enroll(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/portrait/enroll") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object":    "virtual_portrait",
			"asset_id":  "ta_abcdef1234567890",
			"name":      "My Spokesperson",
			"image_url": "https://example.com/character.jpg",
			"settlement": map[string]any{
				"success": true,
				"tx_hash": "0x9f3a",
				"network": "base",
			},
			"usage": map[string]any{
				"compatible_models": []string{"bytedance/seedance-2.0"},
				"how_to_use":        "pass as real_face_asset_id",
			},
		})
	}))
	defer server.Close()

	c, _ := NewPortraitClient(testPrivateKey, WithPortraitAPIURL(server.URL))
	out, err := c.Enroll(context.Background(), "My Spokesperson", "https://example.com/character.jpg")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if out.AssetID != "ta_abcdef1234567890" {
		t.Errorf("asset_id = %q", out.AssetID)
	}
	if out.Settlement == nil || out.Settlement.TxHash != "0x9f3a" {
		t.Errorf("settlement = %+v", out.Settlement)
	}
	if out.Usage == nil || len(out.Usage.CompatibleModels) != 1 {
		t.Errorf("usage = %+v", out.Usage)
	}
	if sawBody["name"] != "My Spokesperson" || sawBody["image_url"] != "https://example.com/character.jpg" {
		t.Errorf("body mismatch: %+v", sawBody)
	}
}

func TestPortraitClient_ListPortraits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/portraits") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"wallet": testWalletAddress,
			"count":  1,
			"portraits": []map[string]any{
				{"assetId": "ta_abc", "name": "Mascot", "imageUrl": "https://example.com/m.jpg", "enrollmentTxHash": "0x1"},
			},
		})
	}))
	defer server.Close()

	c, _ := NewPortraitClient(testPrivateKey, WithPortraitAPIURL(server.URL))
	out, err := c.ListPortraits(context.Background(), "")
	if err != nil {
		t.Fatalf("ListPortraits: %v", err)
	}
	if out.Count != 1 || len(out.Portraits) != 1 {
		t.Fatalf("unexpected list: %+v", out)
	}
	if out.Portraits[0].AssetID != "ta_abc" || out.Portraits[0].Name != "Mascot" {
		t.Errorf("portrait = %+v", out.Portraits[0])
	}
}
