package blockrun

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testBasePaymentOption is the Base-chain counterpart of testPaymentOption:
// an EIP-712-payable option carrying an ERC-20 asset address.
func testBasePaymentOption() PaymentOption {
	return PaymentOption{
		Scheme:            "exact",
		Network:           "base",
		Amount:            "1000",
		Asset:             USDCBase,
		PayTo:             "0x1234567890123456789012345678901234567890",
		MaxTimeoutSeconds: 300,
	}
}

// multiOptionPaidGetServer serves a 402 advertising every option in accepts,
// then 200 once a PAYMENT-SIGNATURE arrives.
func multiOptionPaidGetServer(t *testing.T, accepts []PaymentOption) (*httptest.Server, *string) {
	t.Helper()
	var sawSignature string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sig := r.Header.Get("PAYMENT-SIGNATURE"); sig != "" {
			sawSignature = sig
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		body, err := json.Marshal(PaymentRequirement{
			X402Version: 2,
			Accepts:     accepts,
			Resource:    ResourceInfo{URL: "https://example.test/resource"},
		})
		if err != nil {
			t.Errorf("marshal payment requirement: %v", err)
		}
		w.Header().Set("payment-required", base64.StdEncoding.EncodeToString(body))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(srv.Close)
	return srv, &sawSignature
}

// acceptedNetwork reads the network the client actually committed to from the
// signed Solana envelope.
func acceptedNetwork(t *testing.T, payload string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	var env struct {
		Accepted PaymentOption `json:"accepted"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env.Accepted.Network
}

// TestExtractPaymentDetailsPicksClientChain pins that option selection follows
// the client's chain rather than the server's ordering. A gateway that lists
// [base, solana] used to hand a Solana client the Base option, which then died
// in createSolanaPaymentPayload on "invalid asset mint" with a payable Solana
// option sitting untouched in accepts[1].
func TestExtractPaymentDetailsPicksClientChain(t *testing.T) {
	solOpt := *testPaymentOption(USDCSolanaMainnet)
	req := &PaymentRequirement{Accepts: []PaymentOption{testBasePaymentOption(), solOpt}}

	for chain, wantNetwork := range map[string]string{
		chainSolana: "solana",
		chainBase:   "base",
	} {
		got, err := ExtractPaymentDetailsForChain(req, chain)
		if err != nil {
			t.Fatalf("%s: %v", chain, err)
		}
		if got.Network != wantNetwork {
			t.Errorf("chain %s selected network %q, want %q", chain, got.Network, wantNetwork)
		}
	}
}

// TestExtractPaymentDetailsCAIP2Networks pins that the CAIP-2 spellings a
// gateway may use are recognised as the same chains as their short names.
func TestExtractPaymentDetailsCAIP2Networks(t *testing.T) {
	baseOpt := testBasePaymentOption()
	baseOpt.Network = "eip155:8453"
	solOpt := *testPaymentOption(USDCSolanaMainnet)
	solOpt.Network = "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdpKuc147dw2N9d"
	req := &PaymentRequirement{Accepts: []PaymentOption{baseOpt, solOpt}}

	got, err := ExtractPaymentDetailsForChain(req, chainSolana)
	if err != nil {
		t.Fatalf("solana: %v", err)
	}
	if got.Network != solOpt.Network {
		t.Errorf("selected %q, want the CAIP-2 Solana option %q", got.Network, solOpt.Network)
	}

	got, err = ExtractPaymentDetailsForChain(req, chainBase)
	if err != nil {
		t.Fatalf("base: %v", err)
	}
	if got.Network != "eip155:8453" {
		t.Errorf("selected %q, want the CAIP-2 Base option", got.Network)
	}
}

// TestExtractPaymentDetailsReportsChainMismatch pins the error text for a 402
// that offers nothing this client can pay. The old code took accepts[0]
// regardless and failed deep inside signing with "feePayer is required in
// payment requirement extra", which blames the server for a client-side
// mismatch.
func TestExtractPaymentDetailsReportsChainMismatch(t *testing.T) {
	req := &PaymentRequirement{Accepts: []PaymentOption{testBasePaymentOption()}}

	_, err := ExtractPaymentDetailsForChain(req, chainSolana)
	if err == nil {
		t.Fatal("expected an error when a Solana client is offered only Base options")
	}
	msg := err.Error()
	for _, want := range []string{chainSolana, `"base"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %s", msg, want)
		}
	}
}

