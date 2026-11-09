package browsertool

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// whitespaceRun matches the runs the extractor is documented to collapse:
// spaces, tabs, newlines, and carriage returns.
var whitespaceRun = regexp.MustCompile(`[ \t\n\r]+`)

// oracle: stdlib regexp — a `[ \t\n\r]+` → " " substitution must agree with
// the hand-rolled rune loop on page-text inputs, including unicode text,
// leading/trailing runs, and empty strings.
func TestCollapseWhitespace_Differential(t *testing.T) {
	cases := []string{
		"",
		"no whitespace at all",
		"hello world",
		"hello\n\n   world\t\tfoo  \nbar",
		"   leading run",
		"trailing run   \n\t ",
		"\n\t\r ",
		"Ünïcödé tëxt with wörds",
		"日本語のテキスト　はそのまま残る", // the ideographic space is not in the collapsed set
		"mixed \r\n\t line feeds \r\r\r keep one space",
		strings.Repeat("padded\t", 50),
		"emoji 👍 trailing spaces   ",
	}
	for _, in := range cases {
		assert.Equal(t, whitespaceRun.ReplaceAllString(in, " "), collapseWhitespace(in), "input %q", in)
	}
}

// urlHostname is the oracle: net/url's own parse + Hostname extraction.
func urlHostname(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err, "oracle URL %q must parse", raw)
	require.NotEmpty(t, u.Host, "oracle URL %q must carry a host", raw)
	return u.Hostname()
}

// oracle: stdlib net/url — for scheme-bearing URLs (the display-target
// shapes: https://, http://, protocol-relative //), the host reduction must
// match url.Parse + Hostname on the same input, including userinfo, ports,
// paths, queries, and fragments.
func TestHostOf_Differential(t *testing.T) {
	cases := []string{
		"https://example.com/",
		"https://example.com/a/b/c",
		"http://example.com",
		"http://localhost:3000/dev",
		"https://example.com:8443/x",
		"https://example.com:443/",
		"https://user@example.com/path",
		"https://user:secret@example.com:9999/path?q=1#frag",
		"https://sub.domain.example.co.uk/deep/path?query=1",
		"https://example.com?q=1",
		"https://example.com#section",
		"https://example.com./trailing-dot",
		"//cdn.example.com/lib.js",
		"//example.com",
		"https://EXAMPLE.COM/UPPERCASE-HOST",
		"https://mixed.Case.Example.com/Path",
		"https://example.com:",
		"http://a.b.c.d.e.f.example.net/",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			assert.Equal(t, urlHostname(t, raw), hostOf(raw))
		})
	}
}
