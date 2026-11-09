package fetchtool

import (
        "context"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/bw"
        "github.com/damien1141/a1/internal/debuglog"
        "github.com/damien1141/a1/internal/tools/tooldef"
)

// SearchEngine is one pluggable search backend. Implementations include
// domain-specific APIs (Wikipedia, Stack Exchange, GitHub, arxiv,
// Frankfurter), the existing DuckDuckGo Lite scraper, and a
// Playwright-driven browser fallback that bypasses captcha by driving
// a real Chromium session.
//
// Engines are stateless and safe for concurrent use — the router holds
// a slice of them and consults Matches() + Search() per query.
type SearchEngine interface {
        // Name returns a short, lowercase identifier for logging and the
        // "engine" field of the SearchResponse. Examples: "wikipedia",
        // "stackexchange", "github", "arxiv", "frankfurter", "playwright",
        // "ddglite".
        Name() string

        // Matches reports whether this engine is likely to produce useful
        // results for the given query. The router calls this in priority
        // order and dispatches to the first engine that returns true. The
        // check must be cheap (regex / keyword scan, no network) so the
        // router can probe every registered engine per query.
        //
        // False positives are tolerable — a Wikipedia match on a coding
        // query just means we try Wikipedia first; if it returns nothing,
        // the router can fall back. False negatives are also tolerable for
        // the catch-all engine (Playwright) which Matches() returns true on
        // every query.
        Matches(query string) bool

        // Search runs the engine and returns a SearchResponse. The router
        // injects the engine's Name() into the response; implementations
        // don't need to set it themselves.
        //
        // Errors are surfaced to the user via the tool result's error
        // field; the router then falls through to the next matching engine.
        Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error)
}

// SearchResponse is the structured outcome of one SearchEngine.Search
// call. The router may transform this into a tooldef.Result for the
// model + UI; engines themselves just produce the raw ranked list and
// an optional direct-answer string (for engines like Frankfurter that
// return a single definitive result rather than a list of links).
type SearchResponse struct {
        // Engine is the engine's Name(). Set by the router, not the engine.
        Engine string

        // Query echoes the input query (after normalization) for the
        // response header. Useful when the router transformed the query
        // before dispatching (e.g. stripped a "what is" prefix).
        Query string

        // Results is the ranked list. May be empty when the engine
        // returned no matches; the router decides whether to fall back to
        // another engine or surface an empty result.
        Results []searchResult

        // Answer is an optional direct-answer string for engines that
        // produce one (Frankfurter returns "100 USD = 92.34 EUR (rate:
        // 0.9234, as of 2024-01-15)"). When non-empty, the router puts
        // this at the top of the tooldef.Result.Content above the result
        // list. Empty for list-style engines.
        Answer string

        // Sources is the pre-built tooldef.Source slice, ready for the UI.
        // Engines that produce a structured result list populate this so
        // the router doesn't have to re-do the host/favicon computation.
        // Empty when the engine has no per-result source-card data (rare;
        // most engines produce sources).
        Sources []tooldef.Source
}

// searchRouter dispatches queries to the first matching domain engine
// in its priority-ordered list; when every match comes up empty (or
// nothing matches), the fallback engines run in order until one
// produces results.
//
// Domain engines (Wikipedia, Stack Exchange, GitHub, arxiv, Frankfurter)
// have tight Matches() predicates and run first. Fallback engines
// (DDG Lite, the BetterWright browser engine when the daemon is
// installed) never Match and only run in Phase 2 — cheap HTTP first,
// the browser when the scraper dies on a captcha wall.
type searchRouter struct {
        engines   []SearchEngine
        fallbacks []SearchEngine
        // jev is the optional decision-model classifier (nil = predicates
        // only, the pre-JEV behavior). When present it picks the engine
        // before the predicate order runs; see jev_classify.go.
        jev *jevClassifier
}

