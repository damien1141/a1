package browsertool

import (
	"encoding/json"
	"os"
	"testing"
)

// TestBrowserOpenNilContext mirrors the slash-command path, which passes a
// nil context.Context (TUI Context is not context.Context). ensureBrowser
// must fall back to background rather than panicking on context.WithCancel.
func TestBrowserOpenNilContext(t *testing.T) {
	if os.Getenv("A1_BROWSER_E2E") != "1" {
		t.Skip("set A1_BROWSER_E2E=1 to run real browser launch")
	}
	res, err := runBrowser(nil, json.RawMessage(`{"action":"open","url":"https://example.com"}`))
	if err != nil {
		t.Fatalf("open with nil ctx failed: %v\ncontent=%s", err, res.Content)
	}
	_, _ = runBrowser(nil, json.RawMessage(`{"action":"close"}`))
}