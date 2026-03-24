package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
)

// SearchOptions contains optional parameters for the Search endpoint.
type SearchOptions struct {
	Sources    []string `json:"sources,omitempty"`     // e.g. ["web", "x", "news"]
	MaxResults int      `json:"max_results,omitempty"` // max results to return
	FromDate   string   `json:"from_date,omitempty"`   // YYYY-MM-DD
	ToDate     string   `json:"to_date,omitempty"`     // YYYY-MM-DD
}

// SearchResult represents the response from the Search endpoint.
type SearchResult struct {
	Query       string   `json:"query"`
	Summary     string   `json:"summary"`
	Citations   []string `json:"citations,omitempty"`
	SourcesUsed int      `json:"sources_used"`
	Model       string   `json:"model,omitempty"`
}

// Search performs a standalone web/X/news search query.
func (c *LLMClient) Search(ctx context.Context, query string, opts *SearchOptions) (*SearchResult, error) {
	if query == "" {
		return nil, &ValidationError{Field: "query", Message: "Query is required"}
	}

	body := map[string]any{
		"query": query,
	}

	if opts != nil {
		if len(opts.Sources) > 0 {
			body["sources"] = opts.Sources
		}
		if opts.MaxResults > 0 {
			body["max_results"] = opts.MaxResults
		}
		if opts.FromDate != "" {
			body["from_date"] = opts.FromDate
		}
		if opts.ToDate != "" {
			body["to_date"] = opts.ToDate
		}
	}

	respBytes, err := c.doRequest(ctx, "/v1/search", body)
	if err != nil {
		return nil, err
	}

	var result SearchResult
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	return &result, nil
}
