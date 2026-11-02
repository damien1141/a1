package fetchtool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/tools/tooldef"
)

// redditEngine searches Reddit's public JSON search
// (https://www.reddit.com/search.json). No API key, but Reddit hard-blocks
// most datacenter IPs — from a server this engine errors and the router
// falls through to the next engine; from a residential IP (the common
// a1 deployment) it works fine and returns exactly what "what do people
// actually think about X" wants: real threads, scores, comment counts.
//
// The engine is registered twice in the default router: as a domain
// engine for explicit reddit-seeking queries (Matches() below), and as a
// participant in the merged web fallback.
type redditEngine struct{}

func newRedditEngine() *redditEngine { return &redditEngine{} }

func (redditEngine) Name() string { return "reddit" }

// redditSignalRe matches queries that explicitly want Reddit: the site
// name, an r/subreddit reference, or "forum/thread" phrasing.
var redditSignalRe = regexp.MustCompile(`(?i)\breddit\b|\br/[a-z0-9_]{2,25}\b|\b(forum|thread)s? about\b`)

func (redditEngine) Matches(query string) bool {
	return redditSignalRe.MatchString(query)
}

// redditSearchResponse is the JSON shape of
// https://www.reddit.com/search.json?q=...&limit=N&sort=relevance
type redditSearchResponse struct {
	Data struct {
		Children []struct {
			Data struct {
				Title       string `json:"title"`
				Permalink   string `json:"permalink"`
				Subreddit   string `json:"subreddit"`
				Score       int    `json:"score"`
				NumComments int    `json:"num_comments"`
				SelfText    string `json:"selftext"`
				Author      string `json:"author"`
			} `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

// parseRedditResults converts the search payload into ranked results +
// pre-built sources. Split from Search so tests can feed fixtures
// without touching the network.
func parseRedditResults(body []byte) ([]searchResult, []tooldef.Source, error) {
	var sr redditSearchResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		// Reddit fronts its block page as HTML — a JSON parse failure here
		// almost always means the request was refused, not malformed.
		return nil, nil, fmt.Errorf("reddit: blocked or non-JSON response (datacenter IPs are refused)")
	}
	results := make([]searchResult, 0, len(sr.Data.Children))
	sources := make([]tooldef.Source, 0, len(sr.Data.Children))
	for i, child := range sr.Data.Children {
		d := child.Data
		title := strings.TrimSpace(d.Title)
		permalink := strings.TrimSpace(d.Permalink)
		if title == "" || permalink == "" {
			continue
		}
		if !strings.HasPrefix(permalink, "http") {
			permalink = "https://www.reddit.com" + permalink
		}
		var snippet string
		if text := collapseWhitespace(stripTags(d.SelfText)); text != "" && text != "[removed]" && text != "[deleted]" {
			snippet = fmt.Sprintf("r/%s · ▲ %d · %d comments · u/%s · %s",
				d.Subreddit, d.Score, d.NumComments, d.Author, truncate(text, 200))
		} else {
			snippet = fmt.Sprintf("r/%s · ▲ %d · %d comments · u/%s",
				d.Subreddit, d.Score, d.NumComments, d.Author)
		}
		host := "www.reddit.com"
		host = strings.TrimPrefix(host, "www.")
		rank := i + 1
		results = append(results, searchResult{
			rank:    rank,
			url:     permalink,
			title:   title,
			snippet: snippet,
			host:    host,
		})
		sources = append(sources, tooldef.Source{
			Rank:       rank,
			URL:        permalink,
			Title:      truncate(title, 200),
			Host:       host,
			FaviconURL: formatFaviconURL(host),
			Snippet:    truncate(snippet, 400),
		})
	}
	return results, sources, nil
}

func (e *redditEngine) Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error) {
	maxResults := opts.maxResults
	if maxResults <= 0 {
		maxResults = searchDefaultMaxResults
	}
	q := redditSignalRe.ReplaceAllString(strings.TrimSpace(query), "")
	q = strings.TrimSpace(q)
	if q == "" {
		q = strings.TrimSpace(query)
	}
	endpoint := fmt.Sprintf("https://www.reddit.com/search.json?q=%s&limit=%d&sort=relevance",
		url.QueryEscape(q), maxResults)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	// Reddit refuses generic script UAs; a descriptive one identifying the
	// client is its documented policy.
	req.Header.Set("User-Agent", "a1-agent/1.0 (https://github.com/damien1141/a1)")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reddit: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reddit: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	results, sources, err := parseRedditResults(body)
	if err != nil {
		return nil, err
	}
	return &SearchResponse{
		Engine:  e.Name(),
		Query:   query,
		Results: results,
		Sources: sources,
	}, nil
}
