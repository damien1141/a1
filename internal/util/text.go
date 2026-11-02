package util

import "strings"

// NormalizeLF converts CRLF and lone CR line endings to LF.
func NormalizeLF(text string) string {
	text = ReplaceAll(text, "\r\n", "\n")
	return ReplaceAll(text, "\r", "\n")
}

// ReplaceAll returns a copy of the string s with all
// non-overlapping instances of old replaced by new.
// If old is empty, it matches at the beginning of the string
// and after each UTF-8 sequence, yielding up to k+1 replacements
// for a k-rune string.
func ReplaceAll(s, old, new string) string {
	return strings.ReplaceAll(s, old, new)
}
