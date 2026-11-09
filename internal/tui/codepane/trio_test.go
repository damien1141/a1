package codepane

import (
	"math/bits"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Size and emptiness boundaries: a file one byte over the cap is refused with
// the cap named, a file exactly at the cap loads, and an empty file yields a
// zero-line document rather than a phantom blank line.
func TestReadFileLines_SizeAndEmptyFiles_Negative(t *testing.T) {
	dir := t.TempDir()

	oversize := filepath.Join(dir, "big.log")
	require.NoError(t, os.WriteFile(oversize, filler(maxFileBytes+1), 0o600))
	_, err := readFileLines(oversize)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the viewer caps at")
	assert.Contains(t, err.Error(), "8.0 MB")

	atCap := filepath.Join(dir, "cap.log")
	require.NoError(t, os.WriteFile(atCap, filler(maxFileBytes), 0o600))
	lines, err := readFileLines(atCap)
	require.NoError(t, err, "a file exactly at the cap still loads")
	assert.Len(t, lines, 1)

	empty := filepath.Join(dir, "empty.txt")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	lines, err = readFileLines(empty)
	require.NoError(t, err)
	assert.Empty(t, lines, "an empty file has no lines, not one blank line")

	blank := filepath.Join(dir, "blank.txt")
	require.NoError(t, os.WriteFile(blank, []byte("\n"), 0o600))
	lines, err = readFileLines(blank)
	require.NoError(t, err)
	assert.Equal(t, []string{""}, lines, "a lone newline is one blank line")
}

// filler builds a single-character payload of n bytes for size-boundary tests.
func filler(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return b
}

// A successfully loaded but line-less file must degrade gracefully: G stays on
// line 0, the selection keys still work, and adding a ref reports that there is
// nothing to add instead of panicking on an empty slice.
func TestPane_EmptyFileKeys_Negative(t *testing.T) {
	h := newHarness(t, map[string]string{"e.txt": ""})
	h.pane.Open("e.txt")
	require.True(t, h.pane.Active())
	require.Empty(t, h.pane.lines)

	h.key(t, 'G')
	assert.Equal(t, 0, h.pane.line, "G on an empty file stays on line 0")

	h.key(t, 'v')
	require.True(t, h.pane.selecting)
	h.key(t, 'a')
	assert.Contains(t, h.toasts, "no file open", "an empty file has nothing to hand to chat")

	h.pane.OpenAt("e.txt", -7)
	assert.Equal(t, 0, h.pane.line, "a negative line request clamps to the top")
}

// oracle: stdlib strings.TrimLeft — firstNonBlank is the byte offset of the
// first rune that is not a space or tab, i.e. len(s)-len(TrimLeft(s, " \t")).
func TestFirstNonBlank_TrimLeftEquivalence_Differential(t *testing.T) {
	cases := []string{
		"", " ", "   ", "\t", " \t\tx", "  x", "func f()", "日", "  日",
		"\u00a0x", "x ", " x ", " \t \t ", "a b", "  a\tb",
	}
	for _, s := range cases {
		expected := len(s) - len(strings.TrimLeft(s, " \t"))
		assert.Equal(t, expected, firstNonBlank(s), "firstNonBlank(%q)", s)
	}

	// Randomized mixes of indentation and content.
	alphabet := []string{" ", "\t", "x", "日", "a", "\n"}
	var sb strings.Builder
	seen := map[string]bool{}
	for i := 0; i < 400; i++ {
		sb.Reset()
		for j := (i*7)%6 + 1; j > 0; j-- {
			sb.WriteString(alphabet[(i*j)%len(alphabet)])
		}
		s := sb.String()
		if seen[s] {
			continue
		}
		seen[s] = true
		expected := len(s) - len(strings.TrimLeft(s, " \t"))
		assert.Equal(t, expected, firstNonBlank(s), "firstNonBlank(%q)", s)
	}
}

// naiveHumanBytes derives the exponent from the bit length of n instead of a
// division loop: floor(log2(n))/10 - 1 for n >= 1024.
func naiveHumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	exp := (bits.Len64(uint64(n))-1)/10 - 1
	div := int64(1) << uint(10*(exp+1))
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + string("KMGTPE"[exp]) + "B"
}

// oracle: bit-length exponent reference (above) over unit boundaries, near
// boundaries, and the int64 range; both must pick the same unit and mantissa.
func TestHumanBytes_BitExponentReference_Differential(t *testing.T) {
	cases := []int64{
		-1, 0, 1, 512, 1023, 1024, 1025, 1536, 2047, 2048, 4095, 4096,
		999999, 1 << 20, 1<<20 - 1, 1<<20 + 1, 8 << 20, 8<<20 + 1,
		1 << 30, 1<<30 - 1, 1 << 40, 1<<50 + 12345, 1<<60 - 1, 1 << 60,
		1<<62 + 5, 1<<63 - 1,
	}
	for _, n := range cases {
		assert.Equal(t, naiveHumanBytes(n), humanBytes(n), "humanBytes(%d)", n)
	}
	for n := int64(1000); n <= 1100; n++ {
		assert.Equal(t, naiveHumanBytes(n), humanBytes(n), "humanBytes(%d)", n)
	}
	for n := int64(1 << 20); n < 1<<20+64; n++ {
		assert.Equal(t, naiveHumanBytes(n), humanBytes(n), "humanBytes(%d)", n)
	}
}

// naiveReadLines applies the line-ending spec with plain stdlib string ops:
// CRLF and lone CR both end lines, the text splits on \n, and one trailing
// empty element from a final newline is dropped.
func naiveReadLines(raw string) []string {
	norm := strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(norm, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// oracle: stdlib line-ending + split reference (above) on the same bytes the
// pane reads; readFileLines must agree on every edge shape.
func TestReadFileLines_LineSplitSemantics_Differential(t *testing.T) {
	corpus := []string{
		"a\nb\n", "a\nb", "", "\n", "\n\n", "a\n\n\nb\n",
		"a\r\nb\r\n", "a\r\nb", "a\rb\n", "a\rb", "\r\n", "\r",
		"日本\n語\n", "no trailing newline", "trail\r\n", "mid\r\nline\n",
		"\ta\n b\n", "last line empty\n\n",
	}
	for i, body := range corpus {
		path := filepath.Join(t.TempDir(), "f"+strconv.Itoa(i)+".txt")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		got, err := readFileLines(path)
		require.NoError(t, err, "case %d: %q", i, body)
		assert.Equal(t, naiveReadLines(body), got, "case %d: body %q", i, body)
	}
}
