package diffreview

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".a1", "review.json")
	file := CommentFile{
		Version: 1,
		Comments: []CommentDraft{{
			Path: "tui/app.go",
			Body: "comment",
			Line: 10,
			Side: SideRight,
		}},
	}

	require.NoError(t, SaveFile(path, file))
	got, err := LoadFile(path)
	require.NoError(t, err)
	require.Len(t, got.Comments, 1)
	assert.Equal(t, 1, got.Version)
	assert.Equal(t, "comment", got.Comments[0].Body)
	assert.Equal(t, 10, got.Comments[0].Line)
}

func TestLoadFileMissingReturnsEmptyFile(t *testing.T) {
	file, err := LoadFile(filepath.Join(t.TempDir(), ".a1", "review.json"))
	require.NoError(t, err)
	assert.Equal(t, 1, file.Version)
	assert.Empty(t, file.Comments)
}

func TestBuildCommentIndexResolvesSingleLineComment(t *testing.T) {
	rows := []Row{
		{Kind: RowAdd, Code: "one", Review: Anchor{Path: "main.go", Line: 1, Side: SideRight}},
		{Kind: RowAdd, Code: "two", Review: Anchor{Path: "main.go", Line: 2, Side: SideRight}},
	}
	drafts := []CommentDraft{{Path: "main.go", Line: 2, Side: SideRight, Body: "looks good"}}

	idx := BuildCommentIndex(rows, drafts)
	got := idx.DraftsForRow(1)
	require.Len(t, got, 1)
	assert.Equal(t, "looks good", got[0].Body)
	assert.Equal(t, []int{1}, idx.TargetRows())
}

func TestBuildCommentIndexResolvesLeftSideComment(t *testing.T) {
	rows := []Row{
		{Kind: RowDelete, Code: "old", Review: Anchor{Path: "main.go", Line: 7, Side: SideLeft}},
		{Kind: RowAdd, Code: "new", Review: Anchor{Path: "main.go", Line: 8, Side: SideRight}},
	}
	drafts := []CommentDraft{{Path: "main.go", Line: 7, Side: SideLeft, Body: "left note"}}

	idx := BuildCommentIndex(rows, drafts)
	got := idx.DraftsForRow(0)
	require.Len(t, got, 1)
	assert.Equal(t, SideLeft, got[0].Side)
}

func TestBuildCommentIndexSkipsUnmatchedComments(t *testing.T) {
	rows := []Row{{Kind: RowAdd, Code: "one", Review: Anchor{Path: "main.go", Line: 1, Side: SideRight}}}
	drafts := []CommentDraft{
		{Path: "other.go", Line: 1, Side: SideRight, Body: "wrong path"},
		{Path: "main.go", Line: 2, Side: SideRight, Body: "wrong line"},
	}
	idx := BuildCommentIndex(rows, drafts)
	assert.Empty(t, idx.TargetRows())
}

func TestFormatPrompt(t *testing.T) {
	out := FormatPrompt([]CommentDraft{
		{Path: "a.go", Line: 4, Side: SideRight, Body: "rename this"},
		{Path: "b.go", Line: 1, Body: "   "},
	})
	assert.Contains(t, out, "a.go:4 (RIGHT)")
	assert.Contains(t, out, "rename this")
	assert.NotContains(t, out, "b.go")
}

func TestGitArgv(t *testing.T) {
	assert.Equal(t, []string{"git", "-c", "color.ui=never", "diff"}, GitArgv(nil))
	assert.Equal(t, []string{"git", "-c", "color.ui=never", "diff", "--staged"}, GitArgv([]string{"staged"}))
	assert.Equal(
		t,
		[]string{"git", "-c", "color.ui=never", "show", "--pretty=medium", "-p", "HEAD"},
		GitArgv([]string{"HEAD"}),
	)
	assert.Equal(t, []string{"git", "-c", "color.ui=never", "diff", "main"}, GitArgv([]string{"main"}))
	assert.Equal(t, []string{"git", "show", "HEAD~1"}, GitArgv([]string{"--", "git", "show", "HEAD~1"}))
}

func TestLabelForSpec(t *testing.T) {
	assert.Equal(t, "working tree", LabelForSpec(nil))
	assert.Equal(t, "staged", LabelForSpec([]string{"staged"}))
	assert.Equal(t, "HEAD", LabelForSpec([]string{"HEAD"}))
}

func TestEmptyNote(t *testing.T) {
	assert.Contains(t, EmptyNote(nil), "No unstaged changes")
	assert.Contains(t, EmptyNote([]string{"staged"}), "No staged changes")
	assert.Contains(t, EmptyNote([]string{"HEAD"}), "HEAD has no changes")
	// A plain revision is compared against the working tree, so the note must
	// name it — otherwise an empty overlay looks like a broken /diff.
	assert.Contains(t, EmptyNote([]string{"HEAD~2"}), "No changes vs HEAD~2")
	assert.Contains(t, EmptyNote([]string{"HEAD~2"}), "Untracked files are not shown")
}

func TestCommentPath(t *testing.T) {
	assert.Equal(t, DefaultFilePath, CommentPath(""))
	assert.Equal(t, filepath.Join("/tmp/proj", DefaultFilePath), CommentPath("/tmp/proj"))
}
