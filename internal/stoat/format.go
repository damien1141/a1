package stoat

import (
	"strings"
	"unicode/utf8"
)

// maxMessageChars bounds one stoat message. The platform's
// message_length limit is 2000, and the backend counts CHARACTERS
// (Rust chars — Unicode scalar values), not bytes and not graphemes:
// a 2000-rune CJK answer is one legal message, while a byte-budget
// splitter would carve it into three. Cuts therefore happen on rune
// boundaries by construction, and the count is runes.
const maxMessageChars = 2000

// chunkMessage splits a reply into sendable chunks. Line boundaries are
// preferred (a chunk keeps its terminating newline so plain
// concatenation is an exact round trip); a line longer than the limit is
// cut exactly at the rune budget.
func chunkMessage(s string) []string {
	if utf8.RuneCountInString(s) <= maxMessageChars {
		return []string{s}
	}
	var chunks []string
	for utf8.RuneCountInString(s) > maxMessageChars {
		// The byte index where rune #maxMessageChars+1 starts: by
		// construction a legal cut point.
		limit := byteIndexOfRune(s, maxMessageChars)
		cut := limit
		if i := strings.LastIndexByte(s[:limit], '\n'); i >= 0 {
			// Include the newline in the chunk: concat(chunks) == s.
			cut = i + 1
		}
		chunks = append(chunks, s[:cut])
		s = s[cut:]
	}
	if len(s) > 0 {
		chunks = append(chunks, s)
	}
	return chunks
}

// byteIndexOfRune returns the byte offset of the n-th rune's start in s
// (len(s) when s is shorter than n runes). Iterating `for i := range s`
// walks rune-start byte indexes — the standard library's own idiom.
func byteIndexOfRune(s string, n int) int {
	for i := range s {
		if n == 0 {
			return i
		}
		n--
	}
	return len(s)
}
