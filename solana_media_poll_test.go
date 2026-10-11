package blockrun

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-tron/base58"
)

// solGateway is a fake sol.blockrun.ai for a media route that settles at the
// POST and detaches a slow job to 202 { id, poll_url }. Poll answers are
// scripted per test; every request is recorded for assertions.
type solGateway struct {
	t        *testing.T
	route    string // e.g. "/api/v1/audio/generations"
	jobID    string // the job id, before percent-encoding
	pollPath string // what the 202 advertises as poll_url ("" = default)
	prHeader string
	// serverBlockhash is the 402's extra.recentBlockhash (pinned to submit).
	serverBlockhash string

	mu         sync.Mutex
	paidPOSTs  int
	polls      []*http.Request
	pollAnswer func(n int, r *http.Request, w http.ResponseWriter)
}

func newSolGateway(t *testing.T, route, jobID string) *solGateway {
	t.Helper()
	opt := testPaymentOption(USDCSolanaMainnet) // Amount 1000 = $0.001
	bh := makeBlockhash(t).String()
	opt.Extra["recentBlockhash"] = bh
	pr := PaymentRequirement{
		X402Version: 2,
		Accepts:     []PaymentOption{*opt},
		Resource:    ResourceInfo{URL: "https://sol.blockrun.ai" + route, Description: "media"},
	}
	prJSON, _ := json.Marshal(pr)
	return &solGateway{t: t, route: route, jobID: jobID, prHeader: base64.StdEncoding.EncodeToString(prJSON), serverBlockhash: bh}
}

func (g *solGateway) defaultPollPath() string {
	return g.route + "/" + strings.ReplaceAll(urlPathEscape(g.jobID), "/", "%2F")
}

func urlPathEscape(s string) string {
	// encodeURIComponent for the characters the tests use.
	r := strings.NewReplacer(" ", "%20", "/", "%2F", "+", "%2B")
	return r.Replace(s)
}

func (g *solGateway) handler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		g.mu.Lock()
		g.polls = append(g.polls, r)
		n := len(g.polls)
		g.mu.Unlock()
		g.pollAnswer(n, r, w)
		return
	}
	if r.URL.Path != g.route {
		g.t.Errorf("POST to %q, want %q", r.URL.Path, g.route)
	}
	if r.Header.Get("PAYMENT-SIGNATURE") == "" {
		w.Header().Set("payment-required", g.prHeader)
		w.WriteHeader(http.StatusPaymentRequired)
		return
	}
	g.mu.Lock()
	g.paidPOSTs++
	g.mu.Unlock()
	poll := g.pollPath
	if poll == "" {
		poll = g.defaultPollPath()
	} else if poll == "-" {
		poll = ""
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": g.jobID, "status": "queued", "payment_status": "settled_optimistic", "poll_url": poll,
	})
}

