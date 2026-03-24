package blockrun

import (
	"context"
	"encoding/json"
	"fmt"
)

// XUserLookup looks up multiple X/Twitter users by username.
func (c *LLMClient) XUserLookup(ctx context.Context, usernames []string) (*XUserLookupResponse, error) {
	if len(usernames) == 0 {
		return nil, &ValidationError{Field: "usernames", Message: "At least one username is required"}
	}

	body := map[string]any{
		"usernames": usernames,
	}

	respBytes, err := c.doRequest(ctx, "/v1/x/users/lookup", body)
	if err != nil {
		return nil, err
	}

	var result XUserLookupResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// XFollowers returns the followers of an X/Twitter user.
func (c *LLMClient) XFollowers(ctx context.Context, username, cursor string) (*XFollowersResponse, error) {
	if username == "" {
		return nil, &ValidationError{Field: "username", Message: "Username is required"}
	}

	body := map[string]any{
		"username": username,
	}
	if cursor != "" {
		body["cursor"] = cursor
	}

	respBytes, err := c.doRequest(ctx, "/v1/x/users/followers", body)
	if err != nil {
		return nil, err
	}

	var result XFollowersResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// XFollowings returns the users that an X/Twitter user follows.
func (c *LLMClient) XFollowings(ctx context.Context, username, cursor string) (*XFollowersResponse, error) {
	if username == "" {
		return nil, &ValidationError{Field: "username", Message: "Username is required"}
	}

	body := map[string]any{
		"username": username,
	}
	if cursor != "" {
		body["cursor"] = cursor
	}

	respBytes, err := c.doRequest(ctx, "/v1/x/users/followings", body)
	if err != nil {
		return nil, err
	}

	var result XFollowersResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// XUserInfo returns detailed information about a single X/Twitter user.
func (c *LLMClient) XUserInfo(ctx context.Context, username string) (*XUserInfoResponse, error) {
	if username == "" {
		return nil, &ValidationError{Field: "username", Message: "Username is required"}
	}

	body := map[string]any{
		"username": username,
	}

	respBytes, err := c.doRequest(ctx, "/v1/x/users/info", body)
	if err != nil {
		return nil, err
	}

	var result XUserInfoResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// XVerifiedFollowers returns verified followers of an X/Twitter user by user ID.
func (c *LLMClient) XVerifiedFollowers(ctx context.Context, userID, cursor string) (*XFollowersResponse, error) {
	if userID == "" {
		return nil, &ValidationError{Field: "userId", Message: "User ID is required"}
	}

	body := map[string]any{
		"userId": userID,
	}
	if cursor != "" {
		body["cursor"] = cursor
	}

	respBytes, err := c.doRequest(ctx, "/v1/x/users/verified-followers", body)
	if err != nil {
		return nil, err
	}

	var result XFollowersResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// XUserTweets returns tweets from an X/Twitter user.
func (c *LLMClient) XUserTweets(ctx context.Context, username string, includeReplies bool, cursor string) (*XTweetsResponse, error) {
	if username == "" {
		return nil, &ValidationError{Field: "username", Message: "Username is required"}
	}

	body := map[string]any{
		"username":       username,
		"includeReplies": includeReplies,
	}
	if cursor != "" {
		body["cursor"] = cursor
	}

	respBytes, err := c.doRequest(ctx, "/v1/x/users/tweets", body)
	if err != nil {
		return nil, err
	}

	var result XTweetsResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// XUserMentions returns mentions of an X/Twitter user.
func (c *LLMClient) XUserMentions(ctx context.Context, username, sinceTime, untilTime, cursor string) (*XTweetsResponse, error) {
	if username == "" {
		return nil, &ValidationError{Field: "username", Message: "Username is required"}
	}

	body := map[string]any{
		"username": username,
	}
	if sinceTime != "" {
		body["sinceTime"] = sinceTime
	}
	if untilTime != "" {
		body["untilTime"] = untilTime
	}
	if cursor != "" {
		body["cursor"] = cursor
	}

	respBytes, err := c.doRequest(ctx, "/v1/x/users/mentions", body)
	if err != nil {
		return nil, err
	}

	var result XTweetsResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// XTweetLookup looks up multiple tweets by ID.
func (c *LLMClient) XTweetLookup(ctx context.Context, tweetIDs []string) (*XTweetLookupResponse, error) {
	if len(tweetIDs) == 0 {
		return nil, &ValidationError{Field: "tweet_ids", Message: "At least one tweet ID is required"}
	}

	body := map[string]any{
		"tweet_ids": tweetIDs,
	}

	respBytes, err := c.doRequest(ctx, "/v1/x/tweets/lookup", body)
	if err != nil {
		return nil, err
	}

	var result XTweetLookupResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// XTweetReplies returns replies to a tweet.
// queryType can be "Latest" or "Default".
func (c *LLMClient) XTweetReplies(ctx context.Context, tweetID, queryType, cursor string) (*XTweetsResponse, error) {
	if tweetID == "" {
		return nil, &ValidationError{Field: "tweetId", Message: "Tweet ID is required"}
	}

	body := map[string]any{
		"tweetId": tweetID,
	}
	if queryType != "" {
		body["queryType"] = queryType
	}
	if cursor != "" {
		body["cursor"] = cursor
	}

	respBytes, err := c.doRequest(ctx, "/v1/x/tweets/replies", body)
	if err != nil {
		return nil, err
	}

	var result XTweetsResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// XTweetThread returns the full thread for a tweet.
func (c *LLMClient) XTweetThread(ctx context.Context, tweetID, cursor string) (*XTweetsResponse, error) {
	if tweetID == "" {
		return nil, &ValidationError{Field: "tweetId", Message: "Tweet ID is required"}
	}

	body := map[string]any{
		"tweetId": tweetID,
	}
	if cursor != "" {
		body["cursor"] = cursor
	}

	respBytes, err := c.doRequest(ctx, "/v1/x/tweets/thread", body)
	if err != nil {
		return nil, err
	}

	var result XTweetsResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// XSearch searches X/Twitter for tweets matching a query.
// queryType can be "Latest", "Top", or "Default".
func (c *LLMClient) XSearch(ctx context.Context, query, queryType, cursor string) (*XSearchResponse, error) {
	if query == "" {
		return nil, &ValidationError{Field: "query", Message: "Search query is required"}
	}

	body := map[string]any{
		"query": query,
	}
	if queryType != "" {
		body["queryType"] = queryType
	}
	if cursor != "" {
		body["cursor"] = cursor
	}

	respBytes, err := c.doRequest(ctx, "/v1/x/search", body)
	if err != nil {
		return nil, err
	}

	var result XSearchResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// XTrending returns current trending topics on X/Twitter.
func (c *LLMClient) XTrending(ctx context.Context) (*XTrendingResponse, error) {
	body := map[string]any{}

	respBytes, err := c.doRequest(ctx, "/v1/x/trending", body)
	if err != nil {
		return nil, err
	}

	var result XTrendingResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// XArticlesRising returns currently rising articles on X/Twitter.
func (c *LLMClient) XArticlesRising(ctx context.Context) (*XArticlesRisingResponse, error) {
	body := map[string]any{}

	respBytes, err := c.doRequest(ctx, "/v1/x/articles/rising", body)
	if err != nil {
		return nil, err
	}

	var result XArticlesRisingResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// XAuthorAnalytics returns analytics data for an X/Twitter author.
func (c *LLMClient) XAuthorAnalytics(ctx context.Context, handle string) (*XAuthorAnalyticsResponse, error) {
	if handle == "" {
		return nil, &ValidationError{Field: "handle", Message: "Handle is required"}
	}

	body := map[string]any{
		"handle": handle,
	}

	respBytes, err := c.doRequest(ctx, "/v1/x/authors", body)
	if err != nil {
		return nil, err
	}

	var result XAuthorAnalyticsResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// XCompareAuthors compares analytics between two X/Twitter authors.
func (c *LLMClient) XCompareAuthors(ctx context.Context, handle1, handle2 string) (*XCompareAuthorsResponse, error) {
	if handle1 == "" {
		return nil, &ValidationError{Field: "handle1", Message: "First handle is required"}
	}
	if handle2 == "" {
		return nil, &ValidationError{Field: "handle2", Message: "Second handle is required"}
	}

	body := map[string]any{
		"handle1": handle1,
		"handle2": handle2,
	}

	respBytes, err := c.doRequest(ctx, "/v1/x/compare", body)
	if err != nil {
		return nil, err
	}

	var result XCompareAuthorsResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}
