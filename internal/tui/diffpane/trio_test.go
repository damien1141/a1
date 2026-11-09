package diffpane

import (
	"os/exec"
	"regexp"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/util/diffreview"
)

const twoFileDiff = `diff --git a/one.txt b/one.txt
--- a/one.txt
+++ b/one.txt
@@ -1,2 +1,2 @@
 ctx
-old1
+new1
diff --git a/two.txt b/two.txt
--- a/two.txt
+++ b/two.txt
@@ -1,1 +1,2 @@
 ctx2
+new2
`

func pressRune(p *Pane, r rune) {
	p.Handle(&components.EventContext{}, xui.KeyEvent{Press: true, Code: xui.KeyRune, Rune: r})
}

func pressCode(p *Pane, code xui.KeyCode) {
	p.Handle(&components.EventContext{}, xui.KeyEvent{Press: true, Code: code})
}

// A rowless document (empty diff) must survive every navigation and action
// key: the cursor clamps to 0, copies and note actions explain themselves, and
// search with no hits reports it instead of moving anywhere.
func TestPane_EmptyDiffDegenerateInput_Negative(t *testing.T) {
	p := New(components.DefaultTheme(), t.TempDir(), nil, nil, nil)
	var toasts []string
	p.onToast = func(msg string) { toasts = append(toasts, msg) }
	copied := false
	p.onCopy = func(string) bool { copied = true; return true }

	p.OpenText("", nil)
	require.True(t, p.Active())
	require.Empty(t, p.rows)
	assert.Equal(t, "empty diff", p.status)

	for _, r := range []rune{'j', 'k', 'h', 'G', 'J', 'K'} {
		pressRune(p, r)
	}
	for _, code := range []xui.KeyCode{xui.KeyEnd, xui.KeyHome, xui.KeyPageDown, xui.KeyPageUp} {
		pressCode(p, code)
	}
	assert.Equal(t, 0, p.cursor, "every move clamps to row 0 on an empty document")
	assert.Equal(t, 0, p.xScroll, "h cannot scroll left past the origin")
	pressRune(p, 'l')
	pressRune(p, 'h')
	assert.Equal(t, 0, p.xScroll, "one h undoes one l at the origin")

	// Search with a query that matches nothing in an empty document.
	pressRune(p, '/')
	require.True(t, p.searchMode)
	pressCode(p, xui.KeyBackspace) // empty query: backspace is a no-op, not a panic
	for _, r := range "zzz" {
		pressRune(p, r)
	}
	pressCode(p, xui.KeyEnter)
	assert.False(t, p.searchMode)
	pressRune(p, 'n')
	assert.Contains(t, toasts, "no matches")

	pressRune(p, 'x')
	assert.Contains(t, toasts, "no note on this line")

	pressRune(p, 'y')
	assert.False(t, copied, "copy on a rowless document is a no-op")

	// Pinned quirk: FormatPrompt emits its instruction header even with zero
	// drafts, so the "no notes to send" guard never fires — 'a' closes the pane
	// and submits the bare header. Reported as a production concern.
	var submitted string
	p.onSubmit = func(text string) { submitted = text }
	pressRune(p, 'a')
	assert.False(t, p.Active(), "'a' with no notes still closes the pane (current behavior)")
	assert.Equal(t, "Address these review comments on the current diff. Cite paths and lines.", submitted)
	assert.NotContains(t, toasts, "no notes to send", "the empty-notes toast is unreachable today")
}

// Out-of-range cursors and a git failure: the clamp keeps the cursor inside the
// document, and a non-repo cwd surfaces git's own error in the status slot.
func TestPane_OutOfRangeCursorAndNonRepo_Negative(t *testing.T) {
	p := New(components.DefaultTheme(), t.TempDir(), nil, nil, nil)
	copied := false
	p.onCopy = func(string) bool { copied = true; return true }
	p.OpenText(sampleDiff, nil)
	require.NotEmpty(t, p.rows)

	p.cursor = len(p.rows) + 50
	pressRune(p, 'y')
	assert.False(t, copied, "an out-of-range cursor copies nothing")

	p.setCursor(-9)
	assert.Equal(t, 0, p.cursor)
	p.setCursor(1 << 20)
	assert.Equal(t, len(p.rows)-1, p.cursor)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	empty := t.TempDir()
	p.OpenGit(empty, nil)
	require.True(t, p.Active())
	require.Empty(t, p.rows)
	assert.Contains(t, p.err, "Not a git repository")
	assert.Equal(t, p.err, p.status, "the load error is what the status row shows")
}

