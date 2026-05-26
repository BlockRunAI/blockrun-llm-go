package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
