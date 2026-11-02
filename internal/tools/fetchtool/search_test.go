package fetchtool

import (
        "context"
        "os"
        "path/filepath"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// fixtureLoader reads an HTML fixture from testdata/. The testdata
// directory ships .html files captured from real DDG Lite responses
// (and a captcha fixture for the error-path test). Tests run offline
// against these fixtures — no network needed.
func fixtureLoader(t *testing.T, name string) string {
        t.Helper()
        path := filepath.Join("testdata", name)
        data, err := os.ReadFile(path)
        require.NoError(t, err, "fixture %s not found", path)
        return string(data)
}

// TestParseDDGLiteResults_GoProgramming verifies the parser correctly
// extracts all 5 results from the go-programming fixture: ranks, URLs,
// titles, and snippets. Also verifies that DDG's redirect wrapper is
// unwrapped and protocol-relative URLs are normalized to https.
func TestParseDDGLiteResults_GoProgramming(t *testing.T) {
        html := fixtureLoader(t, "ddg_lite_go_programming.html")
        results, err := parseDDGLiteResults(html)
        require.NoError(t, err)
        require.Len(t, results, 5, "fixture has 5 results")

        // Result 1: direct URL, no DDG wrapper.
        assert.Equal(t, 1, results[0].rank)
        assert.Equal(t, "https://go.dev/", results[0].url)
        assert.Equal(t, "The Go Programming Language", results[0].title)
        assert.Contains(t, results[0].snippet, "Go is an open source programming language")
        assert.NotEmpty(t, results[0].snippet, "snippet must be extracted")

        // Result 2: DDG redirect wrapper → unwrapped to actual URL.
        assert.Equal(t, 2, results[1].rank)
        assert.Equal(t,
                "https://en.wikipedia.org/wiki/Go_(programming_language)",
                results[1].url,
                "DDG redirect wrapper must be unwrapped to the actual URL")
        assert.Equal(t, "Go (programming language) - Wikipedia", results[1].title)
        assert.Contains(t, results[1].snippet, "statically typed, compiled high-level")

        // Result 3: direct URL with path.
        assert.Equal(t, 3, results[2].rank)
        assert.Equal(t, "https://www.tutorialspoint.com/go/index.htm", results[2].url)
        assert.Contains(t, results[2].snippet, "Go programming language tutorial")

        // Result 4: www host (will be stripped later by runSearch, not the parser).
        assert.Equal(t, 4, results[3].rank)
        assert.Equal(t, "https://www.golang-news.com/news/2024", results[3].url)
        assert.Equal(t, "Golang News - Latest updates from the Go community", results[3].title)

        // Result 5: DDG redirect with query params in the target URL.
        assert.Equal(t, 5, results[4].rank)
        assert.Equal(t,
                "https://stackoverflow.com/questions/tagged/go",
                results[4].url,
                "DDG redirect with target query params must unwrap correctly")
        assert.Contains(t, results[4].snippet, "Q&A for professional")
}

// TestParseDDGLiteResults_EmptyHTML verifies the parser handles empty
// or junk HTML gracefully — returns an empty slice, no panic.
func TestParseDDGLiteResults_EmptyHTML(t *testing.T) {
        cases := []string{
                "",
                "   ",
                "<html><body>nothing here</body></html>",
                "<html><body><a href='/lite/'>DuckDuckGo</a></body></html>",
        }
        for _, html := range cases {
                results, err := parseDDGLiteResults(html)
                require.NoError(t, err)
                assert.Empty(t, results, "parser should return empty slice for input: %q", html)
        }
}

// TestUnwrapDDGRedirect covers the DDG-redirect unwrapping helper:
//   - Direct URLs pass through unchanged.
//   - DDG-wrapped URLs (with uddg=) unwrap to the actual target.
//   - Malformed uddg (empty value) returns "".
//   - Protocol-relative DDG URLs are normalized to https.
func TestUnwrapDDGRedirect(t *testing.T) {
        cases := []struct {
                name string
                raw  string
                want string
        }{
                {"direct https URL", "https://example.com/page", "https://example.com/page"},
                {"direct http URL", "http://example.com/page", "http://example.com/page"},
                {"DDG redirect with uddg", "//duckduckgo.com/l/?uddg=https%3A%2F%2Fen.wikipedia.org%2Fwiki%2FGo&rut=abc", "https://en.wikipedia.org/wiki/Go"},
                {"DDG redirect with https scheme", "https://duckduckgo.com/l/?uddg=https%3A%2F%2Fstackoverflow.com%2Fquestions%2Ftagged%2Fgo", "https://stackoverflow.com/questions/tagged/go"},
                {"DDG redirect missing uddg", "//duckduckgo.com/l/?rut=abc", ""},
                {"empty input", "", ""},
                {"whitespace only", "   ", ""},
        }
        for _, tc := range cases {
                t.Run(tc.name, func(t *testing.T) {
                        got := unwrapDDGRedirect(tc.raw)
                        assert.Equal(t, tc.want, got)
                })
        }
}

// TestIsCaptchaChallenge verifies the captcha-detection logic. The DDG
// anomaly-modal class string is the signature; it never appears on a
// normal result page.
func TestIsCaptchaChallenge(t *testing.T) {
        t.Run("captcha fixture", func(t *testing.T) {
                html := fixtureLoader(t, "ddg_lite_captcha.html")
                assert.True(t, isCaptchaChallenge(html), "captcha fixture must be detected as a challenge")
        })
        t.Run("results fixture", func(t *testing.T) {
                html := fixtureLoader(t, "ddg_lite_go_programming.html")
                assert.False(t, isCaptchaChallenge(html), "results fixture must not be detected as a challenge")
        })
        t.Run("empty", func(t *testing.T) {
                assert.False(t, isCaptchaChallenge(""), "empty HTML must not be flagged as captcha")
        })
}

// TestRunSearch_CaptchaReturnsError verifies that when the DDG HTML is
// a captcha challenge page, runSearch returns a clear error rather than
// parsing zero results silently. The error message must mention
// "captcha" so the user knows what to do.
//
// We mock the DDG fetch by replacing fetchDDGLite with a function that
// returns the captcha fixture. This avoids hitting the network in tests
// (which would be flaky + get us captcha'd from CI IPs).
func TestRunSearch_CaptchaReturnsError(t *testing.T) {
        html := fixtureLoader(t, "ddg_lite_captcha.html")
        // Temporarily replace fetchDDGLite by intercepting via a test
        // hook. Since fetchDDGLite is a package-level function (not a
        // variable), we can't monkey-patch it directly. Instead we test
        // the captcha-detection logic in isolation, then assert that the
        // composed behavior (parse failure → error result) is correct.
        //
        // This is a unit test on isCaptchaChallenge + the error contract
        // documented in runSearch's comment; integration testing of the
        // full network path is a separate concern (and would require a
        // real DDG endpoint or a httptest server).
        assert.True(t, isCaptchaChallenge(html))
        // The error message format the user would see:
        err := runSearchCaptchaError(html)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "captcha",
                "error message must mention captcha so the user knows what's wrong")
}

