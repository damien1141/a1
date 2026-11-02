package fetchtool

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- Matches predicates -------------------------------------------------

func TestMDNMatches(t *testing.T) {
	e := newMDNEngine()
	cases := []struct {
		query string
		want  bool
	}{
		{"how does css flexbox work", true},
		{"css grid subgrid support", true},
		{"dom event listener basics", true},
		{"svg viewBox attribute", true},
		{"mdn array map", true},
		{"http status 418 meaning", true},
		{"javascript function decorator", false}, // language query → SE
		{"python function decorator", false},
		{"how to handle error in go", false},
		{"best pizza in new york", false},
		{"idempotence", false}, // concept word, no web-platform term
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			assert.Equal(t, tc.want, e.Matches(tc.query))
		})
	}
}

func TestHackerNewsMatches(t *testing.T) {
	e := newHackerNewsEngine()
	cases := []struct {
		query string
		want  bool
	}{
		{"hacker news thread on rust rewrites", true},
		{"what do people think of bun", true},
		{"anyone tried zed editor", true},
		{"opinions on tailwind", true},
		{"discussion of python packaging", true},
		{"how to handle error in go", false},
		{"best pizza in new york", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			assert.Equal(t, tc.want, e.Matches(tc.query))
		})
	}
}

func TestNpmMatches(t *testing.T) {
	e := newNpmEngine()
	cases := []struct {
		query string
		want  bool
	}{
		{"npm package for date parsing", true},
		{"is there a javascript library for charts", true},
		{"typescript module for validation", true},
		{"yarn add lodash", true},
		{"bun add zod", true},
		{"is there a go library for csv parsing", false}, // other ecosystem → github
		{"find a package for redis in python", false},
		{"how to handle error in go", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			assert.Equal(t, tc.want, e.Matches(tc.query))
		})
	}
}

func TestCratesMatches(t *testing.T) {
	e := newCratesEngine()
	cases := []struct {
		query string
		want  bool
	}{
		{"cargo add serde", true},
		{"rust crate for json parsing", true},
		{"crates.io search", true},
		{"best rust package for cli", true},
		{"is there a go library for csv parsing", false},
		{"how to handle error in go", false},
		{"best pizza in new york", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			assert.Equal(t, tc.want, e.Matches(tc.query))
		})
	}
}

func TestRedditMatches(t *testing.T) {
	e := newRedditEngine()
	cases := []struct {
		query string
		want  bool
	}{
		{"reddit thread about arch vs ubuntu", true},
		{"r/golang workflow tips", true},
		{"forums about mechanical keyboards", true},
		{"hacker news thread on rust rewrites", false}, // HN signal, not reddit
		{"what do people think of bun", false},         // generic discussion → HN
		{"best pizza in new york", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			assert.Equal(t, tc.want, e.Matches(tc.query))
		})
	}
}

// --- Payload parsing ----------------------------------------------------

func TestParseMDNResults(t *testing.T) {
	body := []byte(`{"documents":[
		{"mdn_url":"/en-US/docs/Web/CSS/flex","title":"flex","locale":"en-us","summary":"The flex CSS shorthand property sets how a flex item will grow or shrink.","score":67.9},
		{"mdn_url":"/en-US/docs/Learn_web_development/Core/CSS_layout/Flexbox","title":"Flexbox","summary":"Flexbox is a one-dimensional layout method for arranging items.","score":66.9}
	]}`)
	results, sources, err := parseMDNResults(body)
	assert.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Len(t, sources, 2)
	assert.Equal(t, "https://developer.mozilla.org/en-US/docs/Web/CSS/flex", results[0].url)
	assert.Equal(t, "flex", results[0].title)
	assert.Equal(t, 1, results[0].rank)
	assert.Equal(t, 2, results[1].rank)
	assert.Equal(t, "developer.mozilla.org", sources[0].Host)
	assert.NotEmpty(t, sources[0].FaviconURL)
	assert.Contains(t, sources[0].Snippet, "flex item")
}

