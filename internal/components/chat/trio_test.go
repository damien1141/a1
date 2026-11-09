package chat

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

func TestActiveTokenHostileCursor_Negative(t *testing.T) {
	// Cursors far outside the value clamp instead of panicking; the query is
	// whatever the clamped position implies.
	q, start, end, ok := ActiveSlash("/resu", -99)
	assert.False(t, ok)
	assert.Equal(t, "", q)
	assert.Equal(t, 0, start)
	assert.Equal(t, 0, end)

	q, start, end, ok = ActiveSlash("/resu", 9999)
	require.True(t, ok)
	assert.Equal(t, "resu", q)
	assert.Equal(t, 0, start)
	assert.Equal(t, 5, end, "end clamps to len(value)")

	_, _, _, ok = ActiveSlash("", 3)
	assert.False(t, ok, "empty value")
	_, _, _, ok = ActiveSlash("/a b", 99)
	assert.False(t, ok, "cursor beyond the space that ends the command token")

	q, _, _, ok = ActiveQuestion("?he", -1)
	assert.False(t, ok)
	q, _, end, ok = ActiveQuestion("?he", 99)
	require.True(t, ok)
	assert.Equal(t, "he", q)
	assert.Equal(t, 3, end)

	q, start, end, ok = ActiveMention("@ab", -7)
	assert.False(t, ok)
	q, start, end, ok = ActiveMention("  @x", 99)
	require.True(t, ok)
	assert.Equal(t, "x", q)
	assert.Equal(t, 2, start)
	assert.Equal(t, 4, end)
	_, _, _, ok = ActiveMention("a@b", 99)
	assert.False(t, ok, "email-style mid-token @ stays inert")
	_, _, _, ok = ActiveMention("", 50)
	assert.False(t, ok, "empty value")
	q, start, end, ok = ActiveMention("@日本", 99)
	require.True(t, ok)
	assert.Equal(t, "日本", q)
	assert.Equal(t, 0, start)
	assert.Equal(t, 7, end, "byte offsets through multibyte runes")

	cmd, q, cs, ce, as, ae, ok := ActiveSlashArgs("/diff --stat", -5)
	assert.False(t, ok)
	cmd, q, cs, ce, as, ae, ok = ActiveSlashArgs("/diff --stat", 999)
	require.True(t, ok)
	assert.Equal(t, "diff", cmd)
	assert.Equal(t, "--stat", q)
	assert.Equal(t, 1, cs)
	assert.Equal(t, 5, ce)
	assert.Equal(t, 6, as)
	assert.Equal(t, 12, ae)
	_, _, _, _, _, _, ok = ActiveSlashArgs("/", 1)
	assert.False(t, ok, "bare slash has no args")
	_, _, _, _, _, _, ok = ActiveSlashArgs("/x", 99)
	assert.False(t, ok, "command with no trailing whitespace has no arg slot")
}

func TestReplaceRangeHostileBounds_Negative(t *testing.T) {
	c := &ChatInput{Value: "hello"}
	c.ReplaceRange(-5, 2, "X")
	assert.Equal(t, "Xllo", c.Value, "start clamps to 0")
	assert.Equal(t, 1, c.Cursor)

	c = &ChatInput{Value: "hello"}
	c.ReplaceRange(3, 99, "X")
	assert.Equal(t, "helX", c.Value, "end clamps to len(value)")
	assert.Equal(t, 4, c.Cursor)

	c = &ChatInput{Value: "hello"}
	c.ReplaceRange(4, 2, "X")
	assert.Equal(t, "heXo", c.Value, "backwards range swaps, then replaces [2:4]")
	assert.Equal(t, 3, c.Cursor)

	// Both bounds negative: each clamps to 0, so the range stays empty and the
	// text inserts at the top instead of slicing Value[:-7].
	c = &ChatInput{Value: "hello"}
	c.ReplaceRange(-5, -7, "X")
	assert.Equal(t, "Xhello", c.Value, "both-negative range collapses to an insertion at 0")
	assert.Equal(t, 1, c.Cursor)

	// A negative end clamps to 0 before the swap, so (2, -3) orders to (0, 2) —
	// the same replacement the (-5, 2) case above pins. Clamping before the
	// swap also makes argument order irrelevant for hostile bounds.
	c = &ChatInput{Value: "hello"}
	c.ReplaceRange(2, -3, "X")
	assert.Equal(t, "Xllo", c.Value, "negative end clamps to 0, then swaps below start")
	assert.Equal(t, 1, c.Cursor)
	c = &ChatInput{Value: "hello"}
	c.ReplaceRange(-3, 2, "X")
	assert.Equal(t, "Xllo", c.Value, "hostile bounds are order-independent")
	assert.Equal(t, 1, c.Cursor)

	// A start past the end used to escape the same way: it swapped below a
	// valid end and then sliced Value[:99] — out of range.
	c = &ChatInput{Value: "hello"}
	c.ReplaceRange(99, 2, "X")
	assert.Equal(t, "heX", c.Value, "start past the end clamps to len, then swaps")
	assert.Equal(t, 3, c.Cursor)

	// Replacement text runs through the composer sanitizer.
	c = &ChatInput{}
	c.ReplaceRange(0, 0, "a\x00b\tc")
	assert.Equal(t, "ab    c", c.Value, "NUL dropped, tab widened to 4 spaces")
	assert.Equal(t, 7, c.Cursor)
}

