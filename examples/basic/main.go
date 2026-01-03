// Example usage of the BlockRun LLM Go SDK.
//
// To run this example:
//   export BASE_CHAIN_WALLET_KEY="0x..."
//   go run main.go
package main

import (
	"fmt"
	"log"

	blockrun "github.com/BlockRunAI/blockrun-llm-go"
)

func main() {
	// Create client (uses BASE_CHAIN_WALLET_KEY env var)
	client, err := blockrun.NewLLMClient("")
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	fmt.Printf("Wallet address: %s\n\n", client.GetWalletAddress())

	// Example 1: Simple 1-line chat
	fmt.Println("=== Example 1: Simple Chat ===")
	response, err := client.Chat("openai/gpt-4o-mini", "What is 2+2? Reply with just the number.")
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
	fmt.Printf("Response: %s\n\n", response)

	// Example 2: Chat with system prompt
	fmt.Println("=== Example 2: Chat with System Prompt ===")
	response, err = client.ChatWithSystem(
		"openai/gpt-4o-mini",
		"Tell me a joke",
		"You are a comedian who only tells programming jokes.",
	)
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
	fmt.Printf("Response: %s\n\n", response)

	// Example 3: Full chat completion with options
	fmt.Println("=== Example 3: Full Chat Completion ===")
	messages := []blockrun.ChatMessage{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "What's the capital of France?"},
	}

	result, err := client.ChatCompletion("openai/gpt-4o-mini", messages, &blockrun.ChatCompletionOptions{
		MaxTokens:   100,
		Temperature: 0.7,
	})
	if err != nil {
		log.Fatalf("ChatCompletion failed: %v", err)
	}
	fmt.Printf("Response: %s\n", result.Choices[0].Message.Content)
	fmt.Printf("Usage: %d prompt + %d completion = %d total tokens\n\n",
		result.Usage.PromptTokens,
		result.Usage.CompletionTokens,
		result.Usage.TotalTokens,
	)

	// Example 4: List available models
	fmt.Println("=== Example 4: List Models ===")
	models, err := client.ListModels()
	if err != nil {
		log.Fatalf("ListModels failed: %v", err)
	}
	fmt.Printf("Available models (%d):\n", len(models))
	for i, model := range models {
		if i >= 5 {
			fmt.Printf("  ... and %d more\n", len(models)-5)
			break
		}
		fmt.Printf("  - %s: $%.4f/$%.4f per 1M tokens\n",
			model.ID, model.InputPrice, model.OutputPrice)
	}
}
