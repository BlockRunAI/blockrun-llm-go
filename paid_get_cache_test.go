package blockrun

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// countingPaidGetServer serves a 402 on an unpaid GET and 200 once a
// PAYMENT-SIGNATURE is presented, echoing the query back so a caller can tell
// two different queries apart. It counts every request it receives.
type countingPaidGetServer struct {
	requests atomic.Int64
	payments atomic.Int64
}

func newCountingPaidGetServer(t *testing.T, opt PaymentOption) (*countingPaidGetServer, *httptest.Server) {
	t.Helper()
	c := &countingPaidGetServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.requests.Add(1)
		if r.Header.Get("PAYMENT-SIGNATURE") != "" {
			c.payments.Add(1)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"q":%q}`, r.URL.Query().Get("q"))
			return
		}
		req := PaymentRequirement{
			X402Version: 2,
			Accepts:     []PaymentOption{opt},
			Resource:    ResourceInfo{URL: "https://example.test/resource"},
		}
		body, err := json.Marshal(req)
		if err != nil {
			t.Errorf("marshal payment requirement: %v", err)
		}
		w.Header().Set("payment-required", base64.StdEncoding.EncodeToString(body))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(srv.Close)
	return c, srv
}

// solanaPaidGetClient is a Solana client wired to local fakes: no network
// leaves the machine, and the cache lives in a temp dir.
func solanaPaidGetClient(t *testing.T, apiURL string, cache *Cache) *baseClient {
	t.Helper()
	resetSolanaBlockhashCacheForTest(t)
	_, rpcSrv := newRPCCounterServer(t, usdcSolanaDecimals)
	return &baseClient{
		chain:        chainSolana,
		solanaKey:    testSolanaKey(t),
		solanaRPCURL: rpcSrv.URL,
		apiURL:       apiURL,
		httpClient:   &http.Client{},
		cache:        cache,
	}
}

// TestPaidGetServesRepeatFromCache pins that a repeated paid GET is served from
// the cache instead of re-paying.
//
// doGetWithPayment used to ignore bc.cache entirely, unlike doGet, so
// WithCache(true) was a silent no-op on exactly the endpoints that cost money:
// /v1/pm/ carries a 30-minute TTL, yet every repeat call re-signed and re-paid.
func TestPaidGetServesRepeatFromCache(t *testing.T) {
	counter, srv := newCountingPaidGetServer(t, *testPaymentOption(USDCSolanaMainnet))
	bc := solanaPaidGetClient(t, srv.URL, newCacheWithDir(t.TempDir()))
	query := map[string]string{"q": "bitcoin"}

	first, err := bc.doGetWithPayment(context.Background(), "/v1/pm/markets", query)
	if err != nil {
		t.Fatalf("first paid GET: %v", err)
	}
	// One 402 + one paid retry.
	if got := counter.requests.Load(); got != 2 {
		t.Fatalf("requests after first call = %d, want 2 (402 + paid retry)", got)
	}

	second, err := bc.doGetWithPayment(context.Background(), "/v1/pm/markets", query)
	if err != nil {
		t.Fatalf("second paid GET: %v", err)
	}
	if string(second) != string(first) {
		t.Errorf("cached body = %s, want %s", second, first)
	}
	if got := counter.requests.Load(); got != 2 {
		t.Errorf("requests after second call = %d, want 2 (cache hit, no network)", got)
	}
	if got := counter.payments.Load(); got != 1 {
		t.Errorf("payments = %d, want 1 — the repeat call re-paid", got)
	}
	if got := bc.GetSpending(); got.Calls != 1 || got.TotalUSD != 0.001 {
		t.Errorf("spending = %+v, want 1 call totalling $0.001", got)
	}
}

// TestPaidGetCacheKeyIncludesQuery pins that two different queries on one
// endpoint do not share a cache entry. Keying on the endpoint alone would make
// the cache serve bitcoin's response for an ethereum query — worse than not
// caching at all.
func TestPaidGetCacheKeyIncludesQuery(t *testing.T) {
	counter, srv := newCountingPaidGetServer(t, *testPaymentOption(USDCSolanaMainnet))
	bc := solanaPaidGetClient(t, srv.URL, newCacheWithDir(t.TempDir()))

	btc, err := bc.doGetWithPayment(context.Background(), "/v1/pm/markets", map[string]string{"q": "bitcoin"})
	if err != nil {
		t.Fatalf("bitcoin query: %v", err)
	}
	eth, err := bc.doGetWithPayment(context.Background(), "/v1/pm/markets", map[string]string{"q": "ethereum"})
	if err != nil {
		t.Fatalf("ethereum query: %v", err)
	}

	if string(btc) != `{"q":"bitcoin"}` {
		t.Errorf("bitcoin body = %s", btc)
	}
	if string(eth) != `{"q":"ethereum"}` {
		t.Errorf("ethereum body = %s — a stale cache entry was served for a different query", eth)
	}
	if got := counter.payments.Load(); got != 2 {
		t.Errorf("payments = %d, want 2 (distinct queries must not share a cache entry)", got)
	}
}

// TestPaidGetWithoutCacheStillWorks pins that a client with no cache configured
// is unaffected — every call goes to the network, as before.
func TestPaidGetWithoutCacheStillWorks(t *testing.T) {
	counter, srv := newCountingPaidGetServer(t, *testPaymentOption(USDCSolanaMainnet))
	bc := solanaPaidGetClient(t, srv.URL, nil)
	query := map[string]string{"q": "bitcoin"}

	for i := 0; i < 2; i++ {
		if _, err := bc.doGetWithPayment(context.Background(), "/v1/pm/markets", query); err != nil {
			t.Fatalf("paid GET %d: %v", i+1, err)
		}
	}
	if got := counter.payments.Load(); got != 2 {
		t.Errorf("payments = %d, want 2 with no cache configured", got)
	}
}

// TestPaidGetNoTTLEndpointNotCached pins that only endpoints with a configured
// TTL are cached. /v1/zerox/ has none, so it must still hit the network every
// time rather than silently serving stale swap quotes.
func TestPaidGetNoTTLEndpointNotCached(t *testing.T) {
	counter, srv := newCountingPaidGetServer(t, *testPaymentOption(USDCSolanaMainnet))
	bc := solanaPaidGetClient(t, srv.URL, newCacheWithDir(t.TempDir()))
	query := map[string]string{"q": "bitcoin"}

	for i := 0; i < 2; i++ {
		if _, err := bc.doGetWithPayment(context.Background(), "/v1/zerox/quote", query); err != nil {
			t.Fatalf("paid GET %d: %v", i+1, err)
		}
	}
	if got := counter.payments.Load(); got != 2 {
		t.Errorf("payments = %d, want 2 — /v1/zerox/ has no TTL and must not be cached", got)
	}
}
