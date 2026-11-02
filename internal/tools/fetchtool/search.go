package fetchtool

import (
        "context"
        "fmt"
        "io"
        "net/http"
        "net/url"
        "strings"
        "sync"
        "time"

        "github.com/damien1141/a1/internal/tools/tooldef"
)

// Search-mode constants. Default values are tuned for token-budget-aware
// auto-fetch: 5 results × 4K chars each = ~20K chars of page content per
// search call, which fits comfortably in a local-LLM context window while
// still giving the model enough body to synthesize a cited answer.
const (
        searchDefaultMaxResults       = 5
        searchDefaultMaxCharsPerPage  = 4000
        searchMaxConcurrentPageFetch  = 5
        searchDefaultTimeout          = 30 * time.Second
        searchMaxBodyBytes            = 1 << 20 // 1 MiB cap on the DDG result page itself
        searchFaviconURLTemplate      = "https://icons.duckduckgo.com/ip3/%s.ico"
        searchDDGLiteEndpoint         = "https://lite.duckduckgo.com/lite/"
        searchBrowserUserAgent        = "Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0"
)

// searchOptions is the validated option set for search-mode fetch calls.
// Mirrors fetchOptions for the page-fetch phase (we reuse fetchOne on the
// auto-fetched result URLs, so the page-fetch behavior matches the
// url-mode tool's).
type searchOptions struct {
        maxResults      int  // top N results to keep (1..N)
        fetchPages      bool // when true, fetch each result's body in parallel
        maxCharsPerPage int  // per-page body cap when fetchPages=true
}

// searchResult is one DDG listing entry, parsed from the result HTML.
// The structure mirrors the well-known Lite endpoint layout: each result
// is a 3-row table block with the actual URL in an <a class="result-link">,
// the snippet in a <td class="result-snippet">, and a displayed URL string
// in a <span class="link-text">.
//
// URLs may be either:
//   - direct: href="https://actual-url"
//   - DDG-wrapped: href="//duckduckgo.com/l/?uddg=<urlencoded actual url>&rut=..."
//
// unwrapDDGRedirect handles both forms.
//
// The content + fetchError fields are populated by the DDG Lite engine
// after auto-fetching result pages — they mirror the corresponding
// tooldef.Source fields so the engine pipeline can build Sources and
// Results independently and cross-link the fetched content after the
// parallel page fetches complete.
type searchResult struct {
        rank       int
        url        string // unwrapped, canonical
        title      string
        snippet    string
        host       string
        content    string // populated by fetchResultPages when fetchPages=true
        fetchError string // populated when the per-page fetch failed
}

// runSearch is the entry point for query-mode fetches. It delegates to
// the smart search router, which dispatches to the first matching
// domain-specific engine (Frankfurter, Stack Exchange, GitHub, arxiv,
// Wikipedia) and falls back to DDG Lite for general queries. The
// router handles engine selection, error fallback, and response
// assembly; this function just normalizes options and converts the
// router's SearchResponse into a tooldef.Result.
//
// The DDG Lite engine's captcha detection lives in the engine itself;
// when it fires, the error is surfaced to the user with a clear
// message ("DuckDuckGo captcha challenge — retry later, use a proxy,
// or switch to a residential IP"). The user can then retry from a
// residential network or rely on the domain-specific engines (which
// never captcha).
func runSearch(ctx context.Context, query string, opts searchOptions) (tooldef.Result, error) {
        query = strings.TrimSpace(query)
        if query == "" {
                return tooldef.Result{}, fmt.Errorf("search: empty query")
        }
        if opts.maxResults <= 0 {
                opts.maxResults = searchDefaultMaxResults
        }
        if opts.maxCharsPerPage <= 0 {
                opts.maxCharsPerPage = searchDefaultMaxCharsPerPage
        }

        router := newSearchRouter()
        resp, err := router.Search(ctx, query, opts)
        if err != nil {
                return tooldef.Result{}, fmt.Errorf("search: %w", err)
        }
        if resp == nil || (len(resp.Results) == 0 && resp.Answer == "") {
                return tooldef.Result{
                        Content: fmt.Sprintf("No results for: %s", query),
                        Detail:  query,
                        Summary: "0 results",
                }, nil
        }
        return resp.toResult(query), nil
}

// fetchDDGLite hits the DuckDuckGo Lite endpoint and returns the raw HTML.
// Uses a browser User-Agent and Accept headers — DDG serves an anomaly
// challenge page to anything that looks like a script. The 30s timeout
// matches fetchDefaultTimeout for symmetry with the url-mode fetches.
func fetchDDGLite(ctx context.Context, query string) (string, error) {
        u := searchDDGLiteEndpoint + "?q=" + url.QueryEscape(query)
        req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
        if err != nil {
                return "", err
        }
        req.Header.Set("User-Agent", searchBrowserUserAgent)
        req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
        req.Header.Set("Accept-Language", "en-US,en;q=0.5")
        req.Header.Set("DNT", "1")
        req.Header.Set("Upgrade-Insecure-Requests", "1")

        client := &http.Client{Timeout: searchDefaultTimeout}
        resp, err := client.Do(req)
        if err != nil {
                return "", err
        }
        defer resp.Body.Close()
        body, err := io.ReadAll(io.LimitReader(resp.Body, searchMaxBodyBytes))
        if err != nil {
                return "", err
        }
        if resp.StatusCode != http.StatusOK {
                return "", fmt.Errorf("DDG Lite returned %d", resp.StatusCode)
        }
        return string(body), nil
}

