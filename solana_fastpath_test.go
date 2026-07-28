package blockrun

// Tests for the Solana payment fast path: hardcoded USDC mint info (no
// getAccountInfo round trip) and the per-endpoint blockhash cache (no
// getLatestBlockhash on the hot path). Mirrors the TS SDK optimization; the
// duplicate-transaction guard the TS SDK needs is deliberately absent here
// because every Go transaction carries a random 16-byte memo nonce, so two
// payments can never be byte-identical.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/mr-tron/base58"
)

// solanaRPCCounter serves getLatestBlockhash and getAccountInfo, counting calls.
type solanaRPCCounter struct {
	blockhashCalls atomic.Int64
	mintCalls      atomic.Int64
	blockhash      string
	mintDecimals   uint8
}

func (c *solanaRPCCounter) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch req.Method {
		case "getLatestBlockhash":
			c.blockhashCalls.Add(1)
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"value":{"blockhash":%q}}}`, c.blockhash)
		case "getAccountInfo":
			c.mintCalls.Add(1)
			raw := make([]byte, 82)
			raw[44] = c.mintDecimals
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"value":{"owner":%q,"data":[%q,"base64"]}}}`,
				tokenProgramAddress, base64.StdEncoding.EncodeToString(raw))
		default:
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"unexpected method %s"}}`, req.Method)
		}
	}
}

func newRPCCounterServer(t *testing.T, decimals uint8) (*solanaRPCCounter, *httptest.Server) {
	t.Helper()
	counter := &solanaRPCCounter{blockhash: makeBlockhash(t).String(), mintDecimals: decimals}
	srv := httptest.NewServer(counter.handler())
	t.Cleanup(srv.Close)
	return counter, srv
}

// resetSolanaBlockhashCacheForTest empties the package-level blockhash cache so
// each test starts cold.
func resetSolanaBlockhashCacheForTest(t *testing.T) {
	t.Helper()
	solanaBlockhashCache.mu.Lock()
	solanaBlockhashCache.entries = map[string]solanaBlockhashEntry{}
	solanaBlockhashCache.mu.Unlock()
}

func testPaymentOption(asset string) *PaymentOption {
	return &PaymentOption{
		Scheme:            "exact",
		Network:           "solana",
		Amount:            "1000",
		Asset:             asset,
		PayTo:             solana.NewWallet().PublicKey().String(),
		MaxTimeoutSeconds: 60,
		Extra:             map[string]any{"feePayer": solana.NewWallet().PublicKey().String()},
	}
}

func testSolanaKey(t *testing.T) string {
	t.Helper()
	priv, err := solana.NewRandomPrivateKey()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	return base58.Encode(priv)
}

// decodePaymentTx unwraps the base64 envelope down to the signed transaction.
func decodePaymentTx(t *testing.T, payload string) *solana.Transaction {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	var env struct {
		Payload map[string]string `json:"payload"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	txBytes, err := base64.StdEncoding.DecodeString(env.Payload["transaction"])
	if err != nil {
		t.Fatalf("decode tx: %v", err)
	}
	tx, err := solana.TransactionFromBytes(txBytes)
	if err != nil {
		t.Fatalf("parse tx: %v", err)
	}
	return tx
}

// transferDecimals extracts the decimals byte from the TransferChecked
// instruction (index 2, last data byte).
func transferDecimals(t *testing.T, tx *solana.Transaction) uint8 {
	t.Helper()
	if len(tx.Message.Instructions) != 4 {
		t.Fatalf("instructions = %d, want 4", len(tx.Message.Instructions))
	}
	data := []byte(tx.Message.Instructions[2].Data)
	if len(data) != 10 {
		t.Fatalf("transfer data len = %d, want 10", len(data))
	}
	return data[9]
}

// TestUSDCPaymentSkipsMintRPC: paying in USDC must not fetch mint info over
// RPC — the mint's token program and decimals (6) are immutable and known.
func TestUSDCPaymentSkipsMintRPC(t *testing.T) {
	resetSolanaBlockhashCacheForTest(t)
	counter, srv := newRPCCounterServer(t, 99) // 99 would be visible if RPC were consulted

	payload, err := CreateSolanaPaymentPayload(testSolanaKey(t), testPaymentOption(USDCSolanaMainnet), "https://x", "", nil, srv.URL)
	if err != nil {
		t.Fatalf("CreateSolanaPaymentPayload: %v", err)
	}
	if got := counter.mintCalls.Load(); got != 0 {
		t.Errorf("getAccountInfo calls = %d, want 0 (USDC mint info is hardcoded)", got)
	}
	if got := transferDecimals(t, decodePaymentTx(t, payload)); got != 6 {
		t.Errorf("decimals = %d, want hardcoded 6", got)
	}
}

