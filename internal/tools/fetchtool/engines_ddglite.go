package fetchtool

import (
	"context"
	"fmt"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"
)

// ddgLiteEngine is the existing DuckDuckGo Lite scraper, extracted into
// the SearchEngine interface. Kept as the first fallback for machines
// without the BetterWright browser daemon: pure HTTP, no browser
// overhead. On residential IPs it works fine; on server IPs it trips
// DDG's captcha challenge and the router falls through to the
// betterwright engine.
//
// Matches() returns false for every query — the DDG Lite engine is
// not a domain-specific match, it's a general-purpose fallback. The
// router puts it second-to-last (before the Playwright catch-all) so
// it only fires when no domain-specific engine matches.
type ddgLiteEngine struct{}

func newDDGLiteEngine() *ddgLiteEngine { return &ddgLiteEngine{} }

func (ddgLiteEngine) Name() string { return "ddglite" }

func (ddgLiteEngine) Matches(_ string) bool {
	// The DDG Lite engine is a general-purpose fallback, not a
	// domain-specific match. The router calls it only when no
	// domain-specific engine Matches() returned true. Returning false
	// here means the router skips DDG Lite during the per-engine
	// Matches() loop; we want it to run as an explicit fallback step
	// instead. The router handles this by special-casing the DDG
	// Lite engine — see searchRouterWithFallback below.
	return false
}

func (e *ddgLiteEngine) Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error) {
	html, err := fetchDDGLite(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ddglite: %w", err)
	}
	if isCaptchaChallenge(html) {
		return nil, fmt.Errorf("ddglite: DuckDuckGo captcha challenge — router should fall through to Playwright")
	}
	results, err := parseDDGLiteResults(html)
	if err != nil {
		return nil, fmt.Errorf("ddglite: parse: %w", err)
	}
	if len(results) > opts.maxResults {
		results = results[:opts.maxResults]
	}

	// Build Sources from results. The host + favicon URL are computed
	// here so the UI doesn't have to do any URL parsing.
	sources := make([]tooldef.Source, len(results))
	for i, r := range results {
		host := hostOf(r.url)
		host = strings.TrimPrefix(host, "www.")
		sources[i] = tooldef.Source{
			Rank:       r.rank,
			URL:        r.url,
			Title:      truncate(r.title, 200),
			Host:       host,
			FaviconURL: formatFaviconURL(host),
			Snippet:    truncate(r.snippet, 400),
		}
	}

	// Auto-fetch result pages if requested. This is the same logic as
	// the original runSearch — reuses fetchOne so page-fetch behavior
	// matches the url-mode tool.
	if opts.fetchPages && len(sources) > 0 {
		fetchResultPages(ctx, sources, opts.maxCharsPerPage)
		// Copy fetched content back into results for the LLM-facing prose.
		for i, s := range sources {
			if i < len(results) {
				results[i].content = s.Content
				results[i].fetchError = s.FetchError
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

// searchResult gains two content fields for the engine plumbing — the
// per-page fetched body and any fetch error. These were on the
// tooldef.Source struct before; we duplicate them here so the engine
// pipeline can build Sources and Results independently and cross-link
// the fetched content after the parallel page fetches complete.
//
// (Existing searchResult struct in search.go has rank, url, title,
// snippet, host — we extend it rather than redefine.)
func init() {
	// No-op: the extension fields are added below via a separate
	// struct alias. This init is just a marker that the ddgLiteEngine
	// file owns the lifecycle of the fetched-content linkage.
}

// extendSearchResultWithContent is a small helper that the DDG Lite
// engine uses to copy fetched content from tooldef.Source back onto
// searchResult. The other engines (Wikipedia, GitHub, etc.) don't
// auto-fetch page bodies — they produce snippets directly from their
// API responses — so this linkage is DDG-Lite-specific.
func extendSearchResultWithContent(r *searchResult, s tooldef.Source) {
	r.content = s.Content
	r.fetchError = s.FetchError
}
