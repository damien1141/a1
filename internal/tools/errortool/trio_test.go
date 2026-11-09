package errortool

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- naive reference implementation (differential oracle) ----------
//
// The oracle re-derives every library pattern with plain byte/string scanning
// — no regexp — mirroring RE2 leftmost-first semantics for exactly the
// constructs the library uses: case-insensitive literals, \s+ runs, [^']*
// quoted captures, line-bounded greedy .*, and two- and three-way
// alternations. Fixture texts never let a matched span cross a newline, so
// entry parsing below stays line-shaped.

// naiveLower lowercases one ASCII byte; multibyte bytes pass through and can
// never equal the oracle's ASCII-only literals.
func naiveLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// naiveHasLit reports whether the lowercase literal lit starts at i.
func naiveHasLit(text string, i int, lit string) bool {
	if i < 0 || i+len(lit) > len(text) {
		return false
	}
	for j := 0; j < len(lit); j++ {
		if naiveLower(text[i+j]) != lit[j] {
			return false
		}
	}
	return true
}

func naiveFindLit(text, lit string, from int) int {
	for i := from; i+len(lit) <= len(text); i++ {
		if naiveHasLit(text, i, lit) {
			return i
		}
	}
	return -1
}

func naiveLastLit(seg, lit string) int {
	last := -1
	for i := 0; i+len(lit) <= len(seg); i++ {
		if naiveHasLit(seg, i, lit) {
			last = i
		}
	}
	return last
}

// \s in Go regexp is exactly [\t\n\f\r ].
func naiveIsSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\f' || b == '\r'
}

func naiveSkipSpace(text string, i int) int {
	for i < len(text) && naiveIsSpace(text[i]) {
		i++
	}
	return i
}

// naiveSkipSpacePlus matches \s+ — at least one whitespace byte at i.
func naiveSkipSpacePlus(text string, i int) (int, bool) {
	if i >= len(text) || !naiveIsSpace(text[i]) {
		return i, false
	}
	return naiveSkipSpace(text, i), true
}

// naiveLineEnd returns the index of the next \n at or after i (or len).
func naiveLineEnd(text string, i int) int {
	for i < len(text) && text[i] != '\n' {
		i++
	}
	return i
}

func naiveIsDigit(b byte) bool { return b >= '0' && b <= '9' }

// `(?i)undefined:\s+(\S+)`
func naiveUndefined(s string) (int, int) {
	const lit = "undefined:"
	for i := naiveFindLit(s, lit, 0); i >= 0; i = naiveFindLit(s, lit, i+1) {
		if k, ok := naiveSkipSpacePlus(s, i+len(lit)); ok && k < len(s) {
			e := k + 1
			for e < len(s) && !naiveIsSpace(s[e]) {
				e++
			}
			return i, e
		}
	}
	return -1, -1
}

// `(?i)cannot use .+ as type` — greedy `.+` lands the match end on the LAST
// " as type" of the line, and needs at least one char before it.
func naiveCannotUse(s string) (int, int) {
	const lit = "cannot use "
	for i := naiveFindLit(s, lit, 0); i >= 0; i = naiveFindLit(s, lit, i+1) {
		segStart := i + len(lit)
		segEnd := naiveLineEnd(s, segStart)
		if j := naiveLastLit(s[segStart:segEnd], " as type"); j >= 1 {
			return i, segStart + j + len(" as type")
		}
	}
	return -1, -1
}

func naiveLiteralMatch(lit string) func(string) (int, int) {
	return func(s string) (int, int) {
		if i := naiveFindLit(s, lit, 0); i >= 0 {
			return i, i + len(lit)
		}
		return -1, -1
	}
}

// `(?i)ModuleNotFoundError:\s+No module named\s+'([^']+)'` (and the KeyError /
// node-require shapes below share the quoted-tail logic).
func naiveModuleNotFound(s string) (int, int) {
	for i := naiveFindLit(s, "modulenotfounderror:", 0); i >= 0; i = naiveFindLit(s, "modulenotfounderror:", i+1) {
		p, ok := naiveSkipSpacePlus(s, i+len("modulenotfounderror:"))
		if !ok || !naiveHasLit(s, p, "no module named") {
			continue
		}
		if e, ok := naiveQuotedTail(s, p+len("no module named")); ok {
			return i, e
		}
	}
	return -1, -1
}

