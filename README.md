# BlockRun LLM Go SDK

> **blockrun-llm-go** is a Go SDK for accessing 40+ large language models and AI services with automatic pay-per-request USDC micropayments via the x402 protocol on Base chain. No API keys required — your wallet signature is your authentication.

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
| **Image Generation** | DALL-E 3, Nano Banana, Flux models |
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
| **free** | nvidia/gpt-oss-120b | nvidia/deepseek-v3.2 | nvidia/qwen3-next-80b-a3b-thinking | nvidia/qwen3-next-80b-a3b-thinking |
| **eco** | moonshot/kimi-k2.5 | deepseek/deepseek-chat | google/gemini-2.5-pro | deepseek/deepseek-reasoner |
| **auto** | moonshot/kimi-k2.5 | google/gemini-2.5-flash | google/gemini-3.1-pro | deepseek/deepseek-reasoner |
| **premium** | google/gemini-2.5-flash | openai/gpt-5.4 | anthropic/claude-opus-4.5 | openai/o3 |

> NVIDIA free tier refreshed 2026-04-21. Retired IDs (`nvidia/nemotron-*`,
> `nvidia/mistral-large-3-675b`, `nvidia/devstral-2-123b`,
> `nvidia/qwen3.5-397b-a17b`, and paid `nvidia/kimi-k2.5`) still resolve via
> backend redirects but the routing table now points at the canonical
> successors.

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

Supported models include `openai/dall-e-3`, `openai/gpt-image-1`, `google/nano-banana`, `google/nano-banana-pro`, `zai/cogview-4`, `xai/grok-imagine-image` ($0.02/image), and `xai/grok-imagine-image-pro` ($0.07/image).

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

## Video Generation

Generate short AI videos with xAI's Grok Imagine Video at $0.05/sec (8s default → $0.42/clip).

```go
videoClient, err := blockrun.NewVideoClient("")

result, err := videoClient.Generate(ctx, "a red apple slowly spinning on a wooden table", nil)
fmt.Println(result.Data[0].URL)             // permanent MP4 URL
fmt.Println(result.Data[0].DurationSeconds) // 8

// Image-to-video
result, err = videoClient.Generate(ctx, "the subject turns and smiles", &blockrun.VideoGenerateOptions{
    ImageURL: "https://example.com/portrait.jpg",
})
```

The client blocks until the video is ready (30-120s typical) because the gateway handles the xAI polling internally.

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
| **OpenAI** | GPT-5.2, GPT-5.2 Codex, GPT-5 Mini, GPT-4o, GPT-4o-mini | $0.05–$21.00 | $0.40–$168.00 |
| **Anthropic** | Claude Opus 4.6, Claude Sonnet 4.6, Claude Haiku 4.5 | $1.00–$5.00 | $5.00–$25.00 |
| **Google** | Gemini 3.1 Pro, Gemini 2.5 Pro, Gemini 2.5 Flash | $0.10–$2.00 | $0.40–$12.00 |
| **xAI** | Grok 4.1 Fast, Grok 3, Grok Code Fast 1 | $0.20–$3.00 | $0.50–$15.00 |
| **DeepSeek** | DeepSeek Chat, DeepSeek Reasoner | $0.28 | $0.42 |
| **Moonshot** | Kimi K2.6 (256K, vision + reasoning) | $0.95 | $4.00 |
| **Moonshot** | Kimi K2.5 (262K context, legacy) | $0.60 | $3.00 |
| **NVIDIA** | GPT-OSS 120B | **FREE** | **FREE** |

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
Pay only for what you use. NVIDIA GPT-OSS 120B is free. $5 USDC gets you thousands of requests.

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
