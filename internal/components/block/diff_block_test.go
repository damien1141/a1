package block_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/block"
	"github.com/damien1141/a1/internal/components/status"
)

const sampleDiff = `--- a/internal/foo.go
+++ b/internal/foo.go
@@ -1,5 +1,6 @@
 package foo
+
+// Added documents the func.
 func Bar() {}
@@ -20,3 +21,4 @@
 func Baz() {}
+    return 1
-    return 0
`

func draw(t *testing.T, w *block.DiffBlock, width int) string {
	t.Helper()
	s := w.Draw(components.DrawContext{Max: components.Size{Width: width, Height: 40}})
	return components.SurfaceText(s)
}

func TestDiffBlockStatCountsChanges(t *testing.T) {
	d := &block.DiffBlock{Path: "internal/foo.go", Diff: sampleDiff}
	added, removed := d.Stat()
	assert.Equal(t, 3, added, "three + lines")
	assert.Equal(t, 1, removed, "one - line")
}

func TestDiffBlockStatIgnoresHeaders(t *testing.T) {
	// A tool that emits a brand-new file ("Created new file.") has no diff
	// body, so the stat must be 0/0 rather than counting the header lines.
	d := &block.DiffBlock{Path: "new.go", Diff: "@file new.go#ABCD\n\nCreated new file."}
	added, removed := d.Stat()
	assert.Equal(t, 0, added)
	assert.Equal(t, 0, removed)
}

func TestDiffBlockCopyTextIncludesStatAndBody(t *testing.T) {
	d := &block.DiffBlock{Path: "a.go", Diff: sampleDiff}
	text := d.CopyText()
	assert.Contains(t, text, "a.go")
	assert.Contains(t, text, "+3 -1")
	assert.Contains(t, text, "+// Added documents the func.")
}

func TestDiffBlockDrawsHeaderAndBody(t *testing.T) {
	d := &block.DiffBlock{
		Path:     "internal/foo.go",
		Diff:     sampleDiff,
		Status:   status.ToolDone,
		Expanded: true,
		Theme:    components.DefaultTheme(),
	}
	got := draw(t, d, 60)
	assert.Contains(t, got, "internal/foo.go")
	assert.Contains(t, got, "+3 -1", "the header shows the change count")
	// SurfaceText trims trailing whitespace, so assert on the meaningful
	// content of each line rather than exact padding.
	assert.Contains(t, got, "+ // Added documents the func.", "an added line is rendered")
	assert.Contains(t, got, "-     return 0", "a removed line is rendered")
	assert.Contains(t, got, "21    func Baz() {}", "a context line keeps its line number")
	// File headers are dropped: the path is already in the card header.
	assert.NotContains(t, got, "--- a/")
	assert.NotContains(t, got, "+++ b/")
	// A separator rules the boundary between hunks.
	assert.Contains(t, got, "·")
}

func TestDiffBlockCollapsedHidesBody(t *testing.T) {
	d := &block.DiffBlock{Path: "internal/foo.go", Diff: sampleDiff, Theme: components.DefaultTheme()}
	got := draw(t, d, 60)
	assert.NotContains(t, got, "+// Added documents the func.", "collapsed hides the body")
	assert.Contains(t, got, "internal/foo.go", "the header stays visible")
}

func TestDiffBlockFailedEditShowsError(t *testing.T) {
	d := &block.DiffBlock{
		Path:   "internal/foo.go",
		Error:  "old_str not found in internal/foo.go",
		Status: status.ToolError,
		Theme:  components.DefaultTheme(),
	}
	require.True(t, d.HasBody(), "a failed edit has a body: the error")
	got := draw(t, d, 60)
	assert.Contains(t, got, "Error:")
	assert.Contains(t, got, "old_str not found")
}

func TestDiffBlockNewFileShowsNoStat(t *testing.T) {
	d := &block.DiffBlock{
		Path:     "brand/new.go",
		Diff:     "@file brand/new.go#ABCD\n\nCreated new file.",
		Status:   status.ToolDone,
		Expanded: true,
		Theme:    components.DefaultTheme(),
	}
	got := draw(t, d, 60)
	assert.Contains(t, got, "brand/new.go")
	assert.Contains(t, got, "Created new file.", "the creation note is still readable")
	assert.NotContains(t, got, "+0 -0", "no bogus stat on a created file")
}

func TestToolBlockResultStat(t *testing.T) {
	cases := []struct {
		name   string
		status status.ToolStatus
		output string
		want   string
	}{
		{"running shows nothing", status.ToolRunning, "partial", ""},
		{"error shows nothing", status.ToolError, "boom", ""},
		{"empty output", status.ToolDone, "", ""},
		{"whitespace only", status.ToolDone, "\n\n  \n", ""},
		{"one line", status.ToolDone, "a.go", "(1 line)"},
		{"many lines", status.ToolDone, "a\nb\n\nc", "(3 lines)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &block.ToolBlock{
				Name:     "grep",
				Detail:   "Paths",
				Output:   tc.output,
				Status:   tc.status,
				Expanded: true,
				Theme:    components.DefaultTheme(),
			}
			got := components.SurfaceText(b.Draw(components.DrawContext{Max: components.Size{Width: 60, Height: 40}}))
			assert.Contains(t, got, "grep")
			if tc.want == "" {
				assert.NotContains(t, got, "(1 line)")
				assert.NotContains(t, got, "lines)")
			} else {
				assert.Contains(t, got, tc.want, "the header reports the result volume")
			}
		})
	}
}

// A body with no @@ header (a brand-new file) must still number from 1: without
// that the first row has no line number and every later row is off by one.
func TestDiffBlockNumbersNewFileFromOne(t *testing.T) {
	d := &block.DiffBlock{
		Path:     "brand/new.go",
		Diff:     "Created brand/new.go (3 lines).\n\n+package main\n+\n+func main() {}",
		Status:   status.ToolDone,
		Expanded: true,
		Theme:    components.DefaultTheme(),
	}
	got := draw(t, d, 60)
	assert.Contains(t, got, "Created brand/new.go (3 lines).", "the summary line is shown")
	assert.NotContains(t, got, "1  +package main", "no phantom unnumbered first row")
	assert.Contains(t, got, "1  + package main", "the first added line is numbered 1")
	assert.Contains(t, got, "3  + func main() {}", "numbering continues to the last line")
}

// The tool's summary prose must not be swallowed as a diff context line, and
// the blank line separating it from the body must not draw a row at all.
func TestDiffBlockSkipsSummaryAndBlankLines(t *testing.T) {
	d := &block.DiffBlock{
		Path:     "a.go",
		Diff:     "Updated a.go.\n\n@@ -1,1 +1,2 @@\n keep\n+added",
		Status:   status.ToolDone,
		Expanded: true,
		Theme:    components.DefaultTheme(),
	}
	got := draw(t, d, 60)
	assert.Contains(t, got, "Updated a.go.")
	// No separator before the first hunk: there is nothing above it to separate.
	assert.NotContains(t, got, "·", "no leading hunk separator")
	assert.Contains(t, got, "+ added")
}

// A second hunk does get a separator, so the two change regions read apart.
func TestDiffBlockSeparatesHunks(t *testing.T) {
	d := &block.DiffBlock{
		Path: "a.go",
		Diff: "@@ -1,2 +1,2 @@\n one\n-two\n+TWO\n" +
			"@@ -40,2 +40,3 @@\n forty\n+forty one",
		Status:   status.ToolDone,
		Expanded: true,
		Theme:    components.DefaultTheme(),
	}
	got := draw(t, d, 60)
	assert.Contains(t, got, "·", "a separator sits between hunks")
}