// `(?i)KeyError:\s+'([^']+)'`
func naiveKeyError(s string) (int, int) {
	for i := naiveFindLit(s, "keyerror:", 0); i >= 0; i = naiveFindLit(s, "keyerror:", i+1) {
		if e, ok := naiveQuotedTail(s, i+len("keyerror:")); ok {
			return i, e
		}
	}
	return -1, -1
}

// naiveQuotedTail matches `\s+'([^']+)'` starting at pos and returns the end.
func naiveQuotedTail(s string, pos int) (int, bool) {
	q, ok := naiveSkipSpacePlus(s, pos)
	if !ok || q >= len(s) || s[q] != '\'' {
		return 0, false
	}
	e := q + 1
	for e < len(s) && s[e] != '\'' {
		e++
	}
	if e > q+1 && e < len(s) {
		return e + 1, true
	}
	return 0, false
}

// `(?i)TypeError:\s+(.*)` — greedy to the end of the line.
func naiveTypeError(s string) (int, int) {
	for i := naiveFindLit(s, "typeerror:", 0); i >= 0; i = naiveFindLit(s, "typeerror:", i+1) {
		if q, ok := naiveSkipSpacePlus(s, i+len("typeerror:")); ok {
			return i, naiveLineEnd(s, q)
		}
	}
	return -1, -1
}

// `(?i)borrow of moved value|cannot borrow.*as mutable|borrowed value does
// not live long enough` — leftmost alternative wins; `cannot borrow` ends at
// the last "as mutable" on its line.
func naiveBorrowChecker(s string) (int, int) {
	type span struct{ start, end int }
	var cands []span
	if i := naiveFindLit(s, "borrow of moved value", 0); i >= 0 {
		cands = append(cands, span{i, i + len("borrow of moved value")})
	}
	for i := naiveFindLit(s, "cannot borrow", 0); i >= 0; i = naiveFindLit(s, "cannot borrow", i+1) {
		segStart := i + len("cannot borrow")
		if j := naiveLastLit(s[segStart:naiveLineEnd(s, segStart)], "as mutable"); j >= 0 {
			cands = append(cands, span{i, segStart + j + len("as mutable")})
			break
		}
	}
	if i := naiveFindLit(s, "borrowed value does not live long enough", 0); i >= 0 {
		cands = append(cands, span{i, i + len("borrowed value does not live long enough")})
	}
	if len(cands) == 0 {
		return -1, -1
	}
	best := cands[0]
	for _, c := range cands[1:] {
		if c.start < best.start {
			best = c
		}
	}
	return best.start, best.end
}

// `(?i)index out of bounds:\s+the len is \d+ but the index is`
func naiveRustIndex(s string) (int, int) {
	for i := naiveFindLit(s, "index out of bounds:", 0); i >= 0; i = naiveFindLit(s, "index out of bounds:", i+1) {
		p, ok := naiveSkipSpacePlus(s, i+len("index out of bounds:"))
		if !ok || !naiveHasLit(s, p, "the len is ") {
			continue
		}
		d := p + len("the len is ")
		d0 := d
		for d < len(s) && naiveIsDigit(s[d]) {
			d++
		}
		if d > d0 && naiveHasLit(s, d, " but the index is") {
			return i, d + len(" but the index is")
		}
	}
	return -1, -1
}

// `(?i)called\s+` + "`unwrap`" + `\s+on\s+a\s+` + "`None`" + `\s+value`
func naiveUnwrapNone(s string) (int, int) {
	tails := []string{"`unwrap`", "on", "a", "`none`", "value"}
	for i := naiveFindLit(s, "called", 0); i >= 0; i = naiveFindLit(s, "called", i+1) {
		p := i + len("called")
		ok := true
		for _, lit := range tails {
			q, plus := naiveSkipSpacePlus(s, p)
			if !plus || !naiveHasLit(s, q, lit) {
				ok = false
				break
			}
			p = q + len(lit)
		}
		if ok {
			return i, p
		}
	}
	return -1, -1
}

// `(?i)Error:\s+Cannot find module\s+'([^']+)'`
func naiveNodeRequire(s string) (int, int) {
	for i := naiveFindLit(s, "error:", 0); i >= 0; i = naiveFindLit(s, "error:", i+1) {
		p, ok := naiveSkipSpacePlus(s, i+len("error:"))
		if !ok || !naiveHasLit(s, p, "cannot find module") {
			continue
		}
		if e, ok := naiveQuotedTail(s, p+len("cannot find module")); ok {
			return i, e
		}
	}
	return -1, -1
}

