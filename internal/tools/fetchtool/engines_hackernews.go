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

// hackerNewsEngine searches the Hacker News Algolia API
// (https://hn.algolia.com/api/v1/search). No API key, no captcha, and one
// of the most reliable public search APIs on the web. Best for "what do
// people think of X" / "discussion of Y" queries — practitioner opinion,
// launch threads, and show-HN posts with the actual maintainers in the
// comments.
//
// The engine is registered twice in the default router: as a domain
// engine for explicit discussion-seeking queries (Matches() below), and
// as a participant in the merged web fallback so every general query gets
// community results interleaved with its DuckDuckGo links.
type hackerNewsEngine struct{}

func newHackerNewsEngine() *hackerNewsEngine { return &hackerNewsEngine{} }

func (hackerNewsEngine) Name() string { return "hackernews" }

// discussionQueryRe matches queries that explicitly ask for community
// opinion or discussion rather than documentation. These are the queries
// where a forum thread is a better first result than a doc page.
var discussionQueryRe = regexp.MustCompile(`(?i)\b(hacker ?news|\bhn\b|discussion|discussions|thread|what do people think|what does (?:the )?community think|opinions? on|anyone (?:use|using|tried|switched to)|experience with|reviews? of|vs\.? thread)\b`)

func (hackerNewsEngine) Matches(query string) bool {
	return discussionQueryRe.MatchString(query)
}

// hnSearchResponse is the JSON shape of
// https://hn.algolia.com/api/v1/search?query=...&tags=story&hitsPerPage=N
type hnSearchResponse struct {
	Hits []struct {
		ObjectID    string `json:"objectID"`
		Title       string `json:"title"`
		StoryTitle  string `json:"story_title"`
		URL         string `json:"url"`
		StoryURL    string `json:"story_url"`
		Author      string `json:"author"`
		Points      int    `json:"points"`
		NumComments int    `json:"num_comments"`
		StoryText   string `json:"story_text"`
		CreatedAt   string `json:"created_at"`
	} `json:"hits"`
}

// parseHackerNewsResults converts the Algolia payload into ranked results
// + pre-built sources. Split from Search so tests can feed fixtures
// without touching the network.
//
// URL policy: a story usually links to the article itself (hit.url); Ask
// HN / text-only stories have no url and point at the HN item instead
// (https://news.ycombinator.com/item?id=<objectID>) — that thread IS the
// content.
func parseHackerNewsResults(body []byte) ([]searchResult, []tooldef.Source, error) {
	var sr hnSearchResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, nil, fmt.Errorf("hackernews: parse: %w", err)
	}
	results := make([]searchResult, 0, len(sr.Hits))
	sources := make([]tooldef.Source, 0, len(sr.Hits))
	for i, hit := range sr.Hits {
		title := strings.TrimSpace(hit.Title)
		if title == "" {
			title = strings.TrimSpace(hit.StoryTitle)
		}
		link := strings.TrimSpace(hit.URL)
		if link == "" {
			link = strings.TrimSpace(hit.StoryURL)
		}
		if link == "" {
			link = "https://news.ycombinator.com/item?id=" + hit.ObjectID
		}
		if title == "" || hit.ObjectID == "" {
			continue
		}
		// The snippet carries the thread metadata the ranking is good for:
		// points, comment count, author, and a text preview when present.
		// story_text arrives HTML-escaped — decode so cards read naturally.
		text := collapseWhitespace(decodeEntities(stripTags(hit.StoryText)))
		var snippet string
		if text != "" {
			snippet = fmt.Sprintf("▲ %d · %d comments · @%s · %s",
				hit.Points, hit.NumComments, hit.Author, truncate(text, 240))
		} else {
			snippet = fmt.Sprintf("▲ %d · %d comments · @%s",
				hit.Points, hit.NumComments, hit.Author)
		}
		host := hostOf(link)
		host = strings.TrimPrefix(host, "www.")
		rank := i + 1
		results = append(results, searchResult{
			rank:    rank,
			url:     link,
			title:   title,
			snippet: snippet,
			host:    host,
		})
		sources = append(sources, tooldef.Source{
			Rank:       rank,
			URL:        link,
			Title:      truncate(title, 200),
			Host:       host,
			FaviconURL: formatFaviconURL(host),
			Snippet:    truncate(snippet, 400),
		})
	}
	return results, sources, nil
}

func (e *hackerNewsEngine) Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error) {
	maxResults := opts.maxResults
	if maxResults <= 0 {
		maxResults = searchDefaultMaxResults
	}
	// Strip the discussion phrasing — Algolia's relevance is much better
	// matching the subject alone ("what do people think of bun" → "bun").
	q := discussionQueryRe.ReplaceAllString(strings.TrimSpace(query), "")
	q = strings.TrimSpace(q)
	if q == "" {
		q = strings.TrimSpace(query)
	}
	// Quote the subject so Algolia treats it as an exact phrase: a bare
	// "bun" prefix-matches "bunch" and surfaces unrelated high-score
	// threads; "\"bun\"" only matches the word itself.
	endpoint := fmt.Sprintf("https://hn.algolia.com/api/v1/search?query=%s&tags=story&hitsPerPage=%d",
		url.QueryEscape(`"`+q+`"`), maxResults)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "a1-agent/1.0 (https://github.com/damien1141/a1)")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hackernews: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hackernews: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	results, sources, err := parseHackerNewsResults(body)
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
