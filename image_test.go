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

func TestNewImageClient(t *testing.T) {
	client, err := NewImageClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create image client: %v", err)
	}

	if client.GetWalletAddress() != testWalletAddress {
		t.Errorf("Expected wallet address %s, got %s", testWalletAddress, client.GetWalletAddress())
	}
}

func TestNewImageClientEmptyKey(t *testing.T) {
	// Clear env vars for test
	t.Setenv("BLOCKRUN_WALLET_KEY", "")
	t.Setenv("BASE_CHAIN_WALLET_KEY", "")

	_, err := NewImageClient("")
	if err == nil {
		t.Error("Expected error for empty key, got nil")
	}
}

func TestImageClientListModels(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/models" {
			t.Errorf("Expected path /v1/images/models, got %s", r.URL.Path)
		}

		models := struct {
			Data []ImageModel `json:"data"`
		}{
			Data: []ImageModel{
				{ID: "google/nano-banana", Name: "Nano Banana", Provider: "google", PricePerImage: 0.01, Available: true},
				{ID: "openai/dall-e-3", Name: "DALL-E 3", Provider: "openai", PricePerImage: 0.04, Available: true},
			},
		}

		json.NewEncoder(w).Encode(models)
	}))
	defer server.Close()

	client, err := NewImageClient(testPrivateKey, WithImageAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create image client: %v", err)
	}

	models, err := client.ListImageModels(context.Background())
	if err != nil {
		t.Fatalf("Failed to list image models: %v", err)
	}

	if len(models) != 2 {
		t.Errorf("Expected 2 image models, got %d", len(models))
	}

	if models[0].ID != "google/nano-banana" {
		t.Errorf("Expected first model google/nano-banana, got %s", models[0].ID)
	}
}

func TestImageClientEditSingleImage(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/image2image" {
			t.Errorf("Expected path /v1/images/image2image, got %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(ImageResponse{Created: 1, Data: []ImageData{{URL: "https://example.com/edited.png"}}})
	}))
	defer server.Close()

	client, err := NewImageClient(testPrivateKey, WithImageAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create image client: %v", err)
	}

	src := "data:image/png;base64,AAAA"
	resp, err := client.Edit(context.Background(), "make the sky purple", []string{src}, nil)
	if err != nil {
		t.Fatalf("Edit failed: %v", err)
	}

	// Single image must be sent as a JSON string, not an array (OpenAI-compatible).
	if img, ok := gotBody["image"].(string); !ok || img != src {
		t.Errorf("Expected image to be string %q, got %#v", src, gotBody["image"])
	}
	if gotBody["model"] != "openai/gpt-image-2" {
		t.Errorf("Expected default model openai/gpt-image-2, got %v", gotBody["model"])
	}
	if gotBody["prompt"] != "make the sky purple" {
		t.Errorf("Expected prompt to be forwarded, got %v", gotBody["prompt"])
	}
	if len(resp.Data) != 1 || resp.Data[0].URL != "https://example.com/edited.png" {
		t.Errorf("Unexpected response data: %#v", resp.Data)
	}
}

func TestImageClientEditMultiImage(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(ImageResponse{Created: 1, Data: []ImageData{{URL: "https://example.com/fused.png"}}})
	}))
	defer server.Close()

	client, _ := NewImageClient(testPrivateKey, WithImageAPIURL(server.URL))

	images := []string{"data:image/png;base64,AAAA", "data:image/png;base64,BBBB"}
	_, err := client.Edit(context.Background(), "fuse the logo onto the photo", images, &ImageEditOptions{Model: "google/nano-banana"})
	if err != nil {
		t.Fatalf("Edit failed: %v", err)
	}

	// Multiple images must be sent as a JSON array (image fusion).
	arr, ok := gotBody["image"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("Expected image to be array of 2, got %#v", gotBody["image"])
	}
	if gotBody["model"] != "google/nano-banana" {
		t.Errorf("Expected model google/nano-banana, got %v", gotBody["model"])
	}
}

func TestImageClientEditOptionsPropagate(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(ImageResponse{Created: 1, Data: []ImageData{{URL: "x"}}})
	}))
	defer server.Close()

	client, _ := NewImageClient(testPrivateKey, WithImageAPIURL(server.URL))

	_, err := client.Edit(context.Background(), "edit", []string{"data:image/png;base64,AAAA"}, &ImageEditOptions{
		Mask: "data:image/png;base64,MASK",
		Size: "512x512",
		N:    3,
	})
	if err != nil {
		t.Fatalf("Edit failed: %v", err)
	}

	if gotBody["mask"] != "data:image/png;base64,MASK" {
		t.Errorf("Expected mask to be forwarded, got %v", gotBody["mask"])
	}
	if gotBody["size"] != "512x512" {
		t.Errorf("Expected size 512x512, got %v", gotBody["size"])
	}
	if gotBody["n"] != float64(3) {
		t.Errorf("Expected n=3, got %v", gotBody["n"])
	}
}

