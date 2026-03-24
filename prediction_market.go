package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// PM fetches prediction market data via GET request.
//
// path identifies the prediction market resource (e.g., "polymarket/events").
// params are optional query parameters appended to the URL.
func (c *LLMClient) PM(ctx context.Context, path string, params map[string]string) (map[string]any, error) {
	if path == "" {
		return nil, &ValidationError{Field: "path", Message: "Path is required"}
	}

	endpoint := "/v1/pm/" + path

	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		endpoint += "?" + q.Encode()
	}

	respBytes, err := c.doGet(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

// PMQuery sends a prediction market query via POST request.
//
// path identifies the prediction market resource (e.g., "polymarket/search").
// query is marshaled directly as the JSON request body.
func (c *LLMClient) PMQuery(ctx context.Context, path string, query any) (map[string]any, error) {
	if path == "" {
		return nil, &ValidationError{Field: "path", Message: "Path is required"}
	}

	// Marshal the query into a map[string]any for doRequest
	queryBytes, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to encode query: %w", err)
	}

	var body map[string]any
	if err := json.Unmarshal(queryBytes, &body); err != nil {
		return nil, fmt.Errorf("failed to decode query as object: %w", err)
	}

	endpoint := "/v1/pm/" + path

	respBytes, err := c.doRequest(ctx, endpoint, body)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}
