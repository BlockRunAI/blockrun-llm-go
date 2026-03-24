package blockrun

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCostLogAppendWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cost_log.jsonl")
	cl := newCostLogWithPath(path)

	if err := cl.Append("/v1/chat/completions", 0.005); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var entry CostLogEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("failed to unmarshal entry: %v", err)
	}

	if entry.Endpoint != "/v1/chat/completions" {
		t.Errorf("expected endpoint /v1/chat/completions, got %s", entry.Endpoint)
	}
	if entry.CostUSD != 0.005 {
		t.Errorf("expected cost 0.005, got %f", entry.CostUSD)
	}
	if entry.Timestamp <= 0 {
		t.Errorf("expected positive timestamp, got %f", entry.Timestamp)
	}
}

func TestCostLogSummaryAggregates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cost_log.jsonl")
	cl := newCostLogWithPath(path)

	cl.Append("/v1/chat/completions", 0.005)
	cl.Append("/v1/chat/completions", 0.010)
	cl.Append("/v1/images/generations", 0.020)

	summary, err := cl.Summary()
	if err != nil {
		t.Fatalf("Summary failed: %v", err)
	}

	if summary.Calls != 3 {
		t.Errorf("expected 3 calls, got %d", summary.Calls)
	}

	expectedTotal := 0.035
	if math.Abs(summary.TotalUSD-expectedTotal) > 1e-9 {
		t.Errorf("expected total %.6f, got %.6f", expectedTotal, summary.TotalUSD)
	}

	chatCost := summary.ByEndpoint["/v1/chat/completions"]
	if math.Abs(chatCost-0.015) > 1e-9 {
		t.Errorf("expected chat cost 0.015, got %.6f", chatCost)
	}

	imageCost := summary.ByEndpoint["/v1/images/generations"]
	if math.Abs(imageCost-0.020) > 1e-9 {
		t.Errorf("expected image cost 0.020, got %.6f", imageCost)
	}
}

func TestCostLogEmptyReturnsZeroSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cost_log.jsonl")
	cl := newCostLogWithPath(path)

	// File doesn't exist yet
	summary, err := cl.Summary()
	if err != nil {
		t.Fatalf("Summary on non-existent file failed: %v", err)
	}

	if summary.TotalUSD != 0 {
		t.Errorf("expected 0 total, got %f", summary.TotalUSD)
	}
	if summary.Calls != 0 {
		t.Errorf("expected 0 calls, got %d", summary.Calls)
	}
	if len(summary.ByEndpoint) != 0 {
		t.Errorf("expected empty ByEndpoint, got %v", summary.ByEndpoint)
	}
}

func TestCostLogMultipleEntriesSameEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cost_log.jsonl")
	cl := newCostLogWithPath(path)

	for i := 0; i < 5; i++ {
		cl.Append("/v1/chat/completions", 0.001)
	}

	summary, err := cl.Summary()
	if err != nil {
		t.Fatalf("Summary failed: %v", err)
	}

	if summary.Calls != 5 {
		t.Errorf("expected 5 calls, got %d", summary.Calls)
	}

	expectedTotal := 0.005
	if math.Abs(summary.TotalUSD-expectedTotal) > 1e-9 {
		t.Errorf("expected total %.6f, got %.6f", expectedTotal, summary.TotalUSD)
	}

	if len(summary.ByEndpoint) != 1 {
		t.Errorf("expected 1 endpoint, got %d", len(summary.ByEndpoint))
	}
}