func TestChatInputClampsHostileCursor_Negative(t *testing.T) {
	ctx := &components.EventContext{}
	c := &ChatInput{MinBodyRows: 3, Value: "alpha\nbeta", Cursor: -100}
	c.Handle(ctx, xui.KeyEvent{Code: xui.KeyHome, Press: true})
	assert.Equal(t, 0, c.Cursor, "negative cursor clamps to 0 before Home")
	c.Handle(ctx, xui.KeyEvent{Code: xui.KeyEnd, Press: true})
	assert.Equal(t, 5, c.Cursor, "End of the clamped (first) line")

	c.Cursor = 999
	c.Handle(ctx, xui.KeyEvent{Code: xui.KeyHome, Press: true})
	assert.Equal(t, 6, c.Cursor, "cursor past the end clamps, then Home finds line 2")
	c.Handle(ctx, xui.KeyEvent{Code: xui.KeyEnd, Press: true})
	assert.Equal(t, 10, c.Cursor)

	typing := &ChatInput{Value: "ab", Cursor: -9}
	typing.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'x', Press: true})
	assert.Equal(t, "xab", typing.Value, "hostile negative cursor inserts at 0")
	assert.Equal(t, 1, typing.Cursor)

	typing = &ChatInput{Value: "ab", Cursor: 999}
	typing.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'x', Press: true})
	assert.Equal(t, "abx", typing.Value, "hostile far cursor appends at the end")
	assert.Equal(t, 3, typing.Cursor)

	deleting := &ChatInput{Value: "ab", Cursor: 999}
	deleting.Handle(ctx, xui.KeyEvent{Code: xui.KeyBackspace, Press: true})
	assert.Equal(t, "a", deleting.Value, "backspace deletes the last rune, not garbage")
	assert.Equal(t, 1, deleting.Cursor)
}

func naiveLineStart(s string, off int) int {
	if off > len(s) {
		off = len(s)
	}
	if off < 0 {
		off = 0
	}
	start := 0
	for _, ln := range strings.Split(s, "\n") {
		if off <= start+len(ln) {
			return start
		}
		start += len(ln) + 1
	}
	return start
}

func naiveLineEnd(s string, off int) int {
	if off > len(s) {
		off = len(s)
	}
	if off < 0 {
		off = 0
	}
	start := 0
	for _, ln := range strings.Split(s, "\n") {
		if off <= start+len(ln) {
			return start + len(ln)
		}
		start += len(ln) + 1
	}
	return len(s)
}

