package chat

import (
	"unicode"
	"unicode/utf8"
)

// ActiveMention reports whether the cursor sits in an @-mention token.
// start/end are byte offsets into value for the range to replace on accept
// (from '@' through the cursor). query is the text after '@' up to the cursor.
func ActiveMention(value string, cursor int) (query string, start, end int, ok bool) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(value) {
		cursor = len(value)
	}
	// Scan backward from cursor for the '@' that opens this mention.
	i := cursor
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(value[:i])
		if r == '@' {
			at := i - size
			if !mentionBoundaryBefore(value, at) {
				return "", 0, 0, false
			}
			return value[at+size : cursor], at, cursor, true
		}
		if r == '\n' || r == '\r' || unicode.IsSpace(r) {
			return "", 0, 0, false
		}
		i -= size
	}
	return "", 0, 0, false
}

func mentionBoundaryBefore(value string, at int) bool {
	if at == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(value[:at])
	if unicode.IsSpace(r) || r == '\n' || r == '\r' {
		return true
	}
	switch r {
	case '(', '[', '{', '<', '"', '\'', '`', ',', ';', ':', '!', '?', '/', '\\', '|', '=', '+':
		return true
	}
	// Reject mid-token like email local@domain.
	if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == '-' {
		return false
	}
	return true
}
