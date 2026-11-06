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
		// Any answer other than a counted failure (below) ends the streak —
		// including a 503/504 "not yet", so failures spread across a long job
		// do not add up to an abandoned paid job.
		if !isCountedPollFailure(resp.StatusCode) {
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

		// A status of failed is final at any HTTP code: a paid job the gateway could
		// not deliver answers 502 and asks the caller to stop.
		if lastStatus == "failed" {
			return nil, nil, &APIError{
				StatusCode: resp.StatusCode,
				Message:    fmt.Sprintf("Upstream generation failed%s: %s", jobFailureNote(body, " (payment was settled at submit)"), string(body)),
			}
		}
		if isPollNotYet(resp.StatusCode) {
			wait = pollRetryAfter(resp.Header, job.interval)
			continue
		}
		// Any other 5xx, or a 429, is the gateway or its edge having a bad
		// moment (the poll route answers 500 from its catch-all). A poll never
		// charges, so ask again — bounded like a dropped request, because a
		// persistent 500 is not going to turn into a track.
		if isCountedPollFailure(resp.StatusCode) {
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
		if isCompletedPoll(resp.StatusCode, lastStatus, data) {
			return body, resp.Header, nil
		}
		if resp.StatusCode == http.StatusAccepted {
			wait = pollRetryAfter(resp.Header, job.interval)
			continue
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

// isPollNotYet is a poll answer that means "ask again" and never counts against
// the job: the gateway's 503 with Retry-After, and an edge's 502 or 504.
func isPollNotYet(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// isCountedPollFailure is a poll answer that is retried but counts toward
// solanaMaxPollTransportErrors: any other 5xx, or a 429.
func isCountedPollFailure(status int) bool {
	if isPollNotYet(status) {
		return false
	}
	return status >= 500 || status == http.StatusTooManyRequests
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
) (respBody []byte, paid bool, err error) {
	submitURL := bc.apiURL + endpoint
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, false, fmt.Errorf("failed to encode request body: %w", err)
	}

	req1, err := http.NewRequestWithContext(ctx, http.MethodPost, submitURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, false, fmt.Errorf("failed to create request: %w", err)
	}
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := bc.httpClient.Do(req1)
	if err != nil {
		return nil, false, fmt.Errorf("request failed: %w", err)
	}
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()

	if resp1.StatusCode == http.StatusOK {
		// Answered without a payment (free/proxy): nothing was charged.
		return body1, false, nil
	}
	if resp1.StatusCode != http.StatusPaymentRequired {
		return nil, false, &APIError{
			StatusCode: resp1.StatusCode,
			Message:    fmt.Sprintf("API error: %s", string(body1)),
		}
	}
	paymentHeader := paymentRequirementsFrom(resp1.Header.Get("payment-required"), body1)
	if paymentHeader == "" {
		return nil, false, &PaymentError{Message: "402 response but no payment requirements found"}
	}
	paymentReq, err := ParsePaymentRequired(paymentHeader)
	if err != nil {
		return nil, false, &PaymentError{Message: fmt.Sprintf("Failed to parse payment requirements: %v", err)}
	}
	paymentOption, err := bc.extractPaymentDetails(paymentReq)
	if err != nil {
		return nil, false, &PaymentError{Message: fmt.Sprintf("Failed to extract payment details: %v", err)}
	}
	resourceURL := paymentReq.Resource.URL
	if resourceURL == "" {
		resourceURL = submitURL
	}
	paymentPayload, err := bc.createPaymentPayload(paymentOption, resourceURL, paymentReq.Resource.Description, paymentReq.Extensions)
	if err != nil {
		return nil, false, &PaymentError{Message: fmt.Sprintf("Failed to create payment: %v", err)}
	}

	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, submitURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, false, fmt.Errorf("failed to create submit request: %w", err)
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("PAYMENT-SIGNATURE", paymentPayload)
	resp2, err := bc.httpClient.Do(req2)
	if err != nil {
		// The signed transfer may have reached the gateway and settled with
		// only the answer lost. Say so: a blind retry could pay twice.
		return nil, false, fmt.Errorf("%s submit failed in transit after the payment was signed; it may have settled, so check your wallet before submitting again: %w", label, err)
	}
	body2, readErr := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if readErr != nil && (resp2.StatusCode == http.StatusOK || resp2.StatusCode == http.StatusAccepted) {
		// Accepted means paid; only the answer was lost.
		bc.recordPaidCall(resp2.Header, paymentOption.Amount, endpoint)
		return nil, true, fmt.Errorf("%s was paid but its response was cut off: %w. Payment was settled at submit; check your wallet before submitting again", label, readErr)
	}

	switch resp2.StatusCode {
	case http.StatusPaymentRequired:
		return nil, false, &PaymentError{Message: "Payment was rejected. Check your wallet balance."}
	case http.StatusOK:
		bc.recordPaidCall(resp2.Header, paymentOption.Amount, endpoint)
		return body2, true, nil
	case http.StatusAccepted:
		// Paid and detached: followSolanaPaidSubmit books it, then follows it.
	default:
		return nil, false, &APIError{
			StatusCode: resp2.StatusCode,
			Message:    paidFailureMessage(resp2.Header, body2),
		}
	}

	result, _, _, err := bc.followSolanaPaidSubmit(ctx, kind, label, endpoint, body2, resp2.StatusCode, &imagePayment{
		option:       paymentOption,
		payload:      paymentPayload,
		resourceURL:  resourceURL,
		description:  paymentReq.Resource.Description,
		extensions:   paymentReq.Extensions,
		submitHeader: resp2.Header,
	}, budget, interval)
	return result, true, err
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
	bc.recordPaidCall(pay.submitHeader, pay.option.Amount, endpoint)
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

// solanaMaxPaidPollRefusals bounds the payments signed for one finished video that
// were not refused outright: a 402 (settlement held by another poll, outcome
// unknown, a stale blockhash at settle), a 5xx or 429, or a lost answer. The next
// poll reads the job's settlement record.
const solanaMaxPaidPollRefusals = 4

// followSolanaVideoJob follows a Solana video job. The submit only verified the
// payment, so nothing is charged until the job finishes, and a failed job is
// never charged.
//
// Each poll carries an ownership proof, which is read-only. A finished, unpaid
// job answers the proof with 402 { status: completed } and an x402 challenge;
// ONE payment is signed from it and the poll is re-sent with it. That is the only
// payment a job normally sees. Paying on every poll is wrong here: if the
// gateway's settlement record is unreachable, a fresh payment on a re-poll can
// charge again, while a proof poll answers 503.
//
// A gateway that refuses the proof (video answers 400, its "no such job") or
// challenges a job that is not finished gets the old flow for the rest of this
// job: an unpaid GET for a fresh challenge, then a payment the gateway only
// verifies until the job completes.
func (c *VideoClient) followSolanaVideoJob(ctx context.Context, endpoint string, submitBody []byte, submitStatus int, pay *imagePayment) (*VideoResponse, error) {
	var submitted struct {
		ID      string `json:"id"`
		PollURL string `json:"poll_url"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(submitBody, &submitted); err != nil {
		return nil, fmt.Errorf("failed to decode submit response: %w", err)
	}
	if submitted.ID == "" || submitted.PollURL == "" {
		return nil, &APIError{StatusCode: submitStatus, Message: fmt.Sprintf("submit response missing id/poll_url: %s", string(submitBody))}
	}
	pollURL, err := c.resolvePollURL(submitted.PollURL)
	if err != nil {
		return nil, err
	}
	priv, err := solanaKeypair(c.solanaKey)
	if err != nil {
		return nil, fmt.Errorf("cannot sign the poll-ownership proof: %w", err)
	}
	jobID := pollJobID(pollURL, submitted.ID)

	lastStatus := submitted.Status
	if lastStatus == "" {
		lastStatus = "queued"
	}
	interval := c.pollInterval
	if interval <= 0 {
		interval = videoPollInterval
	}

	proofPolls := true
	wait := interval
	failures := 0
	refusalsLeft := solanaMaxPaidPollRefusals
	// paidAmount is the amount last signed for this job; "" until one is sent.
	paidAmount := ""

	resumeNote := func() string {
		if paidAmount == "" {
			return "No payment was taken."
		}
		return fmt.Sprintf("A payment was sent and may have settled: resume by polling %s with the same wallet before submitting again.", pollURL)
	}
	get := func(sign func(*http.Request)) (*http.Response, []byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return nil, nil, err
		}
		sign(req)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		return resp, body, err
	}
	deliver := func(resp *http.Response, body []byte) (*VideoResponse, error) {
		// A job that read as settled on an earlier call books nothing here.
		if paidAmount != "" {
			c.recordPaidCall(resp.Header, paidAmount, endpoint)
		}
		return decodeVideoResponse(body, resp.Header)
	}
	failed := func(resp *http.Response, body []byte) error {
		// A clip that settled and then could not be delivered answers 502 with
		// payment.status set. The money moved, so it is booked.
		note := jobFailureNote(body, " (no payment was taken)")
		if jobReportsSettled(body) && paidAmount != "" {
			c.recordPaidCall(resp.Header, paidAmount, endpoint)
		}
		return &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Upstream generation failed%s: %s", note, string(body))}
	}

	deadline := time.Now().Add(videoPollBudget)
	for time.Now().Before(deadline) {
		if wait > 0 {
			if remaining := time.Until(deadline); wait > remaining {
				wait = remaining
			}
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("video polling stopped: %w. %s", ctx.Err(), resumeNote())
			case <-time.After(wait):
			}
		}
		wait = interval

		// challenge is the 402 this poll pays against, when it pays at all.
		var challengeHeader http.Header
		var challengeBody []byte

		resp, body, err := get(func(req *http.Request) {
			if proofPolls {
				for k, v := range pollAuthHeaders(priv, pollKindVideo, jobID, time.Now()) {
					req.Header.Set(k, v)
				}
			}
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("video polling stopped: %w. %s", ctx.Err(), resumeNote())
			}
			failures++
			if failures >= solanaMaxPollTransportErrors {
				return nil, fmt.Errorf("video poll failed %d times in a row: %w. %s", failures, err, resumeNote())
			}
			continue
		}
		if !isCountedPollFailure(resp.StatusCode) {
			failures = 0
		}

		var data struct {
			Status string `json:"status"`
			Error  any    `json:"error"`
		}
		_ = json.Unmarshal(body, &data)
		if data.Status != "" {
			lastStatus = data.Status
		}

		switch {
		case lastStatus == "failed":
			return nil, failed(resp, body)
		case resp.StatusCode == http.StatusPaymentRequired:
			if proofPolls && data.Status != "completed" {
				proofPolls = false
				wait = 0
				log.Printf("blockrun: video job %s: the gateway answered the poll-ownership proof with a payment challenge; falling back to pay-per-poll for this job", submitted.ID)
				continue
			}
			challengeHeader, challengeBody = resp.Header, body
		case proofPolls && resp.StatusCode == http.StatusBadRequest:
			proofPolls = false
			wait = 0
			log.Printf("blockrun: video job %s: the gateway refused the poll-ownership proof (HTTP 400); falling back to pay-per-poll for this job", submitted.ID)
			continue
		case resp.StatusCode == http.StatusOK && lastStatus == "completed":
			return deliver(resp, body)
		case resp.StatusCode == http.StatusAccepted || isPollNotYet(resp.StatusCode):
			wait = pollRetryAfter(resp.Header, interval)
			continue
		case isCountedPollFailure(resp.StatusCode):
			failures++
			if failures >= solanaMaxPollTransportErrors {
				return nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Poll failed %d times in a row: %s. %s", failures, string(body), resumeNote())}
			}
			wait = pollRetryAfter(resp.Header, interval)
			continue
		case resp.StatusCode == http.StatusOK:
			continue
		default:
			return nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("Poll failed: %s. %s", string(body), resumeNote())}
		}

		// Pay from this challenge: one payment, on a fresh blockhash.
		payload, option, err := c.signFromChallenge(challengeHeader, challengeBody, pollURL, pay)
		if err != nil {
			return nil, &PaymentError{Message: fmt.Sprintf("Failed to pay for the finished video: %v. %s", err, resumeNote())}
		}
		paidAmount = option.Amount

		paidResp, paidBody, err := get(func(req *http.Request) { req.Header.Set("PAYMENT-SIGNATURE", payload) })
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("video polling stopped: %w. %s", ctx.Err(), resumeNote())
			}
			// Only the answer may be lost. The next poll reads the settlement record.
			refusalsLeft--
			if refusalsLeft <= 0 {
				return nil, &PaymentError{Message: fmt.Sprintf("Paid video poll failed in transit: %v. %s", err, resumeNote())}
			}
			continue
		}
		var paid struct {
			Status string `json:"status"`
			Error  any    `json:"error"`
		}
		_ = json.Unmarshal(paidBody, &paid)
		if paid.Status != "" {
			lastStatus = paid.Status
		}

		switch {
		case lastStatus == "failed":
			return nil, failed(paidResp, paidBody)
		case paidResp.StatusCode == http.StatusOK && lastStatus == "completed":
			return deliver(paidResp, paidBody)
		case paidResp.StatusCode == http.StatusPaymentRequired:
			if paid.Error == "Payment verification failed" && !strings.Contains(strings.ToLower(string(paidBody)), "blockhash") {
				// Refused at verification: settlement never ran.
				return nil, &PaymentError{Message: fmt.Sprintf("Payment for the finished video was rejected. Check your Solana USDC balance, then resume by polling %s with the same wallet.", pollURL)}
			}
			refusalsLeft--
			if refusalsLeft <= 0 {
				return nil, &PaymentError{Message: fmt.Sprintf("Payment for the finished video did not settle after %d attempts: %s. %s", solanaMaxPaidPollRefusals, strings.TrimSpace(string(paidBody)), resumeNote())}
			}
			wait = pollRetryAfter(paidResp.Header, interval)
		case paidResp.StatusCode == http.StatusAccepted:
			wait = pollRetryAfter(paidResp.Header, interval)
		case isPollNotYet(paidResp.StatusCode) || isCountedPollFailure(paidResp.StatusCode):
			// The proof poll before the next attempt answers normally, so only this
			// count can stop a run of failing payments from signing to the deadline.
			refusalsLeft--
			if refusalsLeft <= 0 {
				return nil, &APIError{StatusCode: paidResp.StatusCode, Message: fmt.Sprintf("Paid video poll failed %d times: %s. %s", solanaMaxPaidPollRefusals, string(paidBody), resumeNote())}
			}
			wait = pollRetryAfter(paidResp.Header, interval)
		case paidResp.StatusCode == http.StatusOK:
			// A 200 that is neither completed nor failed: keep polling.
		default:
			return nil, &APIError{StatusCode: paidResp.StatusCode, Message: fmt.Sprintf("Poll failed: %s. %s", string(paidBody), resumeNote())}
		}
	}

	return nil, &APIError{
		StatusCode: http.StatusGatewayTimeout,
		Message:    fmt.Sprintf("Video generation did not complete within %.0fs (last status: %s). %s", videoPollBudget.Seconds(), lastStatus, resumeNote()),
	}
}

// signFromChallenge signs a Solana payment from a poll's 402, on a blockhash
// fetched now. A challenge with no usable requirements falls back to the submit's
// payment terms, which the gateway quoted for the same job.
func (c *VideoClient) signFromChallenge(header http.Header, body []byte, pollURL string, pay *imagePayment) (string, *PaymentOption, error) {
	option, resourceURL, description, extensions := pay.option, pollURL, pay.description, pay.extensions
	if raw := paymentRequirementsFrom(header.Get("payment-required"), body); raw != "" {
		req, err := ParsePaymentRequired(raw)
		if err != nil {
			return "", nil, err
		}
		if option, err = c.extractPaymentDetails(req); err != nil {
			return "", nil, err
		}
		// The job was quoted at submit. Selection only checks the chain family, and
		// the signer transfers whichever mint it is handed, while spend tracking
		// reads the amount as six-decimal USDC.
		if option.Asset != pay.option.Asset || option.Network != pay.option.Network {
			return "", nil, fmt.Errorf("the completion challenge asks for asset %s on %s, but the job was submitted with %s on %s",
				option.Asset, option.Network, pay.option.Asset, pay.option.Network)
		}
		description, extensions = req.Resource.Description, req.Extensions
		if req.Resource.URL != "" {
			resourceURL = req.Resource.URL
		}
	}
	payload, err := c.signPayment(option, resourceURL, description, extensions, false)
	if err != nil {
		return "", nil, err
	}
	return payload, option, nil
}
