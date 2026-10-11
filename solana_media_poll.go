package blockrun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Solana media jobs that are paid at the POST (image, music).
//
// sol.blockrun.ai settles an image or music payment when the POST is accepted.
// A slow job then answers 202 { id, poll_url, status } and every later poll is
// delivery only — to the wallet that paid. These helpers follow such a job
// with the poll-ownership proof (poll_ownership.go), so a poll never signs a
// payment. Base and the account rail never come here: on Base an image settles
// on the first completed poll, which is a different contract.

const (
	// solanaMaxPollTransportErrors bounds consecutive poll requests that never
	// got an answer. A proof poll is free, so a dropped one is retried; this
	// many in a row means the network is gone, not hiccuping.
	solanaMaxPollTransportErrors = 5
	// solanaMaxPollRetryAfter caps a poll's Retry-After hint.
	solanaMaxPollRetryAfter = 120 * time.Second
)

// solanaPaidJob is one Solana media job whose payment settled at the POST.
type solanaPaidJob struct {
	// kind is the poll proof's <kind>: pollKindImage or pollKindAudio.
	kind string
	// label names the job in errors ("Image generation", "Music generation").
	label string
	id    string
	// pollURL is the resolved, same-origin absolute poll URL.
	pollURL string
	status  string
	// pay is the submit's payment context. It is used only after the gateway
	// refuses the proof, to poll the old way (a signed payment per poll).
	pay      *imagePayment
	budget   time.Duration
	interval time.Duration
}

// paidNote is appended to every error raised after a Solana paid submit: the
// money has moved, so the caller must not read a failure as "retry from
// scratch" — that would pay twice.
func (j *solanaPaidJob) paidNote() string {
	return fmt.Sprintf(
		"Payment was settled at submit; do not submit again — the job (id %s) stays claimable by polling %s with the same wallet.",
		j.id, j.pollURL,
	)
}