// start serves the gateway under /api, the way sol.blockrun.ai mounts it, so
// the client's apiURL ends in /api exactly like DefaultSolanaAPIURL.
func (g *solGateway) start() (apiURL string, rpcURL string) {
	g.t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(g.handler))
	g.t.Cleanup(srv.Close)
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"value":{"blockhash":%q}}}`, makeBlockhash(g.t).String())
	}))
	g.t.Cleanup(rpc.Close)
	resetSolanaBlockhashCacheForTest(g.t)
	return srv.URL + "/api", rpc.URL
}

func (g *solGateway) pollRequests() []*http.Request {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]*http.Request(nil), g.polls...)
}

// assertFreshPollPayment checks a fallback poll's payment is signed against a
// blockhash fetched for the poll, not the submit-time one the 402 pinned (dead
// within ~60-90s). Signature inequality alone proves nothing: every tx carries
// a random memo nonce.
func assertFreshPollPayment(t *testing.T, g *solGateway, payload string) {
	t.Helper()
	if payload == "" {
		t.Fatal("fallback poll carried no PAYMENT-SIGNATURE")
	}
	tx := decodePaymentTx(t, payload)
	if got := tx.Message.RecentBlockhash.String(); got == g.serverBlockhash {
		t.Errorf("fallback poll re-used the submit-time blockhash %s", got)
	}
}

// assertOwnershipProof checks a poll carries a valid proof for kind/jobID by
// wallet, and no payment.
func assertOwnershipProof(t *testing.T, r *http.Request, kind, jobID, wallet string) {
	t.Helper()
	if sig := r.Header.Get("PAYMENT-SIGNATURE"); sig != "" {
		t.Errorf("proof poll carried a payment (PAYMENT-SIGNATURE set)")
	}
	if r.Header.Get("X-PAYMENT") != "" {
		t.Errorf("proof poll carried an X-PAYMENT header")
	}
	if got := r.Header.Get(pollHeaderWallet); got != wallet {
		t.Errorf("x-poll-wallet = %q, want the paying wallet %q", got, wallet)
	}
	ts, err := strconv.ParseInt(r.Header.Get(pollHeaderTimestamp), 10, 64)
	if err != nil {
		t.Fatalf("x-poll-timestamp %q: %v", r.Header.Get(pollHeaderTimestamp), err)
	}
	if d := time.Now().Unix() - ts; d < -300 || d > 300 {
		t.Errorf("x-poll-timestamp %d is %ds from now (must be unix SECONDS within ±300)", ts, d)
	}
	pub, err := base58.Decode(r.Header.Get(pollHeaderWallet))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		t.Fatalf("x-poll-wallet is not a base58 32-byte key: %v", err)
	}
	sig, err := base58.Decode(r.Header.Get(pollHeaderSignature))
	if err != nil || len(sig) != ed25519.SignatureSize {
		t.Fatalf("x-poll-signature is not a base58 64-byte signature: %v", err)
	}
	msg := fmt.Sprintf("blockrun-poll:v1:%s:%s:%d", kind, jobID, ts)
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(msg), sig) {
		t.Errorf("x-poll-signature does not verify over %q", msg)
	}
}

func completedMusicBody(id string) map[string]any {
	return map[string]any{
		"id": id, "object": "audio.generation.job", "status": "completed", "model": "minimax/music-2.5+",
		"created": 1760000000,
		"data":    []map[string]any{{"url": "https://cdn.example.com/track.mp3", "duration_seconds": 182.5}},
	}
}

func newSolMusicClient(t *testing.T, apiURL, rpcURL string) *MusicClient {
	t.Helper()
	c, err := NewMusicClientSolana(testSolanaKey(t), rpcURL, WithMusicAPIURL(apiURL))
	if err != nil {
		t.Fatalf("NewMusicClientSolana: %v", err)
	}
	c.pollInterval = 5 * time.Millisecond
	return c
}

func newSolImageClient(t *testing.T, apiURL, rpcURL string) *ImageClient {
	t.Helper()
	c, err := NewImageClientSolana(testSolanaKey(t), rpcURL, WithImageAPIURL(apiURL))
	if err != nil {
		t.Fatalf("NewImageClientSolana: %v", err)
	}
	c.pollInterval = 5 * time.Millisecond
	return c
}

// captureLog redirects the standard logger for the test and returns its buffer.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	return &buf
}

// Defect 1: a Solana music POST that detaches to 202 is paid. The SDK must
// follow it to the track — polling with the ownership proof, never a payment —
// and book the POST's cost exactly once. The job id is one that needs
// percent-encoding, and apiURL ends in /api while poll_url starts with /api,
// so a doubled /api or an undecoded id both fail here.
func TestMusicSolana202PollsWithOwnershipProof(t *testing.T) {
	g := newSolGateway(t, "/api/v1/audio/generations", "aud job+1")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		if n < 3 {
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": g.jobID, "status": "in_progress"})
			return
		}
		_ = json.NewEncoder(w).Encode(completedMusicBody(g.jobID))
	}
	apiURL, rpcURL := g.start()
	c := newSolMusicClient(t, apiURL, rpcURL)

	resp, err := c.Generate(context.Background(), "lofi beat", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].URL != "https://cdn.example.com/track.mp3" {
		t.Fatalf("unexpected track: %+v", resp.Data)
	}
	polls := g.pollRequests()
	if len(polls) != 3 {
		t.Fatalf("polls = %d, want 3", len(polls))
	}
	for _, p := range polls {
		if got, want := p.URL.EscapedPath(), "/api/v1/audio/generations/aud%20job%2B1"; got != want {
			t.Errorf("poll path = %q, want %q (doubled /api or mangled id)", got, want)
		}
		assertOwnershipProof(t, p, "audio", "aud job+1", c.GetWalletAddress())
	}
	if g.paidPOSTs != 1 {
		t.Errorf("paid POSTs = %d, want 1", g.paidPOSTs)
	}
	if s := c.GetSpending(); s.TotalUSD != 0.001 || s.Calls != 1 {
		t.Errorf("spending = %+v, want $0.001 over 1 call (booked from the POST)", s)
	}
}

// A paid track that fails upstream after the 202 was still charged: the cost
// stays booked and the error must say so instead of inviting a free retry.
func TestMusicSolanaFailedAfter202IsChargedAndSaysSo(t *testing.T) {
	g := newSolGateway(t, "/api/v1/audio/generations", "aud_fail")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": g.jobID, "status": "failed", "error": "upstream exploded"})
	}
	apiURL, rpcURL := g.start()
	c := newSolMusicClient(t, apiURL, rpcURL)

	_, err := c.Generate(context.Background(), "doomed", nil)
	if err == nil || !strings.Contains(err.Error(), "settled at submit") {
		t.Fatalf("want a settled-at-submit failure, got %v", err)
	}
	if got := c.GetSpending().TotalUSD; got != 0.001 {
		t.Errorf("spending = %f, want 0.001 booked at submit", got)
	}
}

// A 404 to the proof is the gateway's refusal (byte-identical to "no such
// job"); the job falls back to pay-per-poll, logged once, with a FRESH
// signature on every poll (a Solana payment dies with its blockhash).
func TestMusicSolanaProofRefusedFallsBackToPayPerPoll(t *testing.T) {
	logs := captureLog(t)
	g := newSolGateway(t, "/api/v1/audio/generations", "aud_old")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		if r.Header.Get(pollHeaderSignature) != "" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "Job not found"})
			return
		}
		if r.Header.Get("PAYMENT-SIGNATURE") == "" {
			t.Errorf("fallback poll %d carried neither a proof nor a payment", n)
		}
		if n < 4 {
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": g.jobID, "status": "in_progress"})
			return
		}
		_ = json.NewEncoder(w).Encode(completedMusicBody(g.jobID))
	}
	apiURL, rpcURL := g.start()
	c := newSolMusicClient(t, apiURL, rpcURL)

	if _, err := c.Generate(context.Background(), "old gateway", nil); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	polls := g.pollRequests()
	if len(polls) != 4 {
		t.Fatalf("polls = %d, want 4 (1 refused proof + 3 paid)", len(polls))
	}
	if polls[0].Header.Get(pollHeaderSignature) == "" {
		t.Errorf("first poll must try the ownership proof")
	}
	for _, p := range polls[1:] {
		if p.Header.Get(pollHeaderSignature) != "" {
			t.Errorf("after the refusal, no further proof may be sent")
		}
		assertFreshPollPayment(t, g, p.Header.Get("PAYMENT-SIGNATURE"))
	}
	if n := strings.Count(logs.String(), "falling back to pay-per-poll"); n != 1 {
		t.Errorf("fallback logged %d times, want once: %q", n, logs.String())
	}
	if s := c.GetSpending(); s.TotalUSD != 0.001 || s.Calls != 1 {
		t.Errorf("spending = %+v, want $0.001 over 1 call (polls never charge)", s)
	}
}

// 503/504 on a proof poll mean "not yet": keep polling.
func TestMusicSolanaTransientPollStatusesAreRetried(t *testing.T) {
	g := newSolGateway(t, "/api/v1/audio/generations", "aud_flaky")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		switch n {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			w.WriteHeader(http.StatusGatewayTimeout)
		default:
			_ = json.NewEncoder(w).Encode(completedMusicBody(g.jobID))
		}
	}
	apiURL, rpcURL := g.start()
	c := newSolMusicClient(t, apiURL, rpcURL)

	if _, err := c.Generate(context.Background(), "flaky", nil); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if n := len(g.pollRequests()); n != 3 {
		t.Errorf("polls = %d, want 3", n)
	}
}

// A paid job must never carry the wallet's proof (or a payment) to another
// origin; the error must still say the submit was paid.
func TestMusicSolanaOffOriginPollURLRefused(t *testing.T) {
	g := newSolGateway(t, "/api/v1/audio/generations", "aud_evil")
	g.pollPath = "https://attacker.example/api/v1/audio/generations/aud_evil"
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		t.Errorf("no poll may be sent")
	}
	apiURL, rpcURL := g.start()
	c := newSolMusicClient(t, apiURL, rpcURL)

	_, err := c.Generate(context.Background(), "x", nil)
	if err == nil || !strings.Contains(err.Error(), "was paid") {
		t.Fatalf("want a paid-but-unusable-poll_url error, got %v", err)
	}
	if got := c.GetSpending().TotalUSD; got != 0.001 {
		t.Errorf("spending = %f, want 0.001", got)
	}
}

// A caller giving up mid-poll gets ctx's error, and is told the job was paid.
func TestMusicSolanaCancelSaysPaid(t *testing.T) {
	g := newSolGateway(t, "/api/v1/audio/generations", "aud_slow")
	ctx, cancel := context.WithCancel(context.Background())
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		cancel()
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": g.jobID, "status": "in_progress"})
	}
	apiURL, rpcURL := g.start()
	c := newSolMusicClient(t, apiURL, rpcURL)

	_, err := c.Generate(ctx, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "do not submit again") || !strings.Contains(err.Error(), "aud_slow") {
		t.Fatalf("want a paid, resumable cancellation error, got %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("ctx should be cancelled")
	}
}

// Defect 2: a slow Solana image job was polled by re-signing a payment on
// every poll. It is now polled with the ownership proof (kind "image").
func TestImageSolanaPollsWithOwnershipProofNotPayment(t *testing.T) {
	g := newSolGateway(t, "/api/v1/images/generations", "img_job1")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		if n < 3 {
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": g.jobID, "status": "in_progress"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": g.jobID, "status": "completed", "created": 1760000000,
			"data": []map[string]any{{"url": "https://cdn.example.com/img.png"}},
		})
	}
	apiURL, rpcURL := g.start()
	c := newSolImageClient(t, apiURL, rpcURL)

	resp, err := c.Generate(context.Background(), "slow", &ImageGenerateOptions{Model: "openai/gpt-image-2"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].URL != "https://cdn.example.com/img.png" {
		t.Fatalf("unexpected data: %+v", resp.Data)
	}
	polls := g.pollRequests()
	if len(polls) != 3 {
		t.Fatalf("polls = %d, want 3", len(polls))
	}
	for _, p := range polls {
		if got, want := p.URL.EscapedPath(), "/api/v1/images/generations/img_job1"; got != want {
			t.Errorf("poll path = %q, want %q", got, want)
		}
		assertOwnershipProof(t, p, "image", "img_job1", c.GetWalletAddress())
	}
	if s := c.GetSpending(); s.TotalUSD != 0.001 || s.Calls != 1 {
		t.Errorf("spending = %+v, want $0.001 over 1 call", s)
	}
}

// A plain 402 to the proof (an older gateway, or a batch-billed image) falls
// back to pay-per-poll for that job, logged once.
func TestImageSolanaPaymentChallengeFallsBack(t *testing.T) {
	logs := captureLog(t)
	g := newSolGateway(t, "/api/v1/images/generations", "img_batch")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		if r.Header.Get(pollHeaderSignature) != "" {
			w.Header().Set("payment-required", g.prHeader)
			w.WriteHeader(http.StatusPaymentRequired)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "Payment Required"})
			return
		}
		if r.Header.Get("PAYMENT-SIGNATURE") == "" {
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		if n < 3 {
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": g.jobID, "status": "in_progress"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": g.jobID, "status": "completed",
			"data": []map[string]any{{"url": "https://cdn.example.com/b.png"}},
		})
	}
	apiURL, rpcURL := g.start()
	c := newSolImageClient(t, apiURL, rpcURL)

	resp, err := c.Generate(context.Background(), "batch", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Data[0].URL != "https://cdn.example.com/b.png" {
		t.Fatalf("unexpected data: %+v", resp.Data)
	}
	if n := len(g.pollRequests()); n != 3 {
		t.Errorf("polls = %d, want 3 (1 proof + 2 paid)", n)
	}
	if n := strings.Count(logs.String(), "falling back to pay-per-poll"); n != 1 {
		t.Errorf("fallback logged %d times, want once: %q", n, logs.String())
	}
}

func TestPollJobID(t *testing.T) {
	cases := []struct{ url, fallback, want string }{
		{"https://sol.blockrun.ai/api/v1/audio/generations/aud_1", "x", "aud_1"},
		{"https://sol.blockrun.ai/api/v1/audio/generations/aud%20job%2B1/", "x", "aud job+1"},
		{"https://sol.blockrun.ai/api/v1/images/generations/img_1?sig=abc", "x", "img_1"},
		{"https://sol.blockrun.ai/", "fallback", "fallback"},
		{"https://sol.blockrun.ai/api/v1/x/%ZZ", "fallback", "fallback"},
	}
	for _, c := range cases {
		if got := pollJobID(c.url, c.fallback); got != c.want {
			t.Errorf("pollJobID(%q) = %q, want %q", c.url, got, c.want)
		}
	}
	if got := pollAuthMessage("audio", "aud_1", 1760000000); got != "blockrun-poll:v1:audio:aud_1:1760000000" {
		t.Errorf("pollAuthMessage = %q", got)
	}
}

// --- Review findings (each goes red without its fix) ---

// An image paid at the POST whose 202 cannot be followed (off-origin or
// missing poll_url) used to return before the cost was booked, with no word
// that it was paid — so GetSpending read $0 and a retry paid twice.
func TestImageSolanaUnfollowable202IsBookedAndSaysPaid(t *testing.T) {
	for name, poll := range map[string]string{
		"off-origin": "https://attacker.example/api/v1/images/generations/img_x",
		"missing":    "-",
	} {
		t.Run(name, func(t *testing.T) {
			g := newSolGateway(t, "/api/v1/images/generations", "img_x")
			g.pollPath = poll
			if poll == "-" {
				g.jobID = "" // 202 with no id
			}
			g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) { t.Errorf("no poll may be sent") }
			apiURL, rpcURL := g.start()
			c := newSolImageClient(t, apiURL, rpcURL)

			_, err := c.Generate(context.Background(), "x", nil)
			if err == nil || !strings.Contains(err.Error(), "settled at submit") {
				t.Fatalf("want a paid error, got %v", err)
			}
			if s := c.GetSpending(); s.TotalUSD != 0.001 || s.Calls != 1 {
				t.Errorf("spending = %+v, want $0.001 over 1 call", s)
			}
		})
	}
}

// A 500, 502 or 429 on a free proof poll used to end a paid job on the spot.
func TestSolanaProofPollRetriesServerErrors(t *testing.T) {
	g := newSolGateway(t, "/api/v1/audio/generations", "aud_500")
	codes := []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusTooManyRequests}
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		if n <= len(codes) {
			w.WriteHeader(codes[n-1])
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "Internal server error"})
			return
		}
		_ = json.NewEncoder(w).Encode(completedMusicBody(g.jobID))
	}
	apiURL, rpcURL := g.start()
	c := newSolMusicClient(t, apiURL, rpcURL)
	if _, err := c.Generate(context.Background(), "x", nil); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if n := len(g.pollRequests()); n != 4 {
		t.Errorf("polls = %d, want 4", n)
	}
}

// ...but a persistent one is bounded, and the error says the job was paid.
func TestSolanaProofPollPersistentServerErrorIsBounded(t *testing.T) {
	g := newSolGateway(t, "/api/v1/audio/generations", "aud_dead")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		w.WriteHeader(http.StatusInternalServerError)
	}
	apiURL, rpcURL := g.start()
	c := newSolMusicClient(t, apiURL, rpcURL)
	_, err := c.Generate(context.Background(), "x", nil)
	if err == nil || !strings.Contains(err.Error(), "do not submit again") {
		t.Fatalf("want a bounded, paid error, got %v", err)
	}
	if n := len(g.pollRequests()); n != solanaMaxPollTransportErrors {
		t.Errorf("polls = %d, want %d", n, solanaMaxPollTransportErrors)
	}
}

// "Retry-After: 0" used to skip the wait and spin the loop for the whole budget.
func TestSolanaRetryAfterZeroDoesNotSpin(t *testing.T) {
	g := newSolGateway(t, "/api/v1/audio/generations", "aud_spin")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": g.jobID, "status": "in_progress"})
	}
	apiURL, rpcURL := g.start()
	c := newSolMusicClient(t, apiURL, rpcURL)
	c.pollInterval = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, _ = c.Generate(ctx, "x", nil)
	if n := len(g.pollRequests()); n > 20 {
		t.Errorf("polls = %d in 300ms at a 20ms interval: Retry-After: 0 spun the loop", n)
	}
}

// A paid track whose body cannot be decoded, or is cut off, must say it was
// paid; a retry would pay again.
func TestMusicSolanaPaidButUnreadableSaysPaid(t *testing.T) {
	for name, write := range map[string]func(w http.ResponseWriter){
		"bad json": func(w http.ResponseWriter) { _, _ = w.Write([]byte("not json")) },
		"cut off": func(w http.ResponseWriter) {
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[`))
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("no hijacker")
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
		},
	} {
		t.Run(name, func(t *testing.T) {
			g := newSolGateway(t, "/api/v1/audio/generations", "unused")
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("PAYMENT-SIGNATURE") == "" {
					w.Header().Set("payment-required", g.prHeader)
					w.WriteHeader(http.StatusPaymentRequired)
					return
				}
				write(w)
			}))
			t.Cleanup(srv.Close)
			_, rpcURL := g.start()
			c := newSolMusicClient(t, srv.URL+"/api", rpcURL)
			_, err := c.Generate(context.Background(), "x", nil)
			if err == nil || !strings.Contains(err.Error(), "settled at submit") {
				t.Fatalf("want a paid error, got %v", err)
			}
			if got := c.GetSpending().TotalUSD; got != 0.001 {
				t.Errorf("spending = %f, want 0.001", got)
			}
		})
	}
}

