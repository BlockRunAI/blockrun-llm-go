package blockrun

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

// solanaPaidGetOption is a 402 requirement that needs zero RPC to sign: USDC
// (mint info hardcoded) plus a server-provided blockhash.
func solanaPaidGetOption(t *testing.T) PaymentOption {
	t.Helper()
	return PaymentOption{
		Scheme:            "exact",
		Network:           "solana",
		Amount:            "1000",
		Asset:             USDCSolanaMainnet,
		PayTo:             solana.NewWallet().PublicKey().String(),
		MaxTimeoutSeconds: 60,
		Extra: map[string]any{
			"feePayer":        solana.NewWallet().PublicKey().String(),
			"recentBlockhash": makeBlockhash(t).String(),
		},
	}
}

// TestPaidGetSolanaSignsInsteadOfRejecting pins the regression that a Solana
// client can pay for GET endpoints.
//
// doGetWithPayment used to guard the 402 branch on `bc.privateKey == nil`, but
// Solana clients leave privateKey nil by design and sign with solanaKey. That
// rejected every paid GET (dex, market, prediction market, defi, surf) with a
// misleading "no wallet is configured" before signing was ever attempted.
func TestPaidGetSolanaSignsInsteadOfRejecting(t *testing.T) {
	srv, sawSignature := paidGetServer(t, solanaPaidGetOption(t))

	bc := &baseClient{
		chain:      chainSolana,
		solanaKey:  testSolanaKey(t),
		apiURL:     srv.URL,
		httpClient: srv.Client(),
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
	// The signature must be a real SVM exact-scheme envelope, not a stub.
	if got := decodePaymentTx(t, *sawSignature).Message.RecentBlockhash; got.IsZero() {
		t.Error("signed transaction carries a zero blockhash")
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
			srv, _ := paidGetServer(t, solanaPaidGetOption(t))
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