func TestParseHackerNewsResults(t *testing.T) {
	body := []byte(`{"hits":[
		{"objectID":"46124267","title":"Anthropic acquires Bun","url":"https://bun.com/blog/bun-joins-anthropic","author":"ryanvogel","points":342,"num_comments":218},
		{"objectID":"3928","title":"Ask HN: best terminal setup?","author":"alice","points":100,"num_comments":400,"story_text":"I have been using tmux for years but wonder what else is out there."}
	]}`)
	results, sources, err := parseHackerNewsResults(body)
	assert.NoError(t, err)
	assert.Len(t, results, 2)
	// Link story: the article URL.
	assert.Equal(t, "https://bun.com/blog/bun-joins-anthropic", results[0].url)
	assert.Equal(t, "bun.com", results[0].host)
	// Ask HN: no article URL → the HN item itself.
	assert.Equal(t, "https://news.ycombinator.com/item?id=3928", results[1].url)
	assert.Equal(t, "news.ycombinator.com", results[1].host)
	assert.Contains(t, sources[1].Snippet, "▲ 100")
	assert.Contains(t, sources[1].Snippet, "tmux")
}

func TestParseNpmResults(t *testing.T) {
	body := []byte(`{"objects":[
		{"package":{"name":"csv","version":"6.6.4","description":"A mature CSV toolset","links":{"npm":"https://www.npmjs.com/package/csv","homepage":"https://csv.js.org"}},"downloads":{"monthly":8997833},"searchScore":2125.98},
		{"package":{"name":"csv-parse","version":"5.6.0","description":"CSV parser implementing the Node.js stream Transform API","links":{"homepage":"https://csv.js.org"}},"downloads":{"monthly":12000000}}
	]}`)
	results, sources, err := parseNpmResults(body)
	assert.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, "csv", results[0].title)
	assert.Equal(t, "https://www.npmjs.com/package/csv", results[0].url)
	assert.Contains(t, sources[0].Snippet, "v6.6.4")
	assert.Contains(t, sources[0].Snippet, "9.0m")
	// Missing links.npm → synthesized package URL.
	assert.Equal(t, "https://www.npmjs.com/package/csv-parse", results[1].url)
	assert.Equal(t, "npmjs.com", sources[1].Host)
}

func TestParseCratesResults(t *testing.T) {
	body := []byte(`{"crates":[
		{"name":"serde","description":"A generic serialization/deserialization framework","max_version":"1.0.229","downloads":1627893651,"documentation":"https://docs.rs/serde","repository":"https://github.com/serde-rs/serde"},
		{"name":"serde_json","description":"A JSON serialization file format","max_version":"1.0.140","downloads":900000000}
	]}`)
	results, sources, err := parseCratesResults(body)
	assert.NoError(t, err)
	assert.Len(t, results, 2)
	// Documentation link preferred: docs.rs is where an agent wants to land.
	assert.Equal(t, "https://docs.rs/serde", results[0].url)
	assert.Equal(t, "docs.rs", results[0].host)
	assert.Contains(t, sources[0].Snippet, "v1.0.229")
	assert.Contains(t, sources[0].Snippet, "1.6b")
	// No documentation link → crates.io fallback.
	assert.Equal(t, "https://crates.io/crates/serde_json", results[1].url)
}

func TestParseRedditResults(t *testing.T) {
	body := []byte(`{"data":{"children":[
		{"data":{"title":"Bun vs Node in 2026","permalink":"/r/node/comments/1abc/bun_vs_node/","subreddit":"node","score":156,"num_comments":89,"selftext":"","author":"dev42"}},
		{"data":{"title":"Removed post","permalink":"/r/programming/comments/1xyz/removed/","subreddit":"programming","score":10,"num_comments":2,"selftext":"[removed]","author":"x"}}
	]}}`)
	results, sources, err := parseRedditResults(body)
	assert.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, "https://www.reddit.com/r/node/comments/1abc/bun_vs_node/", results[0].url)
	assert.Equal(t, "reddit.com", sources[0].Host)
	assert.Contains(t, sources[0].Snippet, "r/node")
	assert.Contains(t, sources[0].Snippet, "▲ 156")
	// A [removed] selftext must not leak into the snippet.
	assert.NotContains(t, sources[1].Snippet, "[removed]")

	// The datacenter block page: HTML, not JSON — must surface as an
	// error so the router falls through.
	_, _, err = parseRedditResults([]byte(`<html><body>blocked</body></html>`))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "blocked or non-JSON")
}

