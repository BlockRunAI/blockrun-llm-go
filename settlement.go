package blockrun

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// What a paid response says about the money.
//
// A 200 on a paid request does not always mean the payment settled. The
// gateways name a settlement they skipped in one of two headers:
//
//	X-Settlement-Skipped: free-fallback | empty-result | unexecuted | <later reason>
//	X-Payment-Waived:     empty-result
//
// Either one means nothing moved, so nothing may be booked. X-Payment-Settled:
// false on its own is NOT a skip: settlement had not landed when the response
// went out, and the payment is still owed.

// notASkip matches header values that say settlement was not skipped.
var notASkip = regexp.MustCompile(`(?i)^(false|0|no|none)$`)

// digitsOnly matches an atomic USDC amount.
var digitsOnly = regexp.MustCompile(`^\d+$`)

// settlementSkipReason returns why the gateway served this paid request without
// settling it, or "" when it did not say so. A response that also proves the
// payment settled is booked as charged and the conflict is logged: money that
// moved is never hidden by a header.
func settlementSkipReason(h http.Header) string {
	reason := ""
	for _, name := range []string{"X-Settlement-Skipped", "X-Payment-Waived"} {
		if v := strings.TrimSpace(h.Get(name)); v != "" && !notASkip.MatchString(v) {
			reason = v
			break
		}
	}
	if reason == "" {
		return ""
	}
	if evidence := settlementEvidence(h); evidence != "" {
		log.Printf("blockrun: the gateway named a skipped settlement (%q) on a response that also carries %s; booked as charged", reason, evidence)
		return ""
	}
	return reason
}

// settlementEvidence names proof in the response that the payment settled: a
// receipt that did not fail and names a transaction or a positive charge, or an
// X-Payment-Receipt hash. It returns "" when there is none.
func settlementEvidence(h http.Header) string {
	if receipt := paymentReceipt(h); receipt != nil {
		if success, ok := receipt["success"].(bool); !ok || success {
			if tx, _ := receipt["transaction"].(string); tx != "" {
				return "a settlement receipt with a transaction"
			}
			if receiptCharged(receipt) {
				return "a settlement receipt with a charge"
			}
		}
	}
	if strings.TrimSpace(h.Get("X-Payment-Receipt")) != "" {
		return "an X-Payment-Receipt transaction"
	}
	return ""
}

// paymentReceipt decodes the base64 JSON PAYMENT-RESPONSE header. A missing or
// malformed header returns nil and never fails the call.
func paymentReceipt(h http.Header) map[string]any {
	raw := h.Get("PAYMENT-RESPONSE")
	if raw == "" {
		raw = h.Get("X-PAYMENT-RESPONSE")
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		decoded, err := enc.DecodeString(raw)
		if err != nil {
			continue
		}
		var receipt map[string]any
		if json.Unmarshal(decoded, &receipt) == nil {
			return receipt
		}
	}
	return nil
}

// receiptCharged reports whether a receipt states a positive charge. A metered
// extra.chargedAmount is the charge, zero included; amount is read only without
// one.
func receiptCharged(receipt map[string]any) bool {
	amount, ok := "", false
	if extra, isMap := receipt["extra"].(map[string]any); isMap {
		amount, ok = extra["chargedAmount"].(string)
	}
	if !ok || !digitsOnly.MatchString(amount) {
		amount, ok = receipt["amount"].(string)
	}
	if !ok || !digitsOnly.MatchString(amount) {
		return false
	}
	n, err := strconv.ParseFloat(amount, 64)
	return err == nil && n > 0
}

// recordPaidCall books a paid response unless the gateway says it skipped
// settlement. Every wallet-rail booking site goes through here.
func (bc *baseClient) recordPaidCall(h http.Header, amount, endpoint string) {
	if settlementSkipReason(h) != "" {
		return
	}
	bc.recordSettledCost(amount, endpoint)
}

// paidFailureMessage is the error text for a failed request that carried a
// payment. It reports only what is known. A body's payment_status comes first
// because the gateway wrote it knowing the settlement; then a transaction in the
// headers. A receipt's success field proves nothing (gateways hard-code it), and
// a missing receipt does not mean free: settlement can land after the error.
func paidFailureMessage(h http.Header, body []byte) string {
	text := strings.TrimSpace(string(body))
	var fields struct {
		PaymentStatus string `json:"payment_status"`
		TxHash        string `json:"tx_hash"`
	}
	_ = json.Unmarshal(body, &fields)

	tx := fields.TxHash
	if tx == "" {
		if receipt := paymentReceipt(h); receipt != nil {
			tx, _ = receipt["transaction"].(string)
		}
	}
	if tx == "" {
		tx = strings.TrimSpace(h.Get("X-Payment-Receipt"))
	}

	switch {
	case fields.PaymentStatus == "not_charged" && tx == "":
		return fmt.Sprintf("API error on the paid request (the gateway reports no charge): %s", text)
	case tx != "":
		return fmt.Sprintf("API error after settlement (payment SETTLED, tx %s): %s", tx, text)
	case fields.PaymentStatus == "settled" || fields.PaymentStatus == "settled_optimistic":
		return fmt.Sprintf("API error after settlement (the gateway reports payment settled): %s", text)
	case fields.PaymentStatus != "":
		return fmt.Sprintf("API error on the paid request (payment status %s; check your wallet before paying again): %s", fields.PaymentStatus, text)
	}
	return fmt.Sprintf("API error on the paid request (no settlement reported; a failed call usually moves no funds, but settlement can land after the error, so check your wallet before assuming nothing was charged): %s", text)
}

// jobFailureNote is the parenthetical for a media job that ended in failed.
// A payment_status in the body wins. Without one, fallback is what this rail's
// contract implies.
func jobFailureNote(body []byte, fallback string) string {
	var fields struct {
		PaymentStatus string `json:"payment_status"`
	}
	_ = json.Unmarshal(body, &fields)
	switch fields.PaymentStatus {
	case "":
		return fallback
	case "not_charged":
		return " (no payment was taken)"
	case "settled", "settled_optimistic":
		return " (payment was taken)"
	}
	return fmt.Sprintf(" (payment status %s; check your wallet before paying again)", fields.PaymentStatus)
}