// pollSolanaPaidJob GET-polls a Solana job that was paid at the POST until it
// reaches completed or failed, and returns the completed poll's body and
// headers. The caller has already booked the cost.
//
// Each poll carries a fresh ownership proof and no payment. If the gateway
// refuses the proof (404, which is also "no such job") or answers it with a
// plain 402 challenge (a gateway that predates poll proofs, or a batch-billed
// image), the rest of this job is polled the old way — a signed payment on
// each poll, which the gateway verifies against the job's payer and does not
// charge again (the submit already settled). That switch is logged once and never reversed.
//
// 202 (queued / in_progress) and 503/504 mean "not yet" and are retried, as is
// a poll that got no answer at all: a proof poll costs nothing, and giving up
// on a job the caller already paid for is the one outcome to avoid.
func (bc *baseClient) pollSolanaPaidJob(ctx context.Context, job *solanaPaidJob) ([]byte, http.Header, error) {
	priv, err := solanaKeypair(bc.solanaKey)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot sign the poll-ownership proof: %w. %s", err, job.paidNote())
	}
	jobID := pollJobID(job.pollURL, job.id)

	proofPolls := true

	lastStatus := job.status
	if lastStatus == "" {
		lastStatus = "queued"
	}
	deadline := time.Now().Add(job.budget)
	wait := job.interval
	transportErrors := 0

	for time.Now().Before(deadline) {
		if wait > 0 {
			if remaining := time.Until(deadline); wait > remaining {
				wait = remaining
			}
			select {
			case <-ctx.Done():
				return nil, nil, fmt.Errorf("%s polling stopped: %w. %s", job.label, ctx.Err(), job.paidNote())
			case <-time.After(wait):
			}
		}
		wait = job.interval

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, job.pollURL, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create poll request: %w", err)
		}
		if proofPolls {
			for k, v := range pollAuthHeaders(priv, job.kind, jobID, time.Now()) {
				req.Header.Set(k, v)
			}
		} else {
			if job.pay == nil {
				return nil, nil, &APIError{
					StatusCode: http.StatusPaymentRequired,
					Message:    fmt.Sprintf("%s poll was refused and there is no payment to poll with. %s", job.label, job.paidNote()),
				}
			}
			// A fresh signature (and blockhash) for every poll: the submit's
			// transaction has already landed, and a blockhash dies in ~60-90s.
			// The gateway only verifies it to learn the payer; it settled at
			// the POST and does not charge a poll.
			sig, err := bc.signPayment(job.pay.option, job.pay.resourceURL, job.pay.description, job.pay.extensions, false)
			if err != nil {
				transportErrors++
				if transportErrors >= solanaMaxPollTransportErrors {
					return nil, nil, fmt.Errorf("%s poll could not be signed %d times in a row: %w. %s", job.label, transportErrors, err, job.paidNote())
				}
				continue
			}
			req.Header.Set("PAYMENT-SIGNATURE", sig)
		}

		resp, err := bc.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, fmt.Errorf("%s polling stopped: %w. %s", job.label, ctx.Err(), job.paidNote())
			}
			transportErrors++
			if transportErrors >= solanaMaxPollTransportErrors {
				return nil, nil, fmt.Errorf("%s poll failed %d times in a row: %w. %s", job.label, transportErrors, err, job.paidNote())
			}
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			// An answer cut off mid-body is as unknown as no answer.
			transportErrors++
			if transportErrors >= solanaMaxPollTransportErrors {
				return nil, nil, fmt.Errorf("%s poll failed %d times in a row: %w. %s", job.label, transportErrors, readErr, job.paidNote())
			}
			continue
		}
		if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			transportErrors = 0
		}

		if proofPolls {
			switch resp.StatusCode {
			case http.StatusNotFound:
				proofPolls = false
				wait = 0
				log.Printf("blockrun: %s job %s: the gateway refused the poll-ownership proof (HTTP 404); falling back to pay-per-poll for this job", job.kind, job.id)
				continue
			case http.StatusPaymentRequired:
				proofPolls = false
				wait = 0
				log.Printf("blockrun: %s job %s: the gateway answered the poll-ownership proof with a payment challenge (an older gateway, or a batch-billed job); falling back to pay-per-poll for this job", job.kind, job.id)
				continue
			}
		}

		var data map[string]any
		_ = json.Unmarshal(body, &data)
		if s, ok := data["status"].(string); ok && s != "" {
			lastStatus = s
		}

		if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout {
			wait = pollRetryAfter(resp.Header, job.interval)
			continue
		}
		// Any other 5xx, or a 429, is the gateway or its edge having a bad
		// moment (the poll route answers 500 from its catch-all). A poll never
		// charges, so ask again — bounded like a dropped request, because a
		// persistent 500 is not going to turn into a track.
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			transportErrors++
			if transportErrors >= solanaMaxPollTransportErrors {
				return nil, nil, &APIError{
					StatusCode: resp.StatusCode,
					Message:    fmt.Sprintf("Poll failed %d times in a row: %s. %s", transportErrors, strings.TrimSpace(string(body)), job.paidNote()),
				}
			}
			wait = pollRetryAfter(resp.Header, job.interval)
			continue
		}
		if resp.StatusCode == http.StatusAccepted && (lastStatus == "queued" || lastStatus == "in_progress") {
			wait = pollRetryAfter(resp.Header, job.interval)
			continue
		}
		if lastStatus == "failed" {
			return nil, nil, &APIError{
				StatusCode: resp.StatusCode,
				Message:    fmt.Sprintf("Upstream generation failed (payment was settled at submit): %s", string(body)),
			}
		}
		if lastStatus == "completed" && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return body, resp.Header, nil
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, nil, &APIError{
				StatusCode: resp.StatusCode,
				Message:    fmt.Sprintf("Poll failed: %s. %s", string(body), job.paidNote()),
			}
		}
		// A 2xx that is neither pending, failed nor completed: keep polling.
	}

	return nil, nil, &APIError{
		StatusCode: http.StatusGatewayTimeout,
		Message: fmt.Sprintf(
			"%s did not complete within %.0fs (last status: %s). %s",
			job.label, job.budget.Seconds(), lastStatus, job.paidNote(),
		),
	}
}

// pollRetryAfter reads a delta-seconds Retry-After, capped, else def. It never
// returns less than def: a "Retry-After: 0" would otherwise skip the wait and
// spin the loop against the gateway for the whole budget.
func pollRetryAfter(h http.Header, def time.Duration) time.Duration {
	raw := strings.TrimSpace(h.Get("Retry-After"))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return def
	}
	d := time.Duration(n) * time.Second
	if d > solanaMaxPollRetryAfter {
		d = solanaMaxPollRetryAfter
	}
	if d < def {
		d = def
	}
	return d
}

