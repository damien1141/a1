package fetchtool

import (
        "regexp"
        "strings"
)

// regexpCompile wraps regexp.Compile. The indirection exists so search.go
// can keep its import block lean and reuse a single helper for both
// findAllStringSubmatch and findFirstStringSubmatch. A pattern that fails
// to compile returns nil matches from the call sites (no panic).
func regexpCompile(pattern string) (*regexp.Regexp, error) {
        return regexp.Compile(pattern)
}

// htmlCommentRe matches HTML comment blocks: <!-- ... -->. The (?s) flag
// enables dotall mode so . matches newlines (some comments span multiple
// lines, especially in DDG's HTML 4.01 output).
var htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

// stripHTMLComments removes all <!-- ... --> blocks from s. Used by the
// DDG Lite parser before regex extraction — without this, literal
// result-link markup in HTML comments (e.g., our own fixture's
// documentation block) would be matched by the parser as if it were a
// real result, producing phantom entries.
//
// Unclosed comments (<!-- to EOF without -->) are stripped to EOF;
// anything past the comment's open token is removed. This matches what
// a real DOM parser does and prevents a malformed comment from
// swallowing only the comment text but leaving its contents in the
// parseable HTML.
func stripHTMLComments(s string) string {
        stripped := htmlCommentRe.ReplaceAllString(s, "")
        // Handle the unclosed-comment edge case: if there's an unmatched
        // <!-- at any point, drop everything from there to EOF.
        if idx := strings.Index(stripped, "<!--"); idx >= 0 {
                stripped = stripped[:idx]
        }
        return stripped
}

