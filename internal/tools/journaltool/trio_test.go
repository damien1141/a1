package journaltool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// naiveEntry mirrors the read-side contract of one .journal line, derived
// here independently of parseJournalLine.
type naiveEntry struct {
	timestamp time.Time
	action    string
	file      string
	line      int
	outcome   string
}

// naiveScanJournal re-derives readJournal with deliberately different
// machinery: bufio.Scanner instead of strings.Split, successive strings.Cut
// field extraction instead of SplitN, strconv.Atoi instead of fmt.Sscanf.
// Blank and malformed lines are skipped; at most limit valid entries are kept.
func naiveScanJournal(content string, limit int) []naiveEntry {
	var entries []naiveEntry
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		tsStr, rest, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		action, rest, ok := strings.Cut(rest, "|")
		if !ok {
			continue
		}
		file, rest, ok := strings.Cut(rest, "|")
		if !ok {
			continue
		}
		lineStr, outcome, ok := strings.Cut(rest, "|")
		if !ok {
			continue
		}
		ts, err := time.Parse(time.RFC3339, strings.TrimSpace(tsStr))
		if err != nil {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(lineStr))
		if err != nil {
			continue
		}
		entries = append(entries, naiveEntry{
			timestamp: ts,
			action:    strings.TrimSpace(action),
			file:      strings.TrimSpace(file),
			line:      n,
			outcome:   strings.TrimSpace(outcome),
		})
		if len(entries) >= limit {
			break
		}
	}
	return entries
}

// naiveRender re-derives renderJournalResults for relative file fields. The
// display rule for a path outside the cwd is stdlib filepath.Clean + ToSlash
// (and filepath.Clean("") is "."), which is what the entries hit when the
// journal stores relative file names.
func naiveRender(entries []naiveEntry) string {
	var sb strings.Builder
	for _, e := range entries {
		rel := filepath.ToSlash(filepath.Clean(e.file))
		sb.WriteString(fmt.Sprintf(
			"%s\t%s\t%s:%d\t%s\n",
			e.timestamp.Format("2006-01-02 15:04:05"),
			e.action, rel, e.line, e.outcome,
		))
	}
	return sb.String()
}

// oracle: naive reference implementation in this test — a bufio.Scanner +
// strings.Cut re-derivation of the .journal line format over a hand-written
// fixture with blank, malformed, unparseable-timestamp, padded, and
// pipe-bearing-outcome lines; the rendered output and entry count must match.
func TestRunJournal_ReadMatchesNaiveScan_Differential(t *testing.T) {
	const fixture = "2024-01-15T10:30:00Z|edit|main.go|42|success\n" +
		"2024-01-15T10:31:00Z|read|helper.go|10|success\n" +
		"\n" +
		"2024-01-15T10:32:00Z|bash|  |0|ok\n" +
		"not-a-journal-line\n" +
		"2024-01-15T10:33:00Z|write|a.go|3|error|extra|pipes\n" +
		"   2024-01-15T10:34:00Z|test|b_test.go|7|passed\n" +
		"bogus-timestamp|edit|c.go|1|fail\n" +
		"2024-01-15T10:35:00Z|decision||0|\n" +
		"too|few|fields\n" +
		"2024-01-15T10:36:00Z|run|d.go|12|ok\n"

	t.Run("all valid lines render in file order", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, ".journal", fixture)
		writeFile(t, root, "main.go", "package main\n")

		raw, err := json.Marshal(journalInput{Path: root, Limit: 50})
		require.NoError(t, err)
		out, err := runJournal(t.Context(), raw)
		require.NoError(t, err)

		want := naiveRender(naiveScanJournal(fixture, 50))
		assert.Equal(t, want, out.Content)
		assert.Equal(t, "7 journal entries", out.Detail)
		assert.Equal(t, want, out.Output)
	})

	t.Run("limit keeps the first valid entries", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, ".journal", fixture)
		writeFile(t, root, "main.go", "package main\n")

		raw, err := json.Marshal(journalInput{Path: root, Limit: 3})
		require.NoError(t, err)
		out, err := runJournal(t.Context(), raw)
		require.NoError(t, err)

		want := naiveRender(naiveScanJournal(fixture, 3))
		assert.Equal(t, want, out.Content)
		assert.Equal(t, "3 journal entries", out.Detail)
	})

	t.Run("empty file reports no entries", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, ".journal", "")
		writeFile(t, root, "main.go", "package main\n")

		raw, err := json.Marshal(journalInput{Path: root, Limit: 10})
		require.NoError(t, err)
		out, err := runJournal(t.Context(), raw)
		require.NoError(t, err)

		assert.Empty(t, naiveScanJournal("", 10))
		assert.Equal(t, "No journal entries found", out.Content)
		assert.Equal(t, "0 entries", out.Detail)
	})

	t.Run("limit zero falls back to the 50-entry default", func(t *testing.T) {
		root := t.TempDir()
		var lines strings.Builder
		for i := 0; i < 55; i++ {
			fmt.Fprintf(&lines, "2024-01-01T%02d:%02d:00Z|act%d|f%d.go|%d|ok\n", i/60, i%60, i, i, i)
		}
		writeFile(t, root, ".journal", lines.String())
		writeFile(t, root, "main.go", "package main\n")

		raw, err := json.Marshal(journalInput{Path: root, Limit: 0})
		require.NoError(t, err)
		out, err := runJournal(t.Context(), raw)
		require.NoError(t, err)

		kept := naiveScanJournal(lines.String(), 50)
		require.Len(t, kept, 50)
		assert.Equal(t, naiveRender(kept), out.Content)
		assert.Equal(t, "50 journal entries", out.Detail)
	})
}

// oracle: stdlib regexp over the on-disk bytes plus the naive scanner above.
// Recording one entry must leave a single line in the documented
// timestamp|action|file|line|outcome format, and both the record echo and a
// follow-up read must equal what the naive scanner derives from those bytes —
// the writer and reader are held to an independent parse of the file.
func TestRunJournal_RecordRoundTrip_Differential(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")

	raw, err := json.Marshal(journalInput{
		Path: root, Action: "test", File: "main.go", Line: 7, Outcome: "ok",
	})
	require.NoError(t, err)
	rec, err := runJournal(t.Context(), raw)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(root, ".journal"))
	require.NoError(t, err)
	lineRe := regexp.MustCompile(
		`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z\|test\|main\.go\|7\|ok\n$`)
	assert.Regexp(t, lineRe, string(data), "on-disk line must match the documented pipe format")
	assert.Regexp(t, `^recorded test @ \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`, rec.Detail)

	want := naiveRender(naiveScanJournal(string(data), 10))
	assert.Equal(t, want, rec.Content, "record echo must match an independent parse of the written bytes")

	raw, err = json.Marshal(journalInput{Path: root, Limit: 10})
	require.NoError(t, err)
	out, err := runJournal(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, want, out.Content)
	assert.Equal(t, "1 journal entries", out.Detail)
}