// oracle: the strings.Split-based references above — line offsets recomputed
// from cumulative byte widths instead of LastIndexByte/IndexByte scans — on
// randomized multiline values and offsets (including out-of-range offsets on
// both sides, which both functions clamp into [0, len]).
func TestLineStartLineEndMatchesSplitOracle_Differential(t *testing.T) {
	alphabet := []string{"a", "β", "\n", "cc", "✓", "日"}
	rng := rand.New(rand.NewSource(3))
	for i := range 300 {
		var sb strings.Builder
		for j := rng.Intn(18); j > 0; j-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		s := sb.String()
		for off := 0; off <= len(s); off += 1 + rng.Intn(3) {
			assert.Equal(t, naiveLineStart(s, off), LineStart(s, off), "case %d: LineStart(%q, %d)", i, s, off)
			assert.Equal(t, naiveLineEnd(s, off), LineEnd(s, off), "case %d: LineEnd(%q, %d)", i, s, off)
		}
		assert.Equal(t, naiveLineStart(s, len(s)+7), LineStart(s, len(s)+7), "past-the-end clamp")
		assert.Equal(t, naiveLineEnd(s, len(s)+7), LineEnd(s, len(s)+7), "past-the-end clamp")
		// Negative offsets clamp to 0 exactly as past-the-end ones clamp to
		// len; without the clamp, s[:off] and s[off:] slice out of range.
		assert.Equal(t, naiveLineStart(s, -1), LineStart(s, -1), "negative clamp")
		assert.Equal(t, naiveLineEnd(s, -1), LineEnd(s, -1), "negative clamp")
		assert.Equal(t, naiveLineStart(s, -99), LineStart(s, -99), "negative clamp")
		assert.Equal(t, naiveLineEnd(s, -99), LineEnd(s, -99), "negative clamp")
	}

	// Hand-picked edges: empty string, leading/trailing newlines, offsets
	// sitting exactly on newline bytes.
	for _, s := range []string{"", "\n", "a\n", "\na", "a\n\nb", "ab"} {
		// Start below 0 to sweep the negative-clamp edge of the hand-picked
		// strings too (empty string included).
		for off := -2; off <= len(s); off++ {
			assert.Equal(t, naiveLineStart(s, off), LineStart(s, off), "LineStart(%q, %d)", s, off)
			assert.Equal(t, naiveLineEnd(s, off), LineEnd(s, off), "LineEnd(%q, %d)", s, off)
		}
	}
}

func naiveSanitize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteByte('\n')
		case r == '\t':
			b.WriteString("    ")
		case r < 0x20, r == 0x7f:
			// control characters are dropped
		case r == '▎', r == '▌', r == '┃':
			// transcript chrome is dropped
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// oracle: the per-rune filter reference above (plus stdlib strings.ReplaceAll
// for the CR/CRLF collapse) on randomized hostile pastes.
func TestSanitizeComposerTextMatchesNaiveFilter_Differential(t *testing.T) {
	alphabet := []string{"a", "日", "\t", "\x00", "\x1b", "\x7f", "\r", "\r\n", "\n", "▎", "▌", "┃", " ", "😀", "e"}
	rng := rand.New(rand.NewSource(9))
	for i := range 400 {
		var sb strings.Builder
		for j := rng.Intn(20); j > 0; j-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		s := sb.String()
		assert.Equal(t, naiveSanitize(s), sanitizeComposerText(s), "case %d: sanitize(%q)", i, s)
	}
	for _, s := range []string{"", "\r\n\r\n", "▎hello", "a\r\rb", "\t\t\t", "混合\x00\t日"} {
		assert.Equal(t, naiveSanitize(s), sanitizeComposerText(s), "sanitize(%q)", s)
	}
}

// forwardActiveMention re-derives the mention state with a forward scan: the
// last '@' before the cursor that no whitespace separates from it, accepted
// only when the rune before the '@' cannot be part of a token (letter, digit,
// '_', '.', '-').
func forwardActiveMention(value string, cursor int) (query string, start, end int, ok bool) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(value) {
		cursor = len(value)
	}
	lastAt := -1
	for off := 0; off < len(value); {
		r, size := utf8.DecodeRuneInString(value[off:])
		if off >= cursor {
			break
		}
		switch {
		case r == '@':
			lastAt = off
		case r == ' ' || r == '\n' || r == '\r' || unicode.IsSpace(r):
			lastAt = -1
		}
		off += size
	}
	if lastAt < 0 {
		return "", 0, 0, false
	}
	if lastAt > 0 {
		prev, _ := utf8.DecodeLastRuneInString(value[:lastAt])
		if unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == '_' || prev == '.' || prev == '-' {
			return "", 0, 0, false
		}
	}
	return value[lastAt+1 : cursor], lastAt, cursor, true
}

