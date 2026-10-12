package browsertool_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/damien1141/a1/internal/tools/browsertool"
)

func TestSummary(t *testing.T) {
	assert.Equal(t, "Read · example.com · 7 results", browsertool.Summary("Read", "https://example.com/x", 7, 0))
	assert.Equal(t, "Clicked", browsertool.Summary("Clicked", "", 0, 0))
	assert.Equal(t, "Searched · duckduckgo.com", browsertool.Summary("Searched", "https://duckduckgo.com/?q=x", 0, 0))
}

func TestSummaryPluralizes(t *testing.T) {
	assert.Equal(t, "Read · a.test · 1 result", browsertool.Summary("Read", "a.test", 1, 0))
	assert.Equal(t, "Read · a.test · 2 results", browsertool.Summary("Read", "a.test", 2, 0))
}

func TestScreenshotSummaryNamesFileNotPath(t *testing.T) {
	assert.Equal(t, "saved browser-screenshot.png",
		browsertool.ScreenshotSummary("/tmp/browser-screenshot.png"),
		"the row shows the filename; the full path stays in the expanded body")
	assert.Equal(t, "screenshot saved", browsertool.ScreenshotSummary(""))
}

func TestProfilesSummary(t *testing.T) {
	assert.Equal(t, "0 profiles", browsertool.ProfilesSummary(0))
	assert.Equal(t, "1 profile", browsertool.ProfilesSummary(1))
	assert.Equal(t, "4 profiles", browsertool.ProfilesSummary(4))
}

func TestRunning(t *testing.T) {
	assert.Equal(t, "navigate…", browsertool.Running("navigate"))
	assert.Equal(t, "Driving the browser…", browsertool.Running(""))
}

func TestHostOf(t *testing.T) {
	assert.Equal(t, "example.com", browsertool.HostOf("https://example.com/a"))
	assert.Equal(t, "example.com", browsertool.HostOf("example.com:8080/x"))
}
