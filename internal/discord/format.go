package discord

import (
	"strings"
)

// maxMessageUnits bounds one Discord message. The cap is 2,000, and the
// unit is the one every Discord client counts — UTF-16 CODE UNITS
// (content.length in the JavaScript every official client and library
// runs): an astral emoji is TWO units, a CJK character is one. A
// rune-counting splitter would happily emit 2,000 astral emoji as "2,000
// characters" and watch Discord reject it as 4,000.
const maxMessageUnits = 2000

// chunkMessage splits a reply into sendable chunks. Line boundaries are
// preferred (a chunk keeps its terminating newline so plain
// concatenation is an exact round trip); a line longer than the limit is
// cut exactly at the unit budget, which by construction never splits a
// rune — the walk stops at a rune boundary because it counts whole
// runes, even when a rune is worth two units.
func chunkMessage(s string) []string {
	if utf16Len(s) <= maxMessageUnits {
		return []string{s}
	}
	var chunks []string
	for utf16Len(s) > maxMessageUnits {
		// The byte index where the unit budget is exhausted: a legal cut
		// point, because the walk only stops between runes.
		limit := byteIndexOfUTF16(s, maxMessageUnits)
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

// utf16Len counts UTF-16 code units: one per BMP rune, two per astral
// rune — JavaScript's string length, which is what Discord enforces.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// byteIndexOfUTF16 returns the byte offset where the first `budget`
// UTF-16 units end (len(s) when the string is shorter). The walk only
// stops at rune boundaries — an astral rune that would cross the budget
// ends the walk BEFORE itself, so the returned index never splits one.
func byteIndexOfUTF16(s string, budget int) int {
	used := 0
	for i, r := range s {
		w := 1
		if r > 0xFFFF {
			w = 2
		}
		if used+w > budget {
			return i
		}
		used += w
	}
	return len(s)
}
