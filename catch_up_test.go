package blockrun

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func basePaymentHeader(t *testing.T, amount string) string {
	t.Helper()
	pr := PaymentRequirement{
		X402Version: 2,
		Accepts: []PaymentOption{{
			Scheme: "exact", Network: "eip155:8453", Amount: amount, Asset: USDCBase,
			PayTo: "0x1234567890123456789012345678901234567890", MaxTimeoutSeconds: 600,
		}},
		Resource: ResourceInfo{URL: "https://blockrun.ai/api/v1/x"},
	}
	raw, err := json.Marshal(pr)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func receiptHeader(t *testing.T, receipt map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(receipt)
	return base64.StdEncoding.EncodeToString(raw)
}

func TestSettlementSkipReason(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"none", nil, ""},
		{"skipped", map[string]string{"X-Settlement-Skipped": "empty-result"}, "empty-result"},
		{"unexecuted rpc", map[string]string{"X-Settlement-Skipped": "unexecuted", "X-Payment-Settled": "false"}, "unexecuted"},
		{"waived", map[string]string{"X-Payment-Waived": "empty-result"}, "empty-result"},
		{"says the opposite", map[string]string{"X-Settlement-Skipped": "false"}, ""},
		{"says none", map[string]string{"X-Payment-Waived": "None"}, ""},
		{"settled false alone is owed", map[string]string{"X-Payment-Settled": "false"}, ""},
		{"a receipt hash beats a skip", map[string]string{"X-Settlement-Skipped": "free-fallback", "X-Payment-Receipt": "0xabc"}, ""},
		{"a receipt with a tx beats a skip", map[string]string{
			"X-Settlement-Skipped": "free-fallback",
			"PAYMENT-RESPONSE":     receiptHeader(t, map[string]any{"success": true, "transaction": "0xabc"}),
		}, ""},
		{"a receipt with a charge beats a skip", map[string]string{
			"X-Payment-Waived": "empty-result",
			"PAYMENT-RESPONSE": receiptHeader(t, map[string]any{"success": true, "extra": map[string]any{"chargedAmount": "5"}}),
		}, ""},
		{"an empty free-fallback receipt is no evidence", map[string]string{
			"X-Settlement-Skipped": "free-fallback",
			"PAYMENT-RESPONSE":     receiptHeader(t, map[string]any{"success": true, "transaction": ""}),
		}, "free-fallback"},
		{"a failed receipt is no evidence", map[string]string{
			"X-Settlement-Skipped": "unexecuted",
			"PAYMENT-RESPONSE":     receiptHeader(t, map[string]any{"success": false, "transaction": "0xabc"}),
		}, "unexecuted"},
		{"a malformed receipt is ignored", map[string]string{"X-Settlement-Skipped": "unexecuted", "PAYMENT-RESPONSE": "%%%"}, "unexecuted"},
	}
	captureLog(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tc.headers {
				h.Set(k, v)
			}
			if got := settlementSkipReason(h); got != tc.want {
				t.Errorf("settlementSkipReason = %q, want %q", got, tc.want)
			}
		})
	}
}