// newSearchRouter constructs the default router with all built-in
// engines in priority order. Engines are stateless so the router owns
// the only instances.
//
// Priority order rationale:
//  1. Time / weather — the clock and forecast subtools. Tight, explicit
//     question shapes ("what time is it", "weather in Reykjavik"); time
//     answers with no network at all and weather with one keyless API
//     call, so they outrank everything. They also always produce an
//     answer — the router never falls through past them — which is why
//     their predicates stay deliberately narrow.
//  2. Frankfurter — tightest predicate (currency pattern), instant
//     response, never wrong when it matches.
//  3. MDN — web-platform reference (css/html/dom/svg); runs before the
//     broad programming predicates so doc queries land on docs.
//  4. npm / crates — language-specific package registries; the
//     authoritative index when the query names the ecosystem, so they
//     run before GitHub's language-agnostic repo search.
//  5. GitHub — library/package discovery for every other language;
//     common in coding queries (before SE: library queries with "go"
//     match both).
//  6. Reddit / Hacker News — community opinion. They run BEFORE Stack
//     Exchange on purpose: SE's broad programming predicate matches any
//     query that so much as names a language, and "what do people think
//     of bun" must reach the forums, not the Q&A index. Reddit edges out
//     HN when the query names it explicitly.
//  7. Stack Exchange — the broad programming signal set, the single
//     most useful engine for a coding agent.
//  8. arxiv — academic queries are rare but unambiguous when they
//     match ("paper", "research", "arxiv").
//  9. Wikipedia — factual queries; less common in coding work but
//     covers the "what is X concept" case well.
//
// The first fallback is the merged web engine: DuckDuckGo Lite run in
// parallel with Hacker News and Reddit, results interleaved 2:1:1 so
// the source list gains community hosts without losing web coverage.
// When its primary (DDG Lite) dies — the captcha wall on datacenter
// IPs — the merged engine errors and the router falls through to the
// BetterWright browser fallback, whose real Chromium session survives
// the bot walls. Registered only when the betterwright CLI resolves,
// so machines without the daemon keep the HTTP-only behavior.
func newSearchRouter() *searchRouter {
        fallbacks := []SearchEngine{newMergedWebEngine()}
        // The BetterWright browser fallback rides behind the merged web
        // engine: a real browser session survives the bot walls that kill
        // DDG Lite on datacenter IPs (the merged engine propagates its
        // primary's error for exactly this handoff). Registered only when
        // the betterwright CLI resolves, so machines without the daemon keep
        // the exact pre-BetterWright behavior.
        if bw.Available() {
                fallbacks = append(fallbacks, newBWSearchEngine())
        }
        return &searchRouter{
                engines: []SearchEngine{
                        newTimeEngine(),     // zero-network direct answer, tightest predicate
                        newWeatherEngine(),  // direct forecast; zero context cost until asked
                        newFrankfurterEngine(),
                        newMDNEngine(),
                        newNpmEngine(),
                        newCratesEngine(),
                        newGitHubEngine(),     // before SE: library queries with "go" match both
                        newRedditEngine(),     // before HN: an explicit reddit ask outranks generic discussion
                        newHackerNewsEngine(), // before SE: discussion phrasings must not fall to the Q&A index
                        newStackExchangeEngine(),
                        newArxivEngine(),
                        newWikipediaEngine(),
                },
                fallbacks: fallbacks,
                jev:       newJevClassifier(),
        }
}

// WithCatchAllEngine appends engine as an additional fallback after the
// built-ins (the merged web engine, betterwright-when-installed). The
// router tries fallback engines in order once every Matches() engine came
// up empty, so an injected engine only ever runs last. Returns the router
// for chaining at construction sites.
func (r *searchRouter) WithCatchAllEngine(engine SearchEngine) *searchRouter {
        if engine != nil {
                r.fallbacks = append(r.fallbacks, engine)
        }
        return r
}

