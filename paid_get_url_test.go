package blockrun

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// echoURLServer answers 200 and records the request URL the server actually
// parsed — path and query as separate, decoded fields.
func echoURLServer(t *testing.T) (*httptest.Server, **url.URL) {
	t.Helper()
	var seen *url.URL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := *r.URL
		seen = &u
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// TestPaidGetEscapesQueryKeys pins that a caller-supplied query key cannot
// forge extra parameters. doGetWithPayment escaped the value and concatenated
// the key raw, so a key of `a&injected=1&b` split into three parameters on the
// server. Every caller (prediction_market, dex, defi, surf) forwards
// user-supplied maps straight through.
func TestPaidGetEscapesQueryKeys(t *testing.T) {
	srv, seen := echoURLServer(t)
	bc := &baseClient{apiURL: srv.URL, httpClient: srv.Client()}

	query := map[string]string{"a&injected=1&b": "x"}
	if _, err := bc.doGetWithPayment(context.Background(), "/v1/pm/markets", query); err != nil {
		t.Fatalf("GET failed: %v", err)
	}

	got := (*seen).Query()
	if _, forged := got["injected"]; forged {
		t.Errorf("query key forged an extra parameter: %v", got)
	}
	if len(got) != 1 || got.Get("a&injected=1&b") != "x" {
		t.Errorf("query = %v, want a single literal key", got)
	}
}

// TestPaidGetEscapesPathSegments pins that a caller-supplied path segment
// cannot rewrite the request target. `"/v1/pm/" + path` was concatenated raw,
// so a path containing '?' or '#' truncated the real path and replaced the
// query wholesale.
func TestPaidGetEscapesPathSegments(t *testing.T) {
	srv, seen := echoURLServer(t)
	bc := &baseClient{apiURL: srv.URL, httpClient: srv.Client()}

	if _, err := bc.doGetWithPayment(context.Background(), "/v1/pm/markets?evil=1", map[string]string{"real": "1"}); err != nil {
		t.Fatalf("GET failed: %v", err)
	}

	if got := (*seen).Path; got != "/v1/pm/markets?evil=1" {
		t.Errorf("path = %q, want the '?' kept inside the path segment", got)
	}
	if got := (*seen).Query(); len(got) != 1 || got.Get("real") != "1" {
		t.Errorf("query = %v, want only the caller's real parameter", got)
	}
}

// TestPaidGetPreservesOrdinaryURLs guards against over-escaping: the common
// case must still produce the plain path and query the gateway expects.
func TestPaidGetPreservesOrdinaryURLs(t *testing.T) {
	srv, seen := echoURLServer(t)
	bc := &baseClient{apiURL: srv.URL, httpClient: srv.Client()}

	query := map[string]string{"symbol": "BTC/USD", "limit": "100"}
	if _, err := bc.doGetWithPayment(context.Background(), "/v1/market/crypto/price/BTC", query); err != nil {
		t.Fatalf("GET failed: %v", err)
	}

	if got := (*seen).Path; got != "/v1/market/crypto/price/BTC" {
		t.Errorf("path = %q, want it unchanged", got)
	}
	got := (*seen).Query()
	if got.Get("symbol") != "BTC/USD" || got.Get("limit") != "100" {
		t.Errorf("query = %v, want both parameters round-tripped", got)
	}
}

// TestPaidGetQueryOrderIsStable pins deterministic encoding. Map iteration
// order used to leak into the URL, so the same call produced a different
// request string each time — noise for gateway logs and caches alike.
func TestPaidGetQueryOrderIsStable(t *testing.T) {
	srv, seen := echoURLServer(t)
	bc := &baseClient{apiURL: srv.URL, httpClient: srv.Client()}

	query := map[string]string{"z": "1", "a": "2", "m": "3"}
	for i := 0; i < 5; i++ {
		if _, err := bc.doGetWithPayment(context.Background(), "/v1/pm/markets", query); err != nil {
			t.Fatalf("GET failed: %v", err)
		}
		if got := (*seen).RawQuery; got != "a=2&m=3&z=1" {
			t.Errorf("RawQuery = %q, want the sorted encoding", got)
		}
	}
}
