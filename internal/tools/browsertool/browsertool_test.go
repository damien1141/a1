package browsertool

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserTool_Definition(t *testing.T) {
	tool := BrowserTool()
	assert.Equal(t, "browser", tool.Definition.Name)
	// Not Readable: a batch of browser calls must run sequentially against the
	// one shared page. Readable=true made the executor run them concurrently.
	assert.False(t, tool.Definition.Readable)
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
	case <-time.After(30 * time.Second):
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
	case <-time.After(30 * time.Second):
		t.Fatal("runBrowser(profile list) deadlocked: re-entrant browserMu.Lock()")
	}
}

// TestNewScreenshotPath_PerCallUniqueness is the regression test for #16: the
// old fixed path /tmp/browser-screenshot.png meant concurrent sessions and
// repeated screenshots in one session clobbered each other. The new
// per-call temp file must produce a unique path each invocation.
func TestNewScreenshotPath_PerCallUniqueness(t *testing.T) {
	p1, cleanup1, err := newScreenshotPath()
	require.NoError(t, err)
	defer cleanup1(true) // remove after test
	p2, cleanup2, err := newScreenshotPath()
	require.NoError(t, err)
	defer cleanup2(true)

	assert.NotEqual(t, p1, p2, "two screenshot calls must produce different paths")
	assert.True(t, strings.HasPrefix(p1, "/tmp/a1-screenshot-"),
		"path should use the a1-screenshot- prefix, got %s", p1)
	assert.True(t, strings.HasSuffix(p1, ".png"),
		"path should end with .png, got %s", p1)
	// File must exist on disk — os.CreateTemp creates an empty file at
	// the returned path; the cleanup closure only closes the handle, it
	// doesn't remove unless asked.
	_, err = os.Stat(p1)
	require.NoError(t, err, "temp file must exist after newScreenshotPath")
}

// TestNewScreenshotPath_CleanupRemovesFile verifies the cleanup closure's
// remove=true path deletes the file. The caller passes remove=true on write
// failure to avoid leaving empty/corrupt files behind.
func TestNewScreenshotPath_CleanupRemovesFile(t *testing.T) {
	p, cleanup, err := newScreenshotPath()
	require.NoError(t, err)
	_, err = os.Stat(p)
	require.NoError(t, err, "file must exist before cleanup")

	cleanup(true) // remove=true

	_, err = os.Stat(p)
	assert.True(t, os.IsNotExist(err), "file must not exist after cleanup(true)")
}

// TestNewScreenshotPath_CleanupKeepsFile verifies the cleanup closure's
// remove=false path keeps the file on disk (just closes the handle).
func TestNewScreenshotPath_CleanupKeepsFile(t *testing.T) {
	p, cleanup, err := newScreenshotPath()
	require.NoError(t, err)
	defer os.Remove(p) // belt-and-suspenders

	cleanup(false) // remove=false

	_, err = os.Stat(p)
	assert.NoError(t, err, "file must still exist after cleanup(false)")
}
