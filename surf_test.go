package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSurfEndpoints_Catalog(t *testing.T) {
	all := SurfEndpoints()
	if len(all) < 70 {
		t.Errorf("expected ~83 catalog entries, got %d", len(all))
	}

	// Spot-check a few well-known endpoints.
	info := SurfEndpointInfo("market/ranking")
	if info == nil {
		t.Fatal("market/ranking should be in catalog")
	}
	if info.Method != "GET" || info.Tier != 1 || info.PriceUSD != 0.001 {
		t.Errorf("market/ranking unexpected info: %+v", info)
	}

	sql := SurfEndpointInfo("onchain/sql")
	if sql == nil || sql.Method != "POST" || sql.Tier != 3 || sql.PriceUSD != 0.020 {
		t.Errorf("onchain/sql unexpected info: %+v", sql)
	}

	if SurfEndpointInfo("not/a/real/endpoint") != nil {
		t.Error("unknown endpoint should return nil")
	}
}

func TestSurfPrice(t *testing.T) {
	p, err := SurfPrice("market/ranking")
	if err != nil || p != 0.001 {
		t.Errorf("SurfPrice(tier 1) = %v, %v", p, err)
	}
	p, err = SurfPrice("token/holders")
	if err != nil || p != 0.005 {
		t.Errorf("SurfPrice(tier 2) = %v, %v", p, err)
	}
	p, err = SurfPrice("onchain/sql")
	if err != nil || p != 0.020 {
		t.Errorf("SurfPrice(tier 3) = %v, %v", p, err)
	}
	if _, err := SurfPrice("nope"); err == nil {
		t.Error("expected error for unknown path")
	}
}

func TestSurfClient_GetRequiredParamValidation(t *testing.T) {
	c, err := NewSurfClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewSurfClient: %v", err)
	}

	// exchange/price requires "pair"
	if _, err := c.Get(context.Background(), "exchange/price", nil); err == nil {
		t.Error("expected error for missing 'pair' param")
	}
	if _, err := c.Get(context.Background(), "exchange/price", map[string]any{"limit": 10}); err == nil {
		t.Error("expected error when 'pair' missing even if other params present")
	}
}

func TestSurfClient_MethodMismatch(t *testing.T) {
	c, err := NewSurfClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewSurfClient: %v", err)
	}
	// onchain/sql is POST — calling Get should fail validation.
	if _, err := c.Get(context.Background(), "onchain/sql", nil); err == nil {
		t.Error("expected method-mismatch error")
	}
	// market/ranking is GET — calling Post should fail.
	if _, err := c.Post(context.Background(), "market/ranking", nil); err == nil {
		t.Error("expected method-mismatch error")
	}
}

func TestSurfClient_Get_FreeEndpoint(t *testing.T) {
	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"symbol": "BTC"}},
		})
	}))
	defer server.Close()

	c, _ := NewSurfClient(testPrivateKey, WithSurfAPIURL(server.URL))

	out, err := c.Get(context.Background(), "market/ranking", map[string]any{"limit": 20})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, ok := out["data"]; !ok {
		t.Errorf("expected data in response: %+v", out)
	}
	if !strings.HasSuffix(gotPath, "/v1/surf/market/ranking") {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if !strings.Contains(gotQuery, "limit=20") {
		t.Errorf("expected limit in query, got %q", gotQuery)
	}
}

func TestSurfClient_Call_AutoRouting(t *testing.T) {
	var method string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	c, _ := NewSurfClient(testPrivateKey, WithSurfAPIURL(server.URL))

	// GET endpoint via Call
	if _, err := c.Call(context.Background(), "market/ranking", SurfCallOptions{}); err != nil {
		t.Fatalf("Call GET: %v", err)
	}
	if method != "GET" {
		t.Errorf("expected GET, got %s", method)
	}

	// POST endpoint via Call — onchain/sql with body
	_, err := c.Call(context.Background(), "onchain/sql", SurfCallOptions{
		Body: map[string]any{"query": "SELECT 1"},
	})
	if err != nil {
		t.Fatalf("Call POST: %v", err)
	}
	if method != "POST" {
		t.Errorf("expected POST, got %s", method)
	}
}

func TestStringifyParams_ListJoin(t *testing.T) {
	out := stringifyParams(map[string]any{
		"ids":    []string{"1", "2", "3"},
		"limit":  20,
		"sym":    "BTC",
		"ignore": nil,
	})
	if out["ids"] != "1,2,3" {
		t.Errorf("ids = %q", out["ids"])
	}
	if out["limit"] != "20" {
		t.Errorf("limit = %q", out["limit"])
	}
	if _, ok := out["ignore"]; ok {
		t.Error("nil values should be skipped")
	}
}
