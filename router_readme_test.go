package blockrun

import (
	"os"
	"strings"
	"testing"
)

// The free profile must not name ids the gateway has dropped from /v1/models.
// Each of these is answered by a MODEL_REDIRECTS target on the gateway; the
// table names that target instead.
func TestFreeRoutingAvoidsRetiredIDs(t *testing.T) {
	retired := []string{
		"nvidia/deepseek-v4-flash",
		"nvidia/llama-4-maverick",
		"nvidia/qwen3-coder-480b",
	}
	for tier, model := range routingTable[RoutingFree] {
		for _, id := range retired {
			if model == id {
				t.Errorf("free/%s routes to retired %s", tier, id)
			}
		}
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
