package blockrun

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// helper to read and unmarshal request body from the mock server handler.
func readRequestBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("Failed to read request body: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal request body: %v", err)
	}
	return result
}

func TestXUserLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/users/lookup" {
			t.Errorf("Expected path /v1/x/users/lookup, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		body := readRequestBody(t, r)
		usernames, ok := body["usernames"].([]any)
		if !ok || len(usernames) != 2 {
			t.Errorf("Expected 2 usernames, got %v", body["usernames"])
		}

		json.NewEncoder(w).Encode(XUserLookupResponse{
			Users: []XUser{
				{ID: "1", Name: "Alice", Username: "alice"},
				{ID: "2", Name: "Bob", Username: "bob"},
			},
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XUserLookup(context.Background(), []string{"alice", "bob"})
	if err != nil {
		t.Fatalf("XUserLookup failed: %v", err)
	}
	if len(resp.Users) != 2 {
		t.Errorf("Expected 2 users, got %d", len(resp.Users))
	}
	if resp.Users[0].Username != "alice" {
		t.Errorf("Expected username alice, got %s", resp.Users[0].Username)
	}
}

func TestXUserLookupValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.XUserLookup(context.Background(), []string{})
	if err == nil {
		t.Error("Expected validation error for empty usernames")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Errorf("Expected ValidationError, got %T", err)
	}

	_, err = client.XUserLookup(context.Background(), nil)
	if err == nil {
		t.Error("Expected validation error for nil usernames")
	}
}

func TestXFollowers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/users/followers" {
			t.Errorf("Expected path /v1/x/users/followers, got %s", r.URL.Path)
		}

		body := readRequestBody(t, r)
		if body["username"] != "elonmusk" {
			t.Errorf("Expected username elonmusk, got %v", body["username"])
		}

		json.NewEncoder(w).Encode(XFollowersResponse{
			Users:         []XUser{{ID: "1", Username: "follower1"}},
			HasNextPage:   true,
			NextCursor:    "cursor123",
			TotalReturned: 1,
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XFollowers(context.Background(), "elonmusk", "")
	if err != nil {
		t.Fatalf("XFollowers failed: %v", err)
	}
	if !resp.HasNextPage {
		t.Error("Expected HasNextPage to be true")
	}
	if resp.NextCursor != "cursor123" {
		t.Errorf("Expected cursor cursor123, got %s", resp.NextCursor)
	}
}

func TestXFollowersValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.XFollowers(context.Background(), "", "")
	if err == nil {
		t.Error("Expected validation error for empty username")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestXFollowings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/users/followings" {
			t.Errorf("Expected path /v1/x/users/followings, got %s", r.URL.Path)
		}

		body := readRequestBody(t, r)
		if body["username"] != "testuser" {
			t.Errorf("Expected username testuser, got %v", body["username"])
		}
		if body["cursor"] != "next123" {
			t.Errorf("Expected cursor next123, got %v", body["cursor"])
		}

		json.NewEncoder(w).Encode(XFollowersResponse{
			Users:         []XUser{{ID: "10", Username: "following1"}},
			HasNextPage:   false,
			TotalReturned: 1,
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XFollowings(context.Background(), "testuser", "next123")
	if err != nil {
		t.Fatalf("XFollowings failed: %v", err)
	}
	if resp.HasNextPage {
		t.Error("Expected HasNextPage to be false")
	}
	if len(resp.Users) != 1 {
		t.Errorf("Expected 1 user, got %d", len(resp.Users))
	}
}

func TestXUserInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/users/info" {
			t.Errorf("Expected path /v1/x/users/info, got %s", r.URL.Path)
		}

		body := readRequestBody(t, r)
		if body["username"] != "blockrunai" {
			t.Errorf("Expected username blockrunai, got %v", body["username"])
		}

		json.NewEncoder(w).Encode(XUserInfoResponse{
			User: XUser{
				ID:             "999",
				Name:           "BlockRun",
				Username:       "blockrunai",
				FollowersCount: 5000,
				Verified:       true,
			},
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XUserInfo(context.Background(), "blockrunai")
	if err != nil {
		t.Fatalf("XUserInfo failed: %v", err)
	}
	if resp.User.Username != "blockrunai" {
		t.Errorf("Expected username blockrunai, got %s", resp.User.Username)
	}
	if !resp.User.Verified {
		t.Error("Expected user to be verified")
	}
}

func TestXUserInfoValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.XUserInfo(context.Background(), "")
	if err == nil {
		t.Error("Expected validation error for empty username")
	}
}

func TestXVerifiedFollowers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/users/verified-followers" {
			t.Errorf("Expected path /v1/x/users/verified-followers, got %s", r.URL.Path)
		}

		body := readRequestBody(t, r)
		if body["userId"] != "12345" {
			t.Errorf("Expected userId 12345, got %v", body["userId"])
		}

		json.NewEncoder(w).Encode(XFollowersResponse{
			Users:         []XUser{{ID: "100", Username: "verified1", Verified: true}},
			HasNextPage:   false,
			TotalReturned: 1,
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XVerifiedFollowers(context.Background(), "12345", "")
	if err != nil {
		t.Fatalf("XVerifiedFollowers failed: %v", err)
	}
	if len(resp.Users) != 1 {
		t.Errorf("Expected 1 user, got %d", len(resp.Users))
	}
}

func TestXVerifiedFollowersValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.XVerifiedFollowers(context.Background(), "", "")
	if err == nil {
		t.Error("Expected validation error for empty userId")
	}
}