// Search dispatches query to the first matching engine. When that
// engine errors or returns no results, the router tries the next
// matching engine. If no domain-specific engine matches, the router
// falls through to the fallback engines — those whose Matches()
// returns false for every query (the merged web engine, then the
// BetterWright browser engine when the daemon is installed), tried in
// order.
//
// Errors from intermediate engines are swallowed — only the final
// error (when even the catch-all fails) is returned to the caller.
// This matches user expectation: "search failed" should mean "we
// tried everything and nothing worked," not "the first engine we
// tried had a network blip."
func (r *searchRouter) Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error) {
        var lastErr error
        // Phase 0: the JEV decision model, when configured, picks the engine
        // (one Choice question, bounded by jevClassifyTimeout). Its pick gets
        // first dibs but no veto: a misspelled query, an empty result, or an
        // engine error falls through to the predicate order untouched, and
        // every failure mode of the call itself (no key, timeout, bad answer)
        // means the predicates run exactly as they always did.
        if r.jev != nil {
                if name, ok := r.jev.classify(ctx, query); ok {
                        if eng := r.engineByName(name); eng != nil {
                                resp, err := eng.Search(ctx, query, opts)
                                if err == nil && resp != nil && (len(resp.Results) > 0 || resp.Answer != "") {
                                        setEngineAndQuery(resp, eng, query)
                                        debuglog.Logf("fetch: jev routed %q to %q", query, name)
                                        return resp, nil
                                }
                                if err != nil {
                                        lastErr = err
                                }
                                debuglog.Logf("fetch: jev pick %q produced nothing for %q; falling back to predicates", name, query)
                        }
                }
        }
        // Phase 1: try domain-specific engines whose Matches() returns true.
        for _, eng := range r.engines {
                if !eng.Matches(query) {
                        continue
                }
                resp, err := eng.Search(ctx, query, opts)
                if err == nil && resp != nil && (len(resp.Results) > 0 || resp.Answer != "") {
                        setEngineAndQuery(resp, eng, query)
                        return resp, nil
                }
                if err != nil {
                        lastErr = err
                }
        }
        // Phase 2: no domain-specific engine matched (or all returned
        // empty). Fall through to the fallback engines in order (the merged
        // web engine first, then the BetterWright browser engine when
        // installed). The first one that produces results wins; a
        // captcha-killed merge falls through to the daemon, and a missing
        // daemon simply never registered.
        for _, eng := range r.fallbacks {
                resp, err := eng.Search(ctx, query, opts)
                if err == nil && resp != nil && (len(resp.Results) > 0 || resp.Answer != "") {
                        setEngineAndQuery(resp, eng, query)
                        return resp, nil
                }
                if err != nil {
                        lastErr = err
                }
        }
        if lastErr != nil {
                return nil, lastErr
        }
        return &SearchResponse{
                Engine: "none",
                Query:  query,
        }, nil
}

// engineByName finds a registered engine (domain or fallback) by name —
// the JEV classifier's pick resolves through here, so an unknown name can
// never dispatch.
func (r *searchRouter) engineByName(name string) SearchEngine {
        for _, eng := range r.engines {
                if eng.Name() == name {
                        return eng
                }
        }
        for _, eng := range r.fallbacks {
                if eng.Name() == name {
                        return eng
                }
        }
        return nil
}

// setEngineAndQuery stamps the engine name and query on a successful
// response — but keeps ones the engine set itself. The merged web
// engine labels its response with the engines that actually contributed
// ("ddglite+hackernews"), which is more informative than its static
// name; single engines that self-label (all of them) are unaffected.
func setEngineAndQuery(resp *SearchResponse, eng SearchEngine, query string) {
        if resp.Engine == "" {
                resp.Engine = eng.Name()
        }
        if resp.Query == "" {
                resp.Query = query
        }
}

// classifyQuery returns the engine name that would handle the given
// query under the default router. Exposed for tests + for users who
// want to see what the router would pick before actually running it.
//
// Returns the first fallback engine's name when no domain-specific
// engine matches.
func classifyQuery(query string) string {
        r := newSearchRouter()
        for _, eng := range r.engines {
                if eng.Matches(query) {
                        return eng.Name()
                }
        }
        // No domain-specific engine matched. Return the first fallback —
        // the engine that would run first in Phase 2.
        if len(r.fallbacks) > 0 {
                return r.fallbacks[0].Name()
        }
        return "none"
}