func TestHumanCount(t *testing.T) {
	assert.Equal(t, "999", humanCount(999))
	assert.Equal(t, "12.3k", humanCount(12345))
	assert.Equal(t, "12.3m", humanCount(12345678))
}

// --- Merged web engine --------------------------------------------------

// mergeStub is a deterministic SearchEngine for merge tests.
type mergeStub struct {
	name    string
	results []searchResult
	err     error
	sawOpts []searchOptions
}

func (s *mergeStub) Name() string        { return s.name }
func (s *mergeStub) Matches(string) bool { return true }
func (s *mergeStub) Search(_ context.Context, query string, opts searchOptions) (*SearchResponse, error) {
	s.sawOpts = append(s.sawOpts, opts)
	if s.err != nil {
		return nil, s.err
	}
	resp := &SearchResponse{Engine: s.name, Query: query}
	for _, r := range s.results {
		resp.Results = append(resp.Results, r)
		resp.Sources = append(resp.Sources, sourceFromResult(r, r.rank))
	}
	return resp, nil
}

func urlsOf(resp *SearchResponse) []string {
	out := make([]string, len(resp.Results))
	for i, r := range resp.Results {
		out[i] = r.url
	}
	return out
}

func TestMergedEngineInterleave(t *testing.T) {
	primary := &mergeStub{name: "p", results: []searchResult{
		{rank: 1, url: "https://p1"}, {rank: 2, url: "https://p2"},
		{rank: 3, url: "https://p3"}, {rank: 4, url: "https://p4"},
		{rank: 5, url: "https://p5"},
	}}
	hn := &mergeStub{name: "h", results: []searchResult{
		{rank: 1, url: "https://h1"}, {rank: 2, url: "https://h2"}, {rank: 3, url: "https://h3"},
	}}
	rd := &mergeStub{name: "r", results: []searchResult{
		{rank: 1, url: "https://r1"}, {rank: 2, url: "https://r2"},
	}}
	m := &mergedEngine{primary: primary, others: []SearchEngine{hn, rd}}

	resp, err := m.Search(context.Background(), "q", searchOptions{maxResults: 5})
	assert.NoError(t, err)
	// Weighted 2:1:1 interleave: p1 p2 h1 r1 | p3 (cap hits at 5).
	assert.Equal(t, []string{"https://p1", "https://p2", "https://h1", "https://r1", "https://p3"}, urlsOf(resp))
	// Ranks renumbered 1..N in merged order.
	for i, r := range resp.Results {
		assert.Equal(t, i+1, r.rank)
	}
	for i, s := range resp.Sources {
		assert.Equal(t, i+1, s.Rank)
	}
	// Engine label lists the contributors, primary first.
	assert.Equal(t, "p+h+r", resp.Engine)
}

func TestMergedEngineDedupesURLs(t *testing.T) {
	primary := &mergeStub{name: "p", results: []searchResult{
		{rank: 1, url: "https://same"}, {rank: 2, url: "https://p2"},
	}}
	other := &mergeStub{name: "h", results: []searchResult{
		{rank: 1, url: "https://same"}, {rank: 2, url: "https://h2"},
	}}
	m := &mergedEngine{primary: primary, others: []SearchEngine{other}}
	resp, err := m.Search(context.Background(), "q", searchOptions{maxResults: 5})
	assert.NoError(t, err)
	assert.Equal(t, []string{"https://same", "https://p2", "https://h2"}, urlsOf(resp))
}

