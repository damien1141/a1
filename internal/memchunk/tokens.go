package memchunk

import (
	"strings"
	"unicode"
)

// stopwords are dropped before overlap scoring. They carry no topical signal,
// so counting them would make every pair look similar.
var stopwords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "but": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
	"to": true, "of": true, "in": true, "on": true, "at": true, "for": true,
	"with": true, "by": true, "from": true, "as": true, "that": true, "this": true,
	"it": true, "its": true, "i": true, "you": true, "we": true, "they": true,
	"he": true, "she": true, "my": true, "your": true, "our": true, "their": true,
	"do": true, "does": true, "did": true, "have": true, "has": true, "had": true,
	"not": true, "no": true, "so": true, "if": true, "then": true, "than": true,
	"there": true, "here": true, "what": true, "which": true, "who": true,
}

// tokenSet lowercases and splits text into content words, dropping stopwords and
// punctuation. Deliberately crude: this ranks candidates for a model to judge, so
// a missed synonym costs one question slot, not a wrong answer.
func tokenSet(s string) map[string]bool {
	out := make(map[string]bool, 16)
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(f) < 3 || stopwords[f] {
			continue
		}
		out[f] = true
	}
	return out
}

// overlap counts shared tokens between two sets.
func overlap(a, b map[string]bool) int {
	if len(a) > len(b) {
		a, b = b, a
	}
	n := 0
	for t := range a {
		if b[t] {
			n++
		}
	}
	return n
}