func TestXUserTweets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/users/tweets" {
			t.Errorf("Expected path /v1/x/users/tweets, got %s", r.URL.Path)
		}

		body := readRequestBody(t, r)
		if body["username"] != "testuser" {
			t.Errorf("Expected username testuser, got %v", body["username"])
		}
		if body["includeReplies"] != true {
			t.Errorf("Expected includeReplies true, got %v", body["includeReplies"])
		}

		json.NewEncoder(w).Encode(XTweetsResponse{
			Tweets:        []XTweet{{ID: "t1", Text: "Hello world", AuthorID: "123"}},
			HasNextPage:   false,
			TotalReturned: 1,
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XUserTweets(context.Background(), "testuser", true, "")
	if err != nil {
		t.Fatalf("XUserTweets failed: %v", err)
	}
	if len(resp.Tweets) != 1 {
		t.Errorf("Expected 1 tweet, got %d", len(resp.Tweets))
	}
	if resp.Tweets[0].Text != "Hello world" {
		t.Errorf("Expected tweet text 'Hello world', got %s", resp.Tweets[0].Text)
	}
}

func TestXUserTweetsValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.XUserTweets(context.Background(), "", false, "")
	if err == nil {
		t.Error("Expected validation error for empty username")
	}
}

func TestXUserMentions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/users/mentions" {
			t.Errorf("Expected path /v1/x/users/mentions, got %s", r.URL.Path)
		}

		body := readRequestBody(t, r)
		if body["username"] != "testuser" {
			t.Errorf("Expected username testuser, got %v", body["username"])
		}
		if body["sinceTime"] != "2024-01-01" {
			t.Errorf("Expected sinceTime 2024-01-01, got %v", body["sinceTime"])
		}

		json.NewEncoder(w).Encode(XTweetsResponse{
			Tweets:        []XTweet{{ID: "m1", Text: "@testuser hello"}},
			HasNextPage:   false,
			TotalReturned: 1,
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XUserMentions(context.Background(), "testuser", "2024-01-01", "", "")
	if err != nil {
		t.Fatalf("XUserMentions failed: %v", err)
	}
	if len(resp.Tweets) != 1 {
		t.Errorf("Expected 1 tweet, got %d", len(resp.Tweets))
	}
}

func TestXUserMentionsValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.XUserMentions(context.Background(), "", "", "", "")
	if err == nil {
		t.Error("Expected validation error for empty username")
	}
}

func TestXTweetLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/tweets/lookup" {
			t.Errorf("Expected path /v1/x/tweets/lookup, got %s", r.URL.Path)
		}

		body := readRequestBody(t, r)
		ids, ok := body["tweet_ids"].([]any)
		if !ok || len(ids) != 2 {
			t.Errorf("Expected 2 tweet_ids, got %v", body["tweet_ids"])
		}

		json.NewEncoder(w).Encode(XTweetLookupResponse{
			Tweets: []XTweet{
				{ID: "111", Text: "Tweet one"},
				{ID: "222", Text: "Tweet two"},
			},
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XTweetLookup(context.Background(), []string{"111", "222"})
	if err != nil {
		t.Fatalf("XTweetLookup failed: %v", err)
	}
	if len(resp.Tweets) != 2 {
		t.Errorf("Expected 2 tweets, got %d", len(resp.Tweets))
	}
}

func TestXTweetLookupValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.XTweetLookup(context.Background(), []string{})
	if err == nil {
		t.Error("Expected validation error for empty tweet_ids")
	}
}

func TestXTweetReplies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/tweets/replies" {
			t.Errorf("Expected path /v1/x/tweets/replies, got %s", r.URL.Path)
		}

		body := readRequestBody(t, r)
		if body["tweetId"] != "999" {
			t.Errorf("Expected tweetId 999, got %v", body["tweetId"])
		}
		if body["queryType"] != "Latest" {
			t.Errorf("Expected queryType Latest, got %v", body["queryType"])
		}

		json.NewEncoder(w).Encode(XTweetsResponse{
			Tweets:        []XTweet{{ID: "r1", Text: "Reply tweet"}},
			HasNextPage:   false,
			TotalReturned: 1,
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XTweetReplies(context.Background(), "999", "Latest", "")
	if err != nil {
		t.Fatalf("XTweetReplies failed: %v", err)
	}
	if len(resp.Tweets) != 1 {
		t.Errorf("Expected 1 tweet, got %d", len(resp.Tweets))
	}
}

func TestXTweetRepliesValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.XTweetReplies(context.Background(), "", "", "")
	if err == nil {
		t.Error("Expected validation error for empty tweetId")
	}
}

func TestXTweetThread(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/tweets/thread" {
			t.Errorf("Expected path /v1/x/tweets/thread, got %s", r.URL.Path)
		}

		body := readRequestBody(t, r)
		if body["tweetId"] != "555" {
			t.Errorf("Expected tweetId 555, got %v", body["tweetId"])
		}

		json.NewEncoder(w).Encode(XTweetsResponse{
			Tweets: []XTweet{
				{ID: "555", Text: "Original tweet"},
				{ID: "556", Text: "Thread continuation"},
			},
			HasNextPage:   false,
			TotalReturned: 2,
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XTweetThread(context.Background(), "555", "")
	if err != nil {
		t.Fatalf("XTweetThread failed: %v", err)
	}
	if len(resp.Tweets) != 2 {
		t.Errorf("Expected 2 tweets, got %d", len(resp.Tweets))
	}
}

func TestXTweetThreadValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.XTweetThread(context.Background(), "", "")
	if err == nil {
		t.Error("Expected validation error for empty tweetId")
	}
}

func TestXSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/search" {
			t.Errorf("Expected path /v1/x/search, got %s", r.URL.Path)
		}

		body := readRequestBody(t, r)
		if body["query"] != "blockchain" {
			t.Errorf("Expected query blockchain, got %v", body["query"])
		}
		if body["queryType"] != "Top" {
			t.Errorf("Expected queryType Top, got %v", body["queryType"])
		}

		json.NewEncoder(w).Encode(XSearchResponse{
			Tweets:        []XTweet{{ID: "s1", Text: "Blockchain is cool"}},
			HasNextPage:   true,
			NextCursor:    "search_cursor",
			TotalReturned: 1,
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XSearch(context.Background(), "blockchain", "Top", "")
	if err != nil {
		t.Fatalf("XSearch failed: %v", err)
	}
	if len(resp.Tweets) != 1 {
		t.Errorf("Expected 1 tweet, got %d", len(resp.Tweets))
	}
	if !resp.HasNextPage {
		t.Error("Expected HasNextPage to be true")
	}
}

func TestXSearchValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.XSearch(context.Background(), "", "", "")
	if err == nil {
		t.Error("Expected validation error for empty query")
	}
}