// isCaptchaChallenge detects DDG's anomaly-modal challenge page. The
// signature is the literal `anomaly-modal` class string, which appears
// in the challenge form but never in a normal result page.
func isCaptchaChallenge(html string) bool {
        return strings.Contains(html, "anomaly-modal")
}

// parseDDGLiteResults extracts the ranked result list from a DDG Lite
// HTML response. Returns an empty slice (no error) when the page has
// results but the parser couldn't find any result-link entries (DDG
// sometimes returns an empty result page for nonsensical queries).
//
// The parser is intentionally regex-based rather than DOM-based:
//   - DDG Lite's HTML is HTML 4.01 Transitional with broken nesting in
//     places; a strict DOM parser would emit warnings and possibly
//     reorder tags.
//   - The structure is stable (has been for years): each result is a
//     3-row table block with class="result-link" / class="result-snippet"
//     / class="link-text".
//   - Regex keeps the parser fast (microseconds for a typical result page)
//     and avoids a new dependency (would otherwise need golang.org/x/net/html).
//
// HTML comments are stripped first because some DDG pages (and our own
// test fixtures) carry documentation in <!-- ... --> blocks that may
// contain literal examples of the result-link markup — without
// stripping, the regex would match inside the comment and produce
// phantom results.
func parseDDGLiteResults(html string) ([]searchResult, error) {
        // Strip HTML comments before parsing. The comment regex handles
        // both <!-- ... --> and the unbalanced edge case where a comment
        // opens but the file ends before closing (treated as comment-to-EOF).
        html = stripHTMLComments(html)
        var results []searchResult
        // Find every <a ... class="result-link" ... href="...">title</a> entry.
        // Use a non-greedy capture so adjacent results don't bleed together.
        linkRe := `<a[^>]*class="result-link"[^>]*href="([^"]*)"[^>]*>([^<]*)</a>`
        links := findAllStringSubmatch(html, linkRe)
        if len(links) == 0 {
                // Some DDG Lite variants put href before class; try the alternate order.
                linkRe = `<a[^>]*href="([^"]*)"[^>]*class="result-link"[^>]*>([^<]*)</a>`
                links = findAllStringSubmatch(html, linkRe)
        }
        for i, match := range links {
                rawURL := match[1]
                title := decodeEntities(match[2])
                title = strings.TrimSpace(title)
                if title == "" || rawURL == "" {
                        continue
                }
                actualURL := unwrapDDGRedirect(rawURL)
                if actualURL == "" {
                        continue
                }
                // Strip protocol-relative URLs (//example.com/path → https://example.com/path).
                if strings.HasPrefix(actualURL, "//") {
                        actualURL = "https:" + actualURL
                }
                // Find the snippet that follows this result-link in the HTML.
                // DDG Lite puts the snippet in a <td class="result-snippet"> on
                // the next table row. We search forward from this link's end
                // position for the first result-snippet class.
                snippet := findSnippetAfter(html, match[0])
                results = append(results, searchResult{
                        rank:    i + 1,
                        url:     actualURL,
                        title:   title,
                        snippet: snippet,
                })
        }
        return results, nil
}

// unwrapDDGRedirect takes a possibly-DDG-wrapped URL and returns the
// actual target. Recognizes:
//   - "//duckduckgo.com/l/?uddg=<urlencoded actual url>&rut=..."
//   - "https://duckduckgo.com/l/?uddg=<urlencoded actual url>&rut=..."
//   - Direct URLs (returned unchanged)
//
// Returns "" when the URL looks like a DDG redirect but the uddg parameter
// is missing or malformed — the caller treats this as "skip this result".
func unwrapDDGRedirect(rawURL string) string {
        rawURL = strings.TrimSpace(rawURL)
        if rawURL == "" {
                return ""
        }
        // Normalize protocol-relative to https for url.Parse.
        if strings.HasPrefix(rawURL, "//") {
                rawURL = "https:" + rawURL
        }
        // Only attempt unwrap on duckduckgo.com/l/ URLs.
        if !strings.Contains(rawURL, "duckduckgo.com/l/") {
                return rawURL
        }
        u, err := url.Parse(rawURL)
        if err != nil {
                return ""
        }
        q := u.Query()
        uddg := q.Get("uddg")
        if uddg == "" {
                return ""
        }
        // uddg is already URL-decoded by url.Parse — return as-is.
        return uddg
}