// solanaPaidMediaPost runs a Solana wallet-rail media POST that the gateway
// settles at submit (music): 402 → sign → paid POST. A 200 is the finished
// result; a 202 { id, poll_url } is followed to completion with the
// poll-ownership proof. The cost is booked the moment the paid POST is
// accepted — 200 or 202 — because that is when the money moved; a job that
// later fails or times out was still charged.
func (bc *baseClient) solanaPaidMediaPost(
	ctx context.Context,
	endpoint string,
	body map[string]any,
	kind, label string,
	budget, interval time.Duration,
) ([]byte, http.Header, error) {
	submitURL := bc.apiURL + endpoint
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encode request body: %w", err)
	}

	req1, err := http.NewRequestWithContext(ctx, http.MethodPost, submitURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := bc.httpClient.Do(req1)
	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()

	if resp1.StatusCode == http.StatusOK {
		// Answered without a payment (free/proxy): nothing was charged.
		return body1, resp1.Header, nil
	}
	if resp1.StatusCode != http.StatusPaymentRequired {
		return nil, nil, &APIError{
			StatusCode: resp1.StatusCode,
			Message:    fmt.Sprintf("API error: %s", string(body1)),
		}
	}
	paymentHeader := paymentRequirementsFrom(resp1.Header.Get("payment-required"), body1)
	if paymentHeader == "" {
		return nil, nil, &PaymentError{Message: "402 response but no payment requirements found"}
	}
	paymentReq, err := ParsePaymentRequired(paymentHeader)
	if err != nil {
		return nil, nil, &PaymentError{Message: fmt.Sprintf("Failed to parse payment requirements: %v", err)}
	}
	paymentOption, err := bc.extractPaymentDetails(paymentReq)
	if err != nil {
		return nil, nil, &PaymentError{Message: fmt.Sprintf("Failed to extract payment details: %v", err)}
	}
	resourceURL := paymentReq.Resource.URL
	if resourceURL == "" {
		resourceURL = submitURL
	}
	paymentPayload, err := bc.createPaymentPayload(paymentOption, resourceURL, paymentReq.Resource.Description, paymentReq.Extensions)
	if err != nil {
		return nil, nil, &PaymentError{Message: fmt.Sprintf("Failed to create payment: %v", err)}
	}

	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, submitURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create submit request: %w", err)
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("PAYMENT-SIGNATURE", paymentPayload)
	resp2, err := bc.httpClient.Do(req2)
	if err != nil {
		// The signed transfer may have reached the gateway and settled with
		// only the answer lost. Say so: a blind retry could pay twice.
		return nil, nil, fmt.Errorf("%s submit failed in transit after the payment was signed; it may have settled, so check your wallet before submitting again: %w", label, err)
	}
	body2, readErr := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if readErr != nil && (resp2.StatusCode == http.StatusOK || resp2.StatusCode == http.StatusAccepted) {
		// Accepted means paid; only the answer was lost.
		bc.recordSettledCost(paymentOption.Amount, endpoint)
		return nil, nil, fmt.Errorf("%s was paid but its response was cut off: %w. Payment was settled at submit; check your wallet before submitting again", label, readErr)
	}

	switch resp2.StatusCode {
	case http.StatusPaymentRequired:
		return nil, nil, &PaymentError{Message: "Payment was rejected. Check your wallet balance."}
	case http.StatusOK:
		bc.recordSettledCost(paymentOption.Amount, endpoint)
		return body2, resp2.Header, nil
	case http.StatusAccepted:
		// Paid and detached: followSolanaPaidSubmit books it, then follows it.
	default:
		return nil, nil, &APIError{
			StatusCode: resp2.StatusCode,
			Message:    fmt.Sprintf("API error after payment: %s", string(body2)),
		}
	}

	result, header, _, err := bc.followSolanaPaidSubmit(ctx, kind, label, endpoint, body2, resp2.StatusCode, &imagePayment{
		option:      paymentOption,
		payload:     paymentPayload,
		resourceURL: resourceURL,
		description: paymentReq.Resource.Description,
		extensions:  paymentReq.Extensions,
	}, budget, interval)
	return result, header, err
}

// followSolanaPaidSubmit takes over a Solana submit that answered 202 after
// the payment was accepted. It books the cost FIRST — the money moved when the
// POST was accepted, so no later failure (an undecodable 202, a missing or
// off-origin poll_url, a failed job, a timeout) may leave it unbooked — then
// follows the job with pollSolanaPaidJob. Every error it returns says the
// payment settled; the returned note is that wording, for the caller's own
// post-processing errors.
func (bc *baseClient) followSolanaPaidSubmit(
	ctx context.Context,
	kind, label, endpoint string,
	submitBody []byte,
	submitStatus int,
	pay *imagePayment,
	budget, interval time.Duration,
) ([]byte, http.Header, string, error) {
	bc.recordSettledCost(pay.option.Amount, endpoint)
	if interval <= 0 {
		interval = musicPollInterval
	}

	var submitted struct {
		ID      string `json:"id"`
		PollURL string `json:"poll_url"`
		Status  string `json:"status"`
	}
	_ = json.Unmarshal(submitBody, &submitted)
	job := &solanaPaidJob{
		kind:     kind,
		label:    label,
		id:       submitted.ID,
		pollURL:  submitted.PollURL,
		status:   submitted.Status,
		pay:      pay,
		budget:   budget,
		interval: interval,
	}
	if submitted.ID == "" || submitted.PollURL == "" {
		return nil, nil, job.paidNote(), &APIError{
			StatusCode: submitStatus,
			Message: fmt.Sprintf("%s was paid but the submit response is missing id/poll_url: %s. Payment was settled at submit; check your wallet before submitting again.",
				label, strings.TrimSpace(string(submitBody))),
		}
	}
	pollURL, err := bc.resolvePollURL(submitted.PollURL)
	if err != nil {
		return nil, nil, job.paidNote(), fmt.Errorf("%s was paid (job %s) but its poll_url is unusable: %w. Payment was settled at submit; do not submit again", label, submitted.ID, err)
	}
	job.pollURL = pollURL
	body, header, err := bc.pollSolanaPaidJob(ctx, job)
	return body, header, job.paidNote(), err
}
