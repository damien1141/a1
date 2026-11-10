package stoat

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChunkMessageUnderLimitIsWhole pins the trivial band: anything up
// to 2000 runes is one message.
func TestChunkMessageUnderLimitIsWhole(t *testing.T) {
	for _, s := range []string{"", "hi", strings.Repeat("a", 2000), strings.Repeat("é", 2000), strings.Repeat("世", 2000)} {
		assert.Equal(t, []string{s}, chunkMessage(s), "len=%d", len(s))
	}
}

// TestChunkMessageRoundTrip_Differential is the chunker's golden table:
// every case's chunk count is hand-computed from the 2000-RUNE budget
// (never from running the splitter), and plain concatenation of the
// chunks must reproduce the input byte for byte — the property that
// makes the transcript and the channel agree on what was said.
func TestChunkMessageRoundTrip_Differential(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		count int // the hand-computed expectation
		runes int // rune count of the input, for the arithmetic
	}{
		{"one over", strings.Repeat("a", 2001), 2, 2001},
		{"exactly two", strings.Repeat("a", 4000), 2, 4000},
		{"two and a tail", strings.Repeat("a", 4001), 3, 4001},
		{"cjk counts runes not bytes", strings.Repeat("世", 2001), 2, 2001},
		{"emoji is one rune each", strings.Repeat("🙂", 2001), 2, 2001},
		{"one long line", strings.Repeat("x", 10000), 5, 10000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.runes, utf8.RuneCountInString(tc.in), "the fixture's own arithmetic")
			chunks := chunkMessage(tc.in)
			assert.Len(t, chunks, tc.count, "budget arithmetic: %d runes at %d per message", tc.runes, maxMessageChars)
			assert.Equal(t, tc.in, strings.Join(chunks, ""), "concat(chunks) == input, byte for byte")
			for i, c := range chunks {
				assert.LessOrEqual(t, utf8.RuneCountInString(c), maxMessageChars, "chunk %d fits", i)
			}
		})
	}
}

// TestChunkMessagePrefersLineBoundaries pins the readability ruling: a
// cut lands on the last line boundary that fits, newline included, so a
// code block is not sliced mid-line when a break is available.
func TestChunkMessagePrefersLineBoundaries(t *testing.T) {
	// 1999 'a' runes, a newline, a short second line, a newline, then a
	// 5000-rune tail: 7101 runes total. The hand arithmetic: the first
	// cut can include the newline (2000 runes), the second cut lands
	// right after line two (101 runes), the tail is hard-cut at the
	// budget into 2000+2000+1000 — five chunks, none slicing a line.
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
// valid UTF-8 on its own, whatever the hard-cut path did.
func TestChunkMessageNeverSplitsARune(t *testing.T) {
	// Mixed 1-3 byte runes, no newlines: every cut is a hard cut.
	var sb strings.Builder
	for i := 0; i < 6000; i++ {
		switch i % 3 {
		case 0:
			sb.WriteString("a")
		case 1:
			sb.WriteString("é")
		case 2:
			sb.WriteString("世")
		}
	}
	in := sb.String()
	for _, c := range chunkMessage(in) {
		assert.True(t, utf8.ValidString(c), "every chunk is valid UTF-8")
	}
	assert.Equal(t, in, strings.Join(chunkMessage(in), ""))
}

// TestByteIndexOfRune pins the rune-walking primitive against the
// standard library's own idiom (RuneCount over prefixes).
func TestByteIndexOfRune(t *testing.T) {
	for _, s := range []string{"", "a", "abc", "a世b🙂c", strings.Repeat("世", 100)} {
		runeCount := utf8.RuneCountInString(s)
		for n := 0; n <= runeCount+1; n++ {
			// The oracle: the byte length of the first n runes.
			var want int
			if n >= runeCount {
				want = len(s)
			} else {
				want = len(string([]rune(s)[:n]))
			}
			assert.Equal(t, want, byteIndexOfRune(s, n), "s=%q n=%d", s, n)
		}
	}
}
