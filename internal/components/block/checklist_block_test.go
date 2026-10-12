package block_test

import (
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/block"
	"github.com/damien1141/a1/internal/todo"
)

func checklistCard(t *testing.T, c *block.ChecklistBlock) string {
	t.Helper()
	return components.SurfaceText(c.Draw(components.DrawContext{
		Max: components.Size{Width: 74, Height: 40},
	}))
}

func checklistItems() []todo.Item {
	return []todo.Item{
		{Text: "Read the current writer", Done: true},
		{Text: "handles escaped quotes", Indent: 2, Detail: true},
		{Text: "Patch the parser", InProgress: true},
		{Text: "Update the tests"},
		{Text: "Update the docs"},
	}
}

func TestChecklistBlockHeaderCounts(t *testing.T) {
	c := &block.ChecklistBlock{Path: "TODO.md", Items: checklistItems(), Theme: components.DefaultTheme()}
	got := checklistCard(t, c)
	assert.Contains(t, got, "☑ TODO.md")
	assert.Contains(t, got, "1 done")
	assert.Contains(t, got, "2 open", "the in-progress task is neither done nor open")
	assert.NotContains(t, got, "1 open", "details are not counted")
}

func TestChecklistBlockDetailsDoNotCount(t *testing.T) {
	c := &block.ChecklistBlock{
		Path: "TODO.md",
		Items: []todo.Item{
			{Text: "a", Done: true},
			{Text: "note one", Indent: 2, Detail: true},
			{Text: "note two", Indent: 2, Detail: true},
		},
		Theme: components.DefaultTheme(),
	}
	got := checklistCard(t, c)
	assert.Contains(t, got, "1 done")
	assert.NotContains(t, got, "open", "two details did not become two open tasks")
}

func TestChecklistBlockCollapsedShowsOnlyHeader(t *testing.T) {
	c := &block.ChecklistBlock{Path: "TODO.md", Items: checklistItems(), Theme: components.DefaultTheme()}
	got := checklistCard(t, c)
	assert.Contains(t, got, "☑ TODO.md")
	assert.NotContains(t, got, "Patch the parser", "collapsed hides the items")
}

func TestChecklistBlockExpandedListsItems(t *testing.T) {
	c := &block.ChecklistBlock{
		Path: "TODO.md", Items: checklistItems(), Expanded: true, Theme: components.DefaultTheme(),
	}
	got := checklistCard(t, c)
	assert.Contains(t, got, "✓ Read the current writer")
	assert.Contains(t, got, "▸ Patch the parser", "the in-flight item gets its own box")
	assert.Contains(t, got, "◦ Update the tests")
	assert.Contains(t, got, "handles escaped quotes", "details render under their parent")
}

func TestChecklistBlockInProgressIsListedFirst(t *testing.T) {
	c := &block.ChecklistBlock{
		Path: "TODO.md", Items: checklistItems(), Expanded: true, Theme: components.DefaultTheme(),
	}
	got := checklistCard(t, c)
	lines := splitLines(got)
	require.GreaterOrEqual(t, len(lines), 2)
	// lines[0] is the header; the first item row is lines[1]. The reference's
	// ordering puts work still ahead of you above completed history.
	assert.Contains(t, lines[0], "☑ TODO.md")
	assert.Contains(t, lines[1], "Patch the parser", "in-progress sorts to the top")
	assert.Contains(t, lines[2], "Update the tests", "then open tasks")
}

func TestChecklistBlockRowCapAndHiddenCount(t *testing.T) {
	items := make([]todo.Item, 0, 12)
	for i := range 12 {
		items = append(items, todo.Item{Text: "task", Done: i%3 == 0})
	}
	c := &block.ChecklistBlock{
		Path: "TODO.md", Items: items, Expanded: true, Theme: components.DefaultTheme(),
	}
	got := checklistCard(t, c)
	assert.Contains(t, got, "… +2 more", "the capped remainder is reported, not silently dropped")
	assert.Equal(t, 10, countChecklistRows(got), "at most MaxRows items render")
}

func TestChecklistBlockMaxRowsRespected(t *testing.T) {
	c := &block.ChecklistBlock{
		Path: "TODO.md", Items: checklistItems(), Expanded: true, MaxRows: 2,
		Theme: components.DefaultTheme(),
	}
	got := checklistCard(t, c)
	assert.Equal(t, 2, countChecklistRows(got), "MaxRows items render")
	assert.Contains(t, got, "… +2 more", "the remainder counts tasks, not rows")
}

func TestChecklistBlockStalenessWarning(t *testing.T) {
	c := &block.ChecklistBlock{
		Path: "TODO.md", Items: checklistItems(), Expanded: true, StaleFor: 7,
		Theme: components.DefaultTheme(),
	}
	assert.Contains(t, checklistCard(t, c), "⚠ 7 file changes since this list moved")
}

func TestChecklistBlockCopyText(t *testing.T) {
	c := &block.ChecklistBlock{Path: "TODO.md", Items: checklistItems()}
	got := c.CopyText()
	assert.Contains(t, got, "TODO.md (1 done, 2 open)")
	assert.Contains(t, got, "- [x] Read the current writer")
	assert.Contains(t, got, "- [~] Patch the parser")
	assert.Contains(t, got, "- [ ] Update the tests")
	assert.Contains(t, got, "  - handles escaped quotes", "indent survives into copy text")
}

func TestChecklistBlockToggle(t *testing.T) {
	c := &block.ChecklistBlock{Path: "TODO.md", Items: checklistItems(), Theme: components.DefaultTheme()}
	assert.False(t, c.Expanded)
	c.Handle(&components.EventContext{}, xui.KeyEvent{Press: true, Code: xui.KeyEnter})
	assert.True(t, c.Expanded)
	c.Handle(&components.EventContext{}, xui.KeyEvent{Press: true, Code: xui.KeyEnter})
	assert.False(t, c.Expanded)
}

func TestChecklistBlockEmptyHasNoBody(t *testing.T) {
	c := &block.ChecklistBlock{Path: "TODO.md", Theme: components.DefaultTheme()}
	assert.False(t, c.HasBody(), "an uncreated file is not a row")
	assert.Equal(t, "TODO.md (0 done, 0 open)\n", c.CopyText())
}

func splitLines(s string) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.TrimSpace(ln) != "" {
			out = append(out, ln)
		}
	}
	return out
}

// countChecklistRows counts item rows: a box glyph, or the "+N more" trailer.
func countChecklistRows(s string) int {
	n := 0
	for _, ln := range splitLines(s) {
		if strings.Contains(ln, "☑") {
			continue
		}
		if strings.Contains(ln, "✓ ") || strings.Contains(ln, "◦ ") || strings.Contains(ln, "▸ ") {
			n++
		}
	}
	return n
}