// TestExtractPaymentDetailsUnknownNetworkStillPayable keeps the selector
// lenient: a network string this SDK does not classify is not evidence of a
// mismatch, so it is still offered rather than rejected outright.
func TestExtractPaymentDetailsUnknownNetworkStillPayable(t *testing.T) {
	opt := testBasePaymentOption()
	opt.Network = ""
	req := &PaymentRequirement{Accepts: []PaymentOption{opt}}

	got, err := ExtractPaymentDetailsForChain(req, chainSolana)
	if err != nil {
		t.Fatalf("unclassifiable network should still be attempted: %v", err)
	}
	if got.Amount != "1000" {
		t.Errorf("amount = %q, want the sole option's", got.Amount)
	}
}

// TestPaidGetSolanaSkipsBaseOptionInMultiOption402 is the end-to-end form:
// a Solana client facing [base, solana] must pay the Solana option.
func TestPaidGetSolanaSkipsBaseOptionInMultiOption402(t *testing.T) {
	resetSolanaBlockhashCacheForTest(t)
	_, rpcSrv := newRPCCounterServer(t, usdcSolanaDecimals)
	srv, sawSignature := multiOptionPaidGetServer(t, []PaymentOption{
		testBasePaymentOption(),
		*testPaymentOption(USDCSolanaMainnet),
	})

	bc := &baseClient{
		chain:        chainSolana,
		solanaKey:    testSolanaKey(t),
		solanaRPCURL: rpcSrv.URL,
		apiURL:       srv.URL,
		httpClient:   srv.Client(),
	}

	if _, err := bc.doGetWithPayment(context.Background(), "/v1/paid", nil); err != nil {
		t.Fatalf("paid GET failed even though accepts carried a Solana option: %v", err)
	}
	if got := acceptedNetwork(t, *sawSignature); got != "solana" {
		t.Errorf("signed for network %q, want solana", got)
	}
}

// TestPaidGetSolanaReportsBaseOnly402 pins that a Solana client hitting a
// Base-only 402 gets a client-side mismatch message, not a server-blaming one.
func TestPaidGetSolanaReportsBaseOnly402(t *testing.T) {
	resetSolanaBlockhashCacheForTest(t)
	_, rpcSrv := newRPCCounterServer(t, usdcSolanaDecimals)
	srv, _ := multiOptionPaidGetServer(t, []PaymentOption{testBasePaymentOption()})

	bc := &baseClient{
		chain:        chainSolana,
		solanaKey:    testSolanaKey(t),
		solanaRPCURL: rpcSrv.URL,
		apiURL:       srv.URL,
		httpClient:   srv.Client(),
	}

	_, err := bc.doGetWithPayment(context.Background(), "/v1/paid", nil)
	if err == nil {
		t.Fatal("expected a chain-mismatch error")
	}
	if strings.Contains(err.Error(), "feePayer is required") {
		t.Errorf("error blames the gateway for a client chain mismatch: %v", err)
	}
	if !strings.Contains(err.Error(), chainSolana) {
		t.Errorf("error %v does not name the client's chain", err)
	}
}

// TestExtractPaymentDetailsPrefersUnknownOverMismatch pins that an
// unclassifiable option is tried before a mismatch is declared: a 402 listing a
// Base option next to one whose network this SDK does not recognise is not
// proof that a Solana client has nothing to pay.
func TestExtractPaymentDetailsPrefersUnknownOverMismatch(t *testing.T) {
	odd := *testPaymentOption(USDCSolanaMainnet)
	odd.Network = "svm-mainnet"
	req := &PaymentRequirement{Accepts: []PaymentOption{testBasePaymentOption(), odd}}

	got, err := ExtractPaymentDetailsForChain(req, chainSolana)
	if err != nil {
		t.Fatalf("expected the unclassifiable option to be attempted: %v", err)
	}
	if got.Network != "svm-mainnet" {
		t.Errorf("selected %q, want the unclassifiable option", got.Network)
	}
}
