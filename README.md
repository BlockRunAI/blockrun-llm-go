# BlockRun LLM Go SDK

Go SDK for [BlockRun](https://blockrun.ai) - access multiple LLM providers (OpenAI, Anthropic, Google, etc.) with automatic x402 micropayments on Base chain.

## Installation

```bash
go get github.com/blockrun/blockrun-llm-go
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

## Security

**Your private key NEVER leaves your machine.**

Here's what happens when you make a request:

1. Your key stays local - only used to sign an EIP-712 typed data message
2. Only the SIGNATURE is sent in the `PAYMENT-SIGNATURE` header
3. BlockRun verifies the signature on-chain via Coinbase CDP facilitator
4. Your actual private key is NEVER transmitted to any server

This is the same security model as:
- Signing a MetaMask transaction
- Any on-chain swap or trade
- Standard EIP-3009 TransferWithAuthorization

## Usage

### Initialize Client

```go
// From environment variable (BASE_CHAIN_WALLET_KEY)
client, err := blockrun.NewLLMClient("")

// Or pass private key directly
client, err := blockrun.NewLLMClient("0x...")

// With custom options
client, err := blockrun.NewLLMClient("0x...",
    blockrun.WithAPIURL("https://custom.api.url"),
    blockrun.WithTimeout(120 * time.Second),
)
```

### Simple Chat

```go
// Basic chat
response, err := client.Chat("openai/gpt-4o", "What is the capital of France?")

// Chat with system prompt
response, err := client.ChatWithSystem(
    "openai/gpt-4o",
    "Tell me a joke",
    "You are a comedian.",
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
```

## Available Models

BlockRun provides access to models from multiple providers:

| Provider | Models |
|----------|--------|
| OpenAI | gpt-4o, gpt-4o-mini, o1, o1-mini |
| Anthropic | claude-sonnet-4, claude-haiku-4 |
| Google | gemini-2.5-pro, gemini-2.5-flash |
| DeepSeek | deepseek-chat, deepseek-reasoner |
| xAI | grok-3, grok-3-mini |

Use `client.ListModels()` for the full list with current pricing.

## How x402 Works

1. You make an API request
2. Server returns `402 Payment Required` with payment details
3. SDK signs an EIP-712 message locally (key never sent)
4. SDK retries with `PAYMENT-SIGNATURE` header
5. Server verifies signature, settles payment on-chain
6. Server returns the AI response

All this happens automatically - you just call `Chat()` or `ChatCompletion()`.

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `BASE_CHAIN_WALLET_KEY` | Your Base chain wallet private key | Required |
| `BLOCKRUN_API_URL` | Custom API endpoint | `https://blockrun.ai/api` |

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

## Requirements

- Go 1.21+
- A wallet with USDC on Base chain

## License

MIT
