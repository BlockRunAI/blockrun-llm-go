package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// solanaBalanceServer serves getTokenAccountsByOwner, recording the owner and
// mint filter it was asked for. amounts are raw base units (USDC has 6).
func solanaBalanceServer(t *testing.T, amounts ...string) (*httptest.Server, *struct{ owner, mint string }) {
	t.Helper()
	seen := &struct{ owner, mint string }{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Method != "getTokenAccountsByOwner" {
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"unexpected method %s"}}`, req.Method)
			return
		}
		if len(req.Params) > 0 {
			seen.owner, _ = req.Params[0].(string)
		}
		if len(req.Params) > 1 {
			if filter, ok := req.Params[1].(map[string]any); ok {
				seen.mint, _ = filter["mint"].(string)
			}
		}
		accounts := make([]string, 0, len(amounts))
		for _, amt := range amounts {
			accounts = append(accounts, fmt.Sprintf(
				`{"account":{"data":{"parsed":{"info":{"tokenAmount":{"amount":%q,"decimals":6}}}}}}`, amt))
		}
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"value":[%s]}}`, strings.Join(accounts, ","))
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

func solanaLLMClient(t *testing.T, rpcURL string) *LLMClient {
	t.Helper()
	c, err := NewLLMClientSolana(testSolanaKey(t), rpcURL)
	if err != nil {
		t.Fatalf("NewLLMClientSolana: %v", err)
	}
	return c
}

// TestGetBalanceSolanaQueriesSolana pins the fix for the silent-wrong-answer
// bug: GetBalance read the Base USDC contract over Base RPC using c.address,
// so a Solana client fed its bs58 pubkey into an eth_call. That does not error
// — it returns a meaningless number, which is worse than failing.
func TestGetBalanceSolanaQueriesSolana(t *testing.T) {
	srv, seen := solanaBalanceServer(t, "10500000") // 10.5 USDC
	c := solanaLLMClient(t, srv.URL)

	got, err := c.GetBalance(context.Background())
	if err != nil {
		t.Fatalf("GetBalance on a Solana client: %v", err)
	}
	if got != 10.5 {
		t.Errorf("balance = %v, want 10.5 from the Solana RPC", got)
	}
	if seen.owner != c.GetWalletAddress() {
		t.Errorf("queried owner %q, want the client's bs58 address %q", seen.owner, c.GetWalletAddress())
	}
	if seen.mint != USDCSolanaMainnet {
		t.Errorf("mint filter = %q, want the USDC SPL mint", seen.mint)
	}
}

// TestGetBalanceSolanaSumsAllTokenAccounts: one owner can hold several token
// accounts for the same mint. Reading only the first under-reports the balance.
func TestGetBalanceSolanaSumsAllTokenAccounts(t *testing.T) {
	srv, _ := solanaBalanceServer(t, "1000000", "2500000", "250000") // 1 + 2.5 + 0.25
	c := solanaLLMClient(t, srv.URL)

	got, err := c.GetBalance(context.Background())
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if got != 3.75 {
		t.Errorf("balance = %v, want 3.75 summed across all token accounts", got)
	}
}

// TestGetBalanceSolanaNoTokenAccount: a funded-nothing wallet has no token
// account at all. That is a zero balance, not an error.
func TestGetBalanceSolanaNoTokenAccount(t *testing.T) {
	srv, _ := solanaBalanceServer(t)
	c := solanaLLMClient(t, srv.URL)

	got, err := c.GetBalance(context.Background())
	if err != nil {
		t.Fatalf("a wallet with no USDC token account should read 0, not error: %v", err)
	}
	if got != 0 {
		t.Errorf("balance = %v, want 0", got)
	}
}

// TestGetBalanceTestnetRejectsSolana: the testnet helper is Base Sepolia. There
// is no configured Solana devnet mint, so it must say so rather than quietly
// querying the wrong chain.
func TestGetBalanceTestnetRejectsSolana(t *testing.T) {
	srv, _ := solanaBalanceServer(t, "1000000")
	c := solanaLLMClient(t, srv.URL)

	_, err := c.GetBalanceTestnet(context.Background())
	if err == nil {
		t.Fatal("expected GetBalanceTestnet to reject a Solana client")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "solana") {
		t.Errorf("error %v should name the chain mismatch", err)
	}
}

// TestGetBalanceBaseStillQueriesBase pins the other half: routing on chain must
// not disturb the Base path.
func TestGetBalanceBaseStillQueriesBase(t *testing.T) {
	var sawMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		sawMethod = req.Method
		_ = json.NewEncoder(w).Encode(rpcResponse{
			JSONRPC: "2.0", ID: 1,
			Result: "0x0000000000000000000000000000000000000000000000000000000000a037a0",
		})
	}))
	defer srv.Close()

	got, err := getUSDCBalance(context.Background(),
		"0x1234567890abcdef1234567890abcdef12345678", USDCBaseContract, []string{srv.URL})
	if err != nil {
		t.Fatalf("Base balance: %v", err)
	}
	if got != 10.5 || sawMethod != "eth_call" {
		t.Errorf("balance = %v via %q, want 10.5 via eth_call", got, sawMethod)
	}
}
