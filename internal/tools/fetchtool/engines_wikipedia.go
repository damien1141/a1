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

// wikipediaEngine searches the free Wikipedia API
// (https://en.wikipedia.org/w/api.php). No API key, no captcha, instant
// JSON response. Best for "what is X", "who is Y", "history of Z"
// queries — exactly the queries a coding agent asks when it needs to
// understand a concept it doesn't already know.
//
// The Matches() predicate looks for typical factual-query prefixes
// ("what is", "who is", "history of") plus concept-y single-word
// queries that are unlikely to be code identifiers. The router puts
// Wikipedia late in the priority order so the tighter engines
// (Stack Exchange, GitHub) get first dibs on programming queries.
type wikipediaEngine struct{}

func newWikipediaEngine() *wikipediaEngine { return &wikipediaEngine{} }

func (wikipediaEngine) Name() string { return "wikipedia" }

// factualQueryRe matches queries that ask for a definition or
// explanation. The patterns are deliberately narrow — Wikipedia should
// fire when the user is asking "what is X concept" rather than "how do
// I do X in code".
var factualQueryRe = regexp.MustCompile(`(?i)^\s*(what is|what's|who is|who was|who are|history of|definition of|define|explain|tell me about|background on)\b`)

// conceptualNounRe matches SINGLE-WORD concept names (no spaces) that
// look like concepts (CamelCase or all-lowercase, no digits, dots,
// slashes, or other code-indicator characters). Catches "monad",
// "polymorphism", "idempotence", "asynchronous" — words that are
// obviously concepts, not code. Multi-word phrases are NOT matched
// because "best pizza in new york" shouldn't route to Wikipedia.
var conceptualNounRe = regexp.MustCompile(`^[A-Za-z][A-Za-z\-]{2,40}$`)

func (e *wikipediaEngine) Matches(query string) bool {
        // Guard: if the query has programming signals (language names,
        // error terms, file extensions), defer to Stack Exchange — it
        // produces better results for coding queries. This prevents
        // "how to handle error in go" from matching as a concept name.
        if hasProgrammingSignals(query) {
                return false
        }
        // Strong signal: explicit factual-query prefix.
        if factualQueryRe.MatchString(query) {
                return true
        }
        // Weaker signal: a single short word that looks like a concept
        // (no digits, no dots, no slashes, no code-y chars). This catches
        // "polymorphism" or "monad" without requiring the user to write
        // "what is polymorphism".
        trimmed := strings.TrimSpace(query)
        if conceptualNounRe.MatchString(trimmed) && !containsCodeSignals(trimmed) {
                // Reject programming-keyword hits — "react" alone is more
                // likely a framework query than a Wikipedia concept.
                lower := strings.ToLower(trimmed)
                if !isProgrammingKeyword(lower) {
                        return true
                }
        }
        return false
}

// hasProgrammingSignals reports whether the query contains programming
// signals (language names, error terms, file extensions) that would
// make it a better fit for Stack Exchange than Wikipedia.
func hasProgrammingSignals(query string) bool {
        return programmingSignalRe.MatchString(query) || fileExtensionRe.MatchString(query)
}

// containsCodeSignals reports whether s contains characters that
// strongly suggest it's a code query rather than a concept name.
func containsCodeSignals(s string) bool {
        return strings.ContainsAny(s, "0123456789.()[]{}<>/\\=+*")
}

// isProgrammingKeyword reports whether s is a well-known programming
// language or framework name that should route to Stack Exchange /
// GitHub, not Wikipedia.
func isProgrammingKeyword(s string) bool {
        switch s {
        case "go", "golang", "python", "rust", "javascript", "typescript",
                "java", "kotlin", "swift", "ruby", "php", "perl", "lua",
                "react", "vue", "angular", "svelte", "ember", "backbone",
                "node", "deno", "bun", "next", "nuxt", "astro",
                "docker", "kubernetes", "k8s", "containerd",
                "django", "flask", "rails", "express", "gin", "echo",
                "sql", "mysql", "postgres", "postgresql", "sqlite", "redis",
                "linux", "windows", "macos", "bsd",
                "html", "css", "scss", "sass",
                "json", "yaml", "toml", "xml",
                "git", "svn", "mercurial":
                return true
        }
        return false
}

// wikipediaSearchResponse is the JSON shape of
// https://en.wikipedia.org/w/api.php?action=query&list=search&srsearch=...
type wikipediaSearchResponse struct {
        Query struct {
                Search []struct {
                        Title   string `json:"title"`
                        PageID  int    `json:"pageid"`
                        Snippet string `json:"snippet"` // contains HTML <span class="searchmatch"> highlights
                } `json:"search"`
        } `json:"query"`
}

func (e *wikipediaEngine) Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error) {
        // Strip the factual-query prefix if present — Wikipedia's search
        // does better with just the noun phrase.
        q := factualQueryRe.ReplaceAllString(strings.TrimSpace(query), "")
        q = strings.TrimSpace(q)
        if q == "" {
                q = strings.TrimSpace(query)
        }

        maxResults := opts.maxResults
        if maxResults <= 0 {
                maxResults = searchDefaultMaxResults
        }

        // Hit the Wikipedia search API.
        endpoint := fmt.Sprintf("https://en.wikipedia.org/w/api.php?action=query&list=search&srsearch=%s&format=json&srlimit=%d",
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
                return nil, fmt.Errorf("wikipedia: %w", err)
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
                return nil, fmt.Errorf("wikipedia: HTTP %d", resp.StatusCode)
        }
        body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
        if err != nil {
                return nil, err
        }
        var sr wikipediaSearchResponse
        if err := json.Unmarshal(body, &sr); err != nil {
                return nil, fmt.Errorf("wikipedia: parse: %w", err)
        }

        results := make([]searchResult, 0, len(sr.Query.Search))
        sources := make([]tooldef.Source, 0, len(sr.Query.Search))
        for i, entry := range sr.Query.Search {
                // Snippet contains <span class="searchmatch"> highlights — strip them.
                snippet := stripTags(entry.Snippet)
                snippet = decodeEntities(snippet)
                snippet = collapseWhitespace(snippet)
                snippet = truncate(snippet, 400)
                pageURL := fmt.Sprintf("https://en.wikipedia.org/?curid=%d", entry.PageID)
                host := "en.wikipedia.org"
                rank := i + 1
                results = append(results, searchResult{
                        rank:    rank,
                        url:     pageURL,
                        title:   entry.Title,
                        snippet: snippet,
                })
                sources = append(sources, tooldef.Source{
                        Rank:       rank,
                        URL:        pageURL,
                        Title:      entry.Title,
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
