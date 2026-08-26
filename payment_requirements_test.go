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
)

// bodyOnly402Server answers the first request with a 402 whose payment
// requirements live in the JSON body and NOT in the payment-required header,
// then 200 once a PAYMENT-SIGNATURE arrives. wrap controls the body shape:
// "x402" nests the requirement under that key, "bare" puts accepts at the top.
func bodyOnly402Server(t *testing.T, opt PaymentOption, wrap string) (*httptest.Server, *string) {
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
		var payload any = req
		if wrap == "x402" {
			payload = map[string]any{"error": "payment required", "x402": req}
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Errorf("marshal 402 body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &sawSignature
}

func testBaseClient(t *testing.T, apiURL string, httpClient *http.Client) *baseClient {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.TrimPrefix(testPrivateKey, "0x"))
	if err != nil {
		t.Fatalf("parse test key: %v", err)
	}
	return &baseClient{
		privateKey: key,
		address:    crypto.PubkeyToAddress(key.PublicKey).Hex(),
		apiURL:     apiURL,
		httpClient: httpClient,
		costLog:    NewCostLog(),
	}
}

// TestParsePaymentRequiredAcceptsRawJSON pins the half of the contract the
// body fallback has always depended on. The fallback hands ParsePaymentRequired
// raw JSON, which starts with '{' and is therefore not valid base64, so every
// body-carried 402 failed with "illegal base64 data at input byte 0".
func TestParsePaymentRequiredAcceptsRawJSON(t *testing.T) {
	raw, err := json.Marshal(PaymentRequirement{
		X402Version: 2,
		Accepts:     []PaymentOption{testBasePaymentOption()},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for name, header := range map[string]string{
		"raw json":         string(raw),
		"padded raw json":  "  " + string(raw) + "\n",
		"base64 (classic)": base64.StdEncoding.EncodeToString(raw),
	} {
		t.Run(name, func(t *testing.T) {
			req, err := ParsePaymentRequired(header)
			if err != nil {
				t.Fatalf("ParsePaymentRequired: %v", err)
			}
			if len(req.Accepts) != 1 || req.Accepts[0].Amount != "1000" {
				t.Errorf("parsed = %+v, want the single 1000 option", req.Accepts)
			}
		})
	}
}

// TestParsePaymentRequiredRejectsGarbage keeps the parser from silently
// accepting a header it cannot understand.
func TestParsePaymentRequiredRejectsGarbage(t *testing.T) {
	for _, header := range []string{"", "   ", "not base64 and not json"} {
		if _, err := ParsePaymentRequired(header); err == nil {
			t.Errorf("ParsePaymentRequired(%q) succeeded, want an error", header)
		}
	}
}

// TestPaidGetPaysFromBodyRequirements pins the GET body fallback end to end.
func TestPaidGetPaysFromBodyRequirements(t *testing.T) {
	for _, wrap := range []string{"x402", "bare"} {
		t.Run(wrap, func(t *testing.T) {
			srv, sawSignature := bodyOnly402Server(t, testBasePaymentOption(), wrap)
			bc := testBaseClient(t, srv.URL, srv.Client())

			body, err := bc.doGetWithPayment(context.Background(), "/v1/paid", nil)
			if err != nil {
				t.Fatalf("paid GET failed on a body-carried 402: %v", err)
			}
			if string(body) != `{"ok":true}` {
				t.Errorf("body = %s, want the paid response", body)
			}
			if *sawSignature == "" {
				t.Fatal("server never saw a PAYMENT-SIGNATURE")
			}
		})
	}
}

// TestPostPaysFromBodyRequirements pins the same for the POST path, which had
// drifted from the GET copy: it marshalled the whole body even when the
// requirement was nested under "x402", and had no "accepts" branch at all.
func TestPostPaysFromBodyRequirements(t *testing.T) {
	for _, wrap := range []string{"x402", "bare"} {
		t.Run(wrap, func(t *testing.T) {
			srv, sawSignature := bodyOnly402Server(t, testBasePaymentOption(), wrap)
			bc := testBaseClient(t, srv.URL, srv.Client())

			body, err := bc.doRequest(context.Background(), "/v1/paid", map[string]any{"q": 1})
			if err != nil {
				t.Fatalf("paid POST failed on a body-carried 402: %v", err)
			}
			if string(body) != `{"ok":true}` {
				t.Errorf("body = %s, want the paid response", body)
			}
			if *sawSignature == "" {
				t.Fatal("server never saw a PAYMENT-SIGNATURE")
			}
		})
	}
}

// TestPaymentRequirementsFromPrefersHeader pins that the shared extractor only
// consults the body when the header is absent.
func TestPaymentRequirementsFromPrefersHeader(t *testing.T) {
	got := paymentRequirementsFrom("HEADER", []byte(`{"accepts":[]}`))
	if got != "HEADER" {
		t.Errorf("got %q, want the header to win", got)
	}
	if got := paymentRequirementsFrom("", []byte(`{"detail":"nope"}`)); got != "" {
		t.Errorf("got %q, want empty for a body with no requirements", got)
	}
}