// Every wallet-rail booking path books nothing for a skipped settlement and the
// signed amount otherwise.
func TestSkippedSettlementBooksNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	prHeader := basePaymentHeader(t, "250000")
	for _, skip := range []bool{true, false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("PAYMENT-SIGNATURE") == "" {
				w.Header().Set("payment-required", prHeader)
				w.WriteHeader(http.StatusPaymentRequired)
				return
			}
			if skip {
				w.Header().Set("X-Settlement-Skipped", "free-fallback")
			}
			if strings.Contains(r.Header.Get("Accept"), "event-stream") {
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
				return
			}
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"markets":[]}`)
		}))

		newClient := func() *LLMClient {
			c, err := NewLLMClient(testPrivateKey, WithAPIURL(srv.URL))
			if err != nil {
				t.Fatal(err)
			}
			c.costLog = nil
			return c
		}
		msgs := []ChatMessage{{Role: "user", Content: "hi"}}
		ctx := context.Background()

		paths := map[string]func(*LLMClient) error{
			"post": func(c *LLMClient) error { _, err := c.ChatCompletion(ctx, "openai/gpt-5.5", msgs, nil); return err },
			"get":  func(c *LLMClient) error { _, err := c.doGetWithPayment(ctx, "/v1/pm/x", nil); return err },
			"stream": func(c *LLMClient) error {
				s, err := c.ChatCompletionStream(ctx, "openai/gpt-5.5", msgs, nil)
				if err == nil {
					_ = s.Close()
				}
				return err
			},
		}
		for name, call := range paths {
			c := newClient()
			if err := call(c); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			got := c.GetSpending()
			wantUSD, wantCalls := 0.25, 1
			if skip {
				wantUSD, wantCalls = 0, 0
			}
			if got.TotalUSD != wantUSD || got.Calls != wantCalls {
				t.Errorf("%s skip=%v: spending %+v, want $%v over %d calls", name, skip, got, wantUSD, wantCalls)
			}
		}
		srv.Close()
	}
}

func TestPaidFailureMessage(t *testing.T) {
	withTx := http.Header{}
	withTx.Set("PAYMENT-RESPONSE", receiptHeader(t, map[string]any{"success": true, "transaction": "0xabc"}))
	hardCoded := http.Header{}
	hardCoded.Set("PAYMENT-RESPONSE", receiptHeader(t, map[string]any{"success": true}))

	cases := []struct {
		name   string
		header http.Header
		body   string
		want   string
	}{
		{"settled tx", withTx, `boom`, "payment SETTLED, tx 0xabc"},
		{"success without a tx proves nothing", hardCoded, `boom`, "no settlement reported"},
		{"nothing known", http.Header{}, `boom`, "check your wallet"},
		{"not charged", http.Header{}, `{"payment_status":"not_charged"}`, "reports no charge"},
		{"unknown", http.Header{}, `{"payment_status":"unknown"}`, "check your wallet before paying again"},
		{"settled in body", http.Header{}, `{"payment_status":"settled","tx_hash":"sig1"}`, "tx sig1"},
	}
	for _, tc := range cases {
		if got := paidFailureMessage(tc.header, []byte(tc.body)); !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q does not contain %q", tc.name, got, tc.want)
		}
	}
}

func TestExtractPayment_OnlyExactIsSigned(t *testing.T) {
	solNet := "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"
	opt, err := ExtractPaymentDetailsForChain(&PaymentRequirement{Accepts: []PaymentOption{
		{Scheme: "batch-settlement", Network: solNet, Amount: "9"},
		{Scheme: "exact", Network: solNet, Amount: "1"},
	}}, "solana")
	if err != nil {
		t.Fatal(err)
	}
	if opt.Scheme != "exact" || opt.Amount != "1" {
		t.Errorf("selected %+v, want the exact option", opt)
	}

	opt, err = ExtractPaymentDetailsForChain(&PaymentRequirement{Accepts: []PaymentOption{
		{Scheme: "upto", Network: "eip155:8453", Amount: "9"},
		{Network: "eip155:8453", Amount: "1"},
	}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if opt.Amount != "1" {
		t.Errorf("selected %+v, want the v1 option with no scheme", opt)
	}

	if _, err = ExtractPaymentDetailsForChain(&PaymentRequirement{Accepts: []PaymentOption{
		{Scheme: "upto", Network: "eip155:8453", Amount: "9"},
	}}, "base"); err == nil || !strings.Contains(err.Error(), "upto") {
		t.Errorf("an upto-only 402 must be refused, not signed as a ceiling transfer: %v", err)
	}
}

func decodeBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestChatOptions_ReasoningEffortAndSmartChat(t *testing.T) {
	var mu sync.Mutex
	var saw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeBody(t, r)
		mu.Lock()
		saw = body
		mu.Unlock()
		if body["stream"] == true {
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()
	c, err := NewLLMClient(testPrivateKey, WithAPIURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	msgs := []ChatMessage{{Role: "user", Content: "hi"}}

	if _, err := c.ChatCompletion(ctx, "m", msgs, &ChatCompletionOptions{ReasoningEffort: ReasoningLow}); err != nil {
		t.Fatal(err)
	}
	if saw["reasoning_effort"] != "low" {
		t.Errorf("chat reasoning_effort = %v", saw["reasoning_effort"])
	}

	if _, err := c.ChatCompletion(ctx, "m", msgs, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := saw["reasoning_effort"]; ok {
		t.Error("unset reasoning_effort must not be sent")
	}

	s, err := c.ChatCompletionStream(ctx, "m", msgs, &ChatCompletionOptions{ReasoningEffort: ReasoningMax, MaxTokens: 7})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if saw["reasoning_effort"] != "max" || saw["max_tokens"] != float64(7) {
		t.Errorf("stream body = %v", saw)
	}

	// SmartChat used to drop every option it documents.
	if _, err := c.SmartChat(ctx, "hello", &SmartChatOptions{MaxTokens: 42, Temperature: 0.5, ReasoningEffort: ReasoningHigh}); err != nil {
		t.Fatal(err)
	}
	if saw["max_tokens"] != float64(42) || saw["temperature"] != 0.5 || saw["reasoning_effort"] != "high" {
		t.Errorf("SmartChat body = %v", saw)
	}
}

func TestToolAndSourceWireFormat(t *testing.T) {
	marshal := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	if got, want := marshal(Tool{Type: "x_search", AllowedXHandles: []string{"@OpenAI", "xai"}, FromDate: "2026-10-04"}),
		`{"type":"x_search","allowed_x_handles":["OpenAI","xai"],"from_date":"2026-10-04"}`; got != want {
		t.Errorf("x_search = %s", got)
	}
	if got, want := marshal(Tool{Type: "web_search", AllowedDomains: []string{"example.com"}}),
		`{"type":"web_search","allowed_domains":["example.com"]}`; got != want {
		t.Errorf("web_search = %s", got)
	}
	if got, want := marshal(Tool{Type: "function", Function: ToolFunction{Name: "f"}}),
		`{"type":"function","function":{"name":"f"}}`; got != want {
		t.Errorf("function = %s", got)
	}
	if got := marshal(SearchParameters{MaxTurns: 2, Sources: []SearchSource{{Type: "x", IncludedXHandles: []string{"@a"}, EnableVideoUnderstanding: true}}}); !strings.Contains(got, `"included_x_handles":["a"]`) ||
		!strings.Contains(got, `"enable_video_understanding":true`) || !strings.Contains(got, `"max_turns":2`) {
		t.Errorf("search parameters = %s", got)
	}
}

func TestValidateVideoOptions(t *testing.T) {
	clip := []VideoReferenceClip{{URL: "https://example.com/a.mp4"}}
	yes := true
	images := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "https://example.com/i.png"
		}
		return out
	}
	ok := []VideoGenerateOptions{
		{},
		{Model: "bytedance/seedance-2.5", ReferenceImageURLs: images(30), OutputFormat: "mov"},
		{Model: "bytedance/seedance-2.0-mini", ReferenceVideos: clip, ReferenceAudios: clip, BitrateMode: "high"},
		{Model: seedance15Pro, CameraFixed: &yes, SafetyIdentifier: "u1", InputType: "text"},
	}
	for _, o := range ok {
		o := o
		if err := validateVideoOptions(&o); err != nil {
			t.Errorf("%+v: %v", o, err)
		}
	}
	bad := map[string]VideoGenerateOptions{
		"10 images on 2.0":       {Model: "bytedance/seedance-2.0", ReferenceImageURLs: images(10)},
		"31 images on 2.5":       {Model: seedance25, ReferenceImageURLs: images(31)},
		"refs on 1.5":            {Model: seedance15Pro, ReferenceImageURLs: images(1)},
		"refs on default model":  {ReferenceVideos: clip},
		"refs with a frame seed": {Model: seedance25, ReferenceVideos: clip, ImageURL: "https://example.com/i.png"},
		"four clips":             {Model: seedance25, ReferenceVideos: append(append(append([]VideoReferenceClip{}, clip...), clip...), append(clip, clip...)...)},
		"audio alone":            {Model: seedance25, ReferenceAudios: clip},
		"clip not http":          {Model: seedance25, ReferenceVideos: []VideoReferenceClip{{URL: "ftp://x"}}},
		"clip role":              {Model: seedance25, ReferenceVideos: []VideoReferenceClip{{URL: "https://x", Role: "first_frame"}}},
		"bitrate value":          {Model: seedance25, BitrateMode: "ultra"},
		"bitrate on 1.5":         {Model: seedance15Pro, BitrateMode: "high"},
		"format on 2.0":          {Model: "bytedance/seedance-2.0", OutputFormat: "mov"},
		"format value":           {Model: seedance25, OutputFormat: "avi"},
		"camera on 2.0":          {Model: "bytedance/seedance-2.0", CameraFixed: &yes},
		"safety on grok":         {Model: "xai/grok-imagine-video", SafetyIdentifier: "u1"},
		"input type":             {InputType: "audio"},
	}
	for name, o := range bad {
		o := o
		if err := validateVideoOptions(&o); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
}

func TestSolanaClient_BaseOnlyFamiliesExplainThemselves(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	c, err := NewLLMClientSolana(testSolanaKey(t), srv.URL, WithAPIURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err1 := c.Defi(ctx, "protocols", nil)
	_, err2 := c.DexPrice(ctx, nil)
	_, err3 := c.DexGaslessSubmit(ctx, map[string]any{"a": 1})
	for i, err := range []error{err1, err2, err3} {
		var v *ValidationError
		if !errors.As(err, &v) {
			t.Errorf("call %d: %v, want a ValidationError", i, err)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("%d requests sent, want none", hits.Load())
	}
}

// --- slow jobs on Base and the account rail ---

func shortJobPolls(t *testing.T) {
	t.Helper()
	budget, interval := jobPollBudget, jobPollInterval
	jobPollBudget, jobPollInterval = 5*time.Second, time.Millisecond
	t.Cleanup(func() { jobPollBudget, jobPollInterval = budget, interval })
}

func TestMusic_BaseSlowTrackIsPolledAndBookedOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	shortJobPolls(t)
	prHeader := basePaymentHeader(t, "157500")
	var polls atomic.Int32
	var pollSig atomic.Value
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			pollSig.Store(r.Header.Get("PAYMENT-SIGNATURE"))
			switch polls.Add(1) {
			case 1:
				w.WriteHeader(http.StatusAccepted)
				_, _ = io.WriteString(w, `{"id":"j1","status":"in_progress"}`)
			case 2:
				w.WriteHeader(http.StatusBadGateway)
			case 3:
				hj, _ := w.(http.Hijacker)
				conn, _, _ := hj.Hijack()
				_ = conn.Close() // the answer never arrives
			default:
				w.Header().Set("x-payment-receipt", "0xtrack")
				_, _ = io.WriteString(w, `{"status":"completed","model":"minimax/music-2.5+","data":[{"url":"https://cdn/t.mp3"}]}`)
			}
			return
		}
		if r.Header.Get("PAYMENT-SIGNATURE") == "" {
			w.Header().Set("payment-required", prHeader)
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"id":"j1","status":"queued","poll_url":"`+srvURL+`/v1/audio/generations/j1"}`)
	}))
	defer srv.Close()
	srvURL = srv.URL

	c, err := NewMusicClient(testPrivateKey, WithMusicAPIURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	c.costLog = nil
	out, err := c.Generate(context.Background(), "lofi", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(out.Data) != 1 || out.TxHash != "0xtrack" {
		t.Errorf("response = %+v", out)
	}
	if s, _ := pollSig.Load().(string); s == "" {
		t.Error("a Base poll must carry the submit's PAYMENT-SIGNATURE")
	}
	if got := c.GetSpending(); got.TotalUSD != 0.1575 || got.Calls != 1 {
		t.Errorf("spending = %+v", got)
	}
}

func TestMusic_AccountRailSlowTrackIsFollowed(t *testing.T) {
	shortJobPolls(t)
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("request without the API key")
		}
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"status":"completed","data":[{"url":"https://cdn/t.mp3"}],"price":{"amount":"0.2"}}`)
			return
		}
		posts.Add(1)
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"id":"j1","status":"queued","poll_url":"/api/v1/audio/generations/j1"}`)
	}))
	defer srv.Close()

	t.Setenv("BLOCKRUN_API_KEY", "")
	c, err := NewMusicClient(testAPIKey, WithMusicAPIURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	c.costLog = nil
	out, err := c.Generate(context.Background(), "lofi", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(out.Data) != 1 || posts.Load() != 1 {
		t.Errorf("response = %+v after %d POSTs", out, posts.Load())
	}
	if got := c.GetSpending(); got.TotalUSD != 0.2 {
		t.Errorf("spending = %+v", got)
	}
}

func TestPollAsyncJob_StopsWhenFailed(t *testing.T) {
	cases := []struct {
		name, body string
		code       int
		want       string
		booked     bool
	}{
		{"unknown money", `{"status":"failed","payment_status":"unknown"}`, 200, "check your wallet", false},
		{"settled, undeliverable 502 stops at once and books", `{"status":"failed","payment_status":"settled"}`, 502, "payment was taken", true},
		{"a payment block counts too", `{"status":"failed","payment":{"status":"already_settled"}}`, 502, "do not pay again", true},
		{"no status means the wallet rail's rule", `{"status":"failed"}`, 200, "no payment was taken", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var polls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				polls.Add(1)
				w.WriteHeader(tc.code)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			c, err := NewVideoClient(testPrivateKey, WithVideoAPIURL(srv.URL))
			if err != nil {
				t.Fatal(err)
			}
			c.costLog = nil
			_, _, err = c.pollAsyncJob(context.Background(), asyncJob{
				label: "x", endpoint: "/v1/x", submitBody: []byte(`{"id":"j","poll_url":"` + srv.URL + `/j"}`),
				pay: &imagePayment{option: &PaymentOption{Amount: "1"}, payload: "sig"}, budget: 2 * time.Second, interval: time.Millisecond,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
			if polls.Load() != 1 {
				t.Errorf("%d polls, want 1", polls.Load())
			}
			wantCalls := 0
			if tc.booked {
				wantCalls = 1
			}
			if got := c.GetSpending(); got.Calls != wantCalls {
				t.Errorf("spending = %+v, want %d booked", got, wantCalls)
			}
		})
	}
}

func TestPollAsyncJob_BudgetAfterALostAnswerDoesNotPromiseFree(t *testing.T) {
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if polls.Add(1) == 1 {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"status":"in_progress"}`)
	}))
	defer srv.Close()
	c, err := NewVideoClient(testPrivateKey, WithVideoAPIURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = c.pollAsyncJob(context.Background(), asyncJob{
		label: "x", endpoint: "/v1/x", submitBody: []byte(`{"id":"j","poll_url":"` + srv.URL + `/j"}`),
		pay: &imagePayment{option: &PaymentOption{Amount: "1"}, payload: "sig"}, budget: 100 * time.Millisecond, interval: time.Millisecond,
	})
	if err == nil || strings.Contains(err.Error(), "No payment was taken") || !strings.Contains(err.Error(), "may have been taken") {
		t.Errorf("err = %v", err)
	}
}

func TestSolanaPoll_PendingCompleted202WaitsForTheResult(t *testing.T) {
	g := newSolGateway(t, "/api/v1/audio/generations", "job-1")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		if n == 1 {
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"status":"completed","payment_status":"pending"}`)
			return
		}
		_ = json.NewEncoder(w).Encode(completedMusicBody("job-1"))
	}
	apiURL, rpcURL := g.start()
	c := newSolMusicClient(t, apiURL, rpcURL)
	c.costLog = nil
	out, err := c.Generate(context.Background(), "lofi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 1 || len(g.pollRequests()) != 2 {
		t.Errorf("response %+v after %d polls", out, len(g.pollRequests()))
	}
}