// TestNonUSDCPaymentFetchesMintInfo: any other mint still resolves its token
// program and decimals over RPC.
func TestNonUSDCPaymentFetchesMintInfo(t *testing.T) {
	resetSolanaBlockhashCacheForTest(t)
	counter, srv := newRPCCounterServer(t, 9)
	otherMint := solana.NewWallet().PublicKey().String()

	payload, err := CreateSolanaPaymentPayload(testSolanaKey(t), testPaymentOption(otherMint), "https://x", "", nil, srv.URL)
	if err != nil {
		t.Fatalf("CreateSolanaPaymentPayload: %v", err)
	}
	if got := counter.mintCalls.Load(); got != 1 {
		t.Errorf("getAccountInfo calls = %d, want 1", got)
	}
	if got := transferDecimals(t, decodePaymentTx(t, payload)); got != 9 {
		t.Errorf("decimals = %d, want 9 from RPC", got)
	}
}

// TestBlockhashCachedWithinTTL: two payments inside the TTL share one
// getLatestBlockhash fetch. Safe despite identical blockhashes because the
// random memo nonce keeps the two transactions distinct.
func TestBlockhashCachedWithinTTL(t *testing.T) {
	resetSolanaBlockhashCacheForTest(t)
	counter, srv := newRPCCounterServer(t, 6)
	key := testSolanaKey(t)

	for i := 0; i < 2; i++ {
		if _, err := CreateSolanaPaymentPayload(key, testPaymentOption(USDCSolanaMainnet), "https://x", "", nil, srv.URL); err != nil {
			t.Fatalf("payment %d: %v", i+1, err)
		}
	}
	if got := counter.blockhashCalls.Load(); got != 1 {
		t.Errorf("getLatestBlockhash calls = %d, want 1 (second payment served from cache)", got)
	}
}

// TestBlockhashRefetchedAfterTTL: an entry older than the TTL is refreshed.
func TestBlockhashRefetchedAfterTTL(t *testing.T) {
	resetSolanaBlockhashCacheForTest(t)
	counter, srv := newRPCCounterServer(t, 6)
	key := testSolanaKey(t)

	base := time.Now()
	solanaTimeNow = func() time.Time { return base }
	defer func() { solanaTimeNow = time.Now }()

	if _, err := CreateSolanaPaymentPayload(key, testPaymentOption(USDCSolanaMainnet), "https://x", "", nil, srv.URL); err != nil {
		t.Fatalf("payment 1: %v", err)
	}
	solanaTimeNow = func() time.Time { return base.Add(solanaBlockhashTTL + time.Second) }
	if _, err := CreateSolanaPaymentPayload(key, testPaymentOption(USDCSolanaMainnet), "https://x", "", nil, srv.URL); err != nil {
		t.Fatalf("payment 2: %v", err)
	}
	if got := counter.blockhashCalls.Load(); got != 2 {
		t.Errorf("getLatestBlockhash calls = %d, want 2 (TTL expired)", got)
	}
}

// TestBlockhashCacheBounded: the cache never tracks more endpoints than its
// limit, so URL-churning callers cannot leak memory.
func TestBlockhashCacheBounded(t *testing.T) {
	resetSolanaBlockhashCacheForTest(t)
	_, srv := newRPCCounterServer(t, 6)
	key := testSolanaKey(t)

	for i := 0; i < maxTrackedSolanaEndpoints+2; i++ {
		url := fmt.Sprintf("%s/?endpoint=%d", srv.URL, i)
		if _, err := CreateSolanaPaymentPayload(key, testPaymentOption(USDCSolanaMainnet), "https://x", "", nil, url); err != nil {
			t.Fatalf("payment %d: %v", i+1, err)
		}
	}
	solanaBlockhashCache.mu.Lock()
	size := len(solanaBlockhashCache.entries)
	solanaBlockhashCache.mu.Unlock()
	if size > maxTrackedSolanaEndpoints {
		t.Errorf("cache size = %d, want <= %d", size, maxTrackedSolanaEndpoints)
	}
}