// toResult converts a SearchResponse into the tooldef.Result shape the
// fetch tool returns. Builds the prose Content (for the LLM) from the
// Answer + ranked Results, and passes through the pre-built Sources
// (for the UI's source pills).
//
// The router calls this after a successful engine dispatch. Engines
// that pre-build Sources (Wikipedia, Stack Exchange, GitHub, arxiv,
// DDG Lite) get a free pass through; engines that only produce
// Results (Frankfurter, Playwright) get their Sources built here
// from the Results list.
func (r *SearchResponse) toResult(query string) tooldef.Result {
        sources := r.Sources
        if len(sources) == 0 && len(r.Results) > 0 {
                sources = make([]tooldef.Source, len(r.Results))
                for i, res := range r.Results {
                        host := hostOf(res.url)
                        host = strings.TrimPrefix(host, "www.")
                        sources[i] = tooldef.Source{
                                Rank:       res.rank,
                                URL:        res.url,
                                Title:      truncate(res.title, 200),
                                Host:       host,
                                FaviconURL: formatFaviconURL(host),
                                Snippet:    truncate(res.snippet, 400),
                                Content:    res.content,
                                FetchError: res.fetchError,
                        }
                }
        }

        var sb strings.Builder
        if r.Answer != "" {
                sb.WriteString(r.Answer)
                sb.WriteString("\n\n")
        }
        sb.WriteString("Engine: " + r.Engine + "\n")
        sb.WriteString("Query: " + query + "\n\n")
        for _, s := range sources {
                sb.WriteString(strings.TrimSpace(formatSourceBlock(s)))
                sb.WriteString("\n")
        }

        return tooldef.Result{
                Content: sb.String(),
                Detail:  query,
                Summary: summarizeSearch(r.Engine, len(sources)),
                Output:  sb.String(),
                Sources: sources,
        }
}

// formatSourceBlock renders one Source as a compact text block for the
// LLM-facing Content. The format mirrors the original search-mode fetch
// output so the LLM sees a consistent shape regardless of which engine
// produced the result.
func formatSourceBlock(s tooldef.Source) string {
        var sb strings.Builder
        sb.WriteString("[#" + itoa(s.Rank) + "] " + s.Title + "\n")
        sb.WriteString("URL: " + s.URL + "\n")
        if s.Snippet != "" {
                sb.WriteString("Snippet: " + s.Snippet + "\n")
        }
        if s.FetchError != "" {
                sb.WriteString("Fetch error: " + s.FetchError + "\n")
        } else if s.Content != "" {
                sb.WriteString("Page:\n" + s.Content)
                if !strings.HasSuffix(s.Content, "\n") {
                        sb.WriteString("\n")
                }
        }
        return sb.String()
}

// summarizeSearch builds the one-line Summary field for the tool row.
// Mirrors the existing fetch Summary format ("5 results · wikipedia")
// so the TUI transcript row stays consistent.
func summarizeSearch(engine string, count int) string {
        if count == 0 {
                return "0 results · " + engine
        }
        return itoa(count) + " results · " + engine
}

// formatFaviconURL builds the DuckDuckGo favicon URL for a host.
// Centralized here so all engines use the same template — when
// Frankfurter returns "100 USD = 92 EUR" the source pill still shows
// the Frankfurter favicon, sourced consistently with every other
// engine.
func formatFaviconURL(host string) string {
        if host == "" {
                return ""
        }
        return "https://icons.duckduckgo.com/ip3/" + host + ".ico"
}

// itoa is a tiny strconv.Itoa alias to avoid pulling strconv into this
// file's import block — every other helper here uses strings + fmt only.
// (We could just use strconv.Itoa, but the indirection keeps the
// import surface lean.)
func itoa(n int) string {
        if n == 0 {
                return "0"
        }
        var buf [20]byte
        i := len(buf)
        negative := n < 0
        if negative {
                n = -n
        }
        for n > 0 {
                i--
                buf[i] = byte('0' + n%10)
                n /= 10
        }
        if negative {
                i--
                buf[i] = '-'
        }
        return string(buf[i:])
}

// _ keeps time in scope for the SearchEngine method set's documentation
// example timestamps; the package uses time elsewhere but search.go
// itself doesn't reference time directly after the refactor.
var _ = time.Now
