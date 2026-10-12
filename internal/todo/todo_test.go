package todo_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/todo"
)

const sample = `# Migration

Some prose that is not an item.

## Steps
- [x] Read the current writer
  - Handles escaped quotes
  - [x] Still fails on a missing trailing newline
- [~] Patch the parser
- [ ] Update the tests
- [ ] Update the docs

` + "```go\n" + `// - [x] not an item, inside a code fence
` + "```" + `

1. [ ] not an item: ordered list, not a checkbox
- [X] uppercase tick
`

func TestParseTextCounts(t *testing.T) {
	items := todo.ParseText(sample)
	require.Len(t, items, 7,
		"6 boxed items plus 1 indented detail; the code-fence and ordered lines do not count")
	assert.True(t, items[0].Done)
	assert.Equal(t, 0, items[0].Indent)
	assert.True(t, items[1].Detail, "an indented unboxed bullet is a detail line")
	assert.Equal(t, "Handles escaped quotes", items[1].Text)
	assert.True(t, items[2].Done, "the nested tick is its own item")
	assert.Equal(t, 2, items[2].Indent)
	assert.True(t, items[3].InProgress)
	assert.Equal(t, "Patch the parser", items[3].Text)
	assert.True(t, items[6].Done, "an uppercase X is a tick")
	assert.Equal(t, "uppercase tick", items[6].Text)
}

func TestFileCountsAndInProgress(t *testing.T) {
	f := todo.File{Items: todo.ParseText(sample)}
	assert.Equal(t, 6, f.Tasks(), "details are not tasks")
	assert.Equal(t, 3, f.Done())
	assert.Equal(t, 2, f.Open(), "the in-progress task is neither done nor open")
	got := f.InProgress()
	require.NotNil(t, got)
	assert.Equal(t, "Patch the parser", got.Text)
}

func TestParseMissingFileIsNotAnError(t *testing.T) {
	f, err := todo.Parse(filepath.Join(t.TempDir(), "absent.md"))
	assert.Error(t, err, "the error is reported so the caller can distinguish absent from empty")
	assert.Empty(t, f.Items)
}

func TestParseRoundTripModTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TODO.md")
	require.NoError(t, os.WriteFile(path, []byte(sample), 0o644))
	f, err := todo.Parse(path)
	require.NoError(t, err)
	assert.NotZero(t, f.ModTime, "mtime is threaded through so the renderer can skip re-reads")
	assert.Equal(t, int64(len(sample)), f.Size)
	assert.Len(t, f.Items, 7)
}

func TestParseRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := todo.Parse(dir)
	assert.Error(t, err)
}

func TestParseEmptyBoxNeedsText(t *testing.T) {
	assert.Empty(t, todo.ParseText("- [ ]\n"))
	assert.Empty(t, todo.ParseText("-\n"))
}

func TestParseTabIndent(t *testing.T) {
	items := todo.ParseText("- [x] a\n\t- [ ] b\n")
	require.Len(t, items, 2)
	assert.Equal(t, 4, items[1].Indent, "a tab counts as four columns")
	assert.False(t, items[1].Done)
}

func TestParseCodeFenceIgnoresBoxes(t *testing.T) {
	// Agents paste "- [ ]" samples inside examples; counting them inflates the
	// total and the row lies about progress.
	text := "- [x] real\n```\n- [ ] not a task\n```\n- [ ] also real\n"
	items := todo.ParseText(text)
	require.Len(t, items, 2, "the fenced sample is skipped")
	assert.Equal(t, "real", items[0].Text)
	assert.Equal(t, "also real", items[1].Text)
}

func TestParseTildeFenceNeedsTildeClose(t *testing.T) {
	text := "~~~\n- [ ] inside\n```\n- [ ] still inside\n~~~\n- [x] outside\n"
	items := todo.ParseText(text)
	require.Len(t, items, 1, "a ``` line does not close a ~~~ block")
	assert.Equal(t, "outside", items[0].Text)
}

func TestParseIgnoresProseAndHeadings(t *testing.T) {
	assert.Empty(t, todo.ParseText("# Title\n\nsome words\n> a quote\n| table |\n"))
}
