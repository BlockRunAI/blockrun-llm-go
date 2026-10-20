package blockrun

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestRoutingTableMatchesLiveCatalog holds every routed id to the live
// gateway catalog: listed, not available:false, and for the free profile
// billed as free. "available" is the gateway's live health flag, so an
// available:false failure can be a transient circuit-breaker trip; refetch
// before retargeting. Opt-in because it needs network:
//
//	BLOCKRUN_LIVE_CATALOG=1 go test -run TestRoutingTableMatchesLiveCatalog
func TestRoutingTableMatchesLiveCatalog(t *testing.T) {
	if os.Getenv("BLOCKRUN_LIVE_CATALOG") == "" {
		t.Skip("BLOCKRUN_LIVE_CATALOG not set, skipping live catalog check")
	}
	url := os.Getenv("BLOCKRUN_CATALOG_URL")
	if url == "" {
		url = "https://blockrun.ai/api/v1/models"
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	var catalog struct {
		Data []struct {
			ID          string `json:"id"`
			BillingMode string `json:"billing_mode"`
			Available   *bool  `json:"available"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	type entry struct {
		billing   string
		available bool
	}
	live := make(map[string]entry, len(catalog.Data))
	for _, m := range catalog.Data {
		live[m.ID] = entry{billing: m.BillingMode, available: m.Available == nil || *m.Available}
	}
	for profile, tiers := range routingTable {
		for tier, model := range tiers {
			e, ok := live[model]
			switch {
			case !ok:
				t.Errorf("%s/%s routes to %s, which /v1/models does not list", profile, tier, model)
			case !e.available:
				t.Errorf("%s/%s routes to %s, which /v1/models marks available:false", profile, tier, model)
			case profile == RoutingFree && e.billing != "free":
				t.Errorf("free/%s routes to %s, billed %q", tier, model, e.billing)
			}
		}
	}
}
