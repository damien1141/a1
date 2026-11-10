package matrix

import (
        "bytes"
        "strings"
        "unicode/utf8"

        "github.com/yuin/goldmark"
        "github.com/yuin/goldmark/extension"
)

// mdToHTML is the reply renderer. GFM tables and strikethrough are on;
// goldmark's default HTML renderer escapes raw HTML, so model output cannot
// inject markup into a room.
var mdToHTML = goldmark.New(goldmark.WithExtensions(extension.GFM))

// renderHTML converts one assistant reply from markdown to Matrix HTML.
// A render failure (or empty output) falls back to "" — the client then
// sends plain text only, never a broken bubble.
func renderHTML(src string) string {
        if strings.TrimSpace(src) == "" {
                return ""
        }
        var buf bytes.Buffer
        if err := mdToHTML.Convert([]byte(src), &buf); err != nil {
                return ""
        }
        return buf.String()
}

// maxChunkBytes bounds one Matrix message. The spec has no hard limit and
// synapse accepts 64 KiB, but a full agent answer (code, diffs, tables) as
// one message is unreadable on phones — a screenful per message keeps the
// room scrollable. Measured in BYTES (the wire limit) with cuts placed on
// rune boundaries (a split rune is an invalid message).
const maxChunkBytes = 3800

// chunkMessage splits a reply into sendable chunks. Line boundaries are
// preferred (a chunk keeps its terminating newline so plain concatenation
// is an exact round trip); a line longer than the limit is hard-cut on a
// rune boundary.
func chunkMessage(s string) []string {
        if len(s) <= maxChunkBytes {
                return []string{s}
        }
        var chunks []string
        for len(s) > maxChunkBytes {
                cut := maxChunkBytes
                if i := strings.LastIndexByte(s[:cut], '\n'); i >= 0 {
                        // Include the newline in the chunk: concat(chunks) == s.
                        cut = i + 1
                } else if !utf8.RuneStart(s[cut]) {
                        // Scan back to a rune boundary so no chunk splits a character
                        // (CJK text is 3 bytes a rune; a naive byte cut corrupts it).
                        for cut > 0 && !utf8.RuneStart(s[cut]) {
                                cut--
                        }
                        if cut == 0 {
                                cut = maxChunkBytes // pathological single rune > limit
                        }
                }
                chunks = append(chunks, s[:cut])
                s = s[cut:]
        }
        if len(s) > 0 {
                chunks = append(chunks, s)
        }
        return chunks
}

// stripReplyFallback removes a reply's quoted fallback. Clients composing
// an m.in_reply_to message prefix the body with the quoted lines (each
// "> "-prefixed) and one blank separator line; stripping them is the
// difference between the agent receiving "do it" and receiving a wall of
// its own words back. Non-reply bodies are returned untouched — a message
// that legitimately starts with "> " (quoting code at the agent) keeps it.
func stripReplyFallback(body string, isReply bool) string {
        if !isReply {
                return body
        }
        lines := strings.Split(body, "\n")
        i := 0
        for i < len(lines) && strings.HasPrefix(lines[i], "> ") {
                i++
        }
        // Exactly one blank line separates quote from reply in the spec's
        // fallback format; strip it, but never eat the reply's own leading
        // blank lines beyond that one.
        if i < len(lines) && strings.TrimSpace(lines[i]) == "" {
                i++
        }
        return strings.Join(lines[i:], "\n")
}
