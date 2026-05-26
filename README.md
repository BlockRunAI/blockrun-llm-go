# BlockRun LLM Go SDK

> **blockrun-llm-go** is a Go SDK for accessing 40+ large language models and AI services with automatic pay-per-request USDC micropayments via the x402 protocol on Base chain. No API keys required — your wallet signature is your authentication.
>
> 🆓 **Includes 9 fully-free NVIDIA-hosted models** — DeepSeek V4 Pro/Flash (1M context), Nemotron Nano Omni (vision), Qwen3, Llama 4, GLM-4.7, Mistral. Zero USDC, no rate-limit gimmicks. Use `blockrun.RoutingFree` or call any `nvidia/*` model directly.

[![Go Reference](https://pkg.go.dev/badge/github.com/blockrunai/blockrun-llm-go.svg)](https://pkg.go.dev/github.com/blockrunai/blockrun-llm-go)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

## Installation

```bash
go get github.com/BlockRunAI/blockrun-llm-go
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    blockrun "github.com/BlockRunAI/blockrun-llm-go"
)

func main() {
    ctx := context.Background()

    client, err := blockrun.NewLLMClient("")  // uses BASE_CHAIN_WALLET_KEY env var
    if err != nil {
        log.Fatal(err)
    }

    response, err := client.Chat(ctx, "openai/gpt-4o", "What is 2+2?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(response)
}
```

### Try It Free (No USDC Required)

Want to kick the tires before funding a wallet? Route to BlockRun's free NVIDIA tier:

```go
// Option 1: call a free model directly
reply, _ := client.Chat(ctx, "nvidia/qwen3-next-80b-a3b-thinking", "Explain x402 in 1 sentence")

// Option 2: let the smart router pick the best free model per request
result, _ := client.SmartChat(ctx, "What is 2+2?", &blockrun.SmartChatOptions{
    RoutingProfile: blockrun.RoutingFree,
})
fmt.Println(result.Model)    // e.g. "nvidia/deepseek-v4-flash"
fmt.Println(result.Response) // "4"
```

**Available free models** (input + output both $0, all NVIDIA-hosted, last refreshed 2026-04-28):

| Model ID | Context | Best For |
|----------|---------|----------|
| `nvidia/deepseek-v4-flash` | 1M | DeepSeek V4 Flash — 284B / 13B active MoE, ~5× faster than V4 Pro. Best free chat / summarization / light reasoning |
| `nvidia/nemotron-3-nano-omni-30b-a3b-reasoning` | 256K | Only vision-capable free model — text + images + video (≤2 min) + audio (≤1 hr) |
| `nvidia/qwen3-next-80b-a3b-thinking` | 131K | 116 tok/s reasoning with thinking mode |
| `nvidia/mistral-small-4-119b` | 131K | 114 tok/s — fastest free chat |
| `nvidia/llama-4-maverick` | 131K | Meta Llama 4 Maverick MoE |
| `nvidia/qwen3-coder-480b` | 131K | Coding-optimised 480B MoE |
| `nvidia/gpt-oss-120b` | 128K | OpenAI open-weight 120B — 123 tok/s. Hidden from `/v1/models` for privacy but direct calls by full ID still work |
| `nvidia/gpt-oss-20b` | 128K | OpenAI open-weight 20B — 155 tok/s. Hidden from `/v1/models` but direct calls still work |

> Need V4-Pro-class reasoning? Use the paid `deepseek/deepseek-v4-pro` ($0.50/$1.00 with the 75% promo through 2026-05-31) — `nvidia/deepseek-v4-pro` is currently hidden because NVIDIA's NIM deployment is hung; backend MODEL_REDIRECTS forwards calls to V4 Flash.

> Note: `nvidia/gpt-oss-120b` and `nvidia/gpt-oss-20b` were retired 2026-04-28 — NVIDIA's free build.nvidia.com tier reserves the right to use prompts/outputs for service improvement, which conflicts with our data-privacy policy.

## How It Works

1. You send a request to BlockRun's API
2. The API returns a 402 Payment Required with the price
3. The SDK signs a USDC payment on Base locally (EIP-712 typed data)
4. The request is retried with the payment proof
5. You receive the response

**Your private key never leaves your machine** — only signatures are transmitted.

## Features

| Feature | Description |
|---------|-------------|
| **Chat & Completion** | OpenAI-compatible chat with 40+ models |
| **Anthropic Client** | Native Anthropic Messages API with automatic x402 payments |
| **Smart Routing** | Auto-selects the best model for your prompt |
| **Streaming** | SSE streaming for real-time responses |
| **Tool Calling** | OpenAI-compatible function/tool calling |
| **X/Twitter Data** | 15 endpoints for users, tweets, search, analytics |
| **Web Search** | Search web, X/Twitter, and news |
| **Prediction Markets** | Polymarket, Kalshi data access |
| **Image Generation** | DALL-E 3, GPT Image 1/2, Nano Banana, Flux, CogView-4, Grok Imagine |
| **Music Generation** | Full-length (~3 min) tracks via MiniMax Music 2.5+ |
| **Video Generation** | Grok Imagine Video, ByteDance Seedance (1.5-pro / 2.0-fast / 2.0) with face/character consistency |
| **Virtual Portraits** | Enroll AI-generated characters as reusable Seedance face assets |
| **RealFace** | Enroll a real person's likeness (on-phone liveness, no KYC) as a Seedance face asset |
| **Voice Calls** | AI-powered outbound phone calls (Bland.ai upstream) |
| **Phone Lookup + Numbers** | Twilio carrier/fraud lookup + provisioned numbers for caller-ID |
| **Surf (asksurf.ai)** | ~83 endpoints: exchange data, on-chain SQL, prediction markets, wallet/social analytics |
| **Response Caching** | Local cache with per-endpoint TTL |
| **Cost Tracking** | Session spending + persistent JSONL log |
| **Balance Checking** | Query USDC balance via Base chain RPC |
| **Agent Wallet Setup** | Auto-create wallets for autonomous agents |

## Anthropic Client

Use the native Anthropic Messages API format with BlockRun's x402 payment gateway.
Works with Claude models and any other BlockRun model (OpenAI, Google, etc.) via Anthropic message format.

```go
client, err := blockrun.NewAnthropicClient("")  // uses BLOCKRUN_WALLET_KEY env var
if err != nil {
    log.Fatal(err)
}

resp, err := client.Messages.Create(ctx, blockrun.AnthropicCreateParams{
    Model:     "claude-sonnet-4-6",
    MaxTokens: 1024,
    Messages: []blockrun.AnthropicMessage{
        {Role: "user", Content: "Hello!"},
    },
})
if err != nil {
    log.Fatal(err)
}
fmt.Println(resp.Text())  // convenience method for text responses
fmt.Println(resp.StopReason)  // "end_turn", "max_tokens", "tool_use", "stop_sequence"
fmt.Printf("Tokens: %d in / %d out\n", resp.Usage.InputTokens, resp.Usage.OutputTokens)
```

With system prompt and tools:

```go
temp := 0.7
resp, err := client.Messages.Create(ctx, blockrun.AnthropicCreateParams{
    Model:     "claude-sonnet-4-6",
    MaxTokens: 2048,
    System:    "You are a helpful assistant.",
    Temperature: &temp,
    Tools: []blockrun.AnthropicTool{
        {
            Name:        "get_weather",
            Description: "Get current weather for a location",
            InputSchema: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "location": map[string]any{"type": "string"},
                },
                "required": []string{"location"},
            },
        },
    },
    Messages: []blockrun.AnthropicMessage{
        {Role: "user", Content: "What's the weather in Tokyo?"},
    },
})
```

Multi-turn conversation with content blocks:

```go
messages := []blockrun.AnthropicMessage{
    {Role: "user", Content: "Analyze this image"},
    {
        Role: "user",
        Content: []blockrun.AnthropicContentBlock{
            {
                Type: "image",
                Source: &blockrun.AnthropicImageSource{
                    Type:      "base64",
                    MediaType: "image/png",
                    Data:      "<base64-encoded-image>",
                },
            },
        },
    },
}
```

## Chat

```go
ctx := context.Background()

// Simple chat
response, err := client.Chat(ctx, "openai/gpt-4o", "Explain quantum computing")

// With system prompt
response, err := client.ChatWithSystem(ctx, "anthropic/claude-sonnet-4.6", "Tell me a joke", "You are a comedian.")

// Full completion with options
result, err := client.ChatCompletion(ctx, "openai/gpt-4o", messages, &blockrun.ChatCompletionOptions{
    MaxTokens:   1024,
    Temperature: 0.7,
})
fmt.Println(result.Choices[0].Message.Content)
```

## Smart Routing (ClawRouter)

Auto-selects the best model based on prompt complexity analysis — all routing is local, <1ms.

```go
// Auto profile (default) — balances cost and quality
resp, err := client.SmartChat(ctx, "Write a binary search in Go", nil)
fmt.Printf("Used: %s (tier: %s)\n", resp.Model, resp.Routing.Tier)

// Economy profile — cheapest models
resp, err := client.SmartChat(ctx, "What is 2+2?", &blockrun.SmartChatOptions{
    RoutingProfile: blockrun.RoutingEco,
})

// Premium profile — top-tier models
resp, err := client.SmartChat(ctx, "Prove P != NP", &blockrun.SmartChatOptions{
    RoutingProfile: blockrun.RoutingPremium,
})
```

| Profile | Simple | Medium | Complex | Reasoning |
|---------|--------|--------|---------|-----------|
| **free** | nvidia/gpt-oss-120b | nvidia/deepseek-v4-flash | nvidia/qwen3-next-80b-a3b-thinking | nvidia/qwen3-next-80b-a3b-thinking |
| **eco** | moonshot/kimi-k2.6 | deepseek/deepseek-chat | google/gemini-2.5-pro | deepseek/deepseek-reasoner |
| **auto** | moonshot/kimi-k2.6 | google/gemini-2.5-flash | google/gemini-3.1-pro | deepseek/deepseek-reasoner |
| **premium** | google/gemini-2.5-flash | openai/gpt-5.5 | anthropic/claude-opus-4.5 | openai/o3 |

> DeepSeek V4 family launched 2026-04-24. The legacy `deepseek/deepseek-chat`
> and `deepseek/deepseek-reasoner` IDs (used by **eco** Medium / Reasoning
> above) are now V4 Flash non-thinking / thinking modes — $0.20 in / $0.40 out
> per 1M, 1M context. The new paid flagship `deepseek/deepseek-v4-pro`
> ($0.50/$1.00 with 75% promo through 2026-05-31) is available via direct
> chat calls; SmartChat keeps `deepseek-reasoner` as the eco/auto reasoning
> primary because V4 Flash thinking is cheaper.
>
> NVIDIA free tier refreshed 2026-04-28: added `nvidia/deepseek-v4-flash`
> (1M context) and `nvidia/nemotron-3-nano-omni` (vision). `nvidia/gpt-oss-120b`
> and `nvidia/gpt-oss-20b` were briefly delisted then **re-enabled
> 2026-04-30** with `available: true` + `hidden: true` — they no longer
> appear in `/v1/models` (so SmartChat won't auto-pick them) but direct
> calls by full ID still return HTTP 200. Retired IDs (`nvidia/nemotron-*`,
> `nvidia/mistral-large-3-675b`, `nvidia/devstral-2-123b`,
> `nvidia/qwen3.5-397b-a17b`, paid `nvidia/kimi-k2.5`) resolve via backend
> redirects. `nvidia/deepseek-v4-pro`, `nvidia/deepseek-v3.2`, and
> `nvidia/glm-4.7` are temporarily hidden (NVIDIA NIM hung) and
> auto-redirect to `nvidia/deepseek-v4-flash` / `nvidia/qwen3-coder-480b`;
> the Free routing primaries above point at visible IDs so `result.Model`
> reflects the model that actually answered.

## Streaming

```go
stream, err := client.ChatCompletionStream(ctx, "openai/gpt-4o", []blockrun.ChatMessage{
    {Role: "user", Content: "Write a poem about Go"},
}, nil)
if err != nil {
    log.Fatal(err)
}
defer stream.Close()

for {
    chunk, err := stream.Next()
    if err != nil {
        log.Fatal(err)
    }
    if chunk == nil {
        break // stream complete
    }
    fmt.Print(chunk.Choices[0].Delta.Content)
}
```

## Tool / Function Calling

```go
result, err := client.ChatCompletion(ctx, "openai/gpt-4o", messages, &blockrun.ChatCompletionOptions{
    Tools: []blockrun.Tool{
        {
            Type: "function",
            Function: blockrun.ToolFunction{
                Name:        "get_weather",
                Description: "Get current weather for a location",
                Parameters: map[string]any{
                    "type": "object",
                    "properties": map[string]any{
                        "location": map[string]any{"type": "string"},
                    },
                    "required": []string{"location"},
                },
            },
        },
    },
    ToolChoice: "auto",
})

// Check if model wants to call a tool
if len(result.Choices[0].Message.ToolCalls) > 0 {
    call := result.Choices[0].Message.ToolCalls[0]
    fmt.Printf("Tool: %s(%s)\n", call.Function.Name, call.Function.Arguments)
}
```

## X/Twitter Data

15 endpoints for real-time X/Twitter intelligence. All powered by AttentionVC.

```go
// Look up users
users, err := client.XUserLookup(ctx, []string{"elonmusk", "vaborsh"})

// Get followers
followers, err := client.XFollowers(ctx, "elonmusk", "")

// Search tweets
results, err := client.XSearch(ctx, "bitcoin", "Latest", "")

// Trending topics
trending, err := client.XTrending(ctx)

// Author analytics
analytics, err := client.XAuthorAnalytics(ctx, "vaborsh")

// Compare authors
comparison, err := client.XCompareAuthors(ctx, "elonmusk", "vaborsh")
```

**All X/Twitter Methods:**

| Method | Endpoint | Price |
|--------|----------|-------|
| `XUserLookup` | User profiles by username | $0.002/user |
| `XFollowers` | Follower list | $0.05/page |
| `XFollowings` | Following list | $0.05/page |
| `XUserInfo` | Detailed profile | $0.002 |
| `XVerifiedFollowers` | Verified followers | $0.048/page |
| `XUserTweets` | User's tweets | $0.032/page |
| `XUserMentions` | Mentions of user | $0.032/page |
| `XTweetLookup` | Tweets by ID | $0.16/batch |
| `XTweetReplies` | Replies to tweet | $0.032/page |
| `XTweetThread` | Full thread | $0.032/page |
| `XSearch` | Search tweets | $0.032/page |
| `XTrending` | Trending topics | $0.002 |
| `XArticlesRising` | Viral articles | $0.05 |
| `XAuthorAnalytics` | Author metrics | $0.02 |
| `XCompareAuthors` | Compare authors | $0.05 |

## Web Search

```go
// Simple search
result, err := client.Search(ctx, "latest AI news", nil)
fmt.Println(result.Summary)
fmt.Println(result.Citations)

// With options
result, err := client.Search(ctx, "Go 1.23 features", &blockrun.SearchOptions{
    Sources:    []string{"web", "news"},
    MaxResults: 5,
    FromDate:   "2025-01-01",
})
```

## Market Data (Pyth)

Realtime quotes and OHLC history for crypto, FX, commodities and 12 global
equity markets. Crypto / FX / commodity are free across price, history and
list; equities (`stocks/{market}` and the `usstock` alias) charge $0.001
per price or history call. The client handles x402 transparently on both
paths — `NewLLMClient` still requires a wallet for the paid routes.

```go
// Free — BTC spot price
btc, err := client.Price(ctx, blockrun.CategoryCrypto, "BTC-USD", nil)
fmt.Println(btc.Price)

// Paid — US equity quote (market is required for CategoryStocks)
aapl, err := client.Price(ctx, blockrun.CategoryStocks, "AAPL",
    &blockrun.PriceOptions{Market: "us"})

// Historical bars (free for crypto, paid for stocks)
bars, err := client.History(ctx, blockrun.CategoryStocks, "AAPL",
    &blockrun.HistoryOptions{
        PriceOptions: blockrun.PriceOptions{Market: "us"},
        Resolution:   "D",
        From:         1700000000,
        To:           1710000000,
    })

// Discovery — always free
symbols, err := client.ListSymbols(ctx, blockrun.CategoryCrypto,
    &blockrun.ListOptions{Query: "sol", Limit: 20})
```

Supported markets for `CategoryStocks`: `us, hk, jp, kr, gb, de, fr, nl,
ie, lu, cn, ca`.

## Prediction Markets

Access Polymarket, Kalshi, and more via Predexon.

```go
// GET endpoints ($0.001/request)
events, err := client.PM(ctx, "polymarket/events", nil)
markets, err := client.PM(ctx, "polymarket/search", map[string]string{"q": "bitcoin"})

// POST query endpoints ($0.005/request)
result, err := client.PMQuery(ctx, "polymarket/query", map[string]any{
    "filter": "active",
    "limit":  10,
})
```

## Image Generation

Supported models: `openai/dall-e-3`, `openai/gpt-image-1`, `openai/gpt-image-2` (ChatGPT Images 2.0 — reasoning-driven, $0.06–0.12/image), `google/nano-banana`, `google/nano-banana-pro`, `zai/cogview-4`, `black-forest/flux-1.1-pro`, `xai/grok-imagine-image` ($0.02/image), `xai/grok-imagine-image-pro` ($0.07/image). Editing and multi-image fusion via `client.Edit()` are supported by `openai/gpt-image-1`, `openai/gpt-image-2`, `google/nano-banana`, and `google/nano-banana-pro`.

```go
imageClient, err := blockrun.NewImageClient("")

result, err := imageClient.Generate(ctx, "A cat astronaut on Mars", &blockrun.ImageGenerateOptions{
    Model: "openai/dall-e-3",
    Size:  "1024x1024",
})
fmt.Println(result.Data[0].URL)       // permanent blockrun-hosted URL
fmt.Println(result.Data[0].SourceURL) // original upstream URL
fmt.Println(result.Data[0].BackedUp)  // true when gateway mirrored to GCS
```

### Editing & fusion

`Edit()` takes one source image for a standard edit, or several to fuse them (up to the provider's limit, typically 4 — Gemini tops out around 3 anchors). Each image must be a base64 data URI (`data:image/...`). The default edit model is `openai/gpt-image-2`.

```go
// Single-image edit
result, err := imageClient.Edit(ctx, "make the sky purple",
    []string{"data:image/png;base64,..."}, nil)

// Multi-image fusion — e.g. drop a brand logo onto a product photo
result, err = imageClient.Edit(ctx, "place the logo on the shirt",
    []string{photoDataURI, logoDataURI},
    &blockrun.ImageEditOptions{Model: "google/nano-banana"})
```

A `mask` (via `ImageEditOptions.Mask`) is supported by the OpenAI models for inpainting, but cannot be combined with multiple source images.

## Music Generation

Generate full-length (~3 minute) tracks via MiniMax Music 2.5+ ($0.1575/track). Generated URLs expire in ~24h — download immediately if you need to keep the track.

```go
musicClient, err := blockrun.NewMusicClient("")

// Instrumental track (default)
result, err := musicClient.Generate(ctx, "upbeat synthwave with neon pads", nil)
fmt.Println(result.Data[0].URL)             // CDN URL — download within ~24h
fmt.Println(result.Data[0].DurationSeconds)

// Vocal track with custom lyrics
instrumental := false
result, err = musicClient.Generate(ctx, "upbeat pop song", &blockrun.MusicGenerateOptions{
    Instrumental: &instrumental,
    Lyrics:       "Hello world, this is my song...",
})
```

The default timeout is 210s since generation takes 1-3 minutes.

## Video Generation

Supported models:

| Model | Price |
|-------|-------|
| `xai/grok-imagine-video` | $0.05/sec (8s default → $0.42/clip) |
| `bytedance/seedance-1.5-pro` | $0.03/sec (5s default, up to 10s, 720p) |
| `bytedance/seedance-2.0-fast` | $0.15/sec (~60-80s gen, sweet-spot price/quality) |
| `bytedance/seedance-2.0` | $0.30/sec (720p Pro) |

```go
videoClient, err := blockrun.NewVideoClient("")

result, err := videoClient.Generate(ctx, "a red apple slowly spinning on a wooden table", nil)
fmt.Println(result.Data[0].URL)             // permanent MP4 URL
fmt.Println(result.Data[0].DurationSeconds) // 8 for xAI default, 5 for Seedance

// Image-to-video (Seedance — cheaper)
result, err = videoClient.Generate(ctx, "the subject turns and smiles", &blockrun.VideoGenerateOptions{
    Model:    "bytedance/seedance-1.5-pro",
    ImageURL: "https://example.com/portrait.jpg",
})

// Face/character consistency (Seedance 2.0 fast/pro) — reuse the same
// person or character across multiple videos via a ta_ asset id from
// PortraitClient or RealFaceClient (see below). Mutually exclusive with ImageURL.
genAudio := true
result, err = videoClient.Generate(ctx, "the spokesperson presents the product", &blockrun.VideoGenerateOptions{
    Model:           "bytedance/seedance-2.0",
    RealFaceAssetID: "ta_abcdef1234567890",
    Resolution:      "1080p",       // 360p / 480p / 720p / 1080p / 4K
    GenerateAudio:   &genAudio,     // *bool — nil defers to model default
})
```

The client blocks until the video is ready (30-120s typical; Seedance is hard-capped at 85s upstream) because the gateway handles async polling internally.

## Virtual Portraits

`PortraitClient` enrolls an AI-generated character image as a reusable face/character asset ($0.01 USDC, one-time, no KYC). The returned `ta_xxxxxxxx` asset id can be passed as `RealFaceAssetID` to `VideoClient.Generate` on Seedance 2.0 / 2.0-fast to keep the same character across multiple videos.

```go
portraitClient, err := blockrun.NewPortraitClient("")

portrait, err := portraitClient.Enroll(ctx, "My Spokesperson", "https://example.com/character.jpg")
fmt.Println(portrait.AssetID)            // ta_abcdef1234567890
fmt.Println(portrait.Settlement.TxHash)  // 0x9f3a…

// List the wallet's enrolled portraits (free)
list, err := portraitClient.ListPortraits(ctx, "") // "" = own wallet
for _, p := range list.Portraits {
    fmt.Println(p.AssetID, p.Name)
}
```

## RealFace

`RealFaceClient` enrolls a *real person's* likeness as a face asset ($0.01 USDC, one-time). Unlike a Virtual Portrait, it proves the enroller is the same person via a brief on-phone liveness check (nod + blink, ~1 minute) — **no KYC**. The flow is three steps:

```go
realfaceClient, err := blockrun.NewRealFaceClient("")

// 1. Start enrollment (free). Render init.H5Link as a QR for the person.
init, err := realfaceClient.Init(ctx, "Jane — Q3 spokesperson", "")
fmt.Println(init.H5Link)  // they scan this + do the liveness check

// 2. Wait until they finish the phone liveness check (polls status).
_, err = realfaceClient.WaitForActive(ctx, init.GroupID, nil)

// 3. Finalize ($0.01 USDC) with the person's face photo.
rf, err := realfaceClient.Enroll(ctx, "Jane — Q3 spokesperson", "https://example.com/jane.jpg", init.GroupID)
fmt.Println(rf.AssetID)            // ta_abcdef1234567890 — use as RealFaceAssetID on Seedance
fmt.Println(rf.Settlement.TxHash)

// List the wallet's enrolled RealFaces (free)
list, err := realfaceClient.ListRealFaces(ctx, "") // "" = own wallet
```

Failures don't charge: `Enroll` returns an `APIError` with status 425 (group not active — finish the phone check first), 422 (face didn't match the live capture), or 502 (upstream failure), and no payment is taken.

## Voice Calls

`VoiceClient` wraps `POST /v1/voice/call` (paid, $0.54/call) and `GET /v1/voice/call/{callId}` (free polling) — AI-powered outbound phone calls powered by Bland.ai. The agent dials the recipient and runs a real-time conversation based on your `Task` instructions. US + Canada destinations.

```go
voiceClient, err := blockrun.NewVoiceClient("")

// Initiate (paid $0.54)
result, err := voiceClient.Call(ctx, blockrun.CallOptions{
    To:          "+14155552671",
    Task:        "You are a friendly assistant calling to confirm a 3pm dentist appointment.",
    Voice:       blockrun.VoiceMaya, // nat / josh / maya / june / paige / derek / florian
    MaxDuration: 5,                  // minutes (1–30)
})
fmt.Println(result.CallID)

// Poll for transcript + recording (free)
status, err := voiceClient.GetCallStatus(ctx, result.CallID)
fmt.Println(status.Status, status.RecordingURL)
```

Bring your own caller-ID: set `From: "+14155552671"` (must be a BlockRun phone number you own; buy via `PhoneClient.BuyNumber` — see next section).

If `From` is empty, the backend auto-picks when your wallet owns exactly one active number; returns 403 `no_active_number` (zero owned) or 400 `ambiguous_from` (two or more).

## Phone Lookup + Number Provisioning

`PhoneClient` wraps `/v1/phone/*` for Twilio-backed phone-number lookup (carrier + fraud) and provisioning the caller-ID numbers required by `VoiceClient.Call`.

```go
phone, err := blockrun.NewPhoneClient("")

// Carrier + line-type ($0.01)
info, err := phone.Lookup(ctx, "+14155552671")
fmt.Println(info.Carrier)

// Carrier + SIM-swap / call-forwarding signals ($0.05)
fraud, err := phone.LookupFraud(ctx, "+14155552671")

// Provision a US number (30-day lease bound to your wallet, $5.00)
bought, err := phone.BuyNumber(ctx, blockrun.BuyNumberOptions{
    Country:  "US",
    AreaCode: "415", // optional 3-digit hint; falls back to any US number
})
fmt.Println(bought.PhoneNumber, bought.ExpiresAt)

// List + renew + release
owned, _ := phone.ListNumbers(ctx)
fmt.Printf("%d numbers active\n", owned.Count)

_, _ = phone.RenewNumber(ctx, bought.PhoneNumber)   // +30 days, $5.00
_, _ = phone.ReleaseNumber(ctx, bought.PhoneNumber) // free, returns to pool
```

| Endpoint | Method | Price |
|----------|--------|-------|
| `/v1/phone/lookup` | POST | $0.01 |
| `/v1/phone/lookup/fraud` | POST | $0.05 |
| `/v1/phone/numbers/buy` | POST | $5.00 (settled only after Twilio confirms) |
| `/v1/phone/numbers/renew` | POST | $5.00 |
| `/v1/phone/numbers/list` | POST | $0.001 |
| `/v1/phone/numbers/release` | POST | free |

Failed buys never charge your wallet — settlement is held until Twilio confirms the purchase.

## Surf (asksurf.ai)

`SurfClient` wraps `/v1/surf/*` — a single backend partner exposing **~83 crypto-intelligence endpoints** (exchange data, on-chain SQL, prediction markets, wallet/social analytics, project intelligence). Tiered pricing matches the backend:

| Tier | Price | Examples |
|------|-------|----------|
| **1** | $0.001 | `market/ranking`, `exchange/price`, `news/feed`, `prediction-market/polymarket/markets` |
| **2** | $0.005 | `token/holders`, `social/mindshare`, `search/web`, `wallet/detail` |
| **3** | $0.020 | `onchain/sql`, `onchain/query`, `onchain/schema` |

```go
surf, err := blockrun.NewSurfClient("")

// Discovery
for _, e := range blockrun.SurfEndpoints() {
    fmt.Printf("%-50s %s tier=%d $%.3f\n", e.Path, e.Method, e.Tier, e.PriceUSD)
}
price, _ := blockrun.SurfPrice("onchain/sql") // 0.020

// GET — pass query params (any value; converted to strings, []string joined with comma)
top, err := surf.Get(ctx, "market/ranking", map[string]any{"limit": 20})
btc, err := surf.Get(ctx, "exchange/price",  map[string]any{"pair": "BTC/USDT"})

// POST — JSON body
sql, err := surf.Post(ctx, "onchain/sql", map[string]any{
    "query": "SELECT count() FROM ethereum.blocks",
})

// Generic helper — auto-routes GET vs POST from the catalog
out, err := surf.Call(ctx, "token/holders", blockrun.SurfCallOptions{
    Params: map[string]any{"address": "0x...", "chain": "ethereum"},
})
```

Required-param validation runs client-side before the network round trip (e.g. `exchange/price` requires `pair`), so missing params surface as a `*ValidationError` instead of a 400 round-trip.

## Response Caching

Enable local caching to avoid redundant API calls.

```go
client, err := blockrun.NewLLMClient("", blockrun.WithCache(true))
```

Cache TTLs by endpoint:
- X/Twitter: 1 hour
- Prediction Markets: 30 minutes
- Search: 15 minutes
- Chat/Images: never cached

## Cost Tracking

```go
// Session spending
spending := client.GetSpending()
fmt.Printf("Session: %d calls, $%.6f\n", spending.Calls, spending.TotalUSD)

// Persistent cost log (across sessions)
summary, err := client.GetCostSummary()
fmt.Printf("Total: $%.4f across %d calls\n", summary.TotalUSD, summary.Calls)
for endpoint, cost := range summary.ByEndpoint {
    fmt.Printf("  %s: $%.4f\n", endpoint, cost)
}
```

## Balance Checking

```go
balance, err := client.GetBalance(ctx)
fmt.Printf("USDC balance: $%.2f\n", balance)

// Testnet
balance, err := client.GetBalanceTestnet(ctx)
```

## Agent Wallet Setup

For autonomous agents that need their own wallet:

```go
// Auto-creates wallet if none exists, prints funding instructions
client, err := blockrun.SetupAgentWallet()

// Check status
address, balance, err := client.Status(ctx)
fmt.Printf("Address: %s, Balance: $%.2f\n", address, balance)

// Scan wallets from multiple providers
wallets := blockrun.ScanWallets()
for _, w := range wallets {
    fmt.Printf("Found wallet: %s\n", w.Address)
}
```

## Available Models

| Provider | Models | Input $/M | Output $/M |
|----------|--------|-----------|------------|
| **OpenAI** | GPT-5.5, GPT-5.4, GPT-5.2, GPT-5.2 Codex, GPT-5 Mini, GPT-4o, GPT-4o-mini | $0.05–$30.00 | $0.40–$180.00 |
| **Anthropic** | Claude Opus 4.6, Claude Sonnet 4.6, Claude Haiku 4.5 | $1.00–$5.00 | $5.00–$25.00 |
| **Google** | Gemini 3.1 Pro, Gemini 2.5 Pro, Gemini 2.5 Flash | $0.10–$2.00 | $0.40–$12.00 |
| **xAI** | Grok 4.1 Fast, Grok 3, Grok Code Fast 1 | $0.20–$3.00 | $0.50–$15.00 |
| **DeepSeek** | DeepSeek Chat, DeepSeek Reasoner | $0.28 | $0.42 |
| **Moonshot** | Kimi K2.6 (256K, vision + reasoning) | $0.95 | $4.00 |
| **Moonshot** | Kimi K2.5 (262K context, legacy) | $0.60 | $3.00 |
| **NVIDIA** | DeepSeek V4 Pro/Flash, Nemotron Nano Omni (vision), Qwen3, Llama 4, GLM-4.7, Mistral (9 models) | **FREE** | **FREE** |

Use `client.ListModels(ctx)` for the full list with current pricing.

## Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `BASE_CHAIN_WALLET_KEY` | Base chain wallet private key | Yes (or pass to constructor) |
| `BLOCKRUN_WALLET_KEY` | Alias for BASE_CHAIN_WALLET_KEY | No |
| `BLOCKRUN_API_URL` | Custom API endpoint | No (default: https://blockrun.ai/api) |

## Error Handling

```go
response, err := client.Chat(ctx, "openai/gpt-4o", "Hello")
if err != nil {
    switch e := err.(type) {
    case *blockrun.ValidationError:
        fmt.Printf("Invalid input: %s - %s\n", e.Field, e.Message)
    case *blockrun.PaymentError:
        fmt.Printf("Payment failed: %s\n", e.Message)
    case *blockrun.APIError:
        fmt.Printf("API error %d: %s\n", e.StatusCode, e.Message)
    }
}
```

## Security

- **Private key stays local**: Only used for EIP-712 signing — never transmitted
- **Non-custodial**: BlockRun never holds your funds
- **On-chain verifiable**: All payments visible on [Basescan](https://basescan.org)
- Use environment variables, never hard-code keys
- Use dedicated wallets with small balances for API payments

## Requirements

- Go 1.22+
- A wallet with USDC on Base chain

## FAQ

**What is blockrun-llm-go?**
A Go SDK for pay-per-request access to 40+ LLMs, X/Twitter data, web search, prediction markets, and image generation. Uses x402 micropayments — no API keys, no subscriptions.

**How much does it cost?**
Pay only for what you use. 9 NVIDIA-hosted models are completely free (DeepSeek V4 Pro/Flash, Nemotron Nano Omni vision, Qwen3, Llama 4, GLM-4.7, Mistral). $5 USDC gets you thousands of paid-model requests.

**Does it support Solana?**
The Go SDK supports Base chain only. For Solana, use the [Python SDK](https://github.com/blockrunai/blockrun-llm) or [TypeScript SDK](https://github.com/blockrunai/blockrun-llm-ts).

**Is streaming supported?**
Yes. Use `ChatCompletionStream` for SSE streaming.

## Links

- [Website](https://blockrun.ai)
- [Documentation](https://github.com/BlockRunAI/awesome-blockrun/tree/main/docs)
- [Python SDK](https://github.com/blockrunai/blockrun-llm)
- [TypeScript SDK](https://github.com/blockrunai/blockrun-llm-ts)
- [GitHub](https://github.com/blockrunai/blockrun-llm-go)
- [Telegram](https://t.me/+mroQv4-4hGgzOGUx)

## License

MIT
