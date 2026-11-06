package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// asyncJob is a media job that answered 202 { id, poll_url } and is followed by
// pollAsyncJob: on the Base wallet rail and on the account rail. Solana wallet
// jobs have their own pollers in solana_media_poll.go, because they prove
// ownership on each poll instead of carrying a payment.
type asyncJob struct {
	// label names the job in errors ("Image generation").
	label string
	// endpoint is the submit path, used to book the cost.
	endpoint     string
	submitBody   []byte
	submitStatus int
	// pay is nil on the account rail, where the API key does this job.
	pay      *imagePayment
	budget   time.Duration
	interval time.Duration
}

// pollAsyncJob GET-polls a job to a terminal state and returns the completed
// poll's body and headers, with the cost booked.
//
// On the Base wallet rail every poll carries the submit's PAYMENT-SIGNATURE and
// the gateway settles on the first poll that sees the job completed. On the
// account rail the completed payload carries its own price.
//
// A poll that gets no answer, and a 5xx or 429, keep polling instead of
// abandoning the job: the lost answer may have been the one that settled it, and
// asking again returns the job as already settled with no second charge. Polling
// stops on a status of failed, including a 502 that says so.
func (bc *baseClient) pollAsyncJob(ctx context.Context, job asyncJob) ([]byte, http.Header, error) {
	var submitted struct {
		ID      string `json:"id"`
		PollURL string `json:"poll_url"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(job.submitBody, &submitted); err != nil {
		return nil, nil, fmt.Errorf("failed to decode submit response: %w", err)
	}
	if submitted.ID == "" || submitted.PollURL == "" {
		return nil, nil, &APIError{
			StatusCode: job.submitStatus,
			Message:    fmt.Sprintf("submit response missing id/poll_url: %s", string(job.submitBody)),
		}
	}
	pollURL, err := bc.resolvePollURL(submitted.PollURL)
	if err != nil {
		return nil, nil, err
	}

	lastStatus := submitted.Status
	if lastStatus == "" {
		lastStatus = "queued"
	}
	deadline := time.Now().Add(job.budget)
	wait := job.interval
	failures := 0
	lostAnswer := false

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(wait):
		}
		wait = job.interval

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create poll request: %w", err)
		}
		if job.pay != nil {
			req.Header.Set("PAYMENT-SIGNATURE", job.pay.payload)
		}
		bc.applyAuth(req)

		resp, err := bc.httpClient.Do(req)
		var body []byte
		if err == nil {
			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			lostAnswer = true
			failures++
			if failures >= solanaMaxPollTransportErrors {
				return nil, nil, fmt.Errorf("poll request failed %d times in a row: %w.%s", failures, err, job.givingUpNote(pollURL, true))
			}
			continue
		}
		if !isCountedPollFailure(resp.StatusCode) {
			failures = 0
		}

		if resp.StatusCode == http.StatusPaymentRequired && job.pay == nil {
			return nil, nil, apiKeyPaymentError(body)
		}

		var data map[string]any
		_ = json.Unmarshal(body, &data)
		if s, ok := data["status"].(string); ok && s != "" {
			lastStatus = s
		}

		if lastStatus == "failed" {
			// Settlement waits for a completed poll on the wallet rail, so a failed
			// job costs nothing unless the gateway says otherwise. Credit is taken
			// at submit, so the account rail makes no such claim.
			fallback := ""
			if job.pay != nil {
				fallback = " (no payment was taken)"
			}
			return nil, nil, &APIError{
				StatusCode: resp.StatusCode,
				Message:    fmt.Sprintf("Upstream generation failed%s: %s", jobFailureNote(body, fallback), string(body)),
			}
		}
		// Success is keyed on the status, not on a literal 200: the charge is final
		// the moment a poll reports completed.
		if isCompletedPoll(resp.StatusCode, lastStatus, data) {
			if job.pay != nil {
				bc.recordPaidCall(resp.Header, job.pay.option.Amount, job.endpoint)
			} else {
				bc.recordAPIKeyCost(body, job.endpoint)
			}
			return body, resp.Header, nil
		}
		if resp.StatusCode == http.StatusAccepted || isPollNotYet(resp.StatusCode) {
			wait = pollRetryAfter(resp.Header, job.interval)
			continue
		}
		if isCountedPollFailure(resp.StatusCode) {
			failures++
			if failures >= solanaMaxPollTransportErrors {
				return nil, nil, &APIError{
					StatusCode: resp.StatusCode,
					Message:    fmt.Sprintf("Poll failed %d times in a row: %s.%s", failures, string(body), job.givingUpNote(pollURL, lostAnswer)),
				}
			}
			wait = pollRetryAfter(resp.Header, job.interval)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, nil, &APIError{
				StatusCode: resp.StatusCode,
				Message:    fmt.Sprintf("Poll failed: %s", string(body)),
			}
		}
	}

	return nil, nil, &APIError{
		StatusCode: http.StatusGatewayTimeout,
		Message: fmt.Sprintf(
			"%s did not complete within %.0fs (last status: %s).%s",
			job.label, job.budget.Seconds(), lastStatus, job.givingUpNote(pollURL, lostAnswer),
		),
	}
}

// givingUpNote says what stopping now means for the money. Only the wallet rail
// defers settlement to a completed poll, so only there is giving up free, and
// only if no poll's answer was lost.
func (j asyncJob) givingUpNote(pollURL string, lostAnswer bool) string {
	if j.pay == nil {
		return fmt.Sprintf(" The account is billed if the job completes; it stays claimable at %s with the same API key.", pollURL)
	}
	if lostAnswer {
		return fmt.Sprintf(" A poll's answer was lost, so payment may have been taken: check your wallet and poll %s with the same wallet before submitting again.", pollURL)
	}
	return " No payment was taken."
}

// isCompletedPoll reports whether a poll answer delivers the finished job. It
// needs a 2xx. A 202 counts only when it carries the result: the gateway also
// answers 202 with no data while a completed job's settlement is still landing.
func isCompletedPoll(code int, status string, data map[string]any) bool {
	if status != "completed" || code < 200 || code >= 300 {
		return false
	}
	if code != http.StatusAccepted {
		return true
	}
	result, ok := data["data"].([]any)
	return ok && len(result) > 0
}
