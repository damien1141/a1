package fetchtool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

const (
	fetchDefaultTimeout = 30 * time.Second
	fetchMaxBytes       = 1 << 20 // 1 MiB
	// Multi-URL mode caps: the agent can fan out across many pages, but a
	// single tool call must not turn into a runaway crawler. 10 URLs keeps
	// one call bounded; 5 concurrent keeps the client + network honest.
	fetchMaxURLs       = 10
	fetchMaxConcurrent = 5
	// Redirect policies for the `redirects` param. CheckRedirect is wired
	// in fetchOne based on which policy the agent picked.
	redirectFollow = "follow" // default — Go's behavior, up to 10 hops
	redirectManual = "manual" // return the 3xx response, Location header visible
	redirectNone   = "none"   // error on any redirect
)

var fetchDescription = `Fetch URLs or search the web over HTTP - no JavaScript, no cookies; the
browser tools cover pages that need those. Send exactly one of
query / url / urls:

- query: web search. Current date/time ("what time is it", "time now")
  and weather ("weather in Reykjavik", "forecast for Tokyo") get direct
  answers from dedicated engines — no clock or forecast is injected into
  your context, so fetch one before date-, time-, or weather-sensitive
  work. Currency queries ("100 usd to eur") get a direct answer;
  web-platform terms go to MDN; npm/crates registry lookups to their
  own engines; programming, academic, and factual queries to the Stack
  Exchange, GitHub, arxiv, or Wikipedia APIs; discussion-seeking queries
  to Hacker News / Reddit; everything else merges DuckDuckGo Lite +
  Hacker News + Reddit results (community links included; may captcha on
  server IPs). Ranked results plus each page body by default.
- url: one URL. HTML becomes readable text, JSON is pretty-printed,
  other text passes through, binaries a placeholder.
- urls: up to 10 in parallel; per-URL blocks; one dead URL fails alone.

30s timeout; GET or POST only; bodies over 1 MiB error out (not
truncated).`

// FetchTool returns the web fetch tool definition + handler.
func FetchTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "fetch",
			Description: fetchDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"query": llm.Object{
						"type":        "string",
						"description": "Web search query.",
					},
					"max_results": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Top N results. Default %d.", searchDefaultMaxResults),
					},
					"fetch_pages": llm.Object{
						"type":        "boolean",
						"description": "Also fetch each result's body (default true).",
					},
					"max_chars_per_page": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Per-page cap when fetch_pages=true. Default %d.", searchDefaultMaxCharsPerPage),
					},
					"url": llm.Object{
						"type":        "string",
						"description": "Single URL to fetch.",
					},
					"urls": llm.Object{
						"type":        "array",
						"description": fmt.Sprintf("Up to %d URLs fetched in parallel.", fetchMaxURLs),
						"items": llm.Object{
							"type": "string",
						},
					},
					"method": llm.Object{
						"type":        "string",
						"description": "GET (default) or POST.",
					},
					"headers": llm.Object{
						"type":        "object",
						"description": "Request headers, e.g. {\"Accept\": \"text/html\"}.",
					},
					"body": llm.Object{
						"type":        "string",
						"description": "Request body for POST.",
					},
					"raw": llm.Object{
						"type":        "boolean",
						"description": "Skip HTML-to-text and JSON pretty-print.",
					},
					"links": llm.Object{
						"type":        "boolean",
						"description": "Also return links grouped by host.",
					},
					"metadata": llm.Object{
						"type":        "boolean",
						"description": "Also return title/OG/date/author metadata.",
					},
					"outline": llm.Object{
						"type":        "boolean",
						"description": "Also return the h1-h6 heading tree.",
					},
					"max_chars": llm.Object{
						"type":        "integer",
						"description": "Body cap. Default 8K; negative = unlimited.",
					},
					"redirects": llm.Object{
						"type":        "string",
						"description": "follow (default) | manual (3xx + Location) | none.",
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in fetchInput
			_ = json.Unmarshal(input, &in)
			// Search-mode: the query is the identity.
			if q := strings.TrimSpace(in.Query); q != "" {
				return q
			}
			// No "fetch " prefix: the row already prints the tool name, and
			// "fetch fetch https://…" is noise. The url alone identifies the call;
			// for multi-URL mode the count is the short identity.
			if u := strings.TrimSpace(in.URL); u != "" {
				return u
			}
			if n := len(in.URLs); n > 0 {
				return fmt.Sprintf("%d urls", n)
			}
			return ""
		},
		Run: runFetch,
	}
}

