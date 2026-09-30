package browsertool

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBrowserTool_Definition(t *testing.T) {
	tool := BrowserTool()
	assert.Equal(t, "browser", tool.Definition.Name)
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

// TestBrowserTool_NoDeadlockOnClose guards against a re-entrant lock bug:
// runBrowser held browserMu for the whole action and then dispatched to
// browserClose / browserProfileList / browserProfileSwitch / browserProfileDelete
// / browserProfileCreate, each of which called browserMu.Lock() again. Go's
// sync.Mutex is not re-entrant, so every /browser close and /browser profile
// call deadlocked in place and froze the agent loop.
//
// We can't spin up Chromium here, but we can verify that dispatches which
// reach the shared-state paths return promptly instead of hanging.
func TestBrowserTool_NoDeadlockOnClose(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer func() { done <- struct{}{} }()
		_, _ = runBrowser(t.Context(), json.RawMessage(`{"action":"close"}`))
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runBrowser(close) deadlocked: re-entrant browserMu.Lock()")
	}
}

func TestBrowserTool_NoDeadlockOnProfileList(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer func() { done <- struct{}{} }()
		// profile list reads the shared maps; with no browser up this errors
		// out at ensureBrowser, but it must NOT hang.
		_, _ = runBrowser(t.Context(), json.RawMessage(`{"action":"profile","profile":"list"}`))
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runBrowser(profile list) deadlocked: re-entrant browserMu.Lock()")
	}
}