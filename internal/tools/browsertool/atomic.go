// Package browsertool atomic.go: typed, single-action browser tools.
//
// The legacy `browser` tool takes a free-string `action` discriminator
// ("open", "navigate", "click", "type", "content", "screenshot", ...).
// LLMs reliably produce the wrong string for that — they send `action: "open"`
// when they mean navigate, or `action: "goto"`, or they fold the URL into
// the action. Each atomic tool here has a fixed schema with the arguments
// it actually needs, so the model only fills in fields, not action names.
//
// Three tools cover the common path:
//
//   - browser_navigate: open or navigate to a URL (replaces open + navigate)
//   - browser_interact: click or type into an element by CSS selector
//   - browser_read:     screenshot or content of the current page
//
// Profiles, proxy, and close stay on the legacy `browser` tool — they're
// rarely called from the agent loop (they're operator actions) and
// splitting them adds tools without saving the model any work.
//
// All three tools share the same shared-state mutex and ensureBrowser
// path as the legacy tool, so they interoperate with /browser.

package browsertool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

// navigateInput is the typed schema for browser_navigate.
type navigateInput struct {
	// URL is required. Scheme auto-prepended so "example.com" works.
	URL string `json:"url"`
	// Profile switches to a named profile before navigating. Empty uses
	// the current profile, same as the legacy open/navigate.
	Profile string `json:"profile,omitempty"`
}

// BrowserNavigateTool returns the typed navigate-or-open tool.
//
// The model sends `{url: "https://..."}` (or `{url: "example.com", profile: "work"}`)
// and gets back the page title. No `action` field, no string discrimination.
//
// Not Readable: navigation mutates the shared page. Concurrent calls would
// race — a later navigate would land before an earlier one's load finished.
// The legacy `browser` tool has the same reasoning.
func BrowserNavigateTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name: "browser_navigate",
			Description: `Navigate the browser to a URL, opening it first if needed — there
is no separate open action. Bare ("example.com") or full URLs work; a
missing scheme is auto-prepended. Returns the page title and current URL.
Follow up with browser_read for page content or browser_interact to
click/type.`,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"url": llm.Object{
						"type":        "string",
						"description": "URL to navigate to. Bare hostnames work.",
					},
					"profile": llm.Object{
						"type":        "string",
						"description": "Optional profile; switches before navigating.",
					},
				},
				Required: []string{"url"},
			},
			// Not Readable: navigation mutates the shared page.
			// Concurrent calls would race (see the doc comment above).
			Readable: false,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in navigateInput
			_ = json.Unmarshal(input, &in)
			u := strings.TrimSpace(in.URL)
			if u == "" {
				return "browser_navigate"
			}
			return fmt.Sprintf("browser_navigate %s", u)
		},
		Run: runBrowserNavigate,
	}
}

func runBrowserNavigate(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in navigateInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse browser_navigate arguments: %w", err)
	}
	// Reuse the legacy dispatch by mapping to a browserInput. The legacy
	// browserOpen already handles profile switching, ensureBrowser, and
	// the lock — we just route through runBrowser (which checks the
	// browser-is-open guard) with action="open".
	legacy, err := json.Marshal(browserInput{
		Action:  "open",
		URL:     in.URL,
		Profile: in.Profile,
	})
	if err != nil {
		return tooldef.Result{}, err
	}
	return runBrowser(ctx, legacy)
}

// BrowserInteractTool returns the typed click-or-type tool.
//
// One tool, two actions: `selector` is always required; `text` selects
// the verb — empty text means click, non-empty means type (fill). The
// model no longer has to remember whether the action is "click" or
// "type"; it just describes what it wants to do.
func BrowserInteractTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name: "browser_interact",
			Description: `Click an element or type text into an input on the current page.
selector is a CSS selector. Omit text to click; provide it to fill the
input with that text. Waits out any navigation the click/type triggers,
so a follow-up browser_read sees the new page. Browser must be open —
call browser_navigate first if needed.`,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"selector": llm.Object{
						"type":        "string",
						"description": "CSS selector for the element to click or fill.",
					},
					"text": llm.Object{
						"type":        "string",
						"description": "Text to type. Omit to click; provide to fill.",
					},
				},
				Required: []string{"selector"},
			},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in interactInput
			_ = json.Unmarshal(input, &in)
			s := strings.TrimSpace(in.Selector)
			if s == "" {
				return "browser_interact"
			}
			if strings.TrimSpace(in.Text) == "" {
				return fmt.Sprintf("browser_interact click %s", s)
			}
			return fmt.Sprintf("browser_interact type %s", s)
		},
		Run: runBrowserInteract,
	}
}