type fetchInput struct {
	URL             string          `json:"url"`
	URLs            []string        `json:"urls,omitempty"`
	Query           string          `json:"query,omitempty"`
	MaxResults      int             `json:"max_results,omitempty"`
	FetchPages      *bool           `json:"fetch_pages,omitempty"`
	MaxCharsPerPage int             `json:"max_chars_per_page,omitempty"`
	Method          string          `json:"method,omitempty"`
	Headers         json.RawMessage `json:"headers,omitempty"`
	Body            string          `json:"body,omitempty"`
	Raw             bool            `json:"raw,omitempty"`
	Links           bool            `json:"links,omitempty"`
	Metadata        bool            `json:"metadata,omitempty"`
	Outline         bool            `json:"outline,omitempty"`
	MaxChars        int             `json:"max_chars,omitempty"`
	Redirects       string          `json:"redirects,omitempty"`
}

// fetchOptions is the validated, normalized option set shared by single and
// multi-URL fetches. Parsing happens once per call; fetchOne reads it without
// mutation, so the same options are safe to share across concurrent fetches.
type fetchOptions struct {
	method    string
	headers   map[string]string
	body      string
	raw       bool
	links     bool
	metadata  bool
	outline   bool
	maxChars  int
	redirects string
}

// fetchResult is the structured outcome of one URL fetch. Errors during the
// HTTP roundtrip land in err; the caller renders them per-block rather than
// failing the whole tool call (so one dead URL in multi-URL mode doesn't kill
// the other results).
type fetchResult struct {
	url         string
	finalURL    string // URL after redirects; empty if no redirects were followed
	location    string // Location header value (set for 3xx responses)
	statusCode  int
	contentType string
	bodyText    string
	bytes       int
	elapsed     time.Duration
	links       []linkEntry
	metadata    *pageMetadata
	outline     []headingEntry
	err         error
}

func runFetch(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in fetchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse fetch arguments: %w", err)
	}

	// Search mode: when query is set, branch into runSearch. Mutually
	// exclusive with url/urls — calling with both returns an argument
	// error rather than guessing which one the agent meant.
	if q := strings.TrimSpace(in.Query); q != "" {
		if strings.TrimSpace(in.URL) != "" || len(in.URLs) > 0 {
			return tooldef.Result{}, fmt.Errorf("fetch: query is mutually exclusive with url/urls")
		}
		fetchPages := true // default-on
		if in.FetchPages != nil {
			fetchPages = *in.FetchPages
		}
		return runSearch(ctx, q, searchOptions{
			maxResults:      in.MaxResults,
			fetchPages:      fetchPages,
			maxCharsPerPage: in.MaxCharsPerPage,
		})
	}

	opts, err := parseOptions(in)
	if err != nil {
		return tooldef.Result{}, err
	}

	// Single-URL mode.
	if u := strings.TrimSpace(in.URL); u != "" {
		startedAt := time.Now()
		res := fetchOne(ctx, u, opts)
		if res.err != nil {
			return tooldef.Result{}, fmt.Errorf("fetch failed: %w", res.err)
		}
		content := renderBlock(res)
		// Summary shows the final-destination host (after redirects), falling
		// back to the requested URL when no redirect happened. This is the
		// place the agent looks to tell "did I end up where I asked to go?"
		summaryURL := res.finalURL
		if summaryURL == "" {
			summaryURL = res.url
		}
		return tooldef.Result{
			Content: content,
			Summary: Summary(summaryURL, res.statusCode, res.bytes, time.Since(startedAt)),
			Output:  content,
		}, nil
	}

	// Multi-URL mode.
	if len(in.URLs) > 0 {
		urls := in.URLs
		if len(urls) > fetchMaxURLs {
			urls = urls[:fetchMaxURLs]
		}
		results := fetchMany(ctx, urls, opts)
		content := renderMultiResult(urls, results)
		ok, fail := 0, 0
		for _, r := range results {
			if r.err != nil {
				fail++
			} else {
				ok++
			}
		}
		summary := fmt.Sprintf("%d ok · %d failed · %d urls", ok, fail, len(urls))
		return tooldef.Result{Content: content, Summary: summary, Output: content}, nil
	}

	return tooldef.Result{}, fmt.Errorf("url is required")
}

