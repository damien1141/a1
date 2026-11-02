package browsertool

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBrowserCloseThenReopen verifies that closing the browser and then
// opening a new one works. Before the fix, browserClose cancelled the
// context but never stopped the Playwright node driver, so the next
// playwright.Run() failed with "browser is already in use" and the agent
// could never spawn a second browser.
func TestBrowserCloseThenReopen(t *testing.T) {
	if os.Getenv("A1_BROWSER_E2E") != "1" {
		t.Skip("set A1_BROWSER_E2E=1 to run real browser launch")
	}
	ctx := context.Background()
	res, err := RunBrowserCommand(ctx, json.RawMessage(`{"action":"open","url":"https://example.com"}`))
	if err != nil {
		t.Fatalf("first open failed: %v\ncontent=%s", err, res.Content)
	}
	assert.Contains(t, res.Content, "https://example.com")

	// Close.
	res, err = runBrowser(ctx, json.RawMessage(`{"action":"close"}`))
	if err != nil {
		t.Fatalf("close failed: %v", err)
	}
	assert.Contains(t, res.Content, "Browser closed")

	// Reopen — must not fail with "browser is already in use".
	res, err = RunBrowserCommand(ctx, json.RawMessage(`{"action":"open","url":"https://example.org"}`))
	if err != nil {
		t.Fatalf("reopen after close failed: %v\ncontent=%s", err, res.Content)
	}
	assert.Contains(t, res.Content, "https://example.org")

	// Clean up.
	_, _ = runBrowser(ctx, json.RawMessage(`{"action":"close"}`))
}

// TestBrowserSearch runs a real Google search in one step. Requires
// A1_BROWSER_E2E=1 and a working display.
func TestBrowserSearch(t *testing.T) {
	if os.Getenv("A1_BROWSER_E2E") != "1" {
		t.Skip("set A1_BROWSER_E2E=1 to run real browser launch")
	}
	ctx := context.Background()
	res, err := runBrowser(ctx, json.RawMessage(`{"action":"search","text":"mountain chicken"}`))
	if err != nil {
		t.Fatalf("search failed: %v\ncontent=%s", err, res.Content)
	}
	assert.Contains(t, res.Content, "mountain chicken")
	_, _ = runBrowser(ctx, json.RawMessage(`{"action":"close"}`))
}
