package blockrun

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBalanceSuccessful(t *testing.T) {
	// 10.5 USDC = 10_500_000 = 0xA037A0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Method != "eth_call" {
			t.Errorf("expected method eth_call, got %s", req.Method)
		}

		resp := rpcResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result:  "0x0000000000000000000000000000000000000000000000000000000000a037a0",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	balance, err := getUSDCBalance(
		context.Background(),
		"0x1234567890abcdef1234567890abcdef12345678",
		USDCBaseContract,
		[]string{server.URL},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if balance != 10.5 {
		t.Errorf("expected balance 10.5, got %f", balance)
	}
}

func TestBalanceZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := rpcResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result:  "0x0000000000000000000000000000000000000000000000000000000000000000",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	balance, err := getUSDCBalance(
		context.Background(),
		"0x1234567890abcdef1234567890abcdef12345678",
		USDCBaseContract,
		[]string{server.URL},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if balance != 0 {
		t.Errorf("expected balance 0, got %f", balance)
	}
}

func TestBalanceRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := rpcResponse{
			JSONRPC: "2.0",
			ID:      1,
			Error: &rpcError{
				Code:    -32000,
				Message: "execution reverted",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	_, err := getUSDCBalance(
		context.Background(),
		"0x1234567890abcdef1234567890abcdef12345678",
		USDCBaseContract,
		[]string{server.URL},
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestBalanceHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	_, err := getUSDCBalance(
		context.Background(),
		"0x1234567890abcdef1234567890abcdef12345678",
		USDCBaseContract,
		[]string{server.URL},
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestBalanceFallbackRPC(t *testing.T) {
	callCount := 0

	// First server fails, second succeeds
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failServer.Close()

	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// 1 USDC = 1_000_000 = 0xF4240
		resp := rpcResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result:  "0x00000000000000000000000000000000000000000000000000000000000f4240",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer successServer.Close()

	balance, err := getUSDCBalance(
		context.Background(),
		"0x1234567890abcdef1234567890abcdef12345678",
		USDCBaseContract,
		[]string{failServer.URL, successServer.URL},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if balance != 1.0 {
		t.Errorf("expected balance 1.0, got %f", balance)
	}

	if callCount != 2 {
		t.Errorf("expected 2 RPC calls (1 fail + 1 success), got %d", callCount)
	}
}

func TestParseUSDCBalance(t *testing.T) {
	tests := []struct {
		name     string
		hex      string
		expected float64
	}{
		{"zero", "0x0", 0},
		{"empty after prefix", "0x", 0},
		{"one USDC", "0xf4240", 1.0},
		{"10.5 USDC", "0xa037a0", 10.5},
		{"large balance", "0x5f5e100", 100.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseUSDCBalance(tt.hex)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, result)
			}
		})
	}
}
