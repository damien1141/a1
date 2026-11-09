package fetchtool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestClassifyQuery verifies the smart router picks the right engine
// for each query type. This is the core test of the routing layer —
// if a query routes to the wrong engine, the user gets worse results.
func TestClassifyQuery(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string // expected engine name
	}{
		// Frankfurter — tightest predicate, highest priority.
		{"currency conversion", "100 usd to eur", "frankfurter"},
		{"currency convert keyword", "convert 50 GBP to JPY", "frankfurter"},
		{"currency arrow", "100 USD → EUR", "frankfurter"},
		{"currency in", "100 USD in EUR", "frankfurter"},

		// Stack Exchange — programming signals.
		{"go error", "how to handle error in go", "stackexchange"},
		{"python function", "python function decorator", "stackexchange"},
		{"rust async", "rust async await example", "stackexchange"},
		{"docker compose", "how to use docker compose", "stackexchange"},
		{"file extension", "main.go compile error", "stackexchange"},
		{"sql query", "how to write a recursive sql query", "stackexchange"},
		{"regex", "regex for email validation", "stackexchange"},

		// GitHub — library/package discovery.
		{"library search", "is there a go library for csv parsing", "github"},
		{"package find", "find a package for redis in python", "github"},
		{"best framework", "best framework for rest api in go", "github"},
		{"github url", "github.com/user/repo", "github"},

		// arxiv — academic signals.
		{"research paper", "research paper on transformers", "arxiv"},
		{"arxiv reference", "arxiv 2401.12345", "arxiv"},
		{"literature review", "literature review on attention mechanisms", "arxiv"},

		// Wikipedia — factual/conceptual.
		{"what is", "what is a monad", "wikipedia"},
		{"who is", "who is Alan Turing", "wikipedia"},
		{"history of", "history of unix", "wikipedia"},
		{"define", "define polymorphism", "wikipedia"},
		{"concept noun", "idempotence", "wikipedia"},

		// MDN — web-platform reference.
		{"css property", "how does css flexbox work", "mdn"},
		{"dom api", "dom event listener basics", "mdn"},
		{"mdn explicit", "mdn array map", "mdn"},

		// npm — JavaScript/TypeScript package discovery.
		{"npm package", "npm package for date parsing", "npm"},
		{"js library", "is there a javascript library for charts", "npm"},

		// crates — Rust package discovery.
		{"cargo add", "cargo add serde", "crates"},
		{"rust crate", "rust crate for json parsing", "crates"},

		// Hacker News — discussion-seeking.
		{"hacker news explicit", "hacker news thread on rust rewrites", "hackernews"},
		{"opinion query", "what do people think of bun", "hackernews"},

		// Reddit — explicit reddit/forum intent.
		{"reddit explicit", "reddit thread about arch vs ubuntu", "reddit"},
		{"subreddit ref", "r/golang workflow tips", "reddit"},

		// Time / weather — the clock + forecast subtools. They outrank
		// everything: zero context cost until asked, direct answers.
		{"weather query", "weather in san francisco", "weather"},
		{"weather bare", "what's the weather", "weather"},
		{"time now", "what time is it", "time"},
		{"date today", "today's date", "time"},

		// Merged web fallback — catch-all for general queries
		// (DuckDuckGo Lite + Hacker News + Reddit interleaved).
		{"general query", "best pizza in new york", "web"},
		{"news query", "latest news about tesla", "web"},
		{"weather dev query", "open-meteo api docs", "stackexchange"}, // dev-signal veto: not the forecast engine
		{"random query", "how to tie a tie", "web"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyQuery(tc.query)
			assert.Equal(t, tc.want, got,
				"query %q should route to %s, got %s", tc.query, tc.want, got)
		})
	}
}

// TestFrankfurterMatches verifies the currency query predicate.
func TestFrankfurterMatches(t *testing.T) {
	e := newFrankfurterEngine()
	cases := []struct {
		query string
		want  bool
	}{
		{"100 usd to eur", true},
		{"Convert 50 GBP to JPY", true},
		{"100 USD → EUR", true},
		{"100 USD in EUR", true},
		{"how much is 100 usd in eur", true},
		{"what is the euro", false},
		{"usd currency", false},
		{"100 dollars to euros", false}, // uses names, not ISO codes
		{"python function", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			got := e.Matches(tc.query)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestStackExchangeMatches verifies the programming signal predicate.
func TestStackExchangeMatches(t *testing.T) {
	e := newStackExchangeEngine()
	cases := []struct {
		query string
		want  bool
	}{
		{"how to handle error in go", true},
		{"python function decorator", true},
		{"rust async await example", true},
		{"docker compose", true},
		{"main.go compile error", true},
		{"sql query optimization", true},
		{"regex for email", true},
		{"kubernetes deployment yaml", true},
		{"what is the weather", false},
		{"best pizza in new york", false},
		{"100 usd to eur", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			got := e.Matches(tc.query)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestGitHubMatches verifies the library discovery predicate.
func TestGitHubMatches(t *testing.T) {
	e := newGitHubEngine()
	cases := []struct {
		query string
		want  bool
	}{
		{"is there a go library for csv parsing", true},
		{"find a package for redis in python", true},
		{"best framework for rest api in go", true},
		{"github.com/user/repo", true},
		{"recommend a react state management library", true},
		{"how to handle error in go", false}, // programming signal but not library discovery
		{"what is a monad", false},
		{"100 usd to eur", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			got := e.Matches(tc.query)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestArxivMatches verifies the academic signal predicate.
func TestArxivMatches(t *testing.T) {
	e := newArxivEngine()
	cases := []struct {
		query string
		want  bool
	}{
		{"research paper on transformers", true},
		{"arxiv 2401.12345", true},
		{"literature review on attention mechanisms", true},
		{"state of the art in nlp", true},
		{"preprint on protein folding", true},
		{"how to handle error in go", false},
		{"what is a monad", false},
		{"100 usd to eur", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			got := e.Matches(tc.query)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestWikipediaMatches verifies the factual/conceptual predicate.
func TestWikipediaMatches(t *testing.T) {
	e := newWikipediaEngine()
	cases := []struct {
		query string
		want  bool
	}{
		{"what is a monad", true},
		{"who is Alan Turing", true},
		{"history of unix", true},
		{"define polymorphism", true},
		{"explain idempotence", true},
		{"idempotence", true},                // single concept word
		{"polymorphism", true},               // single concept word
		{"react", false},                     // programming keyword → SE, not Wikipedia
		{"how to handle error in go", false}, // programming → SE
		{"100 usd to eur", false},            // currency → Frankfurter
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			got := e.Matches(tc.query)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestDDGLiteMatches verifies the catch-all predicate (always false).
func TestDDGLiteMatches(t *testing.T) {
	e := newDDGLiteEngine()
	// DDG Lite's Matches() returns false for every query — it's the
	// explicit catch-all, not a domain-specific match. The router
	// tries it after all domain engines have been checked.
	assert.False(t, e.Matches("anything at all"))
	assert.False(t, e.Matches(""))
	assert.False(t, e.Matches("how to code in go"))
}