func TestXTrending(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/trending" {
			t.Errorf("Expected path /v1/x/trending, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		json.NewEncoder(w).Encode(XTrendingResponse{
			Topics: []XTrendingTopic{
				{Name: "#Bitcoin", TweetCount: 100000},
				{Name: "#AI", TweetCount: 50000},
			},
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XTrending(context.Background())
	if err != nil {
		t.Fatalf("XTrending failed: %v", err)
	}
	if len(resp.Topics) != 2 {
		t.Errorf("Expected 2 topics, got %d", len(resp.Topics))
	}
	if resp.Topics[0].Name != "#Bitcoin" {
		t.Errorf("Expected first topic #Bitcoin, got %s", resp.Topics[0].Name)
	}
}

func TestXArticlesRising(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/articles/rising" {
			t.Errorf("Expected path /v1/x/articles/rising, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		json.NewEncoder(w).Encode(XArticlesRisingResponse{
			Articles: []XArticle{
				{Title: "AI Revolution", URL: "https://example.com/ai", Author: "journalist1"},
			},
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XArticlesRising(context.Background())
	if err != nil {
		t.Fatalf("XArticlesRising failed: %v", err)
	}
	if len(resp.Articles) != 1 {
		t.Errorf("Expected 1 article, got %d", len(resp.Articles))
	}
	if resp.Articles[0].Title != "AI Revolution" {
		t.Errorf("Expected title 'AI Revolution', got %s", resp.Articles[0].Title)
	}
}

func TestXAuthorAnalytics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/authors" {
			t.Errorf("Expected path /v1/x/authors, got %s", r.URL.Path)
		}

		body := readRequestBody(t, r)
		if body["handle"] != "blockrunai" {
			t.Errorf("Expected handle blockrunai, got %v", body["handle"])
		}

		json.NewEncoder(w).Encode(XAuthorAnalyticsResponse{
			Author: XAuthorAnalytics{
				Handle:         "blockrunai",
				FollowersCount: 10000,
				TweetCount:     500,
			},
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XAuthorAnalytics(context.Background(), "blockrunai")
	if err != nil {
		t.Fatalf("XAuthorAnalytics failed: %v", err)
	}
	if resp.Author.Handle != "blockrunai" {
		t.Errorf("Expected handle blockrunai, got %s", resp.Author.Handle)
	}
	if resp.Author.FollowersCount != 10000 {
		t.Errorf("Expected 10000 followers, got %d", resp.Author.FollowersCount)
	}
}

func TestXAuthorAnalyticsValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.XAuthorAnalytics(context.Background(), "")
	if err == nil {
		t.Error("Expected validation error for empty handle")
	}
}

func TestXCompareAuthors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/x/compare" {
			t.Errorf("Expected path /v1/x/compare, got %s", r.URL.Path)
		}

		body := readRequestBody(t, r)
		if body["handle1"] != "alice" {
			t.Errorf("Expected handle1 alice, got %v", body["handle1"])
		}
		if body["handle2"] != "bob" {
			t.Errorf("Expected handle2 bob, got %v", body["handle2"])
		}

		json.NewEncoder(w).Encode(XCompareAuthorsResponse{
			Author1: XAuthorAnalytics{Handle: "alice", FollowersCount: 5000},
			Author2: XAuthorAnalytics{Handle: "bob", FollowersCount: 8000},
			Comparison: map[string]any{
				"follower_ratio": 0.625,
			},
		})
	}))
	defer server.Close()

	client, err := NewLLMClient(testPrivateKey, WithAPIURL(server.URL))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.XCompareAuthors(context.Background(), "alice", "bob")
	if err != nil {
		t.Fatalf("XCompareAuthors failed: %v", err)
	}
	if resp.Author1.Handle != "alice" {
		t.Errorf("Expected author1 alice, got %s", resp.Author1.Handle)
	}
	if resp.Author2.Handle != "bob" {
		t.Errorf("Expected author2 bob, got %s", resp.Author2.Handle)
	}
	if resp.Comparison == nil {
		t.Error("Expected comparison data")
	}
}

func TestXCompareAuthorsValidation(t *testing.T) {
	client, err := NewLLMClient(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.XCompareAuthors(context.Background(), "", "bob")
	if err == nil {
		t.Error("Expected validation error for empty handle1")
	}

	_, err = client.XCompareAuthors(context.Background(), "alice", "")
	if err == nil {
		t.Error("Expected validation error for empty handle2")
	}
}
