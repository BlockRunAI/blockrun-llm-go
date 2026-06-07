package blockrun

// Exa Web Search — POST /v1/exa/{path}.
//
// Neural + keyword web search, similarity search, content extraction, and
// grounded answers, powered by Exa. $0.01/request (contents $0.002/URL).
// Methods live on *LLMClient and pay automatically via x402.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Exa calls an Exa endpoint with a raw JSON body (generic escape hatch).
//
// path is one of: "search", "find-similar", "contents", "answer".
func (c *LLMClient) Exa(ctx context.Context, path string, body map[string]any) (map[string]any, error) {
	if strings.TrimSpace(path) == "" {
		return nil, &ValidationError{Field: "path", Message: "Path is required"}
	}

	respBytes, err := c.doRequest(ctx, "/v1/exa/"+path, body)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

// ExaSearch runs a neural/keyword web search ($0.01/request).
//
// extra carries additional Exa parameters (numResults, category,
// useAutoprompt, ...); pass nil when unneeded.
func (c *LLMClient) ExaSearch(ctx context.Context, query string, extra map[string]any) (map[string]any, error) {
	if strings.TrimSpace(query) == "" {
		return nil, &ValidationError{Field: "query", Message: "Query is required"}
	}
	body := map[string]any{"query": query}
	for k, v := range extra {
		body[k] = v
	}
	return c.Exa(ctx, "search", body)
}

// ExaFindSimilar finds pages semantically similar to a URL ($0.01/request).
func (c *LLMClient) ExaFindSimilar(ctx context.Context, pageURL string, extra map[string]any) (map[string]any, error) {
	if strings.TrimSpace(pageURL) == "" {
		return nil, &ValidationError{Field: "url", Message: "URL is required"}
	}
	body := map[string]any{"url": pageURL}
	for k, v := range extra {
		body[k] = v
	}
	return c.Exa(ctx, "find-similar", body)
}

// ExaContents extracts full text content from URLs ($0.002/URL).
func (c *LLMClient) ExaContents(ctx context.Context, urls []string, extra map[string]any) (map[string]any, error) {
	if len(urls) == 0 {
		return nil, &ValidationError{Field: "urls", Message: "At least one URL is required"}
	}
	body := map[string]any{"urls": urls}
	for k, v := range extra {
		body[k] = v
	}
	return c.Exa(ctx, "contents", body)
}

// ExaAnswer returns an AI answer grounded in live web search ($0.01/request).
func (c *LLMClient) ExaAnswer(ctx context.Context, query string, extra map[string]any) (map[string]any, error) {
	if strings.TrimSpace(query) == "" {
		return nil, &ValidationError{Field: "query", Message: "Query is required"}
	}
	body := map[string]any{"query": query}
	for k, v := range extra {
		body[k] = v
	}
	return c.Exa(ctx, "answer", body)
}
