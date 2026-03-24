package blockrun

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheKey(t *testing.T) {
	// Same inputs should produce the same key
	key1 := cacheKey("/v1/x/tweets", map[string]any{"query": "test"})
	key2 := cacheKey("/v1/x/tweets", map[string]any{"query": "test"})
	if key1 != key2 {
		t.Errorf("cacheKey not consistent: %s != %s", key1, key2)
	}

	// Different inputs should produce different keys
	key3 := cacheKey("/v1/x/tweets", map[string]any{"query": "other"})
	if key1 == key3 {
		t.Errorf("cacheKey collision: same key for different inputs")
	}

	// Different endpoints should produce different keys
	key4 := cacheKey("/v1/pm/markets", map[string]any{"query": "test"})
	if key1 == key4 {
		t.Errorf("cacheKey collision: same key for different endpoints")
	}

	// Nil body should work
	key5 := cacheKey("/v1/search", nil)
	key6 := cacheKey("/v1/search", nil)
	if key5 != key6 {
		t.Errorf("cacheKey not consistent with nil body: %s != %s", key5, key6)
	}
}

func TestTTLFor(t *testing.T) {
	tests := []struct {
		endpoint string
		wantZero bool
		wantMin  time.Duration
	}{
		{"/v1/x/tweets", false, 1 * time.Hour},
		{"/v1/x/user/profile", false, 1 * time.Hour},
		{"/v1/pm/markets", false, 30 * time.Minute},
		{"/v1/pm/events", false, 30 * time.Minute},
		{"/v1/search", false, 15 * time.Minute},
		{"/v1/search?q=test", false, 15 * time.Minute},
		{"/v1/chat/completions", true, 0},
		{"/v1/images/generate", true, 0},
		{"/v1/unknown", true, 0},
	}

	for _, tt := range tests {
		ttl := ttlFor(tt.endpoint)
		if tt.wantZero && ttl != 0 {
			t.Errorf("ttlFor(%q) = %v, want 0", tt.endpoint, ttl)
		}
		if !tt.wantZero && ttl < tt.wantMin {
			t.Errorf("ttlFor(%q) = %v, want >= %v", tt.endpoint, ttl, tt.wantMin)
		}
	}
}

func TestCacheHitMiss(t *testing.T) {
	dir := t.TempDir()
	c := newCacheWithDir(dir)

	endpoint := "/v1/x/tweets"
	body := map[string]any{"query": "golang"}
	response := []byte(`{"data":[{"text":"hello"}]}`)

	// Miss before set
	if _, ok := c.Get(endpoint, body); ok {
		t.Error("expected cache miss before Set")
	}

	// Set and hit
	c.Set(endpoint, body, response)
	cached, ok := c.Get(endpoint, body)
	if !ok {
		t.Fatal("expected cache hit after Set")
	}
	if string(cached) != string(response) {
		t.Errorf("cached response = %s, want %s", cached, response)
	}

	// Different body should miss
	if _, ok := c.Get(endpoint, map[string]any{"query": "rust"}); ok {
		t.Error("expected cache miss for different body")
	}
}

func TestCacheNoStoreForZeroTTL(t *testing.T) {
	dir := t.TempDir()
	c := newCacheWithDir(dir)

	// Chat endpoints have TTL=0, should not be cached
	endpoint := "/v1/chat/completions"
	body := map[string]any{"model": "gpt-4"}
	response := []byte(`{"choices": []}`)

	c.Set(endpoint, body, response)
	if _, ok := c.Get(endpoint, body); ok {
		t.Error("expected no cache for zero-TTL endpoint")
	}

	// Verify no file was written
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected empty cache dir for zero-TTL endpoint, got %d files", len(entries))
	}
}

func TestCacheTTLExpiration(t *testing.T) {
	dir := t.TempDir()
	c := newCacheWithDir(dir)

	endpoint := "/v1/x/tweets"
	body := map[string]any{"query": "test"}
	response := []byte(`{"data": []}`)

	// Write a cache entry with a timestamp in the past
	key := cacheKey(endpoint, body)
	path := filepath.Join(dir, key+".json")

	// Set cached_at to 2 hours ago (TTL for /v1/x/ is 1 hour)
	pastTime := float64(time.Now().Add(-2*time.Hour).UnixNano()) / 1e9
	entryJSON := []byte(`{"cached_at":` + formatFloat(pastTime) + `,"endpoint":"` + endpoint + `","response":` + string(response) + `}`)
	os.WriteFile(path, entryJSON, 0644)

	// Should miss due to expiration
	if _, ok := c.Get(endpoint, body); ok {
		t.Error("expected cache miss for expired entry")
	}
}

func TestCacheClear(t *testing.T) {
	dir := t.TempDir()
	c := newCacheWithDir(dir)

	// Add some entries
	c.Set("/v1/x/tweets", map[string]any{"q": "a"}, []byte(`{"a":1}`))
	c.Set("/v1/pm/markets", map[string]any{"q": "b"}, []byte(`{"b":2}`))

	// Verify files exist
	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		t.Fatal("expected cache files to exist before Clear")
	}

	// Clear
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear() error: %v", err)
	}

	// Verify files removed
	entries, _ = os.ReadDir(dir)
	jsonCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			jsonCount++
		}
	}
	if jsonCount != 0 {
		t.Errorf("expected 0 json files after Clear, got %d", jsonCount)
	}

	// Verify cache misses
	if _, ok := c.Get("/v1/x/tweets", map[string]any{"q": "a"}); ok {
		t.Error("expected cache miss after Clear")
	}
}

func TestCacheGetWithNilBody(t *testing.T) {
	dir := t.TempDir()
	c := newCacheWithDir(dir)

	endpoint := "/v1/search"
	response := []byte(`{"results":[]}`)

	c.Set(endpoint, nil, response)
	cached, ok := c.Get(endpoint, nil)
	if !ok {
		t.Fatal("expected cache hit with nil body")
	}
	if string(cached) != string(response) {
		t.Errorf("cached = %s, want %s", cached, response)
	}
}

// formatFloat formats a float64 without scientific notation.
func formatFloat(f float64) string {
	return fmt.Sprintf("%.6f", f)
}

