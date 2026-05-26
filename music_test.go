package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewMusicClient_RequiresKey(t *testing.T) {
	t.Setenv("BLOCKRUN_WALLET_KEY", "")
	t.Setenv("BASE_CHAIN_WALLET_KEY", "")

	if _, err := NewMusicClient(""); err == nil {
		t.Fatal("expected error when no private key is configured")
	}
}

func TestNewMusicClient_EnvKey(t *testing.T) {
	t.Setenv("BLOCKRUN_WALLET_KEY", testPrivateKey)

	c, err := NewMusicClient("")
	if err != nil {
		t.Fatalf("NewMusicClient: %v", err)
	}
	if c.GetWalletAddress() != testWalletAddress {
		t.Errorf("address mismatch: got %s", c.GetWalletAddress())
	}
}

func TestMusicClient_GenerateValidation(t *testing.T) {
	c, err := NewMusicClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewMusicClient: %v", err)
	}

	if _, err := c.Generate(context.Background(), "  ", nil); err == nil {
		t.Error("expected error for blank prompt")
	}

	instrumental := true
	if _, err := c.Generate(context.Background(), "a song", &MusicGenerateOptions{
		Instrumental: &instrumental,
		Lyrics:       "la la la",
	}); err == nil {
		t.Error("expected error when lyrics combined with instrumental=true")
	}
}

func TestMusicClient_GenerateInstrumental(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/audio/generations") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1717000000,
			"model":   "minimax/music-2.5+",
			"data": []map[string]any{
				{"url": "https://cdn.example.com/track.mp3", "duration_seconds": 182.5},
			},
		})
	}))
	defer server.Close()

	c, _ := NewMusicClient(testPrivateKey, WithMusicAPIURL(server.URL))
	out, err := c.Generate(context.Background(), "upbeat synthwave with neon pads", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(out.Data) != 1 || out.Data[0].URL != "https://cdn.example.com/track.mp3" {
		t.Fatalf("unexpected response: %+v", out)
	}
	if out.Data[0].DurationSeconds != 182.5 {
		t.Errorf("duration_seconds = %v", out.Data[0].DurationSeconds)
	}
	// Defaults: instrumental track, default model.
	if sawBody["instrumental"] != true {
		t.Errorf("expected instrumental=true by default, got %v", sawBody["instrumental"])
	}
	if sawBody["model"] != DefaultMusicModel {
		t.Errorf("model = %v", sawBody["model"])
	}
	if _, ok := sawBody["lyrics"]; ok {
		t.Errorf("did not expect lyrics field, got %v", sawBody["lyrics"])
	}
}

func TestMusicClient_GenerateWithLyrics(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1717000000,
			"model":   "minimax/music-2.5+",
			"data":    []map[string]any{{"url": "https://cdn.example.com/vocal.mp3"}},
		})
	}))
	defer server.Close()

	c, _ := NewMusicClient(testPrivateKey, WithMusicAPIURL(server.URL))
	instrumental := false
	_, err := c.Generate(context.Background(), "upbeat pop", &MusicGenerateOptions{
		Instrumental: &instrumental,
		Lyrics:       "Hello world, this is my song",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if sawBody["instrumental"] != false {
		t.Errorf("expected instrumental=false, got %v", sawBody["instrumental"])
	}
	if sawBody["lyrics"] != "Hello world, this is my song" {
		t.Errorf("lyrics = %v", sawBody["lyrics"])
	}
}
