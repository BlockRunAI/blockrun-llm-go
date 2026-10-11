package blockrun

import (
	"crypto/ed25519"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mr-tron/base58"
)

// Solana poll-ownership proof (sol.blockrun.ai src/lib/poll-ownership.ts).
//
// sol.blockrun.ai settles image and music payments at the POST: a signed Solana
// transaction is pinned to a recent blockhash (~60-90s of life), far too short
// to wait out a slow render. A slow job answers 202 { id, poll_url } and the
// poll only delivers — to the wallet that paid. That wallet used to prove
// itself by signing a fresh payment on every poll. The gateway also accepts a
// detached ed25519 signature, by the paying wallet, over a short text naming
// the job:
//
//	blockrun-poll:v1:<kind>:<job id>:<unix seconds>
//
// sent as x-poll-wallet / x-poll-timestamp / x-poll-signature (base58, except
// the decimal timestamp). It is not a transaction: there is nothing a
// facilitator can settle, so a proof poll can never charge. The gateway
// accepts a timestamp within ±300s of its clock, so a proof is signed fresh for
// every poll. Every refusal (wrong wallet, bad signature, stale timestamp, a
// job with no bound payer) answers 404 "Job not found", byte-identical to a job
// that does not exist.

const (
	pollHeaderWallet    = "x-poll-wallet"
	pollHeaderTimestamp = "x-poll-timestamp"
	pollHeaderSignature = "x-poll-signature"

	// pollKindImage and pollKindAudio are the <kind> the gateway verifies the
	// proof against. Music jobs are "audio" (the route is /v1/audio/...).
	pollKindImage = "image"
	pollKindAudio = "audio"
)

// pollAuthMessage is the exact UTF-8 text the gateway verifies.
func pollAuthMessage(kind, jobID string, timestampS int64) string {
	return "blockrun-poll:v1:" + kind + ":" + jobID + ":" + strconv.FormatInt(timestampS, 10)
}

// pollAuthHeaders signs the ownership proof for one poll of jobID at now. priv
// is the paying wallet's key, so x-poll-wallet is always the submit's payer.
func pollAuthHeaders(priv ed25519.PrivateKey, kind, jobID string, now time.Time) map[string]string {
	ts := now.Unix()
	sig := ed25519.Sign(priv, []byte(pollAuthMessage(kind, jobID, ts)))
	return map[string]string{
		pollHeaderWallet:    base58.Encode(priv.Public().(ed25519.PublicKey)),
		pollHeaderTimestamp: strconv.FormatInt(ts, 10),
		pollHeaderSignature: base58.Encode(sig),
	}
}

// pollJobID is the job id the gateway verifies the signature against: the
// poll_url's last path segment, percent-decoded (the route reads it with
// decodeURIComponent). The submit's `id` is the same value and is the fallback
// when the poll URL has no usable segment. The query string is not part of it.
func pollJobID(pollURL, fallback string) string {
	u, err := url.Parse(pollURL)
	if err != nil {
		return fallback
	}
	p := strings.TrimRight(u.EscapedPath(), "/")
	i := strings.LastIndex(p, "/")
	if i < 0 || i == len(p)-1 {
		return fallback
	}
	seg, err := url.PathUnescape(p[i+1:])
	if err != nil || seg == "" {
		return fallback
	}
	return seg
}
