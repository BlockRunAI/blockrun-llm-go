// Example usage of the BlockRun LLM Go SDK.
//
// To run this example:
//
//	export BASE_CHAIN_WALLET_KEY="0x..."
//	go run main.go
package main

import (
	"context"
	"fmt"
	"log"

	blockrun "github.com/BlockRunAI/blockrun-llm-go"
)

func main() {
	ctx := context.Background()

	// Create client (uses BASE_CHAIN_WALLET_KEY env var)
	client, err := blockrun.NewLLMClient("")
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	fmt.Printf("Wallet address: %s\n\n", client.GetWalletAddress())

	// Example 1: Simple 1-line chat
	fmt.Println("=== Simple Chat ===")
	response, err := client.Chat(ctx, "openai/gpt-4o-mini", "What is 2+2? Reply with just the number.")
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
	fmt.Printf("Response: %s\n\n", response)

	// Example 2: Chat with system prompt
	fmt.Println("=== Chat with System Prompt ===")
	response, err = client.ChatWithSystem(ctx, "openai/gpt-4o-mini", "Tell me a joke", "You are a comedian who only tells programming jokes.")
	if err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
	fmt.Printf("Response: %s\n\n", response)

	// Example 3: Full chat completion with options
	fmt.Println("=== Full Chat Completion ===")
	messages := []blockrun.ChatMessage{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "What's the capital of France?"},
	}
	result, err := client.ChatCompletion(ctx, "openai/gpt-4o-mini", messages, &blockrun.ChatCompletionOptions{
		MaxTokens:   100,
		Temperature: 0.7,
	})
	if err != nil {
		log.Fatalf("ChatCompletion failed: %v", err)
	}
	fmt.Printf("Response: %s\n", result.Choices[0].Message.Content)
	fmt.Printf("Tokens: %d prompt + %d completion = %d total\n\n", result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.TotalTokens)

	// Example 4: Smart routing (auto-selects best model)
	fmt.Println("=== Smart Chat (Auto-Routing) ===")
	smart, err := client.SmartChat(ctx, "Write a haiku about Go programming", nil)
	if err != nil {
		log.Fatalf("SmartChat failed: %v", err)
	}
	fmt.Printf("Model: %s (tier: %s)\n", smart.Model, smart.Routing.Tier)
	fmt.Printf("Response: %s\n\n", smart.Response)

	// Example 5: Streaming
	fmt.Println("=== Streaming ===")
	stream, err := client.ChatCompletionStream(ctx, "openai/gpt-4o-mini", []blockrun.ChatMessage{
		{Role: "user", Content: "Count from 1 to 5"},
	}, nil)
	if err != nil {
		log.Fatalf("Stream failed: %v", err)
	}
	defer stream.Close()
	for {
		chunk, err := stream.Next()
		if err != nil {
			log.Fatalf("Stream error: %v", err)
		}
		if chunk == nil {
			break
		}
		if len(chunk.Choices) > 0 {
			fmt.Print(chunk.Choices[0].Delta.Content)
		}
	}
	fmt.Println()

	// Example 6: X/Twitter data
	fmt.Println("=== X/Twitter Trending ===")
	trending, err := client.XTrending(ctx)
	if err != nil {
		log.Printf("XTrending: %v", err)
	} else {
		fmt.Printf("Trending topics: %d\n\n", len(trending.Topics))
	}

	// Example 7: Search
	fmt.Println("=== Web Search ===")
	searchResult, err := client.Search(ctx, "latest Go 1.23 features", nil)
	if err != nil {
		log.Printf("Search: %v", err)
	} else {
		fmt.Printf("Summary: %s\n\n", searchResult.Summary)
	}

	// Example 8: Check balance and spending
	fmt.Println("=== Wallet Status ===")
	spending := client.GetSpending()
	fmt.Printf("Session: %d calls, $%.6f spent\n", spending.Calls, spending.TotalUSD)

	// Example 9: List models
	fmt.Println("=== Available Models ===")
	models, err := client.ListModels(ctx)
	if err != nil {
		log.Fatalf("ListModels failed: %v", err)
	}
	fmt.Printf("%d models available\n", len(models))
	for i, m := range models {
		if i >= 5 {
			fmt.Printf("  ... and %d more\n", len(models)-5)
			break
		}
		fmt.Printf("  - %s: $%.4f/$%.4f per 1M tokens\n", m.ID, m.InputPrice, m.OutputPrice)
	}
}
