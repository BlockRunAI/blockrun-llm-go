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

func TestVideoClient_GenerateNewParamValidation(t *testing.T) {
	c, err := NewVideoClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewVideoClient: %v", err)
	}
	ctx := context.Background()

	// LastFrameURL requires ImageURL.
	if _, err := c.Generate(ctx, "a scene", &VideoGenerateOptions{
		LastFrameURL: "https://example.com/last.jpg",
	}); err == nil {
		t.Error("expected error for LastFrameURL without ImageURL")
	}

	// LastFrameURL and RealFaceAssetID are mutually exclusive.
	if _, err := c.Generate(ctx, "a scene", &VideoGenerateOptions{
		ImageURL:        "https://example.com/first.jpg",
		LastFrameURL:    "https://example.com/last.jpg",
		RealFaceAssetID: "ta_abc",
	}); err == nil {
		t.Error("expected error when LastFrameURL and RealFaceAssetID are combined")
	}

	// ReferenceImageURLs excludes the other image inputs.
	if _, err := c.Generate(ctx, "a scene", &VideoGenerateOptions{
		ImageURL:           "https://example.com/seed.jpg",
		ReferenceImageURLs: []string{"https://example.com/r.jpg"},
	}); err == nil {
		t.Error("expected error when ReferenceImageURLs is combined with ImageURL")
	}
	if _, err := c.Generate(ctx, "a scene", &VideoGenerateOptions{
		RealFaceAssetID:    "ta_abc",
		ReferenceImageURLs: []string{"https://example.com/r.jpg"},
	}); err == nil {
		t.Error("expected error when ReferenceImageURLs is combined with RealFaceAssetID")
	}

	// At most 9 reference images.
	many := make([]string, 10)
	for i := range many {
		many[i] = "https://example.com/r.jpg"
	}
	if _, err := c.Generate(ctx, "a scene", &VideoGenerateOptions{
		ReferenceImageURLs: many,
	}); err == nil {
		t.Error("expected error for more than 9 ReferenceImageURLs")
	}
}

func TestVideoClient_GenerateFirstLastFrame(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1749000000,
			"model":   "bytedance/seedance-1.5-pro",
			"data": []map[string]any{
				{"url": "https://cdn.example.com/v.mp4", "duration_seconds": 5},
			},
		})
	}))
	defer server.Close()

	c, _ := NewVideoClient(testPrivateKey, WithVideoAPIURL(server.URL))
	seed := 42
	watermark := false
	_, err := c.Generate(context.Background(), "the flower blooms", &VideoGenerateOptions{
		Model:           "bytedance/seedance-1.5-pro",
		ImageURL:        "https://example.com/bud.jpg",
		LastFrameURL:    "https://example.com/bloom.jpg",
		AspectRatio:     "16:9",
		Seed:            &seed,
		Watermark:       &watermark,
		ReturnLastFrame: true,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if sawBody["image_url"] != "https://example.com/bud.jpg" {
		t.Errorf("image_url = %v", sawBody["image_url"])
	}
	if sawBody["last_frame_url"] != "https://example.com/bloom.jpg" {
		t.Errorf("last_frame_url = %v", sawBody["last_frame_url"])
	}
	if sawBody["aspect_ratio"] != "16:9" {
		t.Errorf("aspect_ratio = %v", sawBody["aspect_ratio"])
	}
	if sawBody["seed"] != float64(42) {
		t.Errorf("seed = %v", sawBody["seed"])
	}
	if sawBody["watermark"] != false {
		t.Errorf("watermark = %v", sawBody["watermark"])
	}
	if sawBody["return_last_frame"] != true {
		t.Errorf("return_last_frame = %v", sawBody["return_last_frame"])
	}
}

func TestVideoClient_GenerateReferenceImages(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1749000000,
			"model":   "bytedance/seedance-2.0",
			"data": []map[string]any{
				{"url": "https://cdn.example.com/v.mp4", "duration_seconds": 5},
			},
		})
	}))
	defer server.Close()

	c, _ := NewVideoClient(testPrivateKey, WithVideoAPIURL(server.URL))
	_, err := c.Generate(context.Background(), "the character from image 1 in the city from image 2", &VideoGenerateOptions{
		Model: "bytedance/seedance-2.0",
		ReferenceImageURLs: []string{
			"https://example.com/character.jpg",
			"https://example.com/city.jpg",
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	refs, ok := sawBody["reference_image_urls"].([]any)
	if !ok || len(refs) != 2 || refs[0] != "https://example.com/character.jpg" {
		t.Errorf("reference_image_urls = %v", sawBody["reference_image_urls"])
	}
	if _, present := sawBody["image_url"]; present {
		t.Errorf("did not expect image_url, got %v", sawBody["image_url"])
	}
}