// parseOptions validates the shared options and returns a normalized
// fetchOptions. Returns an error on invalid method, headers, or redirects
// policy.
func parseOptions(in fetchInput) (fetchOptions, error) {
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodPost {
		return fetchOptions{}, fmt.Errorf("unsupported method %q: use GET or POST", method)
	}
	headers, err := parseHeadersInput(in.Headers)
	if err != nil {
		return fetchOptions{}, err
	}
	redirects := strings.ToLower(strings.TrimSpace(in.Redirects))
	if redirects == "" {
		redirects = redirectFollow
	}
	switch redirects {
	case redirectFollow, redirectManual, redirectNone:
	default:
		return fetchOptions{}, fmt.Errorf("invalid redirects %q: use follow, manual, or none", in.Redirects)
	}
	return fetchOptions{
		method:    method,
		headers:   headers,
		body:      strings.TrimSpace(in.Body),
		raw:       in.Raw,
		links:     in.Links,
		metadata:  in.Metadata,
		outline:   in.Outline,
		maxChars:  in.MaxChars,
		redirects: redirects,
	}, nil
}

// parseHeadersInput accepts headers in two forms:
//   - A JSON object: {"Accept": "text/html", "X-Custom": "1"} (preferred).
//   - A JSON-encoded string containing a JSON object:
//     "{\"Accept\":\"text/html\"}" (legacy, kept for backward compatibility).
//
// Empty / null input returns an empty (no-header) map. Any other shape or
// invalid JSON returns an "invalid headers JSON" error so the existing
// error-asserting tests still match.
func parseHeadersInput(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]string{}, nil
	}
	// Object form: {\"k\":\"v\", ...}.
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err == nil {
		return m, nil
	}
	// Legacy string form: a JSON string whose value is itself a JSON object.
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("invalid headers JSON: expected object or string, got %s", string(raw))
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return map[string]string{}, nil
	}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("invalid headers JSON: %w", err)
	}
	return m, nil
}

