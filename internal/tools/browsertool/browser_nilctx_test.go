package browsertool

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBrowserOpenNilContext mirrors the slash-command path, which passes a
// nil context.Context (TUI Context is not context.Context). ensureBrowser
// must fall back to background rather than panicking on context.WithCancel.
func TestBrowserOpenNilContext(t *testing.T) {
	if os.Getenv("A1_BROWSER_E2E") != "1" {
		t.Skip("set A1_BROWSER_E2E=1 to run real browser launch")
	}
	res, err := RunBrowserCommand(nil, json.RawMessage(`{"action":"open","url":"https://example.com"}`))
	if err != nil {
		t.Fatalf("open with nil ctx failed: %v\ncontent=%s", err, res.Content)
	}
	_, _ = runBrowser(nil, json.RawMessage(`{"action":"close"}`))
}

// The browser tool drives one shared page, so a batch must run sequentially.
// Readable=true let the executor run two actions concurrently against the same
// page — safe under the mutex, but wrong: a click could land before its navigate.
func TestBrowserToolIsNotReadable(t *testing.T) {
	tool := BrowserTool()
	assert.False(t, tool.Definition.Readable,
		"a stateful, sequential tool must not be marked read-only")
}