func TestImageClientEditNoImages(t *testing.T) {
	client, _ := NewImageClient(testPrivateKey)
	_, err := client.Edit(context.Background(), "edit", nil, nil)
	if err == nil {
		t.Error("Expected error when no images provided, got nil")
	}
}

func TestImageClientGetSpending(t *testing.T) {
	client, err := NewImageClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create image client: %v", err)
	}

	// Initial spending should be zero
	spending := client.GetSpending()
	if spending.TotalUSD != 0 {
		t.Errorf("Expected initial TotalUSD 0, got %f", spending.TotalUSD)
	}
	if spending.Calls != 0 {
		t.Errorf("Expected initial Calls 0, got %d", spending.Calls)
	}
}

// newMockAsyncImageServer emulates the gateway's hybrid image pipeline slow
// path: 402 on the unauth submit, 202 { id, poll_url } on the paid submit,
// then in_progress → completed on successive poll GETs. This is the contract
// the gateway actually serves for slow models (e.g. gpt-image-2) — settlement
// happens on the first completed poll, not at submit.
func newMockAsyncImageServer(t *testing.T, pollsBeforeDone int) *httptest.Server {
	t.Helper()
	pr := PaymentRequirement{
		X402Version: 2,
		Accepts: []PaymentOption{{
			Scheme:            "exact",
			Network:           "eip155:8453",
			Amount:            "40000",
			Asset:             USDCBase,
			PayTo:             "0x1234567890123456789012345678901234567890",
			MaxTimeoutSeconds: 600,
		}},
		Resource: ResourceInfo{
			URL:         "https://blockrun.ai/api/v1/images/generations",
			Description: "Image",
		},
	}
	prJSON, _ := json.Marshal(pr)
	prHeader := base64.StdEncoding.EncodeToString(prJSON)

	polls := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.Header.Get("PAYMENT-SIGNATURE") == "" {
				t.Error("poll GET missing PAYMENT-SIGNATURE")
			}
			polls++
			if polls <= pollsBeforeDone {
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "img_job1", "status": "in_progress",
				})
				return
			}
			w.Header().Set("x-payment-receipt", "0xfeedface")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "img_job1",
				"status":  "completed",
				"created": 1749000000,
				"data": []map[string]any{
					{"url": "https://cdn.example.com/img.png", "backed_up": true},
				},
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
			"id":       "img_job1",
			"status":   "queued",
			"poll_url": "http://" + r.Host + "/v1/images/generations/img_job1",
		})
	}))
}

func TestImageClientGenerateAsyncEnvelope(t *testing.T) {
	server := newMockAsyncImageServer(t, 2)
	defer server.Close()

	client, err := NewImageClient(testPrivateKey, WithImageAPIURL(server.URL))
	if err != nil {
		t.Fatalf("NewImageClient: %v", err)
	}
	client.pollInterval = 10 * time.Millisecond

	resp, err := client.Generate(context.Background(), "slow masterpiece", &ImageGenerateOptions{
		Model: "openai/gpt-image-2",
	})
	if err != nil {
		t.Fatalf("Generate via async envelope: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].URL != "https://cdn.example.com/img.png" {
		t.Errorf("unexpected data: %+v", resp.Data)
	}
	if resp.TxHash != "0xfeedface" {
		t.Errorf("expected settlement receipt 0xfeedface, got %q", resp.TxHash)
	}
	// Cost is recorded once completion is observed (40000 micro-USDC = $0.04).
	if got := client.GetSpending().TotalUSD; got != 0.04 {
		t.Errorf("expected $0.04 recorded, got %f", got)
	}
}

func TestImageClientGenerateAsyncFailedNotCharged(t *testing.T) {
	pr := PaymentRequirement{
		X402Version: 2,
		Accepts: []PaymentOption{{
			Scheme: "exact", Network: "eip155:8453", Amount: "40000",
			Asset: USDCBase, PayTo: "0x1234567890123456789012345678901234567890",
			MaxTimeoutSeconds: 600,
		}},
		Resource: ResourceInfo{URL: "https://blockrun.ai/api/v1/images/generations", Description: "Image"},
	}
	prJSON, _ := json.Marshal(pr)
	prHeader := base64.StdEncoding.EncodeToString(prJSON)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "img_job1", "status": "failed", "error": "upstream exploded",
				"payment_status": "not_charged",
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
			"id": "img_job1", "status": "queued",
			"poll_url": "http://" + r.Host + "/v1/images/generations/img_job1",
		})
	}))
	defer server.Close()

	client, err := NewImageClient(testPrivateKey, WithImageAPIURL(server.URL))
	if err != nil {
		t.Fatalf("NewImageClient: %v", err)
	}
	client.pollInterval = 10 * time.Millisecond

	_, err = client.Generate(context.Background(), "doomed", &ImageGenerateOptions{Model: "openai/gpt-image-2"})
	if err == nil {
		t.Fatal("expected error for failed job")
	}
	if got := client.GetSpending().TotalUSD; got != 0 {
		t.Errorf("failed job must not record spending, got %f", got)
	}
}
