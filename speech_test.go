package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewSpeechClient_RequiresKey(t *testing.T) {
	t.Setenv("BLOCKRUN_WALLET_KEY", "")
	t.Setenv("BASE_CHAIN_WALLET_KEY", "")

	if _, err := NewSpeechClient(""); err == nil {
		t.Fatal("expected error when no private key is configured")
	}
}

func TestNewSpeechClient_EnvKey(t *testing.T) {
	t.Setenv("BLOCKRUN_WALLET_KEY", testPrivateKey)

	c, err := NewSpeechClient("")
	if err != nil {
		t.Fatalf("NewSpeechClient: %v", err)
	}
	if c.GetWalletAddress() != testWalletAddress {
		t.Errorf("address mismatch: got %s", c.GetWalletAddress())
	}
}

func TestSpeechClient_GenerateValidation(t *testing.T) {
	c, err := NewSpeechClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewSpeechClient: %v", err)
	}

	if _, err := c.Generate(context.Background(), "  ", nil); err == nil {
		t.Error("expected error for blank input")
	}
	if _, err := c.SoundEffect(context.Background(), "", nil); err == nil {
		t.Error("expected error for blank text")
	}
}

func TestSpeechClient_GenerateDefaults(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/audio/speech") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1749000000,
			"model":   "elevenlabs/flash-v2.5",
			"data": []map[string]any{
				{"url": "https://cdn.example.com/speech.mp3", "format": "mp3", "characters": 20},
			},
		})
	}))
	defer server.Close()

	c, _ := NewSpeechClient(testPrivateKey, WithSpeechAPIURL(server.URL))
	out, err := c.Generate(context.Background(), "Welcome to BlockRun.", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(out.Data) != 1 || out.Data[0].URL != "https://cdn.example.com/speech.mp3" {
		t.Fatalf("unexpected response: %+v", out)
	}
	if out.Data[0].Characters != 20 {
		t.Errorf("characters = %v", out.Data[0].Characters)
	}
	// Defaults: default model, no optional fields.
	if sawBody["model"] != DefaultSpeechModel {
		t.Errorf("model = %v", sawBody["model"])
	}
	for _, field := range []string{"voice", "response_format", "speed"} {
		if _, ok := sawBody[field]; ok {
			t.Errorf("did not expect %s field, got %v", field, sawBody[field])
		}
	}
}

func TestSpeechClient_GenerateWithOptions(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1749000000,
			"model":   "elevenlabs/v3",
			"data":    []map[string]any{{"url": "https://cdn.example.com/speech.wav"}},
		})
	}))
	defer server.Close()

	c, _ := NewSpeechClient(testPrivateKey, WithSpeechAPIURL(server.URL))
	speed := 1.1
	_, err := c.Generate(context.Background(), "Breaking news.", &SpeechGenerateOptions{
		Model:          "elevenlabs/v3",
		Voice:          "george",
		ResponseFormat: "wav",
		Speed:          &speed,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if sawBody["model"] != "elevenlabs/v3" {
		t.Errorf("model = %v", sawBody["model"])
	}
	if sawBody["voice"] != "george" {
		t.Errorf("voice = %v", sawBody["voice"])
	}
	if sawBody["response_format"] != "wav" {
		t.Errorf("response_format = %v", sawBody["response_format"])
	}
	if sawBody["speed"] != 1.1 {
		t.Errorf("speed = %v", sawBody["speed"])
	}
}

func TestSpeechClient_SoundEffect(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/audio/sound-effects") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1749000000,
			"model":   "elevenlabs/sound-effects",
			"data":    []map[string]any{{"url": "https://cdn.example.com/fx.mp3", "format": "mp3"}},
		})
	}))
	defer server.Close()

	c, _ := NewSpeechClient(testPrivateKey, WithSpeechAPIURL(server.URL))
	duration := 5.0
	influence := 0.7
	out, err := c.SoundEffect(context.Background(), "rain on a tin roof", &SoundEffectOptions{
		DurationSeconds: &duration,
		PromptInfluence: &influence,
	})
	if err != nil {
		t.Fatalf("SoundEffect: %v", err)
	}
	if out.Data[0].URL != "https://cdn.example.com/fx.mp3" {
		t.Fatalf("unexpected response: %+v", out)
	}
	if sawBody["model"] != DefaultSoundFxModel {
		t.Errorf("model = %v", sawBody["model"])
	}
	if sawBody["duration_seconds"] != 5.0 {
		t.Errorf("duration_seconds = %v", sawBody["duration_seconds"])
	}
	if sawBody["prompt_influence"] != 0.7 {
		t.Errorf("prompt_influence = %v", sawBody["prompt_influence"])
	}
}

func TestSpeechClient_ListVoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/v1/audio/voices") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"voice_id": "EXAVITQu4vr4xnSDxMaL", "name": "Sarah", "alias": "sarah"},
				{"voice_id": "JBFqnCBsd6RMkjVDRZzb", "name": "George", "alias": "george"},
			},
		})
	}))
	defer server.Close()

	c, _ := NewSpeechClient(testPrivateKey, WithSpeechAPIURL(server.URL))
	voices, err := c.ListVoices(context.Background())
	if err != nil {
		t.Fatalf("ListVoices: %v", err)
	}
	if len(voices) != 2 {
		t.Fatalf("expected 2 voices, got %d", len(voices))
	}
	if voices[0].Alias != "sarah" || voices[1].VoiceID != "JBFqnCBsd6RMkjVDRZzb" {
		t.Errorf("unexpected voices: %+v", voices)
	}
}
