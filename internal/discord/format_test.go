package discord

import (
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChunkMessageUnderLimitIsWhole pins the trivial band: anything up
// to 2,000 UTF-16 units is one message.
func TestChunkMessageUnderLimitIsWhole(t *testing.T) {
	for _, s := range []string{"", "hi", strings.Repeat("a", 2000), strings.Repeat("é", 2000), strings.Repeat("世", 2000)} {
		assert.Equal(t, []string{s}, chunkMessage(s), "units=%d", utf16Len(s))
	}
}

// TestChunkMessageCountsUTF16UnitsNotRunes is the differential that
// separates this chunker from a rune counter: astral emoji are TWO
// units each in Discord's counting (every official client runs
// content.length in JavaScript). 1,000 emoji are 2,000 units — one
// message. 1,001 are 2,002 — two, where a rune counter would happily
// send "2,000 characters" and watch Discord reject it as 4,000.
func TestChunkMessageCountsUTF16UnitsNotRunes(t *testing.T) {
	emoji := strings.Repeat("🙂", 1000)
	assert.Equal(t, []string{emoji}, chunkMessage(emoji), "1,000 astral emoji are exactly 2,000 units")

	two := strings.Repeat("🙂", 1001)
	chunks := chunkMessage(two)
	require.Len(t, chunks, 2, "1,001 emoji overflow the budget by two units")
	assert.Equal(t, two, strings.Join(chunks, ""))
	for i, c := range chunks {
		assert.LessOrEqual(t, utf16Len(c), maxMessageUnits, "chunk %d fits", i)
	}
}

// TestChunkMessageRoundTrip_Differential is the chunker's golden table:
// every case's chunk count is hand-computed from the 2,000-UNIT budget
// (never from running the splitter), and plain concatenation of the
// chunks must reproduce the input byte for byte — the property that
// makes the transcript and the channel agree on what was said.
func TestChunkMessageRoundTrip_Differential(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		count int // the hand-computed expectation
		units int // UTF-16 unit count of the input, for the arithmetic
	}{
		{"one over", strings.Repeat("a", 2001), 2, 2001},
		{"exactly two", strings.Repeat("a", 4000), 2, 4000},
		{"two and a tail", strings.Repeat("a", 4001), 3, 4001},
		{"cjk is one unit each", strings.Repeat("世", 2001), 2, 2001},
		{"emoji is two units each", strings.Repeat("🙂", 2001), 3, 4002},
		{"mixed budget", strings.Repeat("a🙂", 1001), 2, 3003},
		{"one long line", strings.Repeat("x", 10000), 5, 10000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.units, utf16Len(tc.in), "the fixture's own arithmetic")
			chunks := chunkMessage(tc.in)
			assert.Len(t, chunks, tc.count, "budget arithmetic: %d units at %d per message", tc.units, maxMessageUnits)
			assert.Equal(t, tc.in, strings.Join(chunks, ""), "concat(chunks) == input, byte for byte")
			for i, c := range chunks {
				assert.LessOrEqual(t, utf16Len(c), maxMessageUnits, "chunk %d fits", i)
			}
		})
	}
}

// TestChunkMessagePrefersLineBoundaries pins the readability ruling: a
// cut lands on the last line boundary that fits, newline included, so a
// code block is not sliced mid-line when a break is available.
func TestChunkMessagePrefersLineBoundaries(t *testing.T) {
	// 1999 'a' units, a newline, a short second line, a newline, then a
	// 5,000-unit tail: 7,101 units total. The hand arithmetic: the
	// first cut can include the newline (2,000 units), the second cut
	// lands right after line two (101 units), the tail is hard-cut at
	// the budget into 2000+2000+1000 — five chunks, none slicing a line.
	head := strings.Repeat("a", 1999)
	line2 := strings.Repeat("b", 100)
	tail := strings.Repeat("c", 5000)
	in := head + "\n" + line2 + "\n" + tail

	chunks := chunkMessage(in)
	require.Len(t, chunks, 5)
	// The first chunk is exactly the head line: the newline rides it.
	assert.Equal(t, head+"\n", chunks[0])
	// The second is the short line, whole.
	assert.Equal(t, line2+"\n", chunks[1])
	assert.Equal(t, in, strings.Join(chunks, ""))
}

// TestChunkMessageNeverSplitsARune pins the wire safety: every chunk is
// valid UTF-8 on its own, whatever the hard-cut path did — the unit
// budget stops the walk BETWEEN runes even when a rune is worth two.
func TestChunkMessageNeverSplitsARune(t *testing.T) {
	// Mixed 1-3 byte runes and astral pairs, no newlines: every cut is
	// a hard cut.
	var sb strings.Builder
	for i := 0; i < 4000; i++ {
		switch i % 4 {
		case 0:
			sb.WriteString("a")
		case 1:
			sb.WriteString("é")
		case 2:
			sb.WriteString("世")
		case 3:
			sb.WriteString("🙂")
		}
	}
	in := sb.String()
	chunks := chunkMessage(in)
	require.Greater(t, len(chunks), 1)
	for i, c := range chunks {
		assert.True(t, utf8.ValidString(c), "chunk %d is valid UTF-8", i)
		assert.True(t, utf8.ValidString(strings.TrimSuffix(c, "\n")), "and so is its line-cut body")
	}
	assert.Equal(t, in, strings.Join(chunks, ""))
}

// TestUTF16Len pins the unit counter against the standard library's own
// conversion (the independent oracle: len(utf16.Encode(runes))).
func TestUTF16Len(t *testing.T) {
	for _, s := range []string{"", "a", "abc", "a世b🙂c", strings.Repeat("🙂", 100), strings.Repeat("世", 100)} {
		assert.Equal(t, utf16Oracle(s), utf16Len(s), "s=%q", s)
	}
}

// TestByteIndexOfUTF16 pins the walk: the prefix it returns is the
// longest one within budget, and it never splits a rune.
func TestByteIndexOfUTF16(t *testing.T) {
	s := "a🙂世b🙂c"
	// Units: a=1, 🙂=2, 世=1, b=1, 🙂=2, c=1 (total 8).
	// Bytes:  a=1, 🙂=4, 世=3, b=1, 🙂=4, c=1 (total 14); rune starts
	// at 0, 1, 5, 8, 9, 13.
	cases := []struct {
		budget int
		want   int
	}{
		{0, 0}, {1, 1}, {2, 1}, {3, 5}, {4, 8}, {5, 9}, {6, 9}, {7, 13}, {8, 14}, {100, 14},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, byteIndexOfUTF16(s, tc.budget), "budget=%d", tc.budget)
		// The returned prefix is within budget.
		assert.LessOrEqual(t, utf16Len(s[:tc.want]), tc.budget, "prefix within budget %d", tc.budget)
	}
}

// utf16Oracle is the independent counter: the standard library's own
// UTF-16 encoder, whose length is by definition the unit count.
func utf16Oracle(s string) int {
	return len(utf16.Encode([]rune(s)))
}