// interactInput is the typed schema for browser_interact.
type interactInput struct {
	// Selector is a CSS selector for the element. Required.
	Selector string `json:"selector"`
	// Text, when non-empty, makes the action a Fill (type). When empty,
	// the action is a Click. This collapses the legacy click/type pair
	// into one tool — the model describes intent, not verb.
	Text string `json:"text,omitempty"`
}

// runBrowserInteract routes to the legacy click or type handler based on
// whether text was provided. Reuses the existing locked-dispatch path so
// the mutex, ensureBrowser, and post-action WaitForLoadState all stay.
func runBrowserInteract(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in interactInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse browser_interact arguments: %w", err)
	}
	action := "click"
	if strings.TrimSpace(in.Text) != "" {
		action = "type"
	}
	legacy, err := json.Marshal(browserInput{
		Action:   action,
		Selector: in.Selector,
		Text:     in.Text,
	})
	if err != nil {
		return tooldef.Result{}, err
	}
	return runBrowser(ctx, legacy)
}

// BrowserReadTool returns the typed read tool.
//
// One tool, two outputs: `mode` selects screenshot (default) or content
// (page text). The model no longer has to remember two action names; it
// picks the output shape it wants.
//
// Not Readable: screenshot/content read the shared page that
// browser_navigate/browser_interact mutate. Concurrent calls would
// race — a screenshot landing mid-navigation. The mutex makes it safe,
// not correct. The legacy `browser` tool has the same reasoning.
func BrowserReadTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name: "browser_read",
			Description: `Read the current page. mode=screenshot (default) saves the viewport
to /tmp/a1-screenshot-*.png and returns the path; mode=content returns
the visible text — boilerplate stripped, link hrefs appended. Browser
must be open — call browser_navigate first if needed.`,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"mode": llm.Object{
						"type":        "string",
						"description": "Output shape: 'screenshot' (default, PNG to /tmp/a1-screenshot-*.png) or 'content' (page text).",
					},
					"name": llm.Object{
						"type":        "string",
						"description": "Optional screenshot name: grades the screenshot:<name> evidence check when declared in a contract.",
					},
				},
			},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in readInput
			_ = json.Unmarshal(input, &in)
			mode := strings.ToLower(strings.TrimSpace(in.Mode))
			if mode == "" {
				mode = "screenshot"
			}
			return fmt.Sprintf("browser_read %s", mode)
		},
		Run: runBrowserRead,
	}
}

// readInput is the typed schema for browser_read.
type readInput struct {
	// Mode selects the output shape: "screenshot" (default) or "content".
	// Empty defaults to screenshot.
	Mode string `json:"mode,omitempty"`
	// Name labels a screenshot for the evidence ledger: declaring
	// screenshot:<name> in a contract and then capturing with the same
	// name grades the capture against its stored baseline.
	Name string `json:"name,omitempty"`
}

// runBrowserRead routes to the legacy screenshot or content handler.
func runBrowserRead(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in readInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse browser_read arguments: %w", err)
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" || mode == "screenshot" {
		legacy, _ := json.Marshal(browserInput{Action: "screenshot", Name: in.Name})
		return runBrowser(ctx, legacy)
	}
	if mode == "content" {
		legacy, _ := json.Marshal(browserInput{Action: "content"})
		return runBrowser(ctx, legacy)
	}
	return tooldef.Result{}, fmt.Errorf("unknown mode %q: use screenshot or content", mode)
}
