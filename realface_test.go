package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewRealFaceClient_RequiresKey(t *testing.T) {
	t.Setenv("BLOCKRUN_WALLET_KEY", "")
	t.Setenv("BASE_CHAIN_WALLET_KEY", "")

	if _, err := NewRealFaceClient(""); err == nil {
		t.Fatal("expected error when no private key is configured")
	}
}

func TestRealFaceClient_Validation(t *testing.T) {
	c, err := NewRealFaceClient(testPrivateKey)
	if err != nil {
		t.Fatalf("NewRealFaceClient: %v", err)
	}
	ctx := context.Background()

	if _, err := c.Init(ctx, "", ""); err == nil {
		t.Error("expected error for empty name")
	}
	if _, err := c.Init(ctx, "Jane", "rf_123"); err == nil {
		t.Error("expected error for malformed groupID on Init")
	}
	if _, err := c.Status(ctx, "not-a-group"); err == nil {
		t.Error("expected error for malformed groupID on Status")
	}
	if _, err := c.Enroll(ctx, "Jane", "https://example.com/j.jpg", "bad"); err == nil {
		t.Error("expected error for malformed groupID on Enroll")
	}
	if _, err := c.Enroll(ctx, "Jane", "ftp://example.com/j.jpg", "legacy_rf_1"); err == nil {
		t.Error("expected error for non-http image url on Enroll")
	}
}

func TestRealFaceClient_Init(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/realface/init") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object":             "realface.init",
			"group_id":           "legacy_rf_8137",
			"h5_link":            "https://blockrun.ai/rf/abc",
			"status":             "pending_validation",
			"expires_in_seconds": 120,
		})
	}))
	defer server.Close()

	c, _ := NewRealFaceClient(testPrivateKey, WithRealFaceAPIURL(server.URL))
	out, err := c.Init(context.Background(), "Jane — Q3 spokesperson", "")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if out.GroupID != "legacy_rf_8137" || out.H5Link == "" {
		t.Fatalf("unexpected init: %+v", out)
	}
	if sawBody["name"] != "Jane — Q3 spokesperson" {
		t.Errorf("name = %v", sawBody["name"])
	}
	if _, ok := sawBody["groupId"]; ok {
		t.Errorf("did not expect groupId when not provided")
	}
}

func TestRealFaceClient_Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/realface/status") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("groupId"); got != "legacy_rf_8137" {
			t.Errorf("groupId query = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object":            "realface.status",
			"group_id":          "legacy_rf_8137",
			"status":            "active",
			"ready_to_finalize": true,
		})
	}))
	defer server.Close()

	c, _ := NewRealFaceClient(testPrivateKey, WithRealFaceAPIURL(server.URL))
	out, err := c.Status(context.Background(), "legacy_rf_8137")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !out.ReadyToFinalize || out.Status != "active" {
		t.Errorf("status = %+v", out)
	}
}

func TestRealFaceClient_WaitForActive(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		status := "pending_validation"
		ready := false
		if calls >= 2 {
			status = "active"
			ready = true
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"group_id":          "legacy_rf_8137",
			"status":            status,
			"ready_to_finalize": ready,
		})
	}))
	defer server.Close()

	c, _ := NewRealFaceClient(testPrivateKey, WithRealFaceAPIURL(server.URL))
	out, err := c.WaitForActive(context.Background(), "legacy_rf_8137", &WaitForActiveOptions{
		Timeout:      2 * time.Second,
		PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("WaitForActive: %v", err)
	}
	if !out.ReadyToFinalize {
		t.Errorf("expected ready_to_finalize, got %+v", out)
	}
	if calls < 2 {
		t.Errorf("expected at least 2 polls, got %d", calls)
	}
}

func TestRealFaceClient_WaitForActive_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"group_id":          "legacy_rf_8137",
			"status":            "pending_validation",
			"ready_to_finalize": false,
		})
	}))
	defer server.Close()

	c, _ := NewRealFaceClient(testPrivateKey, WithRealFaceAPIURL(server.URL))
	_, err := c.WaitForActive(context.Background(), "legacy_rf_8137", &WaitForActiveOptions{
		Timeout:      30 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusGatewayTimeout {
		t.Errorf("expected 504 APIError, got %v", err)
	}
}

func TestRealFaceClient_Enroll(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/realface/enroll") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object":    "realface",
			"asset_id":  "ta_real1234567890",
			"group_id":  "legacy_rf_8137",
			"name":      "Jane",
			"image_url": "https://example.com/jane.jpg",
			"settlement": map[string]any{
				"success": true,
				"tx_hash": "0x9f3a",
			},
		})
	}))
	defer server.Close()

	c, _ := NewRealFaceClient(testPrivateKey, WithRealFaceAPIURL(server.URL))
	out, err := c.Enroll(context.Background(), "Jane", "https://example.com/jane.jpg", "legacy_rf_8137")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if out.AssetID != "ta_real1234567890" {
		t.Errorf("asset_id = %q", out.AssetID)
	}
	if sawBody["group_id"] != "legacy_rf_8137" {
		t.Errorf("body group_id = %v", sawBody["group_id"])
	}
}

func TestRealFaceClient_ListRealFaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/realfaces") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"wallet": testWalletAddress,
			"count":  1,
			"realfaces": []map[string]any{
				{"assetId": "ta_real", "name": "Jane", "byteplusAssetId": "bp_1"},
			},
		})
	}))
	defer server.Close()

	c, _ := NewRealFaceClient(testPrivateKey, WithRealFaceAPIURL(server.URL))
	out, err := c.ListRealFaces(context.Background(), "")
	if err != nil {
		t.Fatalf("ListRealFaces: %v", err)
	}
	if out.Count != 1 || len(out.RealFaces) != 1 {
		t.Fatalf("unexpected list: %+v", out)
	}
	if out.RealFaces[0].AssetID != "ta_real" || out.RealFaces[0].ByteplusAssetID != "bp_1" {
		t.Errorf("realface = %+v", out.RealFaces[0])
	}
}