// `(?i)out of memory|fatal error: runtime: cannot allocate memory`
func naiveOutOfMemory(s string) (int, int) {
	if i := naiveFindLit(s, "out of memory", 0); i >= 0 {
		return i, i + len("out of memory")
	}
	if i := naiveFindLit(s, "fatal error: runtime: cannot allocate memory", 0); i >= 0 {
		return i, i + len("fatal error: runtime: cannot allocate memory")
	}
	return -1, -1
}

type naivePattern struct {
	name    string
	matched string
	match   func(string) (int, int)
}

// naivePatterns mirrors the library's order — the tool appends matches in
// library order, so the oracle's hit sequence must line up entry for entry.
var naivePatterns = []naivePattern{
	{name: "Go: undefined variable", match: naiveUndefined},
	{name: "Go: type mismatch", match: naiveCannotUse},
	{name: "Go: missing return", match: naiveLiteralMatch("missing return at end of function")},
	{name: "Go: nil pointer dereference", match: naiveLiteralMatch("runtime error: invalid memory address or nil pointer dereference")},
	{name: "Go: index out of range", match: naiveLiteralMatch("runtime error: index out of range")},
	{name: "Python: module not found", match: naiveModuleNotFound},
	{name: "Python: key error", match: naiveKeyError},
	{name: "Python: type error", match: naiveTypeError},
	{name: "Rust: borrow checker error", match: naiveBorrowChecker},
	{name: "Rust: index out of bounds", match: naiveRustIndex},
	{name: "Rust: unwrap on None", match: naiveUnwrapNone},
	{name: "Node: require not found", match: naiveNodeRequire},
	{name: "Generic: permission denied", match: naiveLiteralMatch("permission denied")},
	{name: "Generic: out of memory", match: naiveOutOfMemory},
	{name: "Generic: connection refused", match: naiveLiteralMatch("connection refused")},
}

func naiveErrorMatches(text string, limit int) []naivePattern {
	text = strings.TrimSpace(text)
	if limit <= 0 {
		limit = errorDefaultLimit
	}
	var hits []naivePattern
	for _, p := range naivePatterns {
		if a, b := p.match(text); a >= 0 {
			hits = append(hits, naivePattern{name: p.name, matched: text[a:b]})
			if len(hits) >= limit {
				break
			}
		}
	}
	return hits
}

type naiveErrorEntry struct {
	name    string
	matched string
}

// parseErrorEntries decodes the tool's "[name]\n  Matched: …" blocks back
// into comparable pairs.
func parseErrorEntries(t *testing.T, content string) []naiveErrorEntry {
	t.Helper()
	var out []naiveErrorEntry
	for _, entry := range strings.Split(content, "\n\n") {
		lines := strings.Split(entry, "\n")
		require.Len(t, lines, 4, "each match must render four lines: %q", entry)
		require.True(t, strings.HasPrefix(lines[0], "["), "entry opens with the bracketed name")
		require.True(t, strings.HasPrefix(lines[1], "  Matched: "), "second line carries the matched span")
		require.True(t, strings.HasPrefix(lines[3], "  Fix: "), "fourth line carries the fix")
		out = append(out, naiveErrorEntry{
			name:    strings.TrimSuffix(strings.TrimPrefix(lines[0], "["), "]"),
			matched: strings.TrimPrefix(lines[1], "  Matched: "),
		})
	}
	return out
}