func TestSolanaImage_SkippedSubmitBooksNothing(t *testing.T) {
	g := newSolGateway(t, "/api/v1/images/generations", "job-1")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"status":"completed","created":1,"data":[{"url":"https://cdn/i.png"}]}`)
	}
	apiURL, rpcURL := g.start()
	c := newSolImageClient(t, apiURL, rpcURL)
	c.costLog = nil
	// followSolanaPaidSubmit books from the submit's headers.
	h := http.Header{}
	h.Set("X-Settlement-Skipped", "free-fallback")
	_, _, _, err := c.followSolanaPaidSubmit(context.Background(), pollKindImage, "Image generation", "/v1/images/generations",
		[]byte(`{"id":"job-1","poll_url":"/api/v1/images/generations/job-1"}`), 202,
		&imagePayment{option: &PaymentOption{Amount: "1000"}, submitHeader: h}, 2*time.Second, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.GetSpending(); got.Calls != 0 || got.TotalUSD != 0 {
		t.Errorf("spending = %+v", got)
	}
}

// --- Solana video: proofs while pending, one payment on completion ---

const solVideoRoute = "/api/v1/videos/generations"

func solVideoClient(t *testing.T, apiURL, rpcURL string) *VideoClient {
	t.Helper()
	c, err := NewVideoClientSolana(testSolanaKey(t), rpcURL, WithVideoAPIURL(apiURL))
	if err != nil {
		t.Fatal(err)
	}
	c.pollInterval = time.Millisecond
	c.costLog = nil
	return c
}

func solChallenge(g *solGateway, w http.ResponseWriter, status string) {
	w.Header().Set("payment-required", g.prHeader)
	w.WriteHeader(http.StatusPaymentRequired)
	_, _ = io.WriteString(w, `{"status":"`+status+`","error":"Payment Required"}`)
}

func solVideoDone(w http.ResponseWriter) {
	w.Header().Set("x-payment-receipt", "sigVideo")
	_, _ = io.WriteString(w, `{"status":"completed","created":1,"model":"xai/grok-imagine-video","data":[{"url":"https://cdn/v.mp4"}]}`)
}

func TestSolanaVideo_ProofPollsThenOnePaymentOnCompletion(t *testing.T) {
	g := newSolGateway(t, solVideoRoute, "job-1")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		switch n {
		case 1:
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"status":"in_progress","payment_status":"verified"}`)
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 3:
			solChallenge(g, w, "completed")
		default:
			solVideoDone(w)
		}
	}
	apiURL, rpcURL := g.start()
	c := solVideoClient(t, apiURL, rpcURL)

	out, err := c.Generate(context.Background(), "a cat", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.TxHash != "sigVideo" || len(out.Data) != 1 {
		t.Errorf("response = %+v", out)
	}
	polls := g.pollRequests()
	if len(polls) != 4 {
		t.Fatalf("%d polls, want 4", len(polls))
	}
	wallet := c.GetWalletAddress()
	for _, r := range polls[:3] {
		assertOwnershipProof(t, r, pollKindVideo, "job-1", wallet)
	}
	assertFreshPollPayment(t, g, polls[3].Header.Get("PAYMENT-SIGNATURE"))
	if got := c.GetSpending(); got.Calls != 1 || got.TotalUSD != 0.001 {
		t.Errorf("spending = %+v", got)
	}
}

