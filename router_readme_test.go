package blockrun

import (
	"os"
	"strings"
	"testing"
)

// retiredRoutingIDs are ids the gateway has dropped from /v1/models or hides
// (hidden:true). Each is answered by a redirect to another model, so routing
// to one makes SmartChatResponse.Model name a model that never served the
// call. available:false is a transient health-breaker state, not retirement,
// so ids carrying it do not belong here. TestRoutingTableMatchesLiveCatalog
// (BLOCKRUN_LIVE_CATALOG=1) checks the table against the live catalog itself.
var retiredRoutingIDs = []string{
	"nvidia/deepseek-v4-flash",
	"nvidia/llama-4-maverick",
	"nvidia/qwen3-coder-480b",
	"nvidia/nemotron-3-super-120b", // hidden:true; gateway-internal cascade rung
	"moonshot/kimi-k2.5",           // hidden
	"moonshot/kimi-k2.6",           // hidden
	"moonshot/kimi-k2.7",           // hidden
}

func TestRoutingTableAvoidsRetiredIDs(t *testing.T) {
	for profile, tiers := range routingTable {
		for tier, model := range tiers {
			for _, id := range retiredRoutingIDs {
				if model == id {
					t.Errorf("%s/%s routes to retired %s", profile, tier, id)
				}
			}
		}
	}
}

// The free profile must stay inside the $0 NVIDIA-hosted tier.
func TestFreeRoutingStaysOnFreeTier(t *testing.T) {
	for tier, model := range routingTable[RoutingFree] {
		if !strings.HasPrefix(model, "nvidia/") {
			t.Errorf("free/%s routes to %s, outside the free NVIDIA tier", tier, model)
		}
	}
}

// The README's SmartChat profile table drifted from routingTable twice (it
// named kimi-k2.6 while the code routed to k2.7, and kept free ids after the
// gateway dropped them). Parse the table and hold it to the code.
func TestReadmeRoutingTableMatchesCode(t *testing.T) {
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	tiers := []RoutingTier{TierSimple, TierMedium, TierComplex, TierReasoning}
	seen := map[RoutingProfile]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "| **") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "| "), "|")
		if len(cells) != 5 {
			continue
		}
		profile := RoutingProfile(strings.Trim(strings.TrimSpace(cells[0]), "*"))
		row, ok := routingTable[profile]
		if !ok {
			continue
		}
		seen[profile] = true
		for i, tier := range tiers {
			got := strings.TrimSpace(cells[i+1])
			if got != row[tier] {
				t.Errorf("README %s/%s = %q, code routes to %q", profile, tier, got, row[tier])
			}
		}
	}
	for profile := range routingTable {
		if !seen[profile] {
			t.Errorf("README routing table has no row for profile %q", profile)
		}
	}
}