// Image: a refused proof (404) falls back to pay-per-poll; a failed job says
// it was paid; the fallback signs fresh each poll and books nothing more.
func TestImageSolanaProofRefusedFallsBackAndFailedSaysPaid(t *testing.T) {
	logs := captureLog(t)
	g := newSolGateway(t, "/api/v1/images/generations", "img_old")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		if r.Header.Get(pollHeaderSignature) != "" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "Job not found"})
			return
		}
		if n < 3 {
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": g.jobID, "status": "in_progress"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": g.jobID, "status": "failed", "error": "boom"})
	}
	apiURL, rpcURL := g.start()
	c := newSolImageClient(t, apiURL, rpcURL)
	_, err := c.Generate(context.Background(), "x", nil)
	if err == nil || !strings.Contains(err.Error(), "settled at submit") {
		t.Fatalf("want a paid failure, got %v", err)
	}
	polls := g.pollRequests()
	if len(polls) != 3 {
		t.Fatalf("polls = %d, want 3 (1 refused proof + 2 paid)", len(polls))
	}
	for _, p := range polls[1:] {
		assertFreshPollPayment(t, g, p.Header.Get("PAYMENT-SIGNATURE"))
	}
	if n := strings.Count(logs.String(), "falling back to pay-per-poll"); n != 1 {
		t.Errorf("fallback logged %d times, want once", n)
	}
	if s := c.GetSpending(); s.TotalUSD != 0.001 || s.Calls != 1 {
		t.Errorf("spending = %+v, want $0.001 over 1 call", s)
	}
}

