package blockrun

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gagliardetto/solana-go"
)

// paidGetServer serves a 402 on the first GET and 200 once a PAYMENT-SIGNATURE
// is presented, recording the signature it saw.
func paidGetServer(t *testing.T, opt PaymentOption) (*httptest.Server, *string) {
	t.Helper()
	var sawSignature string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sig := r.Header.Get("PAYMENT-SIGNATURE"); sig != "" {
			sawSignature = sig
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		req := PaymentRequirement{
			X402Version: 2,
			Accepts:     []PaymentOption{opt},
			Resource:    ResourceInfo{URL: "https://example.test/resource"},
		}
		body, err := json.Marshal(req)
		if err != nil {
			t.Errorf("marshal payment requirement: %v", err)
		}
		w.Header().Set("payment-required", base64.StdEncoding.EncodeToString(body))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(srv.Close)
	return srv, &sawSignature
}

// TestPaidGetSolanaSignsInsteadOfRejecting pins the regression that a Solana
// client can pay for GET endpoints.
//
// doGetWithPayment used to guard the 402 branch on `bc.privateKey == nil`, but
// Solana clients leave privateKey nil by design and sign with solanaKey. That
// rejected every paid GET (dex, market, prediction market, defi) with a
// misleading "no wallet is configured" before signing was ever attempted.
//
// solanaRPCURL is pinned to a local fake so signing touches no network: USDC
// mint info is hardcoded and the blockhash comes from the fake.
func TestPaidGetSolanaSignsInsteadOfRejecting(t *testing.T) {
	resetSolanaBlockhashCacheForTest(t)
	counter, rpcSrv := newRPCCounterServer(t, usdcSolanaDecimals)
	srv, sawSignature := paidGetServer(t, *testPaymentOption(USDCSolanaMainnet))

	bc := &baseClient{
		chain:        chainSolana,
		solanaKey:    testSolanaKey(t),
		solanaRPCURL: rpcSrv.URL,
		apiURL:       srv.URL,
		httpClient:   srv.Client(),
	}

	body, err := bc.doGetWithPayment(context.Background(), "/v1/paid", nil)
	if err != nil {
		t.Fatalf("paid GET failed for a Solana client with a configured wallet: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %s, want the paid response", body)
	}
	if *sawSignature == "" {
		t.Fatal("server never saw a PAYMENT-SIGNATURE, so the client did not sign")
	}
	// A real SVM exact-scheme envelope carries the blockhash the RPC served,
	// so this fails on a stub as well as on a zero hash.
	want := solana.MustHashFromBase58(counter.blockhash)
	if got := decodePaymentTx(t, *sawSignature).Message.RecentBlockhash; !got.Equals(want) {
		t.Errorf("blockhash = %s, want %s from the RPC", got, want)
	}
	if got := bc.GetSpending(); got.Calls != 1 || got.TotalUSD != 0.001 {
		t.Errorf("spending = %+v, want 1 call totalling $0.001", got)
	}
}

// TestPaidGetBaseSignsInsteadOfRejecting pins the other half of hasWallet: a
// Base client with a privateKey must still pay for a GET. Without it, breaking
// the Base branch of hasWallet leaves the suite green even though every paid
// GET on the SDK's default chain would fail.
func TestPaidGetBaseSignsInsteadOfRejecting(t *testing.T) {
	key, err := crypto.HexToECDSA(strings.TrimPrefix(testPrivateKey, "0x"))
	if err != nil {
		t.Fatalf("parse test key: %v", err)
	}
	srv, sawSignature := paidGetServer(t, PaymentOption{
		Scheme:            "exact",
		Network:           "base",
		Amount:            "1000",
		Asset:             USDCBase,
		PayTo:             "0x1234567890123456789012345678901234567890",
		MaxTimeoutSeconds: 300,
	})

	bc := &baseClient{
		privateKey: key,
		address:    crypto.PubkeyToAddress(key.PublicKey).Hex(),
		apiURL:     srv.URL,
		httpClient: srv.Client(),
	}

	body, err := bc.doGetWithPayment(context.Background(), "/v1/paid", nil)
	if err != nil {
		t.Fatalf("paid GET failed for a Base client with a configured wallet: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %s, want the paid response", body)
	}
	if *sawSignature == "" {
		t.Fatal("server never saw a PAYMENT-SIGNATURE, so the client did not sign")
	}
}

// TestPaidGetWithoutWalletStillRejected pins that the guard still fires when no
// signing key is configured on either chain.
func TestPaidGetWithoutWalletStillRejected(t *testing.T) {
	for name, bc := range map[string]*baseClient{
		"solana without solanaKey": {chain: chainSolana},
		"base without privateKey":  {},
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := paidGetServer(t, *testPaymentOption(USDCSolanaMainnet))
			bc.apiURL = srv.URL
			bc.httpClient = srv.Client()

			_, err := bc.doGetWithPayment(context.Background(), "/v1/paid", nil)
			if err == nil {
				t.Fatal("expected a 402 rejection when no wallet is configured")
			}
			if !strings.Contains(err.Error(), "no wallet is configured") {
				t.Errorf("error = %v, want it to mention no wallet configured", err)
			}
		})
	}
}
