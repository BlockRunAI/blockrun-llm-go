package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
)

// SearchOptions contains optional parameters for the Search endpoint.
type SearchOptions struct {
	// Sources are "web" (the default) and "news". sol.blockrun.ai also takes "x",
	// at a higher flat price, which sets the price of the whole call. blockrun.ai
	// refuses "x" with a 400 before quoting: on Base, search X in chat on an xai/*
	// model with an x_search tool.
	Sources    []string `json:"sources,omitempty"`
	MaxResults int      `json:"max_results,omitempty"` // max results to return
	FromDate   string   `json:"from_date,omitempty"`   // YYYY-MM-DD
	ToDate     string   `json:"to_date,omitempty"`     // YYYY-MM-DD
}

// SearchResult represents the response from the Search endpoint.
type SearchResult struct {
	Query   string `json:"query"`
	Summary string `json:"summary"`
	// Citations are the cited URLs.
	Citations []string `json:"-"`
	// CitationSources are the same citations with their titles.
	CitationSources []SearchCitation `json:"citations,omitempty"`
	SourcesUsed     int              `json:"sources_used"`
	Model           string           `json:"model,omitempty"`
}

// SearchCitation is one citation in a search result.
type SearchCitation struct {
	URL string `json:"url"`
	// Title is the page title, or the URL's host when the gateway has none.
	Title string `json:"title,omitempty"`
}

// UnmarshalJSON accepts citations as objects, which both gateways send now, or
// as bare URLs, which they used to. The search has been paid for by the time this
// runs, so an entry it cannot read is skipped rather than failing the whole call.
func (r *SearchResult) UnmarshalJSON(data []byte) error {
	type plain SearchResult
	aux := struct {
		*plain
		Citations []json.RawMessage `json:"citations"`
	}{plain: (*plain)(r)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	r.Citations, r.CitationSources = nil, nil
	for _, raw := range aux.Citations {
		var citation SearchCitation
		var url string
		if err := json.Unmarshal(raw, &url); err == nil {
			citation.URL = url
		} else if err := json.Unmarshal(raw, &citation); err != nil {
			continue
		}
		if citation.URL == "" {
			continue
		}
		r.Citations = append(r.Citations, citation.URL)
		r.CitationSources = append(r.CitationSources, citation)
	}
	return nil
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
		return nil, fmt.Errorf("failed to decode search response, and the call may already be paid, so check your wallet before retrying: %w", err)
	}

	return &result, nil
}
