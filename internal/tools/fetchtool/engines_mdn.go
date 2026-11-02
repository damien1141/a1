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

// mdnEngine searches the free MDN Web Docs API
// (https://developer.mozilla.org/api/v1/search). No API key, no captcha,
// instant JSON. Best for web-platform reference queries — CSS properties,
// HTML elements, DOM APIs, HTTP semantics — which are exactly the queries
// where a doc page beats a Stack Overflow thread.
//
// The Matches() predicate fires only on strong web-platform signals (css,
// html, dom, svg, explicit "mdn", …). It deliberately does NOT match on
// "javascript" alone: that word is too broad — "javascript function
// decorator" should keep routing to Stack Exchange, not MDN.
type mdnEngine struct{}

func newMDNEngine() *mdnEngine { return &mdnEngine{} }

func (mdnEngine) Name() string { return "mdn" }

// webPlatformRe matches queries with unmistakable web-platform flavor.
// Every term here is either an MDN section name (css, html, dom, svg, http
// header/status, web api) or a term that almost never appears outside a
// browser-docs context (flexbox, queryselector, event listener, …).
// False positives are tolerable (the router falls through when MDN
// returns nothing); false negatives just mean the broader engines handle
// the query, which is the pre-MDN behavior.
var webPlatformRe = regexp.MustCompile(`(?i)\b(css|flexbox|grid layout|subgrid|html\b|dom\b|svg\b|mdn|web api|browser api|http header|http status|http method|event listener|addeventlistener|queryselector|getelementbyid|getelementsbytagname|canvas api)\b`)

func (mdnEngine) Matches(query string) bool {
	return webPlatformRe.MatchString(query)
}

// mdnSearchResponse is the JSON shape of
// https://developer.mozilla.org/api/v1/search?q=...&locale=en-US&limit=N
//
// documents[].mdn_url is site-relative ("/en-US/docs/Web/CSS/flex");
// the absolute URL is prefixed at parse time.
type mdnSearchResponse struct {
	Documents []struct {
		MDNURL  string  `json:"mdn_url"`
		Title   string  `json:"title"`
		Slug    string  `json:"slug"`
		Locale  string  `json:"locale"`
		Summary string  `json:"summary"`
		Score   float64 `json:"score"`
	} `json:"documents"`
}

// parseMDNResults converts the MDN API payload into ranked results +
// pre-built sources. Split from Search so tests can feed fixtures
// without touching the network.
func parseMDNResults(body []byte) ([]searchResult, []tooldef.Source, error) {
	var sr mdnSearchResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, nil, fmt.Errorf("mdn: parse: %w", err)
	}
	results := make([]searchResult, 0, len(sr.Documents))
	sources := make([]tooldef.Source, 0, len(sr.Documents))
	for i, doc := range sr.Documents {
		docURL := strings.TrimSpace(doc.MDNURL)
		title := strings.TrimSpace(doc.Title)
		if docURL == "" || title == "" {
			continue
		}
		if !strings.HasPrefix(docURL, "http") {
			docURL = "https://developer.mozilla.org" + docURL
		}
		snippet := collapseWhitespace(stripTags(doc.Summary))
		snippet = truncate(snippet, 400)
		host := "developer.mozilla.org"
		rank := i + 1
		results = append(results, searchResult{
			rank:    rank,
			url:     docURL,
			title:   title,
			snippet: snippet,
			host:    host,
		})
		sources = append(sources, tooldef.Source{
			Rank:       rank,
			URL:        docURL,
			Title:      truncate(title, 200),
			Host:       host,
			FaviconURL: formatFaviconURL(host),
			Snippet:    snippet,
		})
	}
	return results, sources, nil
}

func (e *mdnEngine) Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error) {
	maxResults := opts.maxResults
	if maxResults <= 0 {
		maxResults = searchDefaultMaxResults
	}
	// MDN search does better with the question phrasing stripped:
	// "how does css flexbox work" → "css flexbox".
	q := strings.TrimSpace(query)
	q = factualQueryRe.ReplaceAllString(q, "")
	q = strings.TrimSpace(q)
	if q == "" {
		q = strings.TrimSpace(query)
	}
	endpoint := fmt.Sprintf("https://developer.mozilla.org/api/v1/search?q=%s&locale=en-US&limit=%d",
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
		return nil, fmt.Errorf("mdn: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mdn: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	results, sources, err := parseMDNResults(body)
	if err != nil {
		return nil, err
	}
	// The API clamps small limits to its own floor (a limit of 4 still
	// returns 10); cap client-side so max_results is honored.
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	if len(sources) > maxResults {
		sources = sources[:maxResults]
	}
	return &SearchResponse{
		Engine:  e.Name(),
		Query:   query,
		Results: results,
		Sources: sources,
	}, nil
}
