# BlockRun Go SDK

> **blockrun-llm-go** is the full Go SDK for BlockRun: <!-- br:models.chatVisible -->76<!-- /br:models.chatVisible --> chat models, plus image, video, music, speech, voice calls, web search, market data, prediction markets, DeFi and DEX data, and JSON-RPC to <!-- br:chains.rpc -->40<!-- /br:chains.rpc --> chains. Every call is paid per request — no subscription, no seats, no minimum.
>
> **Two ways to pay, same SDK, same catalogue.** Sign up at
> **[user.blockrun.ai](https://user.blockrun.ai)** for an API key and prepaid
> credit (top up with a card), or hold USDC in your own wallet and let each
> request settle itself over x402 — on **Solana or Base**. Pick whichever fits;
> the constructor takes either credential and everything below works the same.
>
> The module keeps the name `blockrun-llm-go` because in Go the repository name *is* the import path, and renaming it would break every existing consumer. The SDK stopped being LLM-only long before v0.19.
>
> 🆓 **Includes <!-- br:models.free -->7<!-- /br:models.free --> fully-free NVIDIA-hosted models** — DeepSeek V4 Pro/Flash (1M context), Nemotron Nano Omni (vision), Qwen3, Llama 4, GLM-4.7, Mistral. Zero USDC, no rate-limit gimmicks. Use `blockrun.RoutingFree` or call any `nvidia/*` model directly.

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

    // Reads BLOCKRUN_API_KEY (an API key from user.blockrun.ai) if set,
    // otherwise BASE_CHAIN_WALLET_KEY and pays x402 on Base.
    // For x402 on Solana use NewLLMClientSolana — see "Pay on Solana".
    client, err := blockrun.NewLLMClient("")
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

```bash
# API key — sign up at https://user.blockrun.ai, then:
export BLOCKRUN_API_KEY=brk_live_...

# …or a wallet, and every call pays itself in USDC:
export SOLANA_WALLET_KEY=...        # with NewLLMClientSolana / NewXClientSolana
export BASE_CHAIN_WALLET_KEY=0x...  # with NewLLMClient / NewXClient
```

The credential can also be passed directly — `NewLLMClient("brk_live_…")` or
`NewLLMClient("0x…")`. Every constructor in the SDK takes it in that same first
argument, and `client.PaymentMode()` reports which rail you ended up on.
`BLOCKRUN_API_KEY` works with every constructor, the `…Solana` ones included.

### Try It Free (No Balance Required)

Want to kick the tires before topping up or funding a wallet? Route to BlockRun's
free NVIDIA tier — it settles $0 on both rails, so an unfunded wallet or a $0
credit account is enough:

```go
// Option 1: call a free model directly
reply, _ := client.Chat(ctx, "nvidia/deepseek-v4-flash", "Explain x402 in 1 sentence")

// Option 2: let the smart router pick the best free model per request
result, _ := client.SmartChat(ctx, "What is 2+2?", &blockrun.SmartChatOptions{
    RoutingProfile: blockrun.RoutingFree,
})
fmt.Println(result.Model)    // e.g. "nvidia/deepseek-v4-flash"
fmt.Println(result.Response) // "4"
```

**Available free models** (input + output both $0, all NVIDIA-hosted, last refreshed 2026-06-07):

| Model ID | Context | Best For |
|----------|---------|----------|
| `nvidia/deepseek-v4-flash` | 1M | DeepSeek V4 Flash — 284B / 13B active MoE, ~5× faster than V4 Pro. Best free chat / summarization / light reasoning |
| `nvidia/nemotron-3-nano-omni-30b-a3b-reasoning` | 256K | Only vision-capable free model — text + images + video (≤2 min) + audio (≤1 hr) |
| `nvidia/llama-4-maverick` | 131K | Meta Llama 4 Maverick MoE |
| `nvidia/mistral-small-4-119b` | 131K | ⚠️ Upstream timing out as of 2026-06-07 — avoid until NVIDIA recovers it |
| `nvidia/qwen3-coder-480b` | 131K | Coding-optimised 480B MoE |
| `nvidia/gpt-oss-120b` | 128K | OpenAI open-weight 120B — 123 tok/s. Hidden from `/v1/models` for privacy but direct calls by full ID still work |
| `nvidia/gpt-oss-20b` | 128K | OpenAI open-weight 20B — 155 tok/s. Hidden from `/v1/models` but direct calls still work |

> Need V4-Pro-class reasoning? Use the paid `deepseek/deepseek-v4-pro` ($0.435/$0.87 — the 75% launch promo became the permanent list price after 2026-05-31) — `nvidia/deepseek-v4-pro` is currently hidden because NVIDIA's NIM deployment is hung; backend MODEL_REDIRECTS forwards calls to V4 Flash.

> Note: `nvidia/gpt-oss-120b` and `nvidia/gpt-oss-20b` are hidden from `/v1/models` — NVIDIA's free build.nvidia.com tier reserves the right to use prompts/outputs for service improvement, so SmartChat never auto-routes to them. Direct calls by full ID still work; opt in only when your data isn't sensitive.

> Retired: `nvidia/qwen3-next-80b-a3b-thinking` hit NVIDIA end-of-life 2026-05-21 (HTTP 410). The gateway auto-redirects pinned callers to `nvidia/llama-4-maverick`.

## How Payment Works

There are two front doors onto the same gateway, the same catalogue and the same
response shapes. You choose one with the credential you hand the constructor.

| | **API key** — `api.blockrun.ai` | **Wallet (x402)** — `sol.blockrun.ai` / `blockrun.ai` |
|---|---|---|
| Authenticates with | `brk_live_…` key from [user.blockrun.ai](https://user.blockrun.ai) | a signature from your own wallet |
| Pays from | prepaid credit on your account | USDC you hold, settled on-chain per call |
| Set up by | signing in with Google, minting a key, topping up with a card | funding a wallet with USDC |
| Chain | none — credit is off-chain | **Solana** or **Base** |
| Custody | BlockRun holds the credit you bought | non-custodial; your key never leaves your machine |
| Best for | teams that cannot run wallets, CI, anyone who wants a card receipt | agents, autonomous spend, no-signup access |

Free models are free on both. You can call them with nothing but a key and $0 of
credit, or with a wallet holding $0 of USDC.

### Option A — API key (user.blockrun.ai)

1. **Sign in** at **[user.blockrun.ai](https://user.blockrun.ai)** with Google.
2. **Mint a key** on the *Keys* page. It is shown once — copy it then.
3. **Top up** on the *Credits* page with a card. Minimum $5. The processing fee
   (5.5% + $0.30) is charged **once, at purchase** — never on a call — so $10.85
   buys $10.00 of credit and every model then bills at the published list price,
   with no per-call minimum and no per-call fee.
4. **Export it** and you are done:

```bash
export BLOCKRUN_API_KEY=brk_live_...
```

```go
client, _ := blockrun.NewLLMClient("")             // picks up BLOCKRUN_API_KEY
fmt.Println(client.PaymentMode())                  // "apikey"

reply, _ := client.Chat(ctx, "openai/gpt-4o", "What is 2+2?")
```

Requests go to `https://api.blockrun.ai/v1` with the key as
`Authorization: Bearer …`. There is no 402 round trip and nothing is signed —
the gateway meters the call at exact usage and draws it from your credit.
Spending, per-call activity and remaining balance are on the dashboard at
[user.blockrun.ai/dashboard](https://user.blockrun.ai/dashboard).

Out of credit, you get a `*PaymentError` naming the account — not a wallet
error — and pointing at the top-up page. Free models keep working.

### Option B — wallet + x402

You hold USDC in your own wallet — on **Solana** or **Base** — and **each
request pays for itself** with an on-chain micropayment. No signup, no account,
nothing custodial.

#### 1. Fund your wallet once

A Solana client (`NewLLMClientSolana`, key via `SOLANA_WALLET_KEY`) needs **USDC
on Solana**. A Base client (`NewLLMClient`, key via `BASE_CHAIN_WALLET_KEY`)
needs **USDC on Base** instead. Three ways to get it there:

- **Transfer USDC** you already hold to your wallet address
  (`client.GetWalletAddress()`) — on the same chain your client pays from.
  Solana USDC (mint `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`) for a Solana
  client; Base USDC for a Base client. Sending Base USDC to a Solana client's
  wallet does not work.
- **Buy with a card** — mint a Coinbase Onramp link and pay with card/bank
  (60+ fiat currencies). This is **free** (you're only funding your own wallet).
  **Base only** — `Onramp` rejects anything that isn't an EVM address:

  ```go
  res, _ := client.Onramp(ctx, client.GetWalletAddress())
  fmt.Println("Buy USDC:", res.URL) // open https://pay.coinbase.com/... and pay
  ```

- **Skip funding entirely** — call the [free NVIDIA models](#try-it-free-no-balance-required); they cost $0 and need no balance.

$5 of USDC covers thousands of paid-model requests. `client.GetBalance(ctx)`
reports it on whichever chain the client pays from — the USDC SPL mint for a
Solana client, the Base USDC contract for a Base one.

#### 2. Every request pays itself (automatic x402)

You never call a "pay" function for inference — the SDK does it inline on each request:

1. You call e.g. `client.Chat(ctx, model, prompt)`.
2. The gateway replies **`402 Payment Required`** with the exact price for that call.
3. The SDK signs the payment **locally** — an ed25519-signed `TransferChecked` transaction on Solana, which BlockRun's facilitator co-signs as fee payer so the transfer costs you no SOL, or EIP-712 typed data on Base.
4. It retries the request with the signed payment proof attached.
5. The gateway settles the payment on-chain and returns your response.

All of this happens in one method call — you just get the response back (or a
`*PaymentError` if the wallet is underfunded).

**Your private key never leaves your machine** — it only signs payments locally;
only the signature is transmitted. BlockRun is non-custodial and never holds your funds.

### What it costs & how to verify

- **Per call, pay-as-you-go.** Price is set per endpoint/model (see [Available Models](#available-models)); free NVIDIA models settle $0.
- **Track it.** `client.GetSpending()` returns this session's total USD + call count; a JSONL cost log persists across runs (see [Cost Tracking](#cost-tracking)). On the API-key rail the dashboard is the authority — see the caveat in that section.
- **Verify.** Wallet settlements are real USDC transfers, auditable on [Solscan](https://solscan.io) for Solana and [Basescan](https://basescan.org) for Base. API-key calls are itemised on [user.blockrun.ai/dashboard](https://user.blockrun.ai/dashboard).

## Pay on Solana

**Solana is the recommended chain for x402 payments**: settlement is sub-second
and BlockRun's facilitator co-signs as fee payer, so a transfer costs you no SOL
and you need to hold nothing but USDC. Base works identically and remains what
the bare `NewLLMClient` constructor uses, so nothing existing changes — but if
you are choosing today, choose Solana.

Every client has a `NewXClientSolana` counterpart that pays **USDC on
Solana** via `sol.blockrun.ai` instead of Base — same API, same verbatim responses:

```go
client, _ := blockrun.NewAnthropicClientSolana("", "")  // bs58 key from ~/.blockrun/.solana-session; default RPC
img, _    := blockrun.NewImageClientSolana("", "")
```

Signatures are `func NewXClientSolana(privateKey, rpcURL string, opts ...XClientOption)`.
`privateKey` is a bs58 Solana key (empty → `SOLANA_WALLET_KEY` → `~/.*/solana-wallet.json`
→ `~/.blockrun/.solana-session`); `rpcURL` fetches mint info for non-USDC assets, plus the
blockhash when the 402 requirement does not carry one (empty →
`SOLANA_RPC_URL` → BlockRun's free proxy). Payment is the x402 **SVM "exact" scheme**: a
locally ed25519-signed `TransferChecked` USDC transaction that BlockRun's facilitator
co-signs (gasless) and settles. Constructors: `NewLLMClientSolana`,
`NewAnthropicClientSolana`, `NewImageClientSolana`, `NewVideoClientSolana`,
`NewSpeechClientSolana`, `NewMusicClientSolana`, `NewVoiceClientSolana`,
`NewPhoneClientSolana`, `NewRealFaceClientSolana`, `NewPortraitClientSolana`,
`NewSurfClientSolana`, `NewRPCClientSolana`.

When a 402 offers more than one chain, the client picks the option matching its
own chain rather than the first one listed. A 402 that offers no option this
client can sign fails immediately with a chain-mismatch error naming what was
offered, instead of failing deep inside signing.

Wallet management has a Solana counterpart for every EVM helper:
`CreateSolanaWallet`, `SaveSolanaWallet`, `GetOrCreateSolanaWallet`,
`GetSolanaWalletAddressFromEnvOrFile`, `ScanSolanaWallets`,
`GetSolanaPaymentLinks`, `GetSolanaPayURI`, and the three
`FormatSolana*Message` funding helpers. Keys are written to
`~/.blockrun/.solana-session` with mode 0600, same as the EVM side.

```go
w, _ := blockrun.GetOrCreateSolanaWallet() // mints + persists on first run
if w.IsNew {
    fmt.Println(blockrun.FormatSolanaWalletCreatedMessage(w.Address))
}
```

`GetSolanaPayURI` builds a [Solana Pay](https://docs.solanapay.com) request
pinned to the USDC mint. Note it is not a mirror of `GetEIP681URI`: EIP-681
encodes a uint256 in base units, Solana Pay a decimal token amount, so 1.5 USDC
is `1500000` in one and `1.5` in the other.

One helper remains **Base-only**. `Onramp` rejects non-EVM addresses, so buying
USDC with a card funds a Base wallet only — a Solana wallet has to be funded by
transfer. `GetBalanceTestnet` also stays Base Sepolia and returns an explicit
error on a Solana client, since no devnet USDC mint is configured; `GetBalance`
itself works on both chains.

## API Keys & Accounts (user.blockrun.ai)

An API key from **[user.blockrun.ai](https://user.blockrun.ai)** replaces the
wallet entirely: no chain, no signing, no on-chain settlement. It is the same
catalogue and the same SDK — chat, streaming, images, video, speech, music,
search, Exa, RPC, DeFi, DEX, Surf, prediction markets, phone, voice, the
Anthropic Messages client, all of it.

### Getting one

| Step | Where |
|---|---|
| Sign in with Google | [user.blockrun.ai](https://user.blockrun.ai) |
| Mint a key (`brk_live_…`, shown once) | [/dashboard/keys](https://user.blockrun.ai/dashboard/keys) |
| Top up with a card — min $5, fee 5.5% + $0.30 charged once at purchase | [/dashboard/credits](https://user.blockrun.ai/dashboard/credits) |
| See every call, its price and your balance | [/dashboard/activity](https://user.blockrun.ai/dashboard/activity) |

A brand-new account starts at $0 of credit and can still call the
[free models](#try-it-free-no-balance-required) — registration, not a card, is the
price of admission to those.

### Using it

Any constructor. The credential argument accepts either kind, and a `brk_`
prefix is what selects this rail:

```go
client, _ := blockrun.NewLLMClient("brk_live_...")
img, _    := blockrun.NewImageClient("")            // or from BLOCKRUN_API_KEY
ac, _     := blockrun.NewAnthropicClient("")        // Anthropic Messages, same key

// Even the Solana constructors take it — a key answers the chain question
// rather than being answered by it.
sol, _ := blockrun.NewLLMClientSolana("brk_live_...", "")
```

Precedence, since it decides whether a call spends credit or on-chain USDC:

1. an explicit argument — `NewLLMClient("brk_live_…")` or `NewLLMClient("0x…")`;
2. `BLOCKRUN_API_KEY`, which **beats** `BLOCKRUN_WALLET_KEY` /
   `BASE_CHAIN_WALLET_KEY` / `SOLANA_WALLET_KEY`;
3. the wallet variables.

So an existing wallet setup is untouched until you set `BLOCKRUN_API_KEY`, and
passing a wallet key explicitly always opts back out. Check which one you got:

```go
if client.PaymentMode() == blockrun.PaymentModeAPIKey {
    // requests go to api.blockrun.ai and draw prepaid credit
}
```

`SetupAgentWallet()` follows the same rule and mints **no** wallet file when a
key is configured — so an agent or skill can call it unconditionally and work on
either rail.

### What changes

- **Wallet-only helpers refuse rather than mislead.** `GetBalance`,
  `GetBalanceTestnet` and `Onramp` return a `*ValidationError` pointing at the
  dashboard. Returning `0` would be indistinguishable from an empty wallet, and
  an agent gating on it would stop calling a well-funded account.
  `GetWalletAddress()` returns `""`.
- **Out of credit is a `*PaymentError`**, not a 402 to sign, and its message
  names the top-up page and quotes the gateway's own reason.
- **`GetSpending()` is a floor, not a total** — see [Cost Tracking](#cost-tracking).
- **Nothing else.** Every method, option and response type is identical.

### Switching between the rails

An explicit wallet credential chooses the wallet rail even when
`BLOCKRUN_API_KEY` is set, so `NewLLMClient("0x…")` always pays on-chain. A
**blank** `BLOCKRUN_API_KEY` counts as unset — that is what `docker -e
BLOCKRUN_API_KEY`, an unpopulated `${{ secrets.X }}`, and a bare
`BLOCKRUN_API_KEY=` line all produce, and none of them mean you wanted the
account rail. A **non-blank** value that is not a key is an error rather than a
silent fall back to a wallet: someone typed a credential and got it wrong, and
spending USDC instead of credit is the wrong way to tell them.

Credentials are read once, at construction. Build a new client to change rails;
an existing one keeps the account it started with.

### Environment

```bash
export BLOCKRUN_API_KEY=brk_live_...
export BLOCKRUN_API_KEY_URL=https://api.blockrun.ai   # optional override
```

`BLOCKRUN_API_KEY_URL` is deliberately not `BLOCKRUN_API_URL`: that one names an
x402 gateway, and an API-key client must never follow it and send your key to a
host you configured for a different rail.

## Features

| Feature | Description |
|---------|-------------|
| **Two payment rails** | API key + prepaid credit (`user.blockrun.ai`), or x402 USDC from your own wallet on Solana / Base |
| **Chat & Completion** | OpenAI-compatible chat with <!-- br:models.chatVisible -->76<!-- /br:models.chatVisible --> models |
| **Anthropic Client** | Native Anthropic Messages API with automatic x402 payments |
| **Smart Routing** | Auto-selects the best model for your prompt |
| **Streaming** | SSE streaming for real-time responses |
| **Tool Calling** | OpenAI-compatible function/tool calling |
| **Multi-chain RPC** | JSON-RPC 2.0 to <!-- br:chains.rpc -->40<!-- /br:chains.rpc --> chains, $0.002/call |
| **Web Search** | Search web, X/Twitter, and news |
| **Prediction Markets** | Polymarket, Kalshi data access |
| **Image Generation** | DALL-E 3, GPT Image 1/2, Nano Banana, Flux, CogView-4, Grok Imagine |
| **Music Generation** | Full-length (~3 min) tracks via MiniMax Music 2.5+ |
| **Text-to-Speech** | BlockRun Voice (ElevenLabs) — TTS from $0.05/1k chars + sound effects |
| **Video Generation** | Grok Imagine Video, ByteDance Seedance (1.5-pro / 2.0-fast / 2.0) with face/character consistency |
| **Virtual Portraits** | Enroll AI-generated characters as reusable Seedance face assets |
| **RealFace** | Enroll a real person's likeness (on-phone liveness, no KYC) as a Seedance face asset |
| **Voice Calls** | AI-powered outbound phone calls (Bland.ai upstream) |
| **Phone Lookup + Numbers** | Twilio carrier/fraud lookup + provisioned numbers for caller-ID |
| **Surf (asksurf.ai)** | ~83 endpoints: exchange data, on-chain SQL, prediction markets, wallet/social analytics |
| **Response Caching** | Local cache with per-endpoint TTL |
| **Cost Tracking** | Session spending + persistent JSONL log |
| **Balance Checking** | Query USDC balance on Solana or Base (wallet rail; credit balance is on the dashboard) |
| **Fund Wallet** | One-time Coinbase Onramp link — buy USDC with a card (Base only) |
| **Agent Wallet Setup** | Auto-create wallets for autonomous agents — a no-op when an API key is configured |

## Anthropic Client

Use the native Anthropic Messages API format with BlockRun's gateway — with an
API key or with x402 from a wallet, same as everything else.
Works with Claude models and any other BlockRun model (OpenAI, Google, etc.) via Anthropic message format.
Pass any model ID — e.g. `claude-fable-5` (Mythos-class, above Opus), `claude-opus-4-8`, or `claude-sonnet-4-6`.

```go
client, err := blockrun.NewAnthropicClient("")  // BLOCKRUN_API_KEY, else a wallet key
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
| **free** | nvidia/deepseek-v4-flash | nvidia/llama-4-maverick | nvidia/qwen3-coder-480b | nvidia/nemotron-3-nano-omni-30b-a3b-reasoning |
| **eco** | moonshot/kimi-k2.6 | deepseek/deepseek-chat | google/gemini-2.5-pro | deepseek/deepseek-reasoner |
| **auto** | moonshot/kimi-k2.6 | google/gemini-3.5-flash | google/gemini-3.1-pro | deepseek/deepseek-reasoner |
| **premium** | google/gemini-3.5-flash | openai/gpt-5.5 | anthropic/claude-opus-4.8 | openai/o3 |

> DeepSeek V4 family launched 2026-04-24. The legacy `deepseek/deepseek-chat`
> and `deepseek/deepseek-reasoner` IDs (used by **eco** Medium / Reasoning
> above) are now V4 Flash non-thinking / thinking modes — $0.20 in / $0.40 out
> per 1M, 1M context. The paid flagship `deepseek/deepseek-v4-pro`
> ($0.435/$0.87 — the 75% launch promo became the permanent list price after
> 2026-05-31) is available via direct chat calls; SmartChat keeps
> `deepseek-reasoner` as the eco/auto reasoning primary because V4 Flash
> thinking is cheaper.
>
> NVIDIA free routing rebuilt 2026-06-07 from a live sweep:
> `nvidia/qwen3-next-80b-a3b-thinking` hit NVIDIA end-of-life 2026-05-21
> (HTTP 410) and `nvidia/mistral-small-4-119b` is timing out upstream — both
> dropped. Free now routes Simple → deepseek-v4-flash (1M context), Medium →
> llama-4-maverick, Complex → qwen3-coder-480b, Reasoning →
> nemotron-3-nano-omni (matches the Python SDK). `nvidia/gpt-oss-120b` /
> `gpt-oss-20b` remain hidden for privacy (direct calls by full ID still
> return HTTP 200). Retired IDs (`nvidia/nemotron-*`,
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

## Multi-chain RPC

`RPCClient` wraps `POST /v1/rpc/{network}` — standard JSON-RPC 2.0 access to
<!-- br:chains.rpc -->40<!-- /br:chains.rpc --> chains through one endpoint (Ethereum, Base, Solana, Polygon, BSC,
Arbitrum, Optimism, Avalanche, Bitcoin, Sui, and more; powered by Tatum's RPC
gateway). No per-chain endpoints and no per-chain provider account: flat
**$0.002 per call**, from credit or USDC; a JSON-RPC batch charges per element.

```go
rpcClient, err := blockrun.NewRPCClient("")

// EVM chains speak eth_* JSON-RPC
block, err := rpcClient.Call(ctx, "ethereum", "eth_blockNumber", nil)
fmt.Println(string(block.Result)) // "0x1499f7c"

balance, err := rpcClient.Call(ctx, "base", "eth_getBalance", []any{
    "0x4200000000000000000000000000000000000006", "latest",
})

// Non-EVM chains speak their native JSON-RPC
slot, err := rpcClient.Call(ctx, "solana", "getSlot", nil)
tip, err := rpcClient.Call(ctx, "bitcoin", "getblockcount", nil)

// Batch: one payment, per-element pricing ($0.002 x N)
out, err := rpcClient.Batch(ctx, "polygon", []blockrun.RPCBatchRequest{
    {Method: "eth_blockNumber"},
    {Method: "eth_gasPrice"},
})

fmt.Println(block.Network)  // "ethereum" (canonical key from X-Network)
fmt.Println(block.CacheHit) // true if served from the gateway's hot cache
fmt.Println(block.TxHash)   // x402 settlement tx
```

40 curated chains are exported as `blockrun.RPCSupportedNetworks`; common
aliases (`eth`, `arb`, `op`, `matic`, `bnb`, `avax`, `sol`, `btc`, `xrp`,
`dot`, ...) resolve server-side (`blockrun.RPCNetworkAliases`). Unknown but
well-formed slugs fall through to a generic `{slug}-mainnet` gateway attempt,
so new chains work without an SDK update. Hot, low-volatility reads
(`eth_chainId`, mined blocks/receipts, `getTransaction`, ...) are served from
a method-aware gateway cache — same price, lower latency.

## Exa Web Search

Neural + keyword web search, similarity search, content extraction, and
grounded answers ($0.01/request; contents $0.002/URL). Powered by Exa.

```go
results, err := client.ExaSearch(ctx, "latest AI safety research", map[string]any{"numResults": 5})
similar, err := client.ExaFindSimilar(ctx, "https://openai.com/research", nil)
content, err := client.ExaContents(ctx, []string{"https://arxiv.org/abs/2303.08774"}, nil)
answer, err := client.ExaAnswer(ctx, "What is x402?", nil)
```

## DeFi Data (Powered by DefiLlama)

GET passthrough to DefiLlama — protocols, TVL, yields, token prices.
$0.005/call ($0.001 for price lookups).

```go
protocols, err := client.DefiProtocols(ctx)              // all protocols + TVL
aave, err := client.DefiProtocol(ctx, "aave")            // one protocol + history
chains, err := client.DefiChains(ctx)                    // TVL by chain
pools, err := client.DefiYields(ctx, map[string]string{"chain": "Base"})
prices, err := client.DefiPrices(ctx, []string{"coingecko:bitcoin"})
```

## DEX Swaps (Powered by 0x)

Free passthrough to the 0x Swap + Gasless APIs — **no x402 payment**
(BlockRun takes an on-chain affiliate fee on executed swaps instead).

```go
price, err := client.DexPrice(ctx, map[string]string{
    "chainId": "8453", "sellToken": "0x...", "buyToken": "0x...",
    "sellAmount": "1000000",
})
quote, err := client.DexQuote(ctx, map[string]string{ /* + "taker" */ })

// Gasless flow: quote -> sign trade.eip712 -> submit -> poll
gq, err := client.DexGaslessQuote(ctx, params)
res, err := client.DexGaslessSubmit(ctx, map[string]any{"trade": signedTrade})
status, err := client.DexGaslessStatus(ctx, res["tradeHash"].(string))

chains, err := client.DexChains(ctx)            // supported swap chains
gchains, err := client.DexGaslessChains(ctx)    // supported gasless chains
```

## Cloud Compute (Powered by Modal)

Pay-per-call sandboxed compute — $0.01/create (CPU; $0.05 with GPU),
$0.001 per exec/status/terminate.

```go
sb, err := client.ModalSandboxCreate(ctx, map[string]any{"image": "python:3.11"})
out, err := client.ModalSandboxExec(ctx, sb["sandbox_id"].(string), []string{"python", "-c", "print(42)"})
fmt.Println(out["stdout"]) // 42
_, err = client.ModalSandboxTerminate(ctx, sb["sandbox_id"].(string))
```

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

## Text-to-Speech & Sound Effects

BlockRun Voice (ElevenLabs) — OpenAI-compatible TTS plus cinematic sound
effects. TTS price scales with character count: `(chars / 1000) × model rate`,
minimum $0.001/request. Synthesis is synchronous (<1s for Flash).

| Model | Price | Max Input | Notes |
|-------|-------|-----------|-------|
| `elevenlabs/flash-v2.5` | $0.05/1k chars | 40k chars | ~75ms latency, 32 languages (default) |
| `elevenlabs/turbo-v2.5` | $0.05/1k chars | 40k chars | ~250ms latency, balanced quality |
| `elevenlabs/multilingual-v2` | $0.10/1k chars | 10k chars | Long-form narration, audiobooks |
| `elevenlabs/v3` | $0.10/1k chars | 5k chars | Max expressiveness, 70+ languages |
| `elevenlabs/sound-effects` | $0.05/generation | 1k chars | Sound effects up to 22s |

```go
speechClient, err := blockrun.NewSpeechClient("")

// Text-to-speech (voice aliases: sarah, george, laura, charlie,
// river, roger, callum, harry — or any raw ElevenLabs voice_id)
result, err := speechClient.Generate(ctx, "Welcome to BlockRun.", &blockrun.SpeechGenerateOptions{
    Voice: "george",
})
fmt.Println(result.Data[0].URL)  // audio URL (mp3 by default)

// Other formats / speed
speed := 1.1
result, err = speechClient.Generate(ctx, "Breaking news from the world of micropayments.", &blockrun.SpeechGenerateOptions{
    Model:          "elevenlabs/v3",
    ResponseFormat: "wav",
    Speed:          &speed,
})

// Sound effects (flat $0.05/generation)
fx, err := speechClient.SoundEffect(ctx, "rain on a tin roof, distant thunder", nil)

// List voices (free, rate-limited)
voices, err := speechClient.ListVoices(ctx)
```

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

// First-and-last-frame interpolation (Seedance only): the model tweens
// from ImageURL (first frame) to LastFrameURL (final frame).
// Priced identically to image-to-video.
result, err = videoClient.Generate(ctx, "the flower blooms in golden morning light", &blockrun.VideoGenerateOptions{
    Model:        "bytedance/seedance-1.5-pro",
    ImageURL:     "https://example.com/bud.jpg",
    LastFrameURL: "https://example.com/bloom.jpg",
})

// Omni / multi-reference (Seedance 2.0 only): up to 9 reference images
// for character/style consistency. Cite them as "image 1", "image 2" in
// the prompt. Mutually exclusive with ImageURL / LastFrameURL /
// RealFaceAssetID.
result, err = videoClient.Generate(ctx, "the character from image 1 walks through the city from image 2", &blockrun.VideoGenerateOptions{
    Model: "bytedance/seedance-2.0",
    ReferenceImageURLs: []string{
        "https://example.com/character.jpg",
        "https://example.com/city.jpg",
    },
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

**On the wallet rail both numbers are exact** — every paid call signs a known
amount, so `TotalUSD` is what actually settled and `Calls` counts the paid ones.

**On the API-key rail they cannot be.** The client is never told what a call
cost: chat, `/v1/messages`, search and most data endpoints are billed post-hoc
from usage at your account's contracted rate, which the SDK does not hold and
will not guess — two implementations of one price sheet drift, and a drifted
number is worse than a blank one. So `Calls` counts every request the gateway
answered (free models included, since nothing in the response distinguishes
them) and `TotalUSD` books only the families that publish a settled `price` in
their body — images and video. Treat it as a floor;
[user.blockrun.ai/dashboard](https://user.blockrun.ai/dashboard) is the
authority on what an account has spent.

## Balance Checking

Wallet rail only — it reads USDC on whichever chain the client pays from
(the SPL mint for a Solana client, the Base USDC contract for a Base one):

```go
balance, err := client.GetBalance(ctx)
fmt.Printf("USDC balance: $%.2f\n", balance)

// Testnet (Base Sepolia)
balance, err := client.GetBalanceTestnet(ctx)
```

An API-key client has no address, so both return a `*ValidationError` rather
than a `0` that reads as an empty wallet. Credit balance lives on
[user.blockrun.ai/dashboard](https://user.blockrun.ai/dashboard).

## Fund Wallet (Coinbase Onramp)

Mint a one-time Coinbase Onramp link to top up your wallet with a card or bank
(60+ fiat currencies → Base USDC). It's **free** — the x402 signature only
authenticates the wallet, so the funding address must match the signing wallet.
The link is single-use and expires in ~5 minutes, so open it immediately and
never cache it. **Base / USDC only.**

```go
res, err := client.Onramp(ctx, client.GetWalletAddress())
if err != nil {
    log.Fatal(err)
}
fmt.Println("Buy USDC:", res.URL) // https://pay.coinbase.com/...
```

## Agent Wallet Setup

For autonomous agents that need their own wallet. With `BLOCKRUN_API_KEY` set it
mints nothing and hands back an API-key client instead, so the same call works on
either rail:

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
| **Anthropic** | Claude Fable 5 (Mythos-class, 1M ctx, always-on thinking), Claude Opus 4.8, Claude Sonnet 4.6, Claude Haiku 4.5 | $1.00–$10.00 | $5.00–$50.00 |
| **Google** | Gemini 3.5 Flash (thinking), Gemini 3.1 Pro, Gemini 2.5 Pro, Gemini 2.5 Flash | $0.10–$2.00 | $0.40–$12.00 |
| **xAI** | Grok 4.3 (1M, reasoning + vision), Grok Build 0.1 (256K, agentic coding) | $1.50 | $3.00–$4.00 |
| **DeepSeek** | DeepSeek V4 Pro, DeepSeek Chat, DeepSeek Reasoner | $0.20–$0.435 | $0.40–$0.87 |
| **ZAI** | GLM-5.1 ($1.40/$4.40), GLM-5 ($0.60/$1.92), GLM-5-Turbo ($1.20/$4.00) | $0.60–$1.40 | $1.92–$4.40 |
| **ElevenLabs** | Flash v2.5, Turbo v2.5, Multilingual v2, v3 (TTS $0.05–0.10/1k chars), Sound Effects ($0.05/gen) | — | — |
| **Moonshot** | Kimi K2.6 (256K, vision + reasoning) | $0.95 | $4.00 |
| **Moonshot** | Kimi K2.5 (262K context, legacy) | $0.60 | $3.00 |
| **NVIDIA** | DeepSeek V4 Pro/Flash, Nemotron Nano Omni (vision), Qwen3, Llama 4, GLM-4.7, Mistral (<!-- br:models.free -->7<!-- /br:models.free --> models) | **FREE** | **FREE** |

Use `client.ListModels(ctx)` for the full list with current pricing.

## Environment Variables

One credential is required — an API key **or** a wallet key. Everything else is
optional.

| Variable | Description | Default |
|----------|-------------|---------|
| `BLOCKRUN_API_KEY` | API key from [user.blockrun.ai](https://user.blockrun.ai) (`brk_live_…`). **Takes precedence over every wallet variable.** | — |
| `SOLANA_WALLET_KEY` | bs58 Solana wallet key, for `NewXClientSolana` | falls back to `~/.*/solana-wallet.json`, then `~/.blockrun/.solana-session` |
| `BASE_CHAIN_WALLET_KEY` | Base chain wallet private key | — |
| `BLOCKRUN_WALLET_KEY` | Alias for `BASE_CHAIN_WALLET_KEY` | — |
| `BLOCKRUN_API_KEY_URL` | Override the API-key gateway | `https://api.blockrun.ai` |
| `BLOCKRUN_SOLANA_API_URL` | Override the Solana x402 gateway | `https://sol.blockrun.ai/api` |
| `BLOCKRUN_API_URL` | Override the Base x402 gateway | `https://blockrun.ai/api` |
| `SOLANA_RPC_URL` | RPC for blockhash + mint info while signing | BlockRun's free proxy |
| `BLOCKRUN_CHAT_TIMEOUT` | Chat HTTP timeout, in seconds | `600` |

## Error Handling

```go
response, err := client.Chat(ctx, "openai/gpt-4o", "Hello")
if err != nil {
    switch e := err.(type) {
    case *blockrun.ValidationError:
        fmt.Printf("Invalid input: %s - %s\n", e.Field, e.Message)
    case *blockrun.PaymentError:
        // Wallet rail: underfunded. API-key rail: out of credit —
        // the message names the top-up page.
        fmt.Printf("Payment failed: %s\n", e.Message)
    case *blockrun.APIError:
        fmt.Printf("API error %d: %s\n", e.StatusCode, e.Message)
    }
}
```

## Security

**Wallet rail**

- **Private key stays local**: Only used for local signing — ed25519 on Solana, EIP-712 on Base — never transmitted
- **Non-custodial**: BlockRun never holds your funds
- **On-chain verifiable**: All payments visible on [Solscan](https://solscan.io) (Solana) or [Basescan](https://basescan.org) (Base)
- Use dedicated wallets with small balances for API payments

**API-key rail**

- An API key **is** transmitted on every request — it authenticates you, so treat it like any bearer token: keep it out of source control, out of client-side code and out of logs
- Rotate or revoke on [/dashboard/keys](https://user.blockrun.ai/dashboard/keys); mint separate keys for prod and staging so the activity log can tell them apart
- The `BLOCKRUN_API_KEY_URL` override exists so a key is never sent to a host configured for the x402 rail
- Credit is prepaid, so a leaked key is capped by the balance on the account, not by a card

**Both**

- Use environment variables, never hard-code credentials

## Requirements

- Go 1.22+
- One credential, either:
  - an API key from [user.blockrun.ai](https://user.blockrun.ai) — see [API Keys & Accounts](#api-keys--accounts-userblockrunai); or
  - a wallet with USDC on Solana (see [Pay on Solana](#pay-on-solana)) or on Base
- **No funds** are needed for the [free models](#try-it-free-no-balance-required) — they settle $0 on either rail. The SDK still wants a credential to build a client with: an unfunded wallet, or a key on a $0 account, both work

## FAQ

**What is blockrun-llm-go?**
The Go SDK for the whole BlockRun API — <!-- br:models.chatVisible -->76<!-- /br:models.chatVisible --> chat models, image, video, music, speech, voice calls, multi-chain RPC, web search, market data, prediction markets, DeFi and DEX data. Pay with an API key and prepaid credit, or with x402 micropayments from your own wallet. No subscriptions either way. The `-llm-` in the name is history, not scope.

**Do I need a crypto wallet?**
No. Sign up at [user.blockrun.ai](https://user.blockrun.ai), mint an API key, top up with a card, and `export BLOCKRUN_API_KEY=brk_live_…`. See [API Keys & Accounts](#api-keys--accounts-userblockrunai). A wallet is the other option, not a requirement.

**Where do I get an API key, and how do I add credit?**
Both on [user.blockrun.ai](https://user.blockrun.ai) — sign in with Google, mint a key on [/dashboard/keys](https://user.blockrun.ai/dashboard/keys) (shown once), top up on [/dashboard/credits](https://user.blockrun.ai/dashboard/credits). Minimum $5. The 5.5% + $0.30 processing fee is charged once at purchase, never on a call.

**I already use a wallet. Does adding API-key support break anything?**
No. Nothing changes unless you set `BLOCKRUN_API_KEY` — and passing a wallet key to a constructor explicitly always opts back out. `client.PaymentMode()` tells you which rail you are on.

**Does it support Solana?**
Yes, and Solana is the recommended chain: settlement is sub-second and BlockRun's facilitator pays the fee, so you hold no SOL. Every client has a `NewXClientSolana` counterpart — same API, same responses. See [Pay on Solana](#pay-on-solana). Base works identically and is still what the bare `NewLLMClient` constructor uses.

**How much does it cost?**
Pay only for what you use. <!-- br:models.free -->7<!-- /br:models.free --> NVIDIA-hosted models are completely free (DeepSeek V4 Pro/Flash, Nemotron Nano Omni vision, Qwen3, Llama 4, GLM-4.7, Mistral). $5 of credit or USDC gets you thousands of paid-model requests.

**Is streaming supported?**
Yes. Use `ChatCompletionStream` for SSE streaming — on both rails.

## Links

- [Website](https://blockrun.ai)
- [Sign up / dashboard / API keys](https://user.blockrun.ai)
- [Documentation](https://github.com/BlockRunAI/awesome-blockrun/tree/main/docs)
- [Python SDK](https://github.com/blockrunai/blockrun-llm)
- [TypeScript SDK](https://github.com/blockrunai/blockrun-llm-ts)
- [GitHub](https://github.com/blockrunai/blockrun-llm-go)
- [Telegram](https://t.me/+mroQv4-4hGgzOGUx)

## License

MIT
