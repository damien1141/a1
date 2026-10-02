package browsertool

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"google.com", "https://google.com"},
		{"  www.example.com  ", "https://www.example.com"},
		{"https://example.com", "https://example.com"},
		{"http://example.com", "http://example.com"},
		{"ftp://example.com", "ftp://example.com"},
		{"about:blank", "about:blank"},
		{"javascript:void(0)", "javascript:void(0)"},
		{"", ""},
		{"  ", ""},
		{"/relative/path", "/relative/path"},
		{"//example.com", "//example.com"},
		{"subdomain.example.co.uk", "https://subdomain.example.co.uk"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, normalizeURL(tt.in), "input %q", tt.in)
	}
}

// TestCollapseWhitespace verifies the text extractor used by browser content
// turns a raw DOM dump into readable prose. (browserContent trims leading
// and trailing whitespace before calling this, so the test input has none.)
func TestCollapseWhitespace(t *testing.T) {
	in := "hello\n\n   world\t\tfoo  \nbar"
	want := "hello world foo bar"
	assert.Equal(t, want, collapseWhitespace(in))
}

// TestContentMaxChars is a compile-time guard that browser content caps its
// output so a single page cannot blow past the context window (a Google
// results page is several MB of raw HTML).
func TestContentMaxChars(t *testing.T) {
	assert.Less(t, contentMaxChars, 20_000, "content cap must stay well under a context window")
}

// TestBrowserOpenBareHostname opens a bare hostname to confirm the scheme is
// auto-prepended. Requires A1_BROWSER_E2E=1 and a working display.
func TestBrowserOpenBareHostname(t *testing.T) {
	if os.Getenv("A1_BROWSER_E2E") != "1" {
		t.Skip("set A1_BROWSER_E2E=1 to run real browser launch")
	}
	ctx := context.Background()
	res, err := runBrowser(ctx, json.RawMessage(`{"action":"open","url":"example.com"}`))
	if err != nil {
		t.Fatalf("open example.com failed: %v\ncontent=%s", err, res.Content)
	}
	assert.Contains(t, res.Content, "https://example.com")
	_, _ = runBrowser(ctx, json.RawMessage(`{"action":"close"}`))
}
