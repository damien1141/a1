package readtool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// naiveNormalizeLF re-derives CRLF/CR → LF normalization with a byte scan.
func naiveNormalizeLF(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\r' {
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			b.WriteByte('\n')
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// naivePad4 left-pads a line number to width four — %4d by hand.
func naivePad4(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 4 {
		s = " " + s
	}
	return s
}

// naiveSplitMarker rebuilds the hard-split notice: the cut length is named
// so the model knows where the line was cut, and the next offset is
// lineNo+1 — the read strictly advances past the over-long line.
func naiveSplitMarker(lineNo, cut int) string {
	return "... line " + strconv.Itoa(lineNo) + " split at " + strconv.Itoa(cut) +
		" bytes. Next offset: " + strconv.Itoa(lineNo+1) + "\n"
}

// naiveSplitLine re-derives the first-line hard split: the %4d prefix, the
// largest cut of content that leaves room for the newline and the marker
// inside the 50 KiB cap, then the marker. The marker's width shifts with
// the cut's digit count, so the cut is settled by decrementing.
func naiveSplitLine(b *strings.Builder, lineNo int, line string) {
	prefix := naivePad4(lineNo) + "|"
	cut := 50*1024 - len(prefix) - 1
	for cut > 0 && len(prefix)+cut+1+len(naiveSplitMarker(lineNo, cut)) > 50*1024 {
		cut--
	}
	if cut > len(line) {
		cut = len(line)
	}
	b.WriteString(prefix)
	b.WriteString(line[:cut])
	b.WriteByte('\n')
	b.WriteString(naiveSplitMarker(lineNo, cut))
}

// naiveRead is the reference reader: os.ReadFile plus the documented
// pagination, line numbering, and 50 KiB byte-cap rules, rebuilt from the
// tool's contract rather than its code.
func naiveRead(t *testing.T, name string, offset, limit int) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	require.NoError(t, err)
	text := naiveNormalizeLF(string(raw))
	header := "@file " + name
	if text == "" {
		return header + "\n(empty file)"
	}
	if offset < 1 {
		offset = 1
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	lines := strings.Split(text, "\n")
	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	bytesN, collected := 0, 0
	for lineNo := offset; lineNo <= len(lines); lineNo++ {
		line := lines[lineNo-1]
		if bytesN+len(line)+1 > 50*1024 {
			if bytesN == 0 && collected == 0 {
				// First line to collect busts the cap alone: hard-split it
				// and point strictly past it, never back at it.
				naiveSplitLine(&b, lineNo, line)
				break
			}
			b.WriteString("\n... truncated at " + strconv.Itoa(50*1024) +
				" bytes. Next offset: " + strconv.Itoa(lineNo) + "\n")
			break
		}
		b.WriteString(naivePad4(lineNo) + "|" + line + "\n")
		bytesN += len(line) + 1
		collected++
		if collected >= limit {
			if lineNo < len(lines) {
				b.WriteString("... truncated at " + strconv.Itoa(limit) +
					" lines. Next offset: " + strconv.Itoa(lineNo+1) + "\n")
			}
			break
		}
	}
	return b.String()
}

// oracle: naive reference implementation — os.ReadFile plus a hand-written
// line-numbering and byte-cap formatter, compared against the tool for
// plain, paginated, CRLF-bearing, oversized, and degenerate files.
func TestRunRead_MatchesNaiveFormatter_Differential(t *testing.T) {
	cases := []struct {
		name   string
		files  map[string]string
		offset int
		limit  int
		read   string
	}{
		{
			name:  "plain file with trailing newline",
			files: map[string]string{"notes.txt": "alpha\nbeta\ngamma\n"},
			read:  "notes.txt",
		},
		{
			name:  "no trailing newline",
			files: map[string]string{"brief.txt": "one\ntwo"},
			read:  "brief.txt",
		},
		{
			name:   "pagination offset and limit",
			files:  map[string]string{"page.txt": "l1\nl2\nl3\nl4\n"},
			offset: 2,
			limit:  1,
			read:   "page.txt",
		},
		{
			name:   "limit reaching past the end mid-file",
			files:  map[string]string{"tail.txt": "l1\nl2\nl3\n"},
			offset: 3,
			limit:  10,
			read:   "tail.txt",
		},
		{
			name:   "offset past the end of the file",
			files:  map[string]string{"gone.txt": "only line\n"},
			offset: 10,
			read:   "gone.txt",
		},
		{
			name:  "crlf and lone cr normalized",
			files: map[string]string{"mixed.txt": "a\r\nb\rc\r\n"},
			read:  "mixed.txt",
		},
		{
			name: "byte cap truncation",
			files: map[string]string{
				"big.txt": strings.Repeat("a", 49000) + "\n" + strings.Repeat("b", 2200) + "\n",
			},
			read: "big.txt",
		},
		{
			name: "first line over the byte cap hard-splits",
			files: map[string]string{
				"huge.txt": strings.Repeat("a", 60000) + "\nsecond line\n",
			},
			read: "huge.txt",
		},
		{
			name:  "empty file",
			files: map[string]string{"void.txt": ""},
			read:  "void.txt",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
			}
			t.Chdir(root)

			raw, err := json.Marshal(readInput{Path: tc.read, Offset: tc.offset, Limit: tc.limit})
			require.NoError(t, err)
			out, err := runRead(t.Context(), raw)
			require.NoError(t, err)

			assert.Equal(t, naiveRead(t, tc.read, tc.offset, tc.limit), out.Content)
			assert.Equal(t, tc.read, out.Detail, "the header path is the display path")
			assert.Equal(t, out.Content, out.Output, "read output mirrors content")
		})
	}
}

// A first line larger than the 50 KiB cap once produced a truncation notice
// whose "Next offset" named the very line being read, so a model following
// the hint re-read the same over-long line forever — zero progress, infinite
// tool loop. The contract now is a hard split: a fitting prefix, a marker
// whose Next offset is line 2, and a follow-up read at that offset that
// returns the rest of the file.
func TestRunRead_FirstLineOverByteCap_Advances_Differential(t *testing.T) {
	root := t.TempDir()
	name := "huge.txt"
	require.NoError(t, os.WriteFile(filepath.Join(root, name),
		[]byte(strings.Repeat("a", 60000)+"\nsecond line\n"), 0o644))
	t.Chdir(root)

	raw, err := json.Marshal(readInput{Path: name})
	require.NoError(t, err)
	out, err := runRead(t.Context(), raw)
	require.NoError(t, err)

	// The split prefix: line 1's numbered prefix plus a large prefix of
	// the line's content (the exact cut is pinned by the naive oracle).
	assert.Contains(t, out.Content, "   1|"+strings.Repeat("a", 50000))
	// The marker names the line, the cut, and strictly the next offset.
	assert.Contains(t, out.Content, "... line 1 split at ")
	assert.Contains(t, out.Content, "Next offset: 2\n")
	assert.NotContains(t, out.Content, "truncated at 51200 bytes",
		"the old notice pointed back at the same over-long line")
	// The read stopped at the split: line 2 waits for the next call.
	assert.NotContains(t, out.Content, "second line")
	assert.Equal(t, naiveRead(t, name, 0, 0), out.Content)

	// Following the hint strictly advances: offset 2 returns the rest.
	raw2, err := json.Marshal(readInput{Path: name, Offset: 2})
	require.NoError(t, err)
	out2, err := runRead(t.Context(), raw2)
	require.NoError(t, err)
	assert.Equal(t, naiveRead(t, name, 2, 0), out2.Content)
	assert.Contains(t, out2.Content, "   2|second line")
}
