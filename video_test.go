package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewVideoClient_RequiresKey(t *testing.T) {
	t.Setenv("BLOCKRUN_WALLET_KEY", "")
	t.Setenv("BASE_CHAIN_WALLET_KEY", "")

	if _, err := NewVideoClient(""); err == nil {
		t.Fatal("expected error when no private key is configured")
	}
}

func TestVideoClient_GenerateValidation(t *testing.T) {
	c, err := NewVideoClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewVideoClient: %v", err)
	}
	ctx := context.Background()

	// ImageURL and RealFaceAssetID are mutually exclusive.
	if _, err := c.Generate(ctx, "a scene", &VideoGenerateOptions{
		ImageURL:        "https://example.com/a.jpg",
		RealFaceAssetID: "ta_abc",
	}); err == nil {
		t.Error("expected error when both ImageURL and RealFaceAssetID are set")
	}

	// RealFaceAssetID must start with ta_.
	if _, err := c.Generate(ctx, "a scene", &VideoGenerateOptions{
		RealFaceAssetID: "abc123",
	}); err == nil {
		t.Error("expected error for RealFaceAssetID without ta_ prefix")
	}
}

func TestVideoClient_GenerateWithFaceAsset(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/videos/generations") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1717000000,
			"model":   "bytedance/seedance-2.0",
			"data": []map[string]any{
				{"url": "https://cdn.example.com/v.mp4", "duration_seconds": 5},
			},
		})
	}))
	defer server.Close()

	c, _ := NewVideoClient(testPrivateKey, WithVideoAPIURL(server.URL))
	genAudio := false
	out, err := c.Generate(context.Background(), "the character waves", &VideoGenerateOptions{
		Model:           "bytedance/seedance-2.0",
		RealFaceAssetID: "ta_abcdef",
		Resolution:      "1080p",
		GenerateAudio:   &genAudio,
		DurationSeconds: 5,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(out.Data) != 1 || out.Data[0].URL != "https://cdn.example.com/v.mp4" {
		t.Fatalf("unexpected response: %+v", out)
	}
	if sawBody["real_face_asset_id"] != "ta_abcdef" {
		t.Errorf("real_face_asset_id = %v", sawBody["real_face_asset_id"])
	}
	if sawBody["resolution"] != "1080p" {
		t.Errorf("resolution = %v", sawBody["resolution"])
	}
	if sawBody["generate_audio"] != false {
		t.Errorf("generate_audio = %v", sawBody["generate_audio"])
	}
	if _, ok := sawBody["image_url"]; ok {
		t.Errorf("did not expect image_url, got %v", sawBody["image_url"])
	}
}
