package blockrun

// XUser represents an X/Twitter user profile.
type XUser struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Username        string `json:"username"`
	Description     string `json:"description,omitempty"`
	FollowersCount  int    `json:"followers_count,omitempty"`
	FollowingCount  int    `json:"following_count,omitempty"`
	TweetCount      int    `json:"tweet_count,omitempty"`
	Verified        bool   `json:"verified,omitempty"`
	ProfileImageURL string `json:"profile_image_url,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
}

// XTweet represents an X/Twitter tweet.
type XTweet struct {
	ID        string         `json:"id"`
	Text      string         `json:"text"`
	AuthorID  string         `json:"author_id,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
	Metrics   map[string]int `json:"public_metrics,omitempty"`
}

// XUserLookupResponse is the response for user lookup.
type XUserLookupResponse struct {
	Users []XUser `json:"users"`
}

// XFollowersResponse is the response for followers/followings listing.
type XFollowersResponse struct {
	Users         []XUser `json:"users"`
	HasNextPage   bool    `json:"has_next_page"`
	NextCursor    string  `json:"next_cursor,omitempty"`
	TotalReturned int     `json:"total_returned"`
}

// XUserInfoResponse is the response for user info lookup.
type XUserInfoResponse struct {
	User XUser `json:"user"`
}

// XTweetsResponse is the response for tweet listing endpoints.
type XTweetsResponse struct {
	Tweets        []XTweet `json:"tweets"`
	HasNextPage   bool     `json:"has_next_page"`
	NextCursor    string   `json:"next_cursor,omitempty"`
	TotalReturned int      `json:"total_returned"`
}

// XTweetLookupResponse is the response for tweet lookup.
type XTweetLookupResponse struct {
	Tweets []XTweet `json:"tweets"`
}

// XSearchResponse is the response for search endpoints.
type XSearchResponse struct {
	Tweets        []XTweet `json:"tweets"`
	HasNextPage   bool     `json:"has_next_page"`
	NextCursor    string   `json:"next_cursor,omitempty"`
	TotalReturned int      `json:"total_returned"`
}

// XTrendingTopic represents a trending topic on X/Twitter.
type XTrendingTopic struct {
	Name       string `json:"name"`
	TweetCount int    `json:"tweet_count,omitempty"`
	Category   string `json:"category,omitempty"`
}

// XTrendingResponse is the response for trending topics.
type XTrendingResponse struct {
	Topics []XTrendingTopic `json:"topics"`
}

// XArticle represents a rising article on X/Twitter.
type XArticle struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
}

// XArticlesRisingResponse is the response for rising articles.
type XArticlesRisingResponse struct {
	Articles []XArticle `json:"articles"`
}

// XAuthorAnalytics represents analytics data for an X/Twitter author.
type XAuthorAnalytics struct {
	Handle         string         `json:"handle"`
	FollowersCount int            `json:"followers_count,omitempty"`
	FollowingCount int            `json:"following_count,omitempty"`
	TweetCount     int            `json:"tweet_count,omitempty"`
	Engagement     map[string]any `json:"engagement,omitempty"`
}

// XAuthorAnalyticsResponse is the response for author analytics.
type XAuthorAnalyticsResponse struct {
	Author XAuthorAnalytics `json:"author"`
}

// XCompareAuthorsResponse is the response for author comparison.
type XCompareAuthorsResponse struct {
	Author1    XAuthorAnalytics `json:"author1"`
	Author2    XAuthorAnalytics `json:"author2"`
	Comparison map[string]any   `json:"comparison,omitempty"`
}
