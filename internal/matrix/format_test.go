package matrix

import (
        "strings"
        "testing"
        "unicode/utf8"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// The format helpers' differential trio: the fallback stripper against
// hand-computed golden cases (what Element actually puts on the wire), the
// chunker as a byte-exact round trip, and the HTML renderer's escape
// behavior.

func TestStripReplyFallback_Differential(t *testing.T) {
        // Golden fixtures hand-derived from the spec's reply fallback format:
        // quoted lines each "> "-prefixed, then exactly one blank line, then the
        // reply. The oracle is the format itself, not the implementation.
        cases := []struct {
                name    string
                in      string
                isReply bool
                want    string
        }{
                {
                        name:    "single-line quote",
                        in:      "> <@alice:hs> original text\n\nthe reply",
                        isReply: true,
                        want:    "the reply",
                },
                {
                        name:    "multi-line quote",
                        in:      "> <@alice:hs> line one\n> line two continues\n\nthe reply",
                        isReply: true,
                        want:    "the reply",
                },
                {
                        name:    "quote with no reply body",
                        in:      "> <@alice:hs> original text\n",
                        isReply: true,
                        want:    "",
                },
                {
                        name:    "non-reply keeps its blockquote",
                        in:      "> not a matrix reply, just quoting code",
                        isReply: false,
                        want:    "> not a matrix reply, just quoting code",
                },
                {
                        name:    "reply keeps its own leading spaces",
                        in:      "> <@alice:hs> q\n\n  indented answer",
                        isReply: true,
                        want:    "  indented answer",
                },
                {
                        name:    "quote marker without the spec's space is not a fallback line",
                        in:      ">no space\n\nreply",
                        isReply: true,
                        want:    ">no space\n\nreply",
                },
                {
                        name:    "only the first blank separator is consumed",
                        in:      "> <@alice:hs> q\n\n\nleading blank in the reply itself",
                        isReply: true,
                        want:    "\nleading blank in the reply itself",
                },
        }
        for _, tc := range cases {
                t.Run(tc.name, func(t *testing.T) {
                        assert.Equal(t, tc.want, stripReplyFallback(tc.in, tc.isReply))
                })
        }
}

func TestChunkMessageRoundTrip_Differential(t *testing.T) {
        // The property the bridge depends on: concatenating the chunks is
        // byte-exact the original, and no chunk exceeds the wire limit. The
        // corpus stresses every cut mode: newline cuts, hard ascii cuts, rune
        // boundaries in CJK (3 bytes/rune) and emoji (4 bytes/rune).
        corpus := []string{
                "",
                "short",
                strings.Repeat("a", maxChunkBytes),   // exactly the limit
                strings.Repeat("a", maxChunkBytes+1), // one byte over
                strings.Repeat("line\n", 2000),       // newline-rich
                strings.Repeat("ab\n", 3000) + "tail",
                strings.Repeat("你好世界", 1300), // 6500 bytes, no newlines
                strings.Repeat("🚀", 1300),    // 5200 bytes, 4-byte runes
                strings.Repeat("x", maxChunkBytes*3),
        }
        for i, s := range corpus {
                chunks := chunkMessage(s)
                require.NotEmpty(t, chunks, "corpus %d: even an empty string yields one chunk", i)
                for j, c := range chunks {
                        assert.LessOrEqual(t, len(c), maxChunkBytes, "corpus %d chunk %d over the wire limit", i, j)
                        assert.True(t, utf8.ValidString(c), "corpus %d chunk %d split a rune", i, j)
                }
                assert.Equal(t, s, strings.Join(chunks, ""), "corpus %d: round trip", i)
        }
}

func TestChunkMessagePrefersLineBoundaries(t *testing.T) {
        s := strings.Repeat("abcdefghij\n", 400) // 4400 bytes, newline every 11th
        chunks := chunkMessage(s)
        require.Len(t, chunks, 2)
        // The first chunk cut at a newline (11 divides the 3800-byte window
        // unevenly, so the cut walks back to the last newline), keeping it.
        assert.True(t, strings.HasSuffix(chunks[0], "\n"), "the newline rides the chunk")
        assert.Equal(t, s, strings.Join(chunks, ""))
}

func TestRenderHTMLDifferential(t *testing.T) {
        // The renderer mirrors the GUI's markdown pipeline (GFM in, HTML out)
        // with one hard guarantee the room depends on: raw HTML in model output
        // is escaped, never passed through.
        assert.Equal(t, "", renderHTML("   "), "blank input renders nothing")

        html := renderHTML("**bold** and `code`")
        assert.Contains(t, html, "<strong>bold</strong>")
        assert.Contains(t, html, "<code>code</code>")

        html = renderHTML("| a | b |\n|---|---|\n| 1 | 2 |")
        assert.Contains(t, html, "<table>", "GFM tables ride the formatted body")

        html = renderHTML("```go\nfmt.Println(1)\n```")
        assert.Contains(t, html, "<pre><code", "fenced code blocks survive")

        html = renderHTML("<script>alert(1)</script>")
        // goldmark's safe mode drops raw HTML entirely, leaving a comment —
        // model output can never inject markup into the room.
        assert.NotContains(t, html, "<script>", "raw HTML never passes through")
        assert.Contains(t, html, "<!-- raw HTML omitted -->")
}
