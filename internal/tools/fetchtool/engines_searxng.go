package fetchtool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/tools/tooldef"
)

// searxngEngine talks to a self-hosted SearXNG instance's JSON API.
//
// Why this engine exists: every other general-web engine here either scrapes
// HTML (DuckDuckGo Lite — reliable on a residential IP, a captcha wall on a
// datacenter one) or needs a browser daemon (BetterWright). Neither is
// dependable enough to be the primary path, which is what "fetch does not work
// reliably" is about. SearXNG is the dependable option: it aggregates the
// upstream engines itself, exposes a documented JSON API, and — because it is
// the user's own instance — never answers with a captcha.
//
// Configuration is deliberately simple:
//
//	search:
//	  searxng_url: "http://127.0.0.1:8888"
//
// or the A1_SEARXNG_URL env var (which wins, so a per-invocation override
// needs no file edit). Unset means the engine is not registered at all — the
// router keeps its exact previous behavior, no probe, no error.
//
// Matches() returns false for every query: like DuckDuckGo Lite this is a
// general-purpose fallback, not a domain-specific match. It ranks FIRST among
// the fallbacks, ahead of the DDG Lite scraper it supersedes.
type searxngEngine struct {
	baseURL string
	client  *http.Client
}

const (
	// searxngTimeout bounds one search round trip. SearXNG fans out to several
	// upstream engines server-side, so it is slower than a direct API call.
	searxngTimeout = 20 * time.Second
	// searxngMaxBody caps the JSON we will parse. A page of results is tens of
	// KB; anything past this is a misconfigured endpoint serving HTML.
	searxngMaxBody = 4 << 20
)

// searxngURL resolves the configured instance URL: env first, then config.
// The trailing slash is trimmed so path joining stays predictable.
func searxngURL(configured string) string {
	if v := strings.TrimSpace(os.Getenv("A1_SEARXNG_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return strings.TrimRight(strings.TrimSpace(configured), "/")
}

func newSearxngEngine(configured string) *searxngEngine {
	base := searxngURL(configured)
	if base == "" {
		return nil
	}
	return &searxngEngine{
		baseURL: base,
		client:  &http.Client{Timeout: searxngTimeout},
	}
}

func (*searxngEngine) Name() string { return "searxng" }

// Matches is false for every query — a general-purpose fallback, dispatched by
// the router's Phase 2 order rather than by predicate.
func (*searxngEngine) Matches(_ string) bool { return false }

// searxngResponse is the subset of SearXNG's JSON shape this engine reads.
// Unknown fields are ignored, so a newer instance keeps working.
type searxngResponse struct {
	Results []struct {
		URL     string `json:"url"`
		Title   string `json:"title"`
		Content string `json:"content"`
		Engine  string `json:"engine"`
	} `json:"results"`
	// Error is set by some deployments instead of an HTTP status.
	Error string `json:"error"`
}

func (e *searxngEngine) Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error) {
	// format=json is what makes this a data API rather than a page scrape;
	// SearXNG's settings.yml must enable it (documented in the error below so a
	// 403 is actionable instead of mysterious).
	q := url.Values{}
	q.Set("q", query)
	q.Set("format", "json")
	q.Set("safesearch", "0")
	endpoint := e.baseURL + "/search?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("searxng: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", searchBrowserUserAgent)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("searxng: %s rejected the JSON API (403) — enable `json` in search.formats in the instance's settings.yml", e.baseURL)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng: %s answered HTTP %d", e.baseURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, searxngMaxBody))
	if err != nil {
		return nil, fmt.Errorf("searxng: read body: %w", err)
	}
	parsed, err := parseSearxngBody(string(body))
	if err != nil {
		// The endpoint answered 200 with something that is not JSON — almost
		// always the HTML UI, i.e. the json format is off. Say so; a bare
		// "invalid character '<'" sends people looking in the wrong place.
		return nil, fmt.Errorf("searxng: %s returned non-JSON (is `json` enabled in search.formats?): %w", e.baseURL, err)
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("searxng: %s: %s", e.baseURL, parsed.Error)
	}
	if len(parsed.Results) == 0 {
		return &SearchResponse{Engine: e.Name(), Query: query}, nil
	}

	out := e.responseFrom(parsed, query, opts)
	sources := out.Sources
	results := out.Results
	if opts.fetchPages && len(sources) > 0 {
		fetchResultPages(ctx, sources, opts.maxCharsPerPage)
		for i := range sources {
			if i < len(results) {
				results[i].content = sources[i].Content
				results[i].fetchError = sources[i].FetchError
			}
		}
	}

	return &SearchResponse{
		Engine:  e.Name(),
		Query:   query,
		Results: results,
		Sources: sources,
	}, nil
}

// parseSearxngBody decodes one SearXNG JSON reply. Split out of Search so the
// decoding contract is testable without a network round trip.
func parseSearxngBody(body string) (searxngResponse, error) {
	var parsed searxngResponse
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return searxngResponse{}, err
	}
	return parsed, nil
}

// responseFrom turns a decoded SearXNG reply into the router's response shape:
// dense ranks, hosts and favicons pre-computed for the UI's source pills.
// Rows with no URL are dropped (a result you cannot follow is not a result),
// which is why ranks are assigned from the surviving count rather than the
// source index.
func (e *searxngEngine) responseFrom(parsed searxngResponse, query string, opts searchOptions) *SearchResponse {
	limit := opts.maxResults
	if limit <= 0 || limit > len(parsed.Results) {
		limit = len(parsed.Results)
	}
	results := make([]searchResult, 0, limit)
	sources := make([]tooldef.Source, 0, limit)
	for i := 0; i < limit; i++ {
		r := parsed.Results[i]
		u := strings.TrimSpace(r.URL)
		if u == "" {
			continue
		}
		host := strings.TrimPrefix(hostOf(u), "www.")
		rank := len(results) + 1
		results = append(results, searchResult{
			rank:    rank,
			url:     u,
			title:   r.Title,
			snippet: r.Content,
			host:    host,
		})
		sources = append(sources, tooldef.Source{
			Rank:       rank,
			URL:        u,
			Title:      truncate(r.Title, 200),
			Host:       host,
			FaviconURL: formatFaviconURL(host),
			Snippet:    truncate(r.Content, 400),
		})
	}
	return &SearchResponse{
		Engine:  e.Name(),
		Query:   query,
		Results: results,
		Sources: sources,
	}
}
