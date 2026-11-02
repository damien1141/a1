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

// npmEngine searches the npm registry's public search endpoint
// (https://registry.npmjs.org/-/v1/search). No API key. Best for
// JavaScript/TypeScript package discovery — "npm package for charts",
// "is there a javascript library for X" — where the registry's
// relevance score plus download counts beat a generic web search.
//
// Registered BEFORE the GitHub engine in the router: when the query says
// JS/TS explicitly, the language's own index is the authoritative answer;
// GitHub stays the engine for language-agnostic discovery.
type npmEngine struct{}

func newNpmEngine() *npmEngine { return &npmEngine{} }

func (npmEngine) Name() string { return "npm" }

// npmSignalRe matches queries that explicitly point at the JavaScript
// ecosystem. "javascript"/"typescript" alone are NOT enough — paired with
// a discovery word (library/package/module) they are. The install-command
// forms (npm install / yarn add / pnpm add / bun add) are the strongest
// possible signal.
var npmSignalRe = regexp.MustCompile(`(?i)\b(npm|yarn add|pnpm add|pnpm install|bun add|node package|nodejs package|javascript (?:library|package|module)|typescript (?:library|package|module)|js (?:library|package|module)|npm package)\b`)

func (npmEngine) Matches(query string) bool {
	return npmSignalRe.MatchString(query)
}

// npmSearchResponse is the JSON shape of
// https://registry.npmjs.org/-/v1/search?text=...&size=N
type npmSearchResponse struct {
	Objects []struct {
		Package struct {
			Name        string `json:"name"`
			Version     string `json:"version"`
			Description string `json:"description"`
			Links       struct {
				NPM        string `json:"npm"`
				Homepage   string `json:"homepage"`
				Repository string `json:"repository"`
			} `json:"links"`
			Keywords []string `json:"keywords"`
		} `json:"package"`
		Downloads struct {
			Monthly int `json:"monthly"`
		} `json:"downloads"`
		SearchScore float64 `json:"searchScore"`
	} `json:"objects"`
}

// parseNpmResults converts the registry payload into ranked results +
// pre-built sources. Split from Search so tests can feed fixtures
// without touching the network.
func parseNpmResults(body []byte) ([]searchResult, []tooldef.Source, error) {
	var sr npmSearchResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, nil, fmt.Errorf("npm: parse: %w", err)
	}
	results := make([]searchResult, 0, len(sr.Objects))
	sources := make([]tooldef.Source, 0, len(sr.Objects))
	for i, obj := range sr.Objects {
		name := strings.TrimSpace(obj.Package.Name)
		if name == "" {
			continue
		}
		link := strings.TrimSpace(obj.Package.Links.NPM)
		if link == "" {
			link = "https://www.npmjs.com/package/" + name
		}
		host := "www.npmjs.com"
		host = strings.TrimPrefix(host, "www.")
		desc := strings.TrimSpace(obj.Package.Description)
		if desc == "" {
			desc = "(no description)"
		}
		snippet := fmt.Sprintf("%s · v%s · %s monthly downloads",
			truncate(desc, 220), obj.Package.Version, humanCount(obj.Downloads.Monthly))
		rank := i + 1
		results = append(results, searchResult{
			rank:    rank,
			url:     link,
			title:   name,
			snippet: snippet,
			host:    host,
		})
		sources = append(sources, tooldef.Source{
			Rank:       rank,
			URL:        link,
			Title:      truncate(name, 200),
			Host:       host,
			FaviconURL: formatFaviconURL(host),
			Snippet:    truncate(snippet, 400),
		})
	}
	return results, sources, nil
}

// humanCount renders a download count compactly: 12345 → "12.3k",
// 12345678 → "12.3m", 1627893651 → "1.6b". Keeps snippets short for the
// UI source cards.
func humanCount(n int) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fb", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func (e *npmEngine) Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error) {
	maxResults := opts.maxResults
	if maxResults <= 0 {
		maxResults = searchDefaultMaxResults
	}
	// Strip discovery phrasing so the registry's keyword search matches
	// the package name/topic ("npm package for csv parsing" → "csv parsing").
	q := npmSignalRe.ReplaceAllString(strings.TrimSpace(query), "")
	q = strings.TrimSpace(q)
	if q == "" {
		q = strings.TrimSpace(query)
	}
	endpoint := fmt.Sprintf("https://registry.npmjs.org/-/v1/search?text=%s&size=%d",
		url.QueryEscape(q), maxResults)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "a1-agent/1.0 (https://github.com/damien1141/a1)")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("npm: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("npm: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	results, sources, err := parseNpmResults(body)
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
