package chat

import "strings"

// ActiveQuestion reports whether the cursor sits in a leading `?` help token.
// Same rules as ActiveSlash: first token only, ends at whitespace.
func ActiveQuestion(value string, cursor int) (query string, start, end int, ok bool) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(value) {
		cursor = len(value)
	}
	if !strings.HasPrefix(value, "?") {
		return "", 0, 0, false
	}
	if cursor < 1 {
		return "", 0, 0, false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if cursor > i {
				return "", 0, 0, false
			}
			break
		}
	}
	return value[1:cursor], 0, cursor, true
}
