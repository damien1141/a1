package components

import (
	"strings"

	"github.com/pulseaiclub/xui"
)

// BylineSeparator joins inline metadata the way a publishing byline does:
// one dim " · " between items, nothing before the first or after the last.
//
// Claude Code's equivalent is a React component that filters null children and
// only renders the separator between real ones. This is the string form of the
// same idea, for the call sites that are building a []string anyway.
const BylineSeparator = " · "

// Byline joins non-empty items with BylineSeparator and returns plain text.
//
// Empty items are dropped silently, which is the whole point: a caller
// assembles a candidate list (tool name, target, duration, counts) and gets
// whichever ones turned out to exist, with no dangling separator and no "a · · b".
func Byline(items ...string) string {
	kept := items[:0:0]
	for _, it := range items {
		if t := strings.TrimSpace(it); t != "" {
			kept = append(kept, t)
		}
	}
	return strings.Join(kept, BylineSeparator)
}

// BylineItem is one entry in a styled byline.
type BylineItem struct {
	Text  string
	Style xui.Style
}

// BylineSpans joins non-empty items into spans, with the separator between
// them — each item keeps its own style and only the separator takes sep, so a
// colored glyph never paints its own dot.
func BylineSpans(items []BylineItem, sep xui.Style) []Span {
	var out []Span
	for _, it := range items {
		t := strings.TrimSpace(it.Text)
		if t == "" {
			continue
		}
		if len(out) > 0 {
			out = append(out, Span{Text: BylineSeparator, Style: sep})
		}
		out = append(out, Span{Text: t, Style: it.Style})
	}
	return out
}