// oracle: naive reference implementation in this test — every pattern of the
// library re-derived with plain string scanning (no regexp; see the naive*
// helpers above), compared against the tool's rendered matches over
// realistic, boundary, and hostile error texts.
func TestRunError_Differential(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		limit int
	}{
		{"go undefined", "main.go:12:5: undefined: handleClick", 0},
		{"go type mismatch", "cannot use x (variable of type int) as type string in argument", 0},
		{"go missing return", "prog.go: missing return at end of function", 0},
		{"go nil pointer", "panic: runtime error: invalid memory address or nil pointer dereference", 0},
		{"go index out of range", "panic: runtime error: index out of range [5] with length 3", 0},
		{"python module", "Traceback (most recent call last):\nModuleNotFoundError: No module named 'requests'", 0},
		{"python key error", "KeyError: 'strategy'", 0},
		{"python type error", `TypeError: can only concatenate str (not "int") to str`, 0},
		{"rust borrow moved", "error[E0382]: borrow of moved value: `s`", 0},
		{"rust borrow mutable", "cannot borrow `x` as mutable because it is also borrowed as immutable", 0},
		{"rust index", "thread 'main' panicked at src/main.rs:5:1:\nindex out of bounds: the len is 3 but the index is 5", 0},
		{"rust unwrap none", "called `unwrap` on a `None` value", 0},
		{"node missing module", "node:internal/modules/cjs/loader:988\nError: Cannot find module 'express'", 0},
		{"permission denied", "open /etc/shadow: permission denied", 0},
		{"out of memory", "fatal error: runtime: cannot allocate memory", 0},
		{"connection refused", "dial tcp 127.0.0.1:8080: connect: connection refused", 0},
		{"multiple matches in one text", "build failed: undefined: foo; cannot use 1 as type int; connection refused", 0},
		{"case-insensitive keywords", "UNDEFINED: Foo", 0},
		{"no word boundary before keyword", "reundefined: x", 0},
		{"tab whitespace run", "undefined:\t\tfoo", 0},
		{"multibyte capture", "undefined: 日本語のエラー", 0},
		{"dangling keyword no match", "undefined: ", 0},
		{"no match at all", "everything is fine, nothing to see here", 0},
		{"limit two of three", "build failed: undefined: foo; cannot use 1 as type int; connection refused", 2},
		{
			// Hits 11 patterns; the default limit of 10 must cut the last.
			"eleven hits default limit", strings.Join([]string{
				"undefined: a",
				"cannot use b as type c",
				"missing return at end of function",
				"runtime error: invalid memory address or nil pointer dereference",
				"runtime error: index out of range",
				"ModuleNotFoundError: No module named 'x'",
				"KeyError: 'k'",
				"TypeError: t",
				"borrow of moved value",
				"index out of bounds: the len is 1 but the index is 2",
				"called `unwrap` on a `None` value",
			}, "\n"), 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(errorInput{Text: tc.text, Limit: tc.limit})
			require.NoError(t, err)
			res, err := runError(t.Context(), raw)
			require.NoError(t, err)

			want := naiveErrorMatches(tc.text, tc.limit)
			if len(want) == 0 {
				assert.Equal(t, "No known error patterns matched", res.Content)
				assert.Equal(t, "0 matches", res.Detail)
				return
			}
			assert.Equal(t, fmt.Sprintf("%d pattern matches", len(want)), res.Detail)
			got := parseErrorEntries(t, res.Content)
			require.Len(t, got, len(want))
			for i, w := range want {
				assert.Equal(t, w.name, got[i].name, "hit %d pattern name", i)
				assert.Equal(t, w.matched, got[i].matched, "hit %d matched span", i)
			}
		})
	}
}

// naiveErrorDetail re-derives the detail line via a map-based decode — a
// different encoding/json path than the struct decode in DetailFromArgs.
func naiveErrorDetail(input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	s, _ := m["text"].(string)
	s = strings.TrimSpace(s)
	if len(s) > 40 {
		s = s[:40] + "..."
	}
	return fmt.Sprintf("error match %s", s)
}

// oracle: naive reference — a map-based encoding/json decode (the tool decodes
// into a struct) plus an independently written trim and 40-byte truncation,
// over normal, oversized, multibyte, and hostile non-JSON payloads.
func TestDetailFromArgs_Differential(t *testing.T) {
	long := strings.Repeat("gate", 15)    // 60 bytes, past the cut
	unicode := strings.Repeat("错误信息", 15) // the byte cut lands mid-rune

	cases := []struct {
		name  string
		input string
	}{
		{"plain", `{"text":"undefined: foo"}`},
		{"padded", `{"text":"  undefined: foo  "}`},
		{"oversized", `{"text":"` + long + `"}`},
		{"multibyte", `{"text":"` + unicode + `"}`},
		{"empty object", `{}`},
		{"missing text key", `{"limit":3}`},
		{"not json", `nope`},
		{"null text", `{"text": null}`},
		{"number text", `{"text": 42}`},
		{"array payload", `[]`},
	}
	tool := ErrorTool()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, naiveErrorDetail(json.RawMessage(tc.input)),
				tool.DetailFromArgs(json.RawMessage(tc.input)))
		})
	}
}
