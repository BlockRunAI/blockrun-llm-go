package blockrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const AccountAPIURL = "https://api.blockrun.ai"
const AccountPortalURL = "https://user.blockrun.ai"

var apiKeyPattern = regexp.MustCompile(`^brk_[A-Za-z0-9_-]+$`)

func newAccountBaseClient(key, apiURL string, timeout time.Duration) (*baseClient, error) {
	if key == "" {
		key = os.Getenv("BLOCKRUN_API_KEY")
	}
	key = strings.TrimSpace(key)
	if !apiKeyPattern.MatchString(key) {
		return nil, &ValidationError{Field: "apiKey", Message: "Set BLOCKRUN_API_KEY or create a key at " + AccountPortalURL + "/dashboard/keys"}
	}
	if apiURL == "" {
		apiURL = os.Getenv("BLOCKRUN_API_BASE_URL")
	}
	if apiURL == "" {
		apiURL = AccountAPIURL
	}
	return &baseClient{apiKey: key, apiURL: apiURL, httpClient: &http.Client{Timeout: timeout}}, nil
}

// AuthMode distinguishes account billing from wallet payments.
func (bc *baseClient) AuthMode() string {
	if bc.apiKey != "" {
		return "api-key"
	}
	return "wallet"
}

func accountURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid account API URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")) {
		return nil, fmt.Errorf("account API URL requires HTTPS (except localhost)")
	}
	return u, nil
}

// Account authentication is installed after caller HTTP options, on a copy of
// their client. Errors reach callers before any x402 handler can sign or retry.
func (bc *baseClient) configureAccount() error {
	bc.apiURL = strings.TrimSuffix(strings.TrimRight(bc.apiURL, "/"), "/v1")
	u, err := accountURL(bc.apiURL)
	if err != nil {
		return err
	}
	client := *bc.httpClient
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = &accountTransport{base: base, key: bc.apiKey, origin: u}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	bc.httpClient = &client
	bc.cache = nil
	return nil
}

type accountTransport struct {
	base   http.RoundTripper
	key    string
	origin *url.URL
}

func (t *accountTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.EqualFold(req.URL.Scheme, t.origin.Scheme) || !strings.EqualFold(req.URL.Host, t.origin.Host) || req.URL.User != nil {
		return nil, fmt.Errorf("refusing to send an account key to another origin")
	}
	r := req.Clone(req.Context())
	if (t.origin.Path == "" || t.origin.Path == "/") && strings.HasPrefix(r.URL.Path, "/api/v1/") {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
		r.URL.RawPath = ""
	}
	for name := range r.Header {
		if strings.Contains(strings.ToLower(name), "payment") || strings.EqualFold(name, "x-api-key") {
			r.Header.Del(name)
		}
	}
	r.Header.Set("Authorization", "Bearer "+t.key)
	resp, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 || resp.StatusCode < 200 {
		defer resp.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body)
		detail, ok := body["error"].(map[string]any)
		if !ok {
			detail = body
		}
		safe := map[string]any{}
		for _, name := range []string{"message", "code", "type", "param"} {
			if value, ok := detail[name].(string); ok {
				safe[name] = strings.ReplaceAll(value, t.key, "[REDACTED]")
			}
		}
		message := "Account API request failed"
		if resp.StatusCode == 402 {
			message = "Insufficient account credits; top up at " + AccountPortalURL + "/dashboard/credits"
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Message: message, Body: safe, RetryAfter: resp.Header.Get("Retry-After")}
	}
	return resp, nil
}

// APIRequest exposes account service endpoints without a dedicated helper.
// The caller owns the response body, including SSE streams. No automatic retry.
func (bc *baseClient) APIRequest(ctx context.Context, method, path string, body any) (*http.Response, error) {
	if bc.apiKey == "" {
		return nil, fmt.Errorf("APIRequest requires account mode")
	}
	u, err := url.Parse(bc.apiURL + "/")
	if err != nil {
		return nil, err
	}
	p, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.ResolveReference(p).String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return bc.httpClient.Do(req)
}

func (bc *baseClient) pollAccount(ctx context.Context, data []byte, status int, budget, interval time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var pollURL string
	for {
		var result struct {
			Status  string `json:"status"`
			PollURL string `json:"poll_url"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, err
		}
		switch result.Status {
		case "completed":
			return data, nil
		case "failed", "cancelled", "canceled":
			return nil, &APIError{StatusCode: 502, Message: "Account job failed or was cancelled"}
		}
		if pollURL == "" {
			pollURL = result.PollURL
		}
		if pollURL == "" {
			if status == 202 || result.Status == "queued" || result.Status == "in_progress" || result.Status == "processing" {
				return nil, &APIError{StatusCode: status, Message: "Async response missing poll_url"}
			}
			return data, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("account job polling stopped; check job before resubmitting: %w", ctx.Err())
		case <-timer.C:
		}
		resp, err := bc.APIRequest(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) {
				switch apiErr.StatusCode {
				case 502, 503, 504, 522, 524:
					// Retry only this existing job's GET, within the original deadline.
					continue
				}
			}
			return nil, err
		}
		data, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		status = resp.StatusCode
	}
}