// findSnippetAfter locates the result-snippet that follows the given link
// anchor in the HTML. DDG Lite's structure puts each result as 3 table
// rows: link, snippet, displayed-URL. We scan forward from the end of
// the link anchor and grab the first <td class="result-snippet">...</td>
// content we find, stopping at the next result-link (which would mean
// we've crossed into the next result).
func findSnippetAfter(html, linkAnchor string) string {
        endIdx := strings.Index(html, linkAnchor)
        if endIdx < 0 {
                return ""
        }
        startSearch := endIdx + len(linkAnchor)
        rest := html[startSearch:]
        // Cap the search window so we don't accidentally pick up a snippet
        // from a much later result if this one's snippet is missing.
        if len(rest) > 4000 {
                rest = rest[:4000]
        }
        // Find the first result-snippet block after this link. The (?s) flag
        // enables dotall mode so . matches newlines — DDG sometimes wraps
        // long snippets across multiple lines.
        snippetRe := `(?s)<td[^>]*class="result-snippet"[^>]*>(.*?)</td>`
        match := findFirstStringSubmatch(rest, snippetRe)
        if match == "" {
                return ""
        }
        // Strip any nested HTML tags (DDG sometimes wraps query terms in <b>).
        snippet := stripTags(match)
        snippet = decodeEntities(snippet)
        snippet = strings.TrimSpace(snippet)
        // Collapse runs of whitespace so multi-line snippets read as one line.
        snippet = collapseWhitespace(snippet)
        return snippet
}

// fetchResultPages fetches the body of each source's URL in parallel and
// populates s.Content (on success) or s.FetchError (on failure). The
// fetchPages semaphore caps concurrency at searchMaxConcurrentPageFetch
// to match the url-mode tool's behavior — DDG result pages can be slow,
// and unbounded parallelism would hammer the network.
//
// Reuses fetchOne (the existing single-URL fetch implementation) so the
// page-fetch behavior — HTML→text extraction, redirect handling, body
// cap, content-type detection — is identical to what the url-mode tool
// produces. The only difference is maxChars is set per-page from the
// search-mode config rather than the url-mode default.
func fetchResultPages(ctx context.Context, sources []tooldef.Source, maxCharsPerPage int) {
        if len(sources) == 0 {
                return
        }
        sem := make(chan struct{}, searchMaxConcurrentPageFetch)
        var wg sync.WaitGroup
        // fetchOne reads from the shared fetchOptions struct; we build a
        // minimal one with the per-page maxChars. The other fields (raw,
        // links, metadata, outline) are off so we get clean HTML→text only.
        opts := fetchOptions{
                method:    "GET",
                maxChars:  maxCharsPerPage,
                redirects: "follow",
        }
        for i := range sources {
                wg.Add(1)
                go func(idx int) {
                        defer wg.Done()
                        sem <- struct{}{}
                        defer func() { <-sem }()
                        res := fetchOne(ctx, sources[idx].URL, opts)
                        if res.err != nil {
                                sources[idx].FetchError = res.err.Error()
                                return
                        }
                        sources[idx].Content = res.bodyText
                }(i)
        }
        wg.Wait()
}

// truncate caps s at maxLen chars, appending "..." when truncated.
// Returns s unchanged when it's already within the limit. Used to keep
// titles and snippets in a sane range for both the LLM context and the
// UI's narrow source pill.
func truncate(s string, maxLen int) string {
        if len(s) <= maxLen {
                return s
        }
        if maxLen <= 3 {
                return s[:maxLen]
        }
        return s[:maxLen-3] + "..."
}

// findAllStringSubmatch is a thin wrapper around regexp compilation +
// FindAllStringSubmatch that returns [][]string (the full match + each
// capture group) for every non-overlapping match in s. Panics on invalid
// regex (caller-controlled patterns, so this is acceptable — a bug in
// the pattern is a programmer error, not a runtime error).
//
// Imported here to keep the regex usage local to this file; the existing
// fetchtool code uses the stdlib regexp package directly, but search.go's
// patterns are scoped to this file.
func findAllStringSubmatch(s, pattern string) [][]string {
        return submatchAll(s, pattern, false)
}

// findFirstStringSubmatch returns the first capture group of the first
// match of pattern in s, or "" when there's no match.
func findFirstStringSubmatch(s, pattern string) string {
        matches := submatchAll(s, pattern, true)
        if len(matches) == 0 || len(matches[0]) < 2 {
                return ""
        }
        return matches[0][1]
}

// submatchAll wraps regexp.Compile + FindAllStringSubmatch. The first
// boolean is "stop after first match" — useful when we only need the
// first result (snippet lookup) and want to short-circuit the regex
// engine for large inputs.
func submatchAll(s, pattern string, firstOnly bool) [][]string {
        re, err := regexpCompile(pattern)
        if err != nil {
                return nil
        }
        matches := re.FindAllStringSubmatch(s, -1)
        if firstOnly && len(matches) > 1 {
                return matches[:1]
        }
        return matches
}