// oracle: the forward-scan reference above on randomized values; ActiveMention
// scans backward, the oracle forward — both must agree on query, offsets, and
// acceptance for rune-aligned and out-of-range cursors.
func TestActiveMentionMatchesForwardScan_Differential(t *testing.T) {
	alphabet := []string{"a", "@", " ", "\n", ".", "_", "1", "日"}
	rng := rand.New(rand.NewSource(13))
	for i := range 300 {
		var sb strings.Builder
		boundaries := []int{0}
		for j := rng.Intn(14); j > 0; j-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
			boundaries = append(boundaries, sb.Len())
		}
		value := sb.String()
		cursors := append(boundaries, len(value)+10, -3)
		for _, cur := range cursors {
			wantQ, wantStart, wantEnd, wantOK := forwardActiveMention(value, cur)
			gotQ, gotStart, gotEnd, gotOK := ActiveMention(value, cur)
			require.Equal(t, wantOK, gotOK, "case %d: ok for (%q, %d)", i, value, cur)
			assert.Equal(t, wantQ, gotQ, "case %d: query for (%q, %d)", i, value, cur)
			assert.Equal(t, wantStart, gotStart, "case %d: start for (%q, %d)", i, value, cur)
			assert.Equal(t, wantEnd, gotEnd, "case %d: end for (%q, %d)", i, value, cur)
		}
	}
}

func naiveRefLabel(r Ref) string {
	start := r.Start
	if start < 1 {
		start = 1
	}
	end := r.End
	if end < start {
		end = start
	}
	if end > start {
		return r.Path + ":" + strconv.Itoa(start) + "-" + strconv.Itoa(end)
	}
	return r.Path + ":" + strconv.Itoa(start)
}

// oracle: naive clamp-then-format reference built with strconv and string
// concatenation, on randomized Ref fields including zero, negative, and
// backwards line ranges.
func TestRefLabelMatchesNaiveClamp_Differential(t *testing.T) {
	paths := []string{"", "main.go", "a/b/c.ts", "日本.md"}
	nums := []int{-3, 0, 1, 2, 5, 17}
	rng := rand.New(rand.NewSource(17))
	for i := range 300 {
		r := Ref{
			Path:  paths[rng.Intn(len(paths))],
			Start: nums[rng.Intn(len(nums))],
			End:   nums[rng.Intn(len(nums))],
		}
		assert.Equal(t, naiveRefLabel(r), r.Label(), "case %d: %+v", i, r)
	}
}

// oracle: structural round trip — Block() output is parsed back with plain
// string ops (split, prefix/suffix) and must reassemble the label, fence
// language, and verbatim text; the only permitted mutation is the single
// newline the writer adds when Text does not end with one.
func TestRefBlockRoundTrip_Differential(t *testing.T) {
	texts := []string{"", "x", "a := 1\nb := 2", "trailing\n", "multi\n\nblank\n", "```\n", "混合 text\n"}
	langs := []string{"", "go", "ts"}
	rng := rand.New(rand.NewSource(21))
	for i := range 200 {
		r := Ref{
			Path:  "main.go",
			Start: rng.Intn(20) - 2,
			End:   rng.Intn(20) - 2,
			Lang:  langs[rng.Intn(len(langs))],
			Text:  texts[rng.Intn(len(texts))],
		}
		block := r.Block()
		lines := strings.Split(block, "\n")
		require.GreaterOrEqual(t, len(lines), 3, "case %d: block needs header, text, fence", i)
		assert.Equal(t, r.Label(), lines[0], "case %d: heading line", i)
		assert.Equal(t, "```"+r.Lang, lines[1], "case %d: fence header carries the language", i)
		assert.Equal(t, "```", lines[len(lines)-1], "case %d: closing fence", i)
		assert.True(t, strings.HasSuffix(block, "\n```"), "case %d: fence closes on its own line", i)

		wantText := r.Text
		if !strings.HasSuffix(wantText, "\n") {
			wantText += "\n"
		}
		wantText = strings.TrimSuffix(wantText, "\n")
		gotText := strings.Join(lines[2:len(lines)-1], "\n")
		assert.Equal(t, wantText, gotText, "case %d: verbatim selection text", i)
	}

	// Golden literal for one fixed state.
	fixed := Ref{Path: "main.go", Start: 12, End: 13, Lang: "go", Text: "a := 1\nb := 2"}
	assert.Equal(t, "main.go:12-13\n```go\na := 1\nb := 2\n```", fixed.Block())
}
