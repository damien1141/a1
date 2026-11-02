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

// cratesEngine searches crates.io's public API
// (https://crates.io/api/v1/crates). No API key, but a descriptive
// User-Agent is required by the crates.io API policy — anonymous agents
// get 403s. Best for Rust crate discovery: "cargo add X", "rust crate
// for Y" — the registry's relevance ordering plus download counts are
// exactly what a Rust-curious agent needs.
//
// The Matches() predicate requires rust/cargo context around the word
// "crate": a bare "crate" is too ambiguous (wooden crates, SQL crates) to
// steal a general query, and a false positive here doesn't fall through —
// the engine would return unrelated Rust packages with a non-empty
// response the router accepts.
type cratesEngine struct{}

func newCratesEngine() *cratesEngine { return &cratesEngine{} }

func (cratesEngine) Name() string { return "crates" }

// cratesSignalRe matches queries with Rust-package intent: the registry
// by name, cargo commands, or "rust crate/package/library" phrasing.
var cratesSignalRe = regexp.MustCompile(`(?i)\b(crates\.io|cargo (?:add|install|search)|rust (?:crate|crates|package|library|module)|crate for)\b`)

func (cratesEngine) Matches(query string) bool {
	return cratesSignalRe.MatchString(query)
}

// cratesSearchResponse is the JSON shape of
// https://crates.io/api/v1/crates?q=...&per_page=N
type cratesSearchResponse struct {
	Crates []struct {
		Name          string `json:"name"`
		Description   string `json:"description"`
		MaxVersion    string `json:"max_version"`
		Downloads     int    `json:"downloads"`
		Repository    string `json:"repository"`
		Documentation string `json:"documentation"`
	} `json:"crates"`
}

// parseCratesResults converts the registry payload into ranked results +
// pre-built sources. Split from Search so tests can feed fixtures
// without touching the network.
//
// URL policy: link to the crate's docs.rs page when the API provides
// one — that's where a coding agent actually wants to land — with
// crates.io as the fallback.
func parseCratesResults(body []byte) ([]searchResult, []tooldef.Source, error) {
	var sr cratesSearchResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, nil, fmt.Errorf("crates: parse: %w", err)
	}
	results := make([]searchResult, 0, len(sr.Crates))
	sources := make([]tooldef.Source, 0, len(sr.Crates))
	for i, c := range sr.Crates {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		link := strings.TrimSpace(c.Documentation)
		if link == "" {
			link = "https://crates.io/crates/" + name
		}
		desc := strings.TrimSpace(c.Description)
		if desc == "" {
			desc = "(no description)"
		}
		version := c.MaxVersion
		if version == "" {
			version = "—"
		}
		snippet := fmt.Sprintf("v%s · %s downloads · %s",
			version, humanCount(c.Downloads), truncate(desc, 220))
		host := hostOf(link)
		host = strings.TrimPrefix(host, "www.")
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

func (e *cratesEngine) Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error) {
	maxResults := opts.maxResults
	if maxResults <= 0 {
		maxResults = searchDefaultMaxResults
	}
	// Strip the rust/cargo phrasing so the registry search matches the
	// crate name ("rust crate for json" → "json").
	q := cratesSignalRe.ReplaceAllString(strings.TrimSpace(query), "")
	q = strings.TrimSpace(q)
	if q == "" {
		q = strings.TrimSpace(query)
	}
	endpoint := fmt.Sprintf("https://crates.io/api/v1/crates?q=%s&per_page=%d",
		url.QueryEscape(q), maxResults)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	// crates.io's policy requires a descriptive UA identifying the client.
	req.Header.Set("User-Agent", "a1-agent/1.0 (https://github.com/damien1141/a1)")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crates: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("crates: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	results, sources, err := parseCratesResults(body)
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
