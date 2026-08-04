package blockrun

import (
	"testing"
)

// TestSignPaymentRejectsKeylessClient pins the no-wallet invariant at the
// choke point every payment path funnels through.
//
// The guard used to live only at the doGetWithPayment call site, leaving five
// other 402 entry points (doRequestHeaders, handleStreamPaymentAndRetry,
// image.go, video.go, rpc.go) to call the signer directly. On Base that meant
// CreatePaymentPayload dereferencing a nil *ecdsa.PrivateKey and panicking the
// caller's goroutine rather than returning a PaymentError.
//
// A panic here would fail the test outright, so this pins "returns an error"
// and "does not panic" at once.
func TestSignPaymentRejectsKeylessClient(t *testing.T) {
	for name, bc := range map[string]*baseClient{
		"base without privateKey":  {},
		"solana without solanaKey": {chain: chainSolana},
	} {
		t.Run(name, func(t *testing.T) {
			payload, err := bc.signPayment(testPaymentOption(USDCSolanaMainnet), "https://example.test", "", nil, true)
			if err == nil {
				t.Fatal("expected a rejection when no wallet is configured")
			}
			if payload != "" {
				t.Errorf("payload = %q, want empty on rejection", payload)
			}
			var pe *PaymentError
			if !asPaymentError(err, &pe) {
				t.Fatalf("error = %T (%v), want *PaymentError", err, err)
			}
			if pe.Message != "endpoint returned 402 but no wallet is configured" {
				t.Errorf("message = %q, want the no-wallet message", pe.Message)
			}
		})
	}
}

// TestCreatePaymentPayloadRejectsKeylessClient pins that the exported wrapper
// inherits the guard, so callers that go through createPaymentPayload (the
// non-poll paths) are covered too.
func TestCreatePaymentPayloadRejectsKeylessClient(t *testing.T) {
	bc := &baseClient{}
	if _, err := bc.createPaymentPayload(testPaymentOption(USDCSolanaMainnet), "https://example.test", "", nil); err == nil {
		t.Fatal("expected a rejection when no wallet is configured")
	}
}

// asPaymentError is errors.As specialised to *PaymentError, kept local so the
// test reads without an errors import at every call site.
func asPaymentError(err error, target **PaymentError) bool {
	pe, ok := err.(*PaymentError)
	if ok {
		*target = pe
	}
	return ok
}
