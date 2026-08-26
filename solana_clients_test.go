package blockrun

import (
	"context"
	"testing"
)

// TestSurfAndRPCHaveSolanaConstructors pins the gap that made README's "every
// client has a NewXClientSolana counterpart" false. SurfClient and RPCClient
// were built only through newBaseClient, which never sets chain, so neither
// could ever be a Solana client — and SurfClient.Get is a paid GET, so surf was
// unreachable for Solana users regardless of the hasWallet fix.
func TestSurfAndRPCHaveSolanaConstructors(t *testing.T) {
	key := testSolanaKey(t)

	surf, err := NewSurfClientSolana(key, "https://rpc.test")
	if err != nil {
		t.Fatalf("NewSurfClientSolana: %v", err)
	}
	assertSolanaClient(t, "surf", surf.baseClient, "https://rpc.test")

	rpc, err := NewRPCClientSolana(key, "https://rpc.test")
	if err != nil {
		t.Fatalf("NewRPCClientSolana: %v", err)
	}
	assertSolanaClient(t, "rpc", rpc.baseClient, "https://rpc.test")
}

func assertSolanaClient(t *testing.T, name string, bc *baseClient, wantRPC string) {
	t.Helper()
	if !bc.isSolana() {
		t.Errorf("%s: chain = %q, want %q", name, bc.chain, chainSolana)
	}
	if !bc.hasWallet() {
		t.Errorf("%s: no signing key configured, so every paid call would be rejected", name)
	}
	if bc.privateKey != nil {
		t.Errorf("%s: a Solana client must not carry an ECDSA key", name)
	}
	if bc.solanaRPCURL != wantRPC {
		t.Errorf("%s: solanaRPCURL = %q, want %q", name, bc.solanaRPCURL, wantRPC)
	}
	if bc.apiURL != DefaultSolanaAPIURL {
		t.Errorf("%s: apiURL = %q, want the Solana gateway %q", name, bc.apiURL, DefaultSolanaAPIURL)
	}
}

// TestSurfSolanaOptionsApply pins that the Solana constructors honour the same
// option funcs as their Base counterparts.
func TestSurfSolanaOptionsApply(t *testing.T) {
	surf, err := NewSurfClientSolana(testSolanaKey(t), "https://rpc.test", WithSurfAPIURL("https://custom.test/api/"))
	if err != nil {
		t.Fatalf("NewSurfClientSolana: %v", err)
	}
	if surf.apiURL != "https://custom.test/api" {
		t.Errorf("apiURL = %q, want the option-supplied URL with its trailing slash trimmed", surf.apiURL)
	}

	rpc, err := NewRPCClientSolana(testSolanaKey(t), "https://rpc.test", WithRPCAPIURL("https://custom.test/api"))
	if err != nil {
		t.Fatalf("NewRPCClientSolana: %v", err)
	}
	if rpc.apiURL != "https://custom.test/api" {
		t.Errorf("apiURL = %q, want the option-supplied URL", rpc.apiURL)
	}
}

// TestSurfSolanaPaidGetSigns is the end-to-end claim of #12: a Solana surf
// client can actually pay for a /v1/surf GET.
func TestSurfSolanaPaidGetSigns(t *testing.T) {
	resetSolanaBlockhashCacheForTest(t)
	_, rpcSrv := newRPCCounterServer(t, usdcSolanaDecimals)
	srv, sawSignature := paidGetServer(t, *testPaymentOption(USDCSolanaMainnet))

	client, err := NewSurfClientSolana(testSolanaKey(t), rpcSrv.URL, WithSurfAPIURL(srv.URL))
	if err != nil {
		t.Fatalf("NewSurfClientSolana: %v", err)
	}
	client.httpClient = srv.Client()

	if _, err := client.Get(context.Background(), "market/fear-greed", nil); err != nil {
		t.Fatalf("surf paid GET failed for a Solana client: %v", err)
	}
	if *sawSignature == "" {
		t.Fatal("server never saw a PAYMENT-SIGNATURE, so the Solana client did not sign")
	}
	if got := acceptedNetwork(t, *sawSignature); got != "solana" {
		t.Errorf("signed for network %q, want solana", got)
	}
}