// 500s spread across a long job, each followed by a 503 "not yet", must not
// add up to the consecutive-failure cap and abandon a paid job.
func TestSolanaInterleavedServerErrorsDoNotAccumulate(t *testing.T) {
	g := newSolGateway(t, "/api/v1/audio/generations", "aud_mixed")
	g.pollAnswer = func(n int, r *http.Request, w http.ResponseWriter) {
		if n <= 2*solanaMaxPollTransportErrors {
			if n%2 == 1 {
				w.WriteHeader(http.StatusInternalServerError)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}
		_ = json.NewEncoder(w).Encode(completedMusicBody(g.jobID))
	}
	apiURL, rpcURL := g.start()
	c := newSolMusicClient(t, apiURL, rpcURL)
	if _, err := c.Generate(context.Background(), "x", nil); err != nil {
		t.Fatalf("Generate: %v", err)
	}
}

// A free (unpaid) 200 on sol that cannot be decoded must not claim a payment.
func TestMusicSolanaFreeAnswerDoesNotClaimPayment(t *testing.T) {
	g := newSolGateway(t, "/api/v1/audio/generations", "unused")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(srv.Close)
	_, rpcURL := g.start()
	c := newSolMusicClient(t, srv.URL+"/api", rpcURL)
	_, err := c.Generate(context.Background(), "x", nil)
	if err == nil {
		t.Fatal("want a decode error")
	}
	if strings.Contains(err.Error(), "settled") {
		t.Errorf("an unpaid answer must not claim a settlement: %v", err)
	}
	if got := c.GetSpending().TotalUSD; got != 0 {
		t.Errorf("spending = %f, want 0", got)
	}
}