func TestMergedEnginePrimaryErrorPropagates(t *testing.T) {
	primary := &mergeStub{name: "p", err: errors.New("captcha wall")}
	other := &mergeStub{name: "h", results: []searchResult{{rank: 1, url: "https://h1"}}}
	m := &mergedEngine{primary: primary, others: []SearchEngine{other}}
	_, err := m.Search(context.Background(), "q", searchOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "captcha wall")
}

func TestMergedEngineSecondaryErrorTolerated(t *testing.T) {
	primary := &mergeStub{name: "p", results: []searchResult{{rank: 1, url: "https://p1"}}}
	blocked := &mergeStub{name: "r", err: errors.New("reddit: HTTP 403")}
	m := &mergedEngine{primary: primary, others: []SearchEngine{blocked}}
	resp, err := m.Search(context.Background(), "q", searchOptions{})
	assert.NoError(t, err)
	assert.Equal(t, []string{"https://p1"}, urlsOf(resp))
	// Only the engine that actually contributed appears in the label.
	assert.Equal(t, "p", resp.Engine)
}

func TestMergedEngineAllEmpty(t *testing.T) {
	m := newMergedWebEngine()
	// newMergedWebEngine's children hit the network; for this test build
	// one from empty stubs instead.
	empty := &mergedEngine{
		primary: &mergeStub{name: "p"},
		others:  []SearchEngine{&mergeStub{name: "h"}},
	}
	resp, err := empty.Search(context.Background(), "q", searchOptions{})
	assert.NoError(t, err)
	assert.Empty(t, resp.Results)
	// Keep the constructed-by-default engine referenced so the unused
	// variable stays meaningful: its name is the router-visible identity.
	assert.Equal(t, "web", m.Name())
}

func TestMergedEngineDisablesChildFetchPages(t *testing.T) {
	primary := &mergeStub{name: "p", results: []searchResult{{rank: 1, url: "https://p1"}}}
	other := &mergeStub{name: "h", results: []searchResult{{rank: 1, url: "https://h1"}}}
	m := &mergedEngine{primary: primary, others: []SearchEngine{other}}
	// fetchPages=true would make the merged engine fetch page bodies for
	// the survivors; children must never see it (pages are fetched once,
	// after the merge picks survivors).
	_, err := m.Search(context.Background(), "q", searchOptions{fetchPages: true})
	assert.NoError(t, err)
	for _, opts := range primary.sawOpts {
		assert.False(t, opts.fetchPages)
	}
	for _, opts := range other.sawOpts {
		assert.False(t, opts.fetchPages)
	}
}

func TestMergedEngineFallsBackToSourceFromResult(t *testing.T) {
	// A child that returns Results but no Sources: the merge builds the
	// source cards itself via sourceFromResult.
	primary := &mergeStub{name: "p", results: []searchResult{{rank: 1, url: "https://p1", title: "t", snippet: "s"}}}
	bare := &bareResultsEngine{name: "b", results: []searchResult{{rank: 1, url: "https://b1", title: "bt", snippet: "bs"}}}
	m := &mergedEngine{primary: primary, others: []SearchEngine{bare}}
	resp, err := m.Search(context.Background(), "q", searchOptions{maxResults: 4})
	assert.NoError(t, err)
	assert.Len(t, resp.Sources, 2)
	var bSource bool
	for _, s := range resp.Sources {
		if s.URL == "https://b1" {
			bSource = true
			assert.Equal(t, "bt", s.Title)
			assert.NotEmpty(t, s.Host)
		}
	}
	assert.True(t, bSource, "bare engine's result must gain a synthesized source card")
}

// bareResultsEngine returns Results without Sources (like a minimal engine).
type bareResultsEngine struct {
	name    string
	results []searchResult
}

func (b *bareResultsEngine) Name() string        { return b.name }
func (b *bareResultsEngine) Matches(string) bool { return true }
func (b *bareResultsEngine) Search(_ context.Context, query string, _ searchOptions) (*SearchResponse, error) {
	return &SearchResponse{Engine: b.name, Query: query, Results: b.results}, nil
}