// fetchOne fetches a single URL and returns a structured result. Errors
// during the HTTP roundtrip land in res.err; the caller renders them.
func fetchOne(ctx context.Context, url string, opts fetchOptions) fetchResult {
	startedAt := time.Now()
	res := fetchResult{url: url}

	req, err := http.NewRequestWithContext(ctx, opts.method, url, strings.NewReader(opts.body))
	if err != nil {
		res.err = fmt.Errorf("build request: %w", err)
		res.elapsed = time.Since(startedAt)
		return res
	}
	req.Header.Set("User-Agent", "a1-fetch/1.0")
	req.Header.Set("Accept", "*/*")
	for k, v := range opts.headers {
		req.Header.Set(k, v)
	}

	// The turn context carries no deadline (the stop button cancels it), so the
	// client timeout is the only bound on a peer that never responds.
	client := &http.Client{
		Timeout: fetchDefaultTimeout,
	}
	// Wire the redirect policy. follow = Go default (no CheckRedirect). manual
	// returns the 3xx response so the agent sees the Location. none errors on
	// any redirect, surfaced as a fetch error in renderBlock.
	switch opts.redirects {
	case redirectManual:
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	case redirectNone:
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) == 0 {
				return nil
			}
			return fmt.Errorf("redirect blocked: %s → %s", via[len(via)-1].URL.String(), req.URL.String())
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		res.err = fmt.Errorf("fetch failed: %w", err)
		res.elapsed = time.Since(startedAt)
		return res
	}
	defer resp.Body.Close()

	limited := &io.LimitedReader{R: resp.Body, N: fetchMaxBytes}
	body, err := io.ReadAll(limited)
	if err != nil {
		res.err = fmt.Errorf("read response: %w", err)
		res.elapsed = time.Since(startedAt)
		return res
	}
	if limited.N <= 0 {
		res.err = fmt.Errorf("response exceeded %d bytes", fetchMaxBytes)
		res.elapsed = time.Since(startedAt)
		return res
	}
	res.bytes = len(body)
	res.statusCode = resp.StatusCode
	res.contentType = resp.Header.Get("Content-Type")
	res.location = resp.Header.Get("Location")
	// After redirects, resp.Request.URL is the final URL the client ended up
	// at. Capture it so renderBlock can show "Final URL:" when it differs
	// from the requested URL.
	if resp.Request != nil && resp.Request.URL != nil {
		res.finalURL = resp.Request.URL.String()
	}
	bodyStr := string(body)

	// Extract structured data from the raw HTML before htmlToText strips
	// the tags. All three are HTML-only — non-HTML responses have nothing
	// to extract.
	if isHTMLContent(res.contentType) {
		if opts.links {
			res.links = extractLinks(bodyStr, url)
		}
		if opts.metadata {
			m := extractMetadata(bodyStr, url)
			res.metadata = &m
		}
		if opts.outline {
			res.outline = extractOutline(bodyStr)
		}
	}

	// Body text: HTML → text, JSON → pretty-print, text passthrough, binary
	// placeholder. raw=true skips both transformations and returns bytes as-is.
	switch {
	case isHTMLContent(res.contentType) && !opts.raw:
		res.bodyText = htmlToText(bodyStr, opts.maxChars)
	case isHTMLContent(res.contentType) && opts.raw:
		res.bodyText = bodyStr
	case isJSONContent(res.contentType) && !opts.raw:
		res.bodyText = prettyPrintJSON(bodyStr, opts.maxChars)
	case isTextContent(res.contentType):
		res.bodyText = bodyStr
	default:
		res.bodyText = fmt.Sprintf("[binary %s %d bytes]", res.contentType, len(body))
	}

	res.elapsed = time.Since(startedAt)
	return res
}

// fetchMany fetches up to N URLs concurrently (cap fetchMaxConcurrent).
// Results are returned in the same order as the input URLs so the renderer
// can match blocks to inputs.
func fetchMany(ctx context.Context, urls []string, opts fetchOptions) []fetchResult {
	results := make([]fetchResult, len(urls))
	sem := make(chan struct{}, fetchMaxConcurrent)
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = fetchOne(ctx, url, opts)
		}(i, u)
	}
	wg.Wait()
	return results
}

func isTextContent(ct string) bool {
	return strings.Contains(ct, "text/") || strings.Contains(ct, "json") ||
		strings.Contains(ct, "xml") || strings.Contains(ct, "javascript") ||
		strings.Contains(ct, "css")
}

func isHTMLContent(ct string) bool {
	return strings.Contains(ct, "html")
}

// isJSONContent reports whether the response is JSON. Catches application/json,
// application/vnd.api+json, application/ld+json, text/json, etc. — anything
// with "json" in the content-type that isn't also HTML.
func isJSONContent(ct string) bool {
	return strings.Contains(ct, "json") && !isHTMLContent(ct)
}
