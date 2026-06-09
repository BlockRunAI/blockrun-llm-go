package blockrun

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newMockVideoServer emulates the real async video pipeline: 402 on the unauth
// submit, 202 { id, poll_url } on the paid submit (capturing the request body
// into sawBody), and a completed job on the poll GET. This is the contract the
// gateway actually serves — submit returns 202, settlement happens on the poll.
func newMockVideoServer(t *testing.T, sawBody *map[string]any, model string) *httptest.Server {
	t.Helper()
	pr := PaymentRequirement{
		X402Version: 2,
		Accepts: []PaymentOption{{
			Scheme:            "exact",
			Network:           "eip155:8453",
			Amount:            "1000",
			Asset:             USDCBase,
			PayTo:             "0x1234567890123456789012345678901234567890",
			MaxTimeoutSeconds: 300,
		}},
		Resource: ResourceInfo{
			URL:         "https://blockrun.ai/api/v1/videos/generations",
			Description: "Video",
		},
	}
	prJSON, _ := json.Marshal(pr)
	prHeader := base64.StdEncoding.EncodeToString(prJSON)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Poll GET -> completed job.
		if r.Method == http.MethodGet {
			w.Header().Set("x-payment-receipt", "0xdeadbeef")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":  "completed",
				"created": 1749000000,
				"model":   model,
				"data": []map[string]any{
					{"url": "https://cdn.example.com/v.mp4", "duration_seconds": 5},
				},
			})
			return
		}
		// Unauth submit -> 402 with payment requirements.
		if r.Header.Get("PAYMENT-SIGNATURE") == "" {
			w.Header().Set("payment-required", prHeader)
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		// Paid submit -> 202 { id, poll_url }. Capture the body for assertions.
		if err := json.NewDecoder(r.Body).Decode(sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "job123",
			"poll_url": "http://" + r.Host + "/v1/videos/generations/job123",
			"status":   "queued",
		})
	}))
}

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
	server := newMockVideoServer(t, &sawBody, "bytedance/seedance-2.0")
	defer server.Close()

	c, _ := NewVideoClient(testPrivateKey, WithVideoAPIURL(server.URL))
	c.pollInterval = time.Millisecond
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
	server := newMockVideoServer(t, &sawBody, "bytedance/seedance-1.5-pro")
	defer server.Close()

	c, _ := NewVideoClient(testPrivateKey, WithVideoAPIURL(server.URL))
	c.pollInterval = time.Millisecond
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
	server := newMockVideoServer(t, &sawBody, "bytedance/seedance-2.0")
	defer server.Close()

	c, _ := NewVideoClient(testPrivateKey, WithVideoAPIURL(server.URL))
	c.pollInterval = time.Millisecond
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

func TestVideoClient_GenerateFromContent(t *testing.T) {
	var sawBody map[string]any
	server := newMockVideoServer(t, &sawBody, "bytedance/seedance-2.0")
	defer server.Close()

	c, _ := NewVideoClient(testPrivateKey, WithVideoAPIURL(server.URL))
	c.pollInterval = time.Millisecond

	out, err := c.GenerateFromContent(
		context.Background(),
		[]map[string]any{
			{"type": "text", "text": "a red apple spinning"},
		},
		&VideoGenerateOptions{
			Model:           "bytedance/seedance-2.0",
			Resolution:      "720p",
			DurationSeconds: 5,
		},
	)
	if err != nil {
		t.Fatalf("GenerateFromContent: %v", err)
	}
	if len(out.Data) != 1 || out.Data[0].URL != "https://cdn.example.com/v.mp4" {
		t.Fatalf("unexpected response: %+v", out)
	}
	if out.TxHash != "0xdeadbeef" {
		t.Errorf("txHash = %q, want the settlement receipt from the completed poll", out.TxHash)
	}

	// content[] is forwarded verbatim to POST /v1/videos.
	content, ok := sawBody["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %v", sawBody["content"])
	}
	first, _ := content[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "a red apple spinning" {
		t.Errorf("content[0] = %v", content[0])
	}
	// Scalar render options forwarded as snake_case.
	if sawBody["model"] != "bytedance/seedance-2.0" {
		t.Errorf("model = %v", sawBody["model"])
	}
	if sawBody["resolution"] != "720p" {
		t.Errorf("resolution = %v", sawBody["resolution"])
	}
	if sawBody["duration_seconds"] != float64(5) {
		t.Errorf("duration_seconds = %v", sawBody["duration_seconds"])
	}
}

func TestVideoClient_GenerateFromContentRejectsEmpty(t *testing.T) {
	c, err := NewVideoClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewVideoClient: %v", err)
	}
	if _, err := c.GenerateFromContent(context.Background(), nil, nil); err == nil {
		t.Error("expected error for empty content")
	}
}

// TestVideoClient_PollCompletedNon200 pins the fix that terminal success is
// keyed on status=="completed", NOT a literal HTTP 200. A poll that reports
// completed with a 202 must still succeed (the gateway has already settled),
// not spin to the deadline and return a "not charged" error.
func TestVideoClient_PollCompletedNon200(t *testing.T) {
	pr := PaymentRequirement{
		X402Version: 2,
		Accepts: []PaymentOption{{
			Scheme: "exact", Network: "eip155:8453", Amount: "1000",
			Asset: USDCBase, PayTo: "0x1234567890123456789012345678901234567890",
			MaxTimeoutSeconds: 300,
		}},
		Resource: ResourceInfo{URL: "https://blockrun.ai/api/v1/videos/generations"},
	}
	prJSON, _ := json.Marshal(pr)
	prHeader := base64.StdEncoding.EncodeToString(prJSON)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// Completed payload returned with a 202, not a 200.
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "completed",
				"model":  "bytedance/seedance-2.0",
				"data":   []map[string]any{{"url": "https://cdn.example.com/v.mp4"}},
			})
			return
		}
		if r.Header.Get("PAYMENT-SIGNATURE") == "" {
			w.Header().Set("payment-required", prHeader)
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "job123",
			"poll_url": "http://" + r.Host + "/v1/videos/generations/job123",
			"status":   "queued",
		})
	}))
	defer server.Close()

	c, _ := NewVideoClient(testPrivateKey, WithVideoAPIURL(server.URL))
	c.pollInterval = time.Millisecond

	out, err := c.Generate(context.Background(), "a red apple", &VideoGenerateOptions{Model: "bytedance/seedance-2.0"})
	if err != nil {
		t.Fatalf("expected success on 202+completed, got error: %v", err)
	}
	if len(out.Data) != 1 || out.Data[0].URL != "https://cdn.example.com/v.mp4" {
		t.Fatalf("unexpected response: %+v", out)
	}
}