func TestSolanaVideo_AlreadySettledBooksNothingNew(t *testing.T) {
	g := newSolGateway(t, solVideoRoute, "job-1")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) { solVideoDone(w) }
	apiURL, rpcURL := g.start()
	c := solVideoClient(t, apiURL, rpcURL)
	if _, err := c.Generate(context.Background(), "a cat", nil); err != nil {
		t.Fatal(err)
	}
	if got := c.GetSpending(); got.Calls != 0 {
		t.Errorf("spending = %+v", got)
	}
}

func TestSolanaVideo_RefusedProofFallsBackToPayingPolls(t *testing.T) {
	captureLog(t)
	g := newSolGateway(t, solVideoRoute, "job-1")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		switch n {
		case 1:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"Invalid job id"}`)
		case 2:
			if r.Header.Get(pollHeaderSignature) != "" {
				t.Error("the proof must not be sent again after a refusal")
			}
			solChallenge(g, w, "in_progress")
		default:
			if r.Header.Get("PAYMENT-SIGNATURE") == "" {
				t.Error("a fallback poll must pay")
			}
			solVideoDone(w)
		}
	}
	apiURL, rpcURL := g.start()
	c := solVideoClient(t, apiURL, rpcURL)
	if _, err := c.Generate(context.Background(), "a cat", nil); err != nil {
		t.Fatal(err)
	}
	if got := c.GetSpending(); got.Calls != 1 {
		t.Errorf("spending = %+v", got)
	}
}

func TestSolanaVideo_FailedJobIsNotCharged(t *testing.T) {
	g := newSolGateway(t, solVideoRoute, "job-1")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"status":"failed","payment_status":"not_charged","error":"upstream"}`)
	}
	apiURL, rpcURL := g.start()
	c := solVideoClient(t, apiURL, rpcURL)
	_, err := c.Generate(context.Background(), "a cat", nil)
	if err == nil || !strings.Contains(err.Error(), "no payment was taken") {
		t.Fatalf("err = %v", err)
	}
	if got := c.GetSpending(); got.Calls != 0 {
		t.Errorf("spending = %+v", got)
	}
}

