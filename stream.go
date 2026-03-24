package blockrun

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ChatCompletionChunk represents a single SSE chunk from a streaming response.
type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
}

// ChunkChoice represents a single choice within a streaming chunk.
type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason string     `json:"finish_reason,omitempty"`
}

// ChunkDelta represents the incremental content in a streaming chunk.
type ChunkDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// Stream reads SSE chunks from an active connection.
type Stream struct {
	scanner *bufio.Scanner
	body    io.ReadCloser
	done    bool
}

// Next returns the next chunk. Returns nil, nil when the stream is complete.
func (s *Stream) Next() (*ChatCompletionChunk, error) {
	if s.done {
		return nil, nil
	}

	for s.scanner.Scan() {
		line := s.scanner.Text()

		// Skip empty lines
		if line == "" {
			continue
		}

		// Skip lines that don't start with "data: "
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// Check for stream termination
		if data == "[DONE]" {
			s.done = true
			return nil, nil
		}

		// Unmarshal the JSON chunk
		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, fmt.Errorf("failed to unmarshal chunk: %w", err)
		}

		return &chunk, nil
	}

	if err := s.scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	// Scanner exhausted without [DONE]
	s.done = true
	return nil, nil
}

// Close closes the underlying connection.
func (s *Stream) Close() error {
	s.done = true
	return s.body.Close()
}

// ChatCompletionStream sends a streaming chat completion request and returns a Stream.
//
// The caller must call Stream.Close() when done reading to release the connection.
func (c *LLMClient) ChatCompletionStream(ctx context.Context, model string, messages []ChatMessage, opts *ChatCompletionOptions) (*Stream, error) {
	// Validate inputs
	if model == "" {
		return nil, &ValidationError{Field: "model", Message: "Model is required"}
	}
	if len(messages) == 0 {
		return nil, &ValidationError{Field: "messages", Message: "At least one message is required"}
	}

	// Build request body
	body := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   true,
	}

	// Apply options
	maxTokens := DefaultMaxTokens
	if opts != nil {
		if opts.MaxTokens > 0 {
			maxTokens = opts.MaxTokens
		}
		if opts.Temperature > 0 {
			body["temperature"] = opts.Temperature
		}
		if opts.TopP > 0 {
			body["top_p"] = opts.TopP
		}
		if opts.SearchParameters != nil {
			body["search_parameters"] = opts.SearchParameters
		} else if opts.Search {
			body["search_parameters"] = map[string]string{"mode": "on"}
		}
		if opts.Tools != nil {
			body["tools"] = opts.Tools
		}
		if opts.ToolChoice != nil {
			body["tool_choice"] = opts.ToolChoice
		}
	}
	body["max_tokens"] = maxTokens

	url := c.apiURL + "/v1/chat/completions"

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request body: %w", err)
	}

	// First attempt
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// Handle 402 Payment Required
	if resp.StatusCode == http.StatusPaymentRequired {
		defer resp.Body.Close()
		return c.handleStreamPaymentAndRetry(ctx, url, jsonBody, resp)
	}

	// Handle other errors
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("API error: %s", string(bodyBytes)),
		}
	}

	return &Stream{
		scanner: bufio.NewScanner(resp.Body),
		body:    resp.Body,
	}, nil
}

// handleStreamPaymentAndRetry handles a 402 response for streaming requests.
func (c *LLMClient) handleStreamPaymentAndRetry(ctx context.Context, url string, jsonBody []byte, resp *http.Response) (*Stream, error) {
	// Get payment required header
	paymentHeader := resp.Header.Get("payment-required")
	if paymentHeader == "" {
		// Try to get from response body
		var respBody map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&respBody); err == nil {
			if _, ok := respBody["x402"]; ok {
				jsonBytes, _ := json.Marshal(respBody)
				paymentHeader = string(jsonBytes)
			}
		}
	}

	if paymentHeader == "" {
		return nil, &PaymentError{Message: "402 response but no payment requirements found"}
	}

	// Parse payment requirements
	paymentReq, err := ParsePaymentRequired(paymentHeader)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to parse payment requirements: %v", err)}
	}

	// Extract payment details
	paymentOption, err := ExtractPaymentDetails(paymentReq)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to extract payment details: %v", err)}
	}

	// Determine resource URL
	resourceURL := paymentReq.Resource.URL
	if resourceURL == "" {
		resourceURL = url
	}

	// Create signed payment payload
	paymentPayload, err := CreatePaymentPayload(
		c.privateKey,
		paymentOption.PayTo,
		paymentOption.Amount,
		paymentOption.Network,
		resourceURL,
		paymentReq.Resource.Description,
		paymentOption.MaxTimeoutSeconds,
		paymentOption.Extra,
		paymentReq.Extensions,
	)
	if err != nil {
		return nil, &PaymentError{Message: fmt.Sprintf("Failed to create payment: %v", err)}
	}

	// Retry with payment signature
	retryReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create retry request: %w", err)
	}
	retryReq.Header.Set("Content-Type", "application/json")
	retryReq.Header.Set("Accept", "text/event-stream")
	retryReq.Header.Set("PAYMENT-SIGNATURE", paymentPayload)

	retryResp, err := c.httpClient.Do(retryReq)
	if err != nil {
		return nil, fmt.Errorf("retry request failed: %w", err)
	}

	// Check for payment rejection
	if retryResp.StatusCode == http.StatusPaymentRequired {
		retryResp.Body.Close()
		return nil, &PaymentError{Message: "Payment was rejected. Check your wallet balance."}
	}

	// Handle other errors
	if retryResp.StatusCode != http.StatusOK {
		defer retryResp.Body.Close()
		bodyBytes, _ := io.ReadAll(retryResp.Body)
		return nil, &APIError{
			StatusCode: retryResp.StatusCode,
			Message:    fmt.Sprintf("API error after payment: %s", string(bodyBytes)),
		}
	}

	// Track spending
	c.mu.Lock()
	c.sessionCalls++
	var costUSD float64
	if amountStr := paymentOption.Amount; amountStr != "" {
		var amountMicro float64
		if _, err := fmt.Sscanf(amountStr, "%f", &amountMicro); err == nil {
			costUSD = amountMicro / 1_000_000
			c.sessionTotalUSD += costUSD
		}
	}
	c.mu.Unlock()

	if c.costLog != nil && costUSD > 0 {
		endpoint := strings.TrimPrefix(url, c.apiURL)
		c.costLog.Append(endpoint, costUSD)
	}

	return &Stream{
		scanner: bufio.NewScanner(retryResp.Body),
		body:    retryResp.Body,
	}, nil
}