// oracle: case-insensitive regexp over the same row fields search concatenates
// (code, rendered text, file name); the pane's ToLower+Contains scan must pick
// exactly the rows the regexp engine does.
func TestUpdateSearch_RegexpOracle_Differential(t *testing.T) {
	p := New(components.DefaultTheme(), t.TempDir(), nil, nil, nil)
	p.OpenText(twoFileDiff, nil)
	require.NotEmpty(t, p.rows)

	oracleMatches := func(q string) []int {
		if q == "" {
			return nil
		}
		re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(q))
		var want []int
		for i, row := range p.rows {
			if re.MatchString(row.Code + " " + row.Text + " " + row.FileName) {
				want = append(want, i)
			}
		}
		return want
	}

	for _, q := range []string{"NEW", "new1", "old", "ctx", "zzz", "TWO.TXT", "@@", "2", "+"} {
		pressRune(p, '/')
		require.True(t, p.searchMode)
		for _, r := range q {
			pressRune(p, r)
		}
		assert.Equal(t, oracleMatches(q), p.searchMatches, "query %q", q)
		pressCode(p, xui.KeyEnter)
		assert.False(t, p.searchMode)
		if want := oracleMatches(q); len(want) > 0 {
			assert.Equal(t, want[0], p.cursor, "the first oracle match owns the cursor for %q", q)
		}
	}

	// n/N cycle forward and backward through the oracle's match list, wrapping.
	pressRune(p, '/')
	for _, r := range "NEW" {
		pressRune(p, r)
	}
	pressCode(p, xui.KeyEnter)
	want := oracleMatches("NEW")
	require.GreaterOrEqual(t, len(want), 2)
	pressRune(p, 'n')
	assert.Equal(t, want[1], p.cursor)
	pressRune(p, 'n')
	assert.Equal(t, want[0], p.cursor, "n wraps back to the first match")
	pressRune(p, 'N')
	assert.Equal(t, want[len(want)-1], p.cursor, "N wraps back to the last match")
}

// oracle: hand-derived row indices for the fixed two-file diff above —
// rows are [file, hunk, ctx, -old1, +new1, blank, file, hunk, ctx2, +new2],
// so file jumps land on 0/6, change jumps on the delete 3 and the add 9.
func TestJumpKeys_GoldenRowIndices_Differential(t *testing.T) {
	p := New(components.DefaultTheme(), t.TempDir(), nil, nil, nil)
	p.OpenText(twoFileDiff, nil)
	require.Len(t, p.rows, 10)
	assert.Equal(t, "2 files", p.status)

	require.Equal(t, diffreview.RowFile, p.rows[0].Kind)
	require.Equal(t, diffreview.RowFile, p.rows[6].Kind)
	require.Equal(t, diffreview.RowDelete, p.rows[3].Kind)
	require.Equal(t, diffreview.RowAdd, p.rows[9].Kind)

	assert.Equal(t, 0, p.cursor)
	pressRune(p, 'J')
	assert.Equal(t, 6, p.cursor, "J jumps to the next file header")
	pressRune(p, 'K')
	assert.Equal(t, 0, p.cursor, "K jumps back to the previous file header")
	pressRune(p, 'K')
	assert.Equal(t, 6, p.cursor, "K at the top wraps to the last file")
	pressRune(p, 'J')
	assert.Equal(t, 0, p.cursor, "J on the last file wraps onto the first")

	pressRune(p, ']')
	pressRune(p, 'c')
	assert.Equal(t, 3, p.cursor, "]c lands on the first change start (the delete)")
	pressRune(p, ']')
	pressRune(p, 'c')
	assert.Equal(t, 9, p.cursor, "]c skips the paired add and lands on the next change")
	pressRune(p, '[')
	pressRune(p, 'c')
	assert.Equal(t, 3, p.cursor, "[c wraps to the previous change start")
	pressRune(p, '[')
	pressRune(p, 'c')
	assert.Equal(t, 9, p.cursor, "[c from the first change wraps to the last")

	pressRune(p, 'G')
	assert.Equal(t, 9, p.cursor)
	pressRune(p, 'g')
	pressRune(p, 'g')
	assert.Equal(t, 0, p.cursor)
}