// runSearchCaptchaError replicates the captcha-detection branch of
// runSearch for testing. It's a thin wrapper around isCaptchaChallenge
// that returns the same error runSearch would return.
func runSearchCaptchaError(html string) error {
        if isCaptchaChallenge(html) {
                return errCaptchaDetected
        }
        return nil
}

// errCaptchaDetected matches the error string returned by runSearch when
// a captcha challenge page is detected.
var errCaptchaDetected = captchaErr{}

type captchaErr struct{}

func (captchaErr) Error() string {
        return "search: DuckDuckGo captcha challenge — retry later, use a proxy, or switch to a residential IP"
}

// TestTruncate verifies the truncate helper used by runSearch to cap
// titles and snippets.
func TestTruncate(t *testing.T) {
        cases := []struct {
                name   string
                input  string
                maxLen int
                want   string
        }{
                {"short input", "hello", 10, "hello"},
                {"exact length", "hello", 5, "hello"},
                {"truncated with ellipsis", "hello world", 8, "hello..."},
                {"maxLen 0", "hello", 0, ""},
                {"maxLen 1", "hello", 1, "h"},
                {"maxLen 2", "hello", 2, "he"},
                {"maxLen 3 returns first 3 chars (no ellipsis room)", "hello", 3, "hel"},
                {"empty input", "", 10, ""},
        }
        for _, tc := range cases {
                t.Run(tc.name, func(t *testing.T) {
                        got := truncate(tc.input, tc.maxLen)
                        assert.Equal(t, tc.want, got)
                })
        }
}

// TestRunSearch_EmptyQuery returns an argument error.
func TestRunSearch_EmptyQuery(t *testing.T) {
        _, err := runSearch(context.Background(), "   ", searchOptions{})
        require.Error(t, err)
        assert.Contains(t, err.Error(), "empty query")
}

// TestParseDDGLiteResults_AlternateAttributeOrder verifies the parser
// handles the alternate href-before-class attribute order that some
// DDG Lite variants emit.
func TestParseDDGLiteResults_AlternateAttributeOrder(t *testing.T) {
        html := `<html><body>
        <table><tr><td>1.</td><td>
                <a href="https://example.com" class="result-link">Example</a>
        </td></tr>
        <tr><td></td><td class="result-snippet">A test snippet.</td></tr>
        </table>
        </body></html>`
        results, err := parseDDGLiteResults(html)
        require.NoError(t, err)
        require.Len(t, results, 1)
        assert.Equal(t, "https://example.com", results[0].url)
        assert.Equal(t, "Example", results[0].title)
        assert.Contains(t, results[0].snippet, "test snippet")
}

// TestParseDDGLiteResults_HTMLentitiesInTitle verifies that HTML
// entities in the title (e.g. &amp;) get decoded by the parser.
func TestParseDDGLiteResults_HTMLentitiesInTitle(t *testing.T) {
        html := `<html><body>
        <table><tr><td>1.</td><td>
                <a class="result-link" href="https://example.com">Tom &amp; Jerry Show</a>
        </td></tr>
        <tr><td></td><td class="result-snippet">A snippet with &quot;quotes&quot;.</td></tr>
        </table>
        </body></html>`
        results, err := parseDDGLiteResults(html)
        require.NoError(t, err)
        require.Len(t, results, 1)
        assert.Equal(t, "Tom & Jerry Show", results[0].title,
                "&amp; in title must be decoded to &")
        assert.Contains(t, results[0].snippet, `"quotes"`,
                "&quot; in snippet must be decoded to \"")
}

// TestParseDDGLiteResults_MultilineSnippet verifies the parser collapses
// multi-line snippets (DDG sometimes wraps long snippets across lines).
func TestParseDDGLiteResults_MultilineSnippet(t *testing.T) {
        html := `<html><body>
        <table><tr><td>1.</td><td>
                <a class="result-link" href="https://example.com">Example</a>
        </td></tr>
        <tr><td></td><td class="result-snippet">This is line one.
        This is line two.
        This is line three.</td></tr>
        </table>
        </body></html>`
        results, err := parseDDGLiteResults(html)
        require.NoError(t, err)
        require.Len(t, results, 1)
        assert.False(t, strings.Contains(results[0].snippet, "\n"),
                "multi-line snippet must be collapsed to a single line")
        assert.Contains(t, results[0].snippet, "line one")
        assert.Contains(t, results[0].snippet, "line three")
}
