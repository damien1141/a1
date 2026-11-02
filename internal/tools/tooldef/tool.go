package tooldef

import (
	"context"
	"encoding/json"

	"github.com/damien1141/a1/internal/llm"
)

// Result is what a tool returns to the model / UI.
type Result struct {
	// Content is sent back to the model as the tool message body.
	Content string
	// Detail identifies the call for the whole run — the url, path, or query.
	// Set it from DetailFromArgs when it comes from arguments; a tool must not
	// overwrite it at completion, because the transcript row needs one stable
	// identity from start to finish.
	Detail string
	// Summary is the one-line outcome, shown beside the identity once the call
	// resolves ("200 OK · 42.1 KB · 1.2s"). Empty hides it. This is where a
	// completion stat belongs; Detail stays the identity.
	Summary string
	// Output is the display body for the TUI (may equal Content).
	Output string
	// Expanded asks the TUI to start the tool row open.
	Expanded bool
	// Sources carries structured source-card data for tools that produce
	// ranked references (search-mode fetch, future search tools). The TUI
	// ignores this field; the web frontend renders it as small pills
	// (favicon + host) below the answer bubble. Empty for non-search
	// tool calls — the existing rendering path is unchanged.
	Sources []Source
	// Images attaches base64-encoded images to the tool result for
	// vision-capable models (screenshots, rendered previews). The
	// executor forwards them on the tool message when the active model
	// opts into images (ModelConfig.ImageEnabled); blind models keep the
	// text Content, which must stand alone. Mirrors llm.Message.Images,
	// which is role-agnostic for exactly this ride-along. The TUI ignores
	// this field.
	Images []llm.Image
	// Screenshot carries what the evidence ledger needs to grade a
	// screenshot:<name> check: the caller-supplied name ("" when the
	// capture is unnamed) and the on-disk PNG the diff baseline compares
	// against. Nil for non-screenshot results.
	Screenshot *ScreenshotFact
	// Handoff reports a state only a human can resolve — a bot challenge,
	// a login wall, a payment step. The executor routes it through the
	// permission ask flow: the same gate that asks before a risky bash
	// command asks before a human takes the wheel. Nil for tools without
	// handoff semantics.
	Handoff *Handoff
}

// ScreenshotFact is the evidence-facing half of a screenshot result: the
// model-facing half rides Images. Name is the check-id suffix the caller
// chose ("screenshot:login" ← name "login"); Path is the captured PNG.
type ScreenshotFact struct {
	Name string
	Path string
}

// Handoff asks the user to take over a step the agent cannot finish alone.
// Reason is the short human-readable why; URL is where the human is needed
// ("" when unknown).
type Handoff struct {
	Reason string
	URL    string
}

// Source is one ranked reference produced by a search-style tool call.
// Populated by the search-mode fetch tool: each entry corresponds to one
// DuckDuckGo result, optionally enriched with the fetched page's content
// when auto-fetch was enabled.
//
// Fields are kept flat (not nested) so the JSON shape stays simple for
// the web frontend to consume directly:
//
//	{
//	  "rank": 1,
//	  "url": "https://example.com/page",
//	  "title": "Page Title",
//	  "host": "example.com",
//	  "favicon_url": "https://icons.duckduckgo.com/ip3/example.com.ico",
//	  "snippet": "Short excerpt from the search result listing...",
//	  "content": "First ~4000 chars of the fetched page body (when fetch_pages=true)",
//	  "fetch_error": "" // populated only when the per-page fetch failed
//	}
type Source struct {
	// Rank is the 1-indexed position in the search results. Used by the
	// UI as a stable key for click handling and (future) inline citation
	// linking. Always > 0.
	Rank int `json:"rank"`
	// URL is the canonical, redirect-unwrapped target URL.
	URL string `json:"url"`
	// Title is the search-result listing title (typically the page's <title>
	// or og:title, as DDG chose to surface it). Truncated to ~120 chars by
	// the parser; the UI further truncates to whatever fits the pill.
	Title string `json:"title"`
	// Host is the URL's hostname (no scheme, no path, no port). Stripped
	// of any leading "www." so the pill reads "example.com" not
	// "www.example.com". Computed by the parser, not the UI.
	Host string `json:"host"`
	// FaviconURL is the icons.duckduckgo.com/ip3/<host>.ico endpoint
	// for this source's host. Always populated when Host is set.
	FaviconURL string `json:"favicon_url"`
	// Snippet is DDG's short result-listing excerpt (typically 1-2
	// sentences, ~200 chars). Useful for the LLM to see context for
	// each ranked result without reading the full page body.
	Snippet string `json:"snippet"`
	// Content is the fetched page body (HTML→text, truncated to
	// max_chars_per_page) when fetch_pages=true. Empty when
	// fetch_pages=false or when the per-page fetch failed (see
	// FetchError).
	Content string `json:"content,omitempty"`
	// FetchError is populated when fetch_pages=true and the per-page
	// fetch for this source failed (network error, non-2xx, timeout).
	// Empty on success. When set, Content is empty and the LLM should
	// treat this source as "URL known, content unavailable".
	FetchError string `json:"fetch_error,omitempty"`
}

// Handler runs a tool given raw JSON arguments.
type Handler func(ctx context.Context, input json.RawMessage) (Result, error)

// Tool is a schema + implementation pair.
type Tool struct {
	Definition llm.ToolDefinition
	Run        Handler
	// DetailFromArgs extracts a one-line detail for the UI before execution.
	DetailFromArgs func(input json.RawMessage) string
}

// Definitions extracts LLM schemas from tools.
func Definitions(tools []Tool) []llm.ToolDefinition {
	out := make([]llm.ToolDefinition, len(tools))
	for i, t := range tools {
		out[i] = t.Definition
	}
	return out
}

// Registry maps tool name → Tool.
type Registry map[string]Tool

// NewRegistry indexes tools by name.
func NewRegistry(tools []Tool) Registry {
	m := make(Registry, len(tools))
	for _, t := range tools {
		m[t.Definition.Name] = t
	}
	return m
}
