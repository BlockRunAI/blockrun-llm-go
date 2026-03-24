package blockrun

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Default TTL values by endpoint pattern.
var defaultTTL = map[string]time.Duration{
	"/v1/x/":      1 * time.Hour,
	"/v1/pm/":     30 * time.Minute,
	"/v1/search":  15 * time.Minute,
	"/v1/chat/":   0, // no cache
	"/v1/images/": 0, // no cache
}

// CacheEntry represents a cached API response with metadata.
type CacheEntry struct {
	CachedAt float64         `json:"cached_at"`
	Endpoint string          `json:"endpoint"`
	Response json.RawMessage `json:"response"`
}

// Cache provides file-backed response caching with per-endpoint TTL.
type Cache struct {
	mu  sync.RWMutex
	dir string
}

// NewCache creates a new Cache that stores entries in ~/.blockrun/cache.
func NewCache() *Cache {
	dir := filepath.Join(os.Getenv("HOME"), ".blockrun", "cache")
	os.MkdirAll(dir, 0755)
	return &Cache{dir: dir}
}

// newCacheWithDir creates a Cache with a custom directory (used for testing).
func newCacheWithDir(dir string) *Cache {
	os.MkdirAll(dir, 0755)
	return &Cache{dir: dir}
}

// cacheKey generates a short SHA256 key from endpoint + body.
func cacheKey(endpoint string, body map[string]any) string {
	data, _ := json.Marshal(map[string]any{"endpoint": endpoint, "body": body})
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)[:16]
}

// ttlFor returns the TTL for the given endpoint based on prefix matching.
// Returns 0 (no caching) for unrecognized endpoints.
func ttlFor(endpoint string) time.Duration {
	for prefix, ttl := range defaultTTL {
		if strings.HasPrefix(endpoint, prefix) {
			return ttl
		}
	}
	return 0
}

// Get retrieves a cached response for the given endpoint and body.
// Returns the cached response bytes and true on hit, or nil and false on miss.
func (c *Cache) Get(endpoint string, body map[string]any) (json.RawMessage, bool) {
	ttl := ttlFor(endpoint)
	if ttl == 0 {
		return nil, false
	}

	key := cacheKey(endpoint, body)
	path := filepath.Join(c.dir, key+".json")

	c.mu.RLock()
	defer c.mu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}

	// Check TTL expiration
	cachedAt := time.Unix(0, int64(entry.CachedAt*1e9))
	if time.Since(cachedAt) > ttl {
		return nil, false
	}

	return entry.Response, true
}

// Set stores a response in the cache if the endpoint has a non-zero TTL.
func (c *Cache) Set(endpoint string, body map[string]any, response []byte) {
	ttl := ttlFor(endpoint)
	if ttl == 0 {
		return
	}

	key := cacheKey(endpoint, body)
	path := filepath.Join(c.dir, key+".json")

	entry := CacheEntry{
		CachedAt: float64(time.Now().UnixNano()) / 1e9,
		Endpoint: endpoint,
		Response: json.RawMessage(response),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	os.WriteFile(path, data, 0644)
}

// Clear removes all cached entries.
func (c *Cache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %w", err)
	}

	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			os.Remove(filepath.Join(c.dir, entry.Name()))
		}
	}

	return nil
}
