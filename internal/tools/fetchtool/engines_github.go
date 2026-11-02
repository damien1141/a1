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

// githubEngine searches the GitHub repository search API
// (https://api.github.com/search/repositories). No API key required
// for low-volume use (60 req/hr unauthenticated, 5000 req/hr with a
// token). Best for "is there a library that does X" / "find a package
// for Y" — the canonical discovery query for a coding agent.
//
// The Matches() predicate looks for library/package discovery signals:
// the words "library", "package", "framework", plus explicit GitHub
// references and "is there a X that does Y" phrasings.
type githubEngine struct{}

func newGitHubEngine() *githubEngine { return &githubEngine{} }

func (githubEngine) Name() string { return "github" }

// libraryDiscoveryRe matches queries that look like the user is
// searching for a library, package, or framework. The signal words
// are common in natural-language discovery queries:
//   - "is there a go library for..."
//   - "find a package that..."
//   - "best framework for..."
//   - "github.com/..." (explicit GitHub URL → definitely GitHub)
var libraryDiscoveryRe = regexp.MustCompile(`(?i)\b(library|libraries|package|packages|module|modules|framework|frameworks|gem|gems|crate|crates|plugin|plugins|extension|extensions|addon|addons|is there a|find a|recommend a|best \w+ for)\b`)

// githubURLRe matches explicit GitHub URLs in the query — when the
// user pastes "github.com/user/repo" they're definitely asking about
// a GitHub repo.
var githubURLRe = regexp.MustCompile(`(?i)github\.com/`)

func (githubEngine) Matches(query string) bool {
	return libraryDiscoveryRe.MatchString(query) || githubURLRe.MatchString(query)
}

// githubSearchResponse is the JSON shape of
// https://api.github.com/search/repositories?q=...
type githubSearchResponse struct {
	TotalCount int `json:"total_count"`
	Items      []struct {
		FullName    string `json:"full_name"` // "owner/repo"
		HTMLURL     string `json:"html_url"`
		Description string `json:"description"`
		Stars       int    `json:"stargazers_count"`
		Language    string `json:"language"`
		Homepage    string `json:"homepage"` // project homepage, may be ""
	} `json:"items"`
}

func (e *githubEngine) Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error) {
	maxResults := opts.maxResults
	if maxResults <= 0 {
		maxResults = searchDefaultMaxResults
	}
	// GitHub search is keyword-based. Strip common discovery phrasing
	// ("is there a go library for X" → "go X") so the keyword search
	// matches the project name/topic rather than the discovery words.
	q := libraryDiscoveryRe.ReplaceAllString(strings.TrimSpace(query), "")
	q = strings.TrimSpace(q)
	if q == "" {
		q = strings.TrimSpace(query)
	}
	endpoint := fmt.Sprintf("https://api.github.com/search/repositories?q=%s&sort=stars&order=desc&per_page=%d",
		url.QueryEscape(q), maxResults)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "a1-agent/1.0")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var sr githubSearchResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, fmt.Errorf("github: parse: %w", err)
	}

	results := make([]searchResult, 0, len(sr.Items))
	sources := make([]tooldef.Source, 0, len(sr.Items))
	for i, item := range sr.Items {
		// Snippet: stars + language + description.
		desc := strings.TrimSpace(item.Description)
		if desc == "" {
			desc = "(no description)"
		}
		lang := item.Language
		if lang == "" {
			lang = "—"
		}
		snippet := fmt.Sprintf("★ %d · %s · %s", item.Stars, lang, truncate(desc, 200))
		host := "github.com"
		rank := i + 1
		results = append(results, searchResult{
			rank:    rank,
			url:     item.HTMLURL,
			title:   item.FullName,
			snippet: snippet,
		})
		sources = append(sources, tooldef.Source{
			Rank:       rank,
			URL:        item.HTMLURL,
			Title:      item.FullName,
			Host:       host,
			FaviconURL: formatFaviconURL(host),
			Snippet:    snippet,
		})
	}

	return &SearchResponse{
		Engine:  e.Name(),
		Query:   query,
		Results: results,
		Sources: sources,
	}, nil
}
