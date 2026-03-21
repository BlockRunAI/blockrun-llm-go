# BlockRun LLM Go SDK

> **blockrun-llm-go** is a Go SDK for accessing 40+ large language models (GPT-5, Claude, Gemini, Grok, DeepSeek, Kimi, and more) with automatic pay-per-request USDC micropayments via the x402 protocol on Base chain. No API keys required — your wallet signature is your authentication. Built for Go developers building autonomous AI agents.

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
    "fmt"
    "log"

    blockrun "github.com/blockrun/blockrun-llm-go"
)

func main() {
    // Create client (uses BASE_CHAIN_WALLET_KEY env var)
    client, err := blockrun.NewLLMClient("")
    if err != nil {
        log.Fatal(err)
    }

    // Simple 1-line chat
    response, err := client.Chat("openai/gpt-4o", "What is 2+2?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(response)
}
```

That's it. The SDK handles x402 payment automatically.

## How It Works

1. You send a request to BlockRun's API
2. The API returns a 402 Payment Required with the price
3. The SDK automatically signs a USDC payment on Base (EIP-712 typed data)
4. The request is retried with the payment proof
5. You receive the AI response

**Your private key never leaves your machine** — it's only used for local EIP-712 signing. This is the same security model as signing a MetaMask transaction.

## Usage Examples

### Initialize Client

```go
// From environment variable (BASE_CHAIN_WALLET_KEY)
client, err := blockrun.NewLLMClient("")

// Or pass private key directly
client, err := blockrun.NewLLMClient("0x...")

// With custom timeout (default: 60s)
client, err := blockrun.NewLLMClient("0x...",
    blockrun.WithTimeout(120 * time.Second),
)
```

### Simple Chat

```go
// Basic chat with any model
response, err := client.Chat("openai/gpt-5.2", "Explain quantum computing")

// Use Codex for coding tasks
response, err := client.Chat("openai/gpt-5.2-codex", "Write a binary search in Go")

// Chat with system prompt
response, err := client.ChatWithSystem(
    "anthropic/claude-opus-4.6",
    "Explain quantum computing",
    "You are a physics professor.",
)
```

### Full Chat Completion (OpenAI-compatible)

```go
messages := []blockrun.ChatMessage{
    {Role: "system", Content: "You are a helpful assistant."},
    {Role: "user", Content: "Hello!"},
}

result, err := client.ChatCompletion("openai/gpt-4o", messages, &blockrun.ChatCompletionOptions{
    MaxTokens:   1024,
    Temperature: 0.7,
    TopP:        0.9,
})

fmt.Println(result.Choices[0].Message.Content)
fmt.Printf("Tokens: %d\n", result.Usage.TotalTokens)
```

### List Available Models

```go
models, err := client.ListModels()
for _, model := range models {
    fmt.Printf("%s: $%.4f/$%.4f per 1M tokens\n",
        model.ID, model.InputPrice, model.OutputPrice)
}
```

### Get Wallet Address

```go
address := client.GetWalletAddress()
fmt.Printf("Wallet: %s\n", address)
fmt.Printf("View transactions: https://basescan.org/address/%s\n", address)
```

## Available Models

BlockRun provides access to 40+ models from 10 providers through a single OpenAI-compatible endpoint.

### Featured Models

| Provider | Models | Input $/M | Output $/M |
|----------|--------|-----------|------------|
| **OpenAI** | GPT-5.2, GPT-5.2 Codex, GPT-5 Mini, GPT-4o, GPT-4o-mini | $0.05–$21.00 | $0.40–$168.00 |
| **Anthropic** | Claude Opus 4.6, Claude Sonnet 4.6, Claude Haiku 4.5 | $1.00–$5.00 | $5.00–$25.00 |
| **Google** | Gemini 3.1 Pro, Gemini 2.5 Pro, Gemini 2.5 Flash | $0.10–$2.00 | $0.40–$12.00 |
| **xAI** | Grok 4.1 Fast, Grok 3, Grok Code Fast 1 | $0.20–$3.00 | $0.50–$15.00 |
| **DeepSeek** | DeepSeek Chat, DeepSeek Reasoner | $0.28 | $0.42 |
| **Moonshot** | Kimi K2.5 (262K context) | $0.60 | $3.00 |
| **NVIDIA** | GPT-OSS 120B | **FREE** | **FREE** |

Use `client.ListModels()` for the full list with current pricing.

## Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `BASE_CHAIN_WALLET_KEY` | Your Base chain wallet private key | Yes (or pass to constructor) |
| `BLOCKRUN_API_URL` | Custom API endpoint | No (default: https://blockrun.ai/api) |

## Setting Up Your Wallet

1. Create a wallet on Base network (Coinbase Wallet, MetaMask, etc.)
2. Get some ETH on Base for gas (small amount, ~$1)
3. Get USDC on Base for API payments
4. Export your private key and set it as `BASE_CHAIN_WALLET_KEY`

```bash
export BASE_CHAIN_WALLET_KEY=0x...your_private_key_here
```

## Error Handling

```go
response, err := client.Chat("openai/gpt-4o", "Hello")
if err != nil {
    switch e := err.(type) {
    case *blockrun.ValidationError:
        fmt.Printf("Invalid input: %s - %s\n", e.Field, e.Message)
    case *blockrun.PaymentError:
        fmt.Printf("Payment failed: %s\n", e.Message)
    case *blockrun.APIError:
        fmt.Printf("API error %d: %s\n", e.StatusCode, e.Message)
    default:
        fmt.Printf("Error: %v\n", err)
    }
}
```

## Security

### Private Key Safety

- **Private key stays local**: Your key is only used for EIP-712 signing on your machine
- **No custody**: BlockRun never holds your funds — settlement is non-custodial
- **Verify transactions**: All payments are on-chain and verifiable on [Basescan](https://basescan.org)

### Best Practices

- Use environment variables, never hard-code keys
- Use dedicated wallets for API payments (separate from main holdings)
- Set spending limits by only funding payment wallets with small amounts
- Never commit private keys to version control

## Requirements

- Go 1.21+
- A wallet with USDC on Base chain

## Frequently Asked Questions

### What is blockrun-llm-go?
blockrun-llm-go is a Go SDK that provides pay-per-request access to 40+ large language models from OpenAI, Anthropic, Google, xAI, DeepSeek, Moonshot, and more. It uses the x402 protocol for automatic USDC micropayments — no API keys, no subscriptions, no vendor lock-in.

### How does payment work?
When you call `Chat()` or `ChatCompletion()`, the SDK automatically handles x402 payment. It signs an EIP-712 typed data message locally using your wallet private key (which never leaves your machine), and includes the signature in the request header. Settlement is non-custodial and instant on Base chain.

### How much does it cost?
Pay only for what you use. Prices start at $0/request (NVIDIA GPT-OSS 120B is free). There are no minimums, subscriptions, or monthly fees. $5 in USDC gets you thousands of requests.

### Does it support Solana?
The Go SDK currently supports Base chain only. For Solana support, use the [Python SDK](https://github.com/blockrunai/blockrun-llm) or [TypeScript SDK](https://github.com/blockrunai/blockrun-llm-ts).

## Links

- [Website](https://blockrun.ai)
- [Documentation](https://github.com/BlockRunAI/awesome-blockrun/tree/main/docs)
- [Python SDK](https://github.com/blockrunai/blockrun-llm)
- [TypeScript SDK](https://github.com/blockrunai/blockrun-llm-ts)
- [GitHub](https://github.com/blockrunai/blockrun-llm-go)
- [Telegram](https://t.me/+mroQv4-4hGgzOGUx)

## License

MIT