func TestSolanaVideo_PaidButUndeliverableStopsAndBooks(t *testing.T) {
	g := newSolGateway(t, solVideoRoute, "job-1")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		if n == 1 {
			solChallenge(g, w, "completed")
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"status":"failed","error":"backup failed","payment":{"status":"settled","tx_hash":"s"}}`)
	}
	apiURL, rpcURL := g.start()
	c := solVideoClient(t, apiURL, rpcURL)
	_, err := c.Generate(context.Background(), "a cat", nil)
	if err == nil || !strings.Contains(err.Error(), "do not pay again") {
		t.Fatalf("err = %v", err)
	}
	if n := len(g.pollRequests()); n != 2 {
		t.Errorf("%d polls after a stated failure, want 2 (no more paying)", n)
	}
	if got := c.GetSpending(); got.Calls != 1 {
		t.Errorf("spending = %+v", got)
	}
}

func TestSolanaVideo_VerificationRefusalStops(t *testing.T) {
	g := newSolGateway(t, solVideoRoute, "job-1")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		if n == 1 {
			solChallenge(g, w, "completed")
			return
		}
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = io.WriteString(w, `{"error":"Payment verification failed","reason":"insufficient_funds"}`)
	}
	apiURL, rpcURL := g.start()
	c := solVideoClient(t, apiURL, rpcURL)
	_, err := c.Generate(context.Background(), "a cat", nil)
	var pe *PaymentError
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "balance") {
		t.Fatalf("err = %v", err)
	}
	if n := len(g.pollRequests()); n != 2 {
		t.Errorf("%d polls, want 2", n)
	}
}

func TestSolanaVideo_HeldSettlementIsRetriedThenBounded(t *testing.T) {
	g := newSolGateway(t, solVideoRoute, "job-1")
	var paid atomic.Int32
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		if r.Header.Get("PAYMENT-SIGNATURE") == "" {
			solChallenge(g, w, "completed")
			return
		}
		paid.Add(1)
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = io.WriteString(w, `{"error":"settlement in progress"}`)
	}
	apiURL, rpcURL := g.start()
	c := solVideoClient(t, apiURL, rpcURL)
	_, err := c.Generate(context.Background(), "a cat", nil)
	if err == nil || !strings.Contains(err.Error(), "may have settled") {
		t.Fatalf("err = %v", err)
	}
	if paid.Load() != solanaMaxPaidPollRefusals {
		t.Errorf("%d paid polls, want %d", paid.Load(), solanaMaxPaidPollRefusals)
	}
}

func TestReceiptCharged_ChargedAmountWins(t *testing.T) {
	cases := []struct {
		receipt map[string]any
		want    bool
	}{
		{map[string]any{"amount": "1000", "extra": map[string]any{"chargedAmount": "0"}}, false},
		{map[string]any{"amount": "0", "extra": map[string]any{"chargedAmount": "5"}}, true},
		{map[string]any{"amount": "1000", "extra": map[string]any{"chargedAmount": "n/a"}}, true},
		{map[string]any{"amount": "1000"}, true},
		{map[string]any{"amount": "0"}, false},
		{map[string]any{}, false},
	}
	for _, tc := range cases {
		if got := receiptCharged(tc.receipt); got != tc.want {
			t.Errorf("receiptCharged(%v) = %v, want %v", tc.receipt, got, tc.want)
		}
	}
}

func TestPollAsyncJob_SettlingIsNeverReportedAsFree(t *testing.T) {
	run := func(t *testing.T, submit string, answer func(n int32, w http.ResponseWriter)) error {
		t.Helper()
		var polls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { answer(polls.Add(1), w) }))
		defer srv.Close()
		c, err := NewVideoClient(testPrivateKey, WithVideoAPIURL(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = c.pollAsyncJob(context.Background(), asyncJob{
			label: "x", endpoint: "/v1/x", submitBody: []byte(strings.Replace(submit, "POLL", srv.URL+"/j", 1)),
			pay: &imagePayment{option: &PaymentOption{Amount: "1"}, payload: "sig"}, budget: 100 * time.Millisecond, interval: time.Millisecond,
		})
		return err
	}
	const plainSubmit = `{"id":"j","poll_url":"POLL"}`
	pending := func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"status":"in_progress","payment_status":"verified"}`)
	}

	t.Run("still verified is free", func(t *testing.T) {
		err := run(t, plainSubmit, func(_ int32, w http.ResponseWriter) { pending(w) })
		if err == nil || !strings.Contains(err.Error(), "No payment was taken") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("settling then pending", func(t *testing.T) {
		err := run(t, plainSubmit, func(n int32, w http.ResponseWriter) {
			if n == 1 {
				w.WriteHeader(http.StatusAccepted)
				_, _ = io.WriteString(w, `{"status":"in_progress","payment_status":"settling"}`)
				return
			}
			pending(w)
		})
		if err == nil || strings.Contains(err.Error(), "No payment was taken") || !strings.Contains(err.Error(), "may have been taken") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("settling then repeated 500s", func(t *testing.T) {
		err := run(t, plainSubmit, func(n int32, w http.ResponseWriter) {
			if n == 1 {
				w.WriteHeader(http.StatusAccepted)
				_, _ = io.WriteString(w, `{"status":"in_progress","payment_status":"settling"}`)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		})
		if err == nil || !strings.Contains(err.Error(), "may have been taken") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("a proxy's bare 502 then pending", func(t *testing.T) {
		err := run(t, plainSubmit, func(n int32, w http.ResponseWriter) {
			if n == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			pending(w)
		})
		if err == nil || !strings.Contains(err.Error(), "may have been taken") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("in doubt at submit", func(t *testing.T) {
		err := run(t, `{"id":"j","poll_url":"POLL","payment_status":"settled_optimistic"}`, func(_ int32, w http.ResponseWriter) { pending(w) })
		if err == nil || !strings.Contains(err.Error(), "may have been taken") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("failed after settling makes no free claim", func(t *testing.T) {
		err := run(t, plainSubmit, func(n int32, w http.ResponseWriter) {
			if n == 1 {
				w.WriteHeader(http.StatusAccepted)
				_, _ = io.WriteString(w, `{"status":"in_progress","payment_status":"settling"}`)
				return
			}
			_, _ = io.WriteString(w, `{"status":"failed"}`)
		})
		if err == nil || strings.Contains(err.Error(), "no payment was taken") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestSolanaVideo_ChallengeForAnotherAssetIsNotSigned(t *testing.T) {
	for name, mutate := range map[string]func(*PaymentOption){
		"asset":   func(o *PaymentOption) { o.Asset = "So11111111111111111111111111111111111111112" },
		"network": func(o *PaymentOption) { o.Network = "solana:EtWTRABZaYq6iMfeYKouRu166VoYwO2u" },
	} {
		t.Run(name, func(t *testing.T) {
			g := newSolGateway(t, solVideoRoute, "job-1")
			opt := testPaymentOption(USDCSolanaMainnet)
			mutate(opt)
			raw, _ := json.Marshal(PaymentRequirement{X402Version: 2, Accepts: []PaymentOption{*opt}})
			other := base64.StdEncoding.EncodeToString(raw)
			g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
				if r.Header.Get("PAYMENT-SIGNATURE") != "" {
					t.Error("a payment was signed against changed terms")
				}
				w.Header().Set("payment-required", other)
				w.WriteHeader(http.StatusPaymentRequired)
				_, _ = io.WriteString(w, `{"status":"completed"}`)
			}
			apiURL, rpcURL := g.start()
			c := solVideoClient(t, apiURL, rpcURL)
			_, err := c.Generate(context.Background(), "a cat", nil)
			var pe *PaymentError
			if !errors.As(err, &pe) || !strings.Contains(err.Error(), "submitted with") {
				t.Fatalf("err = %v", err)
			}
			if got := c.GetSpending(); got.Calls != 0 {
				t.Errorf("spending = %+v", got)
			}
		})
	}
}

func TestSolanaVideo_FailingPaidPollsStopSigning(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusTooManyRequests, http.StatusBadGateway} {
		g := newSolGateway(t, solVideoRoute, "job-1")
		var paid atomic.Int32
		g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
			if r.Header.Get("PAYMENT-SIGNATURE") == "" {
				solChallenge(g, w, "completed")
				return
			}
			paid.Add(1)
			w.WriteHeader(status)
		}
		apiURL, rpcURL := g.start()
		c := solVideoClient(t, apiURL, rpcURL)
		if _, err := c.Generate(context.Background(), "a cat", nil); err == nil || !strings.Contains(err.Error(), "may have settled") {
			t.Errorf("status %d: err = %v", status, err)
		}
		if paid.Load() != solanaMaxPaidPollRefusals {
			t.Errorf("status %d: %d payments signed, want %d", status, paid.Load(), solanaMaxPaidPollRefusals)
		}
	}
}

func TestPollAsyncJob_CancelKeepsTheMoneyNote(t *testing.T) {
	var polls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls.Add(1)
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"status":"in_progress","payment_status":"settling"}`)
		cancel()
	}))
	defer srv.Close()
	c, err := NewVideoClient(testPrivateKey, WithVideoAPIURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = c.pollAsyncJob(ctx, asyncJob{
		label: "x", endpoint: "/v1/x", submitBody: []byte(`{"id":"j","poll_url":"` + srv.URL + `/j"}`),
		pay: &imagePayment{option: &PaymentOption{Amount: "1"}, payload: "sig"}, budget: 5 * time.Second, interval: time.Millisecond,
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want it to wrap context.Canceled", err)
	}
	if err == nil || !strings.Contains(err.Error(), "may have been taken") {
		t.Errorf("err = %v", err)
	}
}
