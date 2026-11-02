package fetchtool

import (
        "context"
        "encoding/json"
        "fmt"
        "net/url"
        "strings"

        "github.com/damien1141/a1/internal/bw"
        "github.com/damien1141/a1/internal/tools/tooldef"
)

// bwSearchEngine searches the web through the BetterWright browser daemon.
// It is the captcha-resistant fallback the router has always wanted: a real
// browser session on a persistent profile, so bot walls that kill the DDG
// Lite HTTP scraper do not fire.
//
// BetterWright blocks automation of the big public search UIs (Google, Bing,
// DuckDuckGo) on its managed browser, so this engine drives Mojeek — an
// independent index with no aggressive bot walls and a stable results
// layout. If that ever changes, the engine errors and the router falls
// through to whatever fallback comes next.
//
// Registered only when the betterwright CLI resolves; without the daemon
// the router behaves exactly as before.
//
// Traffic rides the BACKGROUND lane on a DEDICATED profile (bw.BackgroundProfile):
// a profile owns its own daemon, so these headless spawns can never
// cold-spawn the shared default-profile daemon headless and make the
// interactive "/browser open" land in an invisible window — the isolation
// that keeps the user's live browser live.
type bwSearchEngine struct{}

func newBWSearchEngine() *bwSearchEngine { return &bwSearchEngine{} }

func (bwSearchEngine) Name() string { return "betterwright" }

func (bwSearchEngine) Matches(_ string) bool {
        // General-purpose fallback, not a domain-specific match — the router
        // tries it (after ddglite) only when every matching engine came up
        // empty.
        return false
}

// bwSearchJS navigates to Mojeek's results page and extracts ranked
// results. Selector sets are deliberately redundant: Mojeek's layout has
// shifted over the years, and a partial harvest beats a parse error.
const bwSearchJS = `await page.goto(%s);
const results = await page.evaluate(() => {
        const out = [];
        const seen = new Set();
        for (const li of document.querySelectorAll('ul.results-standard li, ul.results li, li.result')) {
                const a = li.querySelector('h2 a, a.title, a.ob, a[href^="http"]');
                if (!a) continue;
                const href = a.href || a.getAttribute('href') || '';
                const title = (a.textContent || '').trim();
                if (!href.startsWith('http') || !title || seen.has(href)) continue;
                seen.add(href);
                const sn = li.querySelector('p.s, p.desc, p');
                out.push({url: href, title: title, snippet: sn ? (sn.textContent || '').trim() : ''});
        }
        return out;
});
return results.slice(0, %d);`

func (e *bwSearchEngine) Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error) {
        q := strings.TrimSpace(query)
        if q == "" {
                return nil, fmt.Errorf("betterwright: empty query")
        }
        max := opts.maxResults
        if max <= 0 {
                max = 5
        }
        searchURL := "https://www.mojeek.com/search?q=" + url.QueryEscape(q)
        js := fmt.Sprintf(bwSearchJS, bw.JSString(searchURL), max)

        env, err := bw.RunLane(ctx, bw.BackgroundSession, bw.BackgroundProfile, false, js)
        if err != nil {
                return nil, fmt.Errorf("betterwright: %w", err)
        }
        var hits []struct {
                URL     string `json:"url"`
                Title   string `json:"title"`
                Snippet string `json:"snippet"`
        }
        if jerr := json.Unmarshal(env.Result, &hits); jerr != nil {
                return nil, fmt.Errorf("betterwright: unexpected daemon result %.200s", string(env.Result))
        }
        if len(hits) == 0 {
                return nil, fmt.Errorf("betterwright: no results parsed from Mojeek")
        }
        if len(hits) > max {
                hits = hits[:max]
        }

        results := make([]searchResult, len(hits))
        sources := make([]tooldef.Source, len(hits))
        for i, h := range hits {
                host := hostOf(h.URL)
                host = strings.TrimPrefix(host, "www.")
                results[i] = searchResult{rank: i + 1, url: h.URL, title: h.Title, snippet: h.Snippet, host: host}
                sources[i] = tooldef.Source{
                        Rank:       i + 1,
                        URL:        h.URL,
                        Title:      truncate(h.Title, 200),
                        Host:       host,
                        FaviconURL: formatFaviconURL(host),
                        Snippet:    truncate(h.Snippet, 400),
                }
        }
        return &SearchResponse{Results: results, Sources: sources}, nil
}
