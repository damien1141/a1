package diffreview

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGitDiff(t *testing.T) {
	input := `diff --git a/main.go b/main.go
index 1111111..2222222 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
-old
+new
+added
`

	doc, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, doc.Files, 1)
	file := doc.Files[0]
	assert.Equal(t, "a/main.go", file.OldName)
	assert.Equal(t, "b/main.go", file.NewName)
	require.Len(t, file.Hunks, 1)
	require.Len(t, file.Hunks[0].Lines, 4)
	assert.Equal(t, Delete, file.Hunks[0].Lines[1].Kind)
	assert.Equal(t, Add, file.Hunks[0].Lines[2].Kind)
}

func TestParseGitShowPreamble(t *testing.T) {
	input := `commit abc123
Author: Example <example@example.com>

diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1 +1 @@
-a
+b
`

	doc, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, doc.Preamble, 3)
	assert.Equal(t, "show", doc.Metadata.SourceKind)
	assert.Equal(t, "abc123", doc.Metadata.CommitID)
	assert.NotEmpty(t, doc.Rows())
}

func TestParseMultipleGitShowCommits(t *testing.T) {
	input := `commit abc123
Author: Example <example@example.com>

diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1 +1 @@
-a
+b
commit def456
Author: Other <other@example.com>

diff --git a/b.txt b/b.txt
--- b/b.txt
+++ b/b.txt
@@ -1 +1 @@
-c
+d
`

	doc, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, doc.Files, 2)
	require.Len(t, doc.Preamble, 3)
	require.Len(t, doc.Files[1].Preamble, 3)
	assert.Equal(t, "commit def456", doc.Files[1].Preamble[0])
	assert.Equal(t, "abc123", doc.Files[0].Metadata.CommitID)
	assert.Equal(t, "def456", doc.Files[1].Metadata.CommitID)
}

func TestParseDoesNotTreatBlankCommitSeparatorAsHunkLine(t *testing.T) {
	input := strings.Join([]string{
		"commit abc123",
		"Author: Example <example@example.com>",
		"",
		"diff --git a/a.ts b/a.ts",
		"--- a/a.ts",
		"+++ b/a.ts",
		"@@ -1 +1 @@",
		" ",
		"",
		"commit def456",
		"Author: Other <other@example.com>",
		"",
		"diff --git a/b.ts b/b.ts",
		"--- a/b.ts",
		"+++ b/b.ts",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		"",
	}, "\n")

	doc, err := Parse(input)
	require.NoError(t, err)
	require.Len(t, doc.Files, 2)
	require.Len(t, doc.Files[0].Hunks[0].Lines, 1)
	line := doc.Files[0].Hunks[0].Lines[0]
	assert.Equal(t, Context, line.Kind)
	assert.Equal(t, 1, line.OldLine)
	assert.Equal(t, " ", line.Text)
	assert.Equal(t, "commit def456", doc.Files[1].Preamble[0])
}

func TestRowsWithOptionsAddsReviewAnchors(t *testing.T) {
	doc, err := Parse(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -10,3 +20,3 @@
 context
-old
+new
`)
	require.NoError(t, err)

	rows := doc.RowsWithOptions(RenderOptions{
		ShowHunkHeaders: true,
		ShowContext:     true,
		ShowLineNumbers: true,
	})

	var contextRow, deleteRow, addRow Row
	for _, row := range rows {
		switch row.Kind {
		case RowContext:
			contextRow = row
		case RowDelete:
			deleteRow = row
		case RowAdd:
			addRow = row
		}
	}

	assert.Equal(t, Anchor{Path: "main.go", Line: 20, Side: SideRight}, contextRow.Review)
	assert.Equal(t, Anchor{Path: "main.go", Line: 11, Side: SideLeft}, deleteRow.Review)
	assert.Equal(t, Anchor{Path: "main.go", Line: 21, Side: SideRight}, addRow.Review)
}

func TestRowsWithOptionsAddsInlineSpansForReplacementLines(t *testing.T) {
	doc, err := Parse(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1 +1 @@
-foo := oldValue + 1
+foo := newValue + 1
`)
	require.NoError(t, err)

	var deleteRow, addRow Row
	for _, row := range doc.Rows() {
		switch row.Kind {
		case RowDelete:
			deleteRow = row
		case RowAdd:
			addRow = row
		}
	}
	require.Len(t, deleteRow.InlineSpans, 1)
	assert.Equal(t, 7, deleteRow.InlineSpans[0].Start)
	assert.Equal(t, 15, deleteRow.InlineSpans[0].End)
	require.Len(t, addRow.InlineSpans, 1)
	assert.Equal(t, 7, addRow.InlineSpans[0].Start)
	assert.Equal(t, 15, addRow.InlineSpans[0].End)
}

func TestRowsWithOptionsDoesNotPairUnrelatedReplacementLines(t *testing.T) {
	doc, err := Parse(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1 +1 @@
-func oldThing() {
+type User struct {
`)
	require.NoError(t, err)
	for _, row := range doc.Rows() {
		assert.Empty(t, row.InlineSpans, row.Text)
	}
}

func TestSideBySideRowsPairsDeleteAdd(t *testing.T) {
	doc, err := Parse(`diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1,2 +1,2 @@
-old
+new
 context
`)
	require.NoError(t, err)
	rows := doc.Rows()
	side := SideBySideRows(rows)
	var paired SideBySideRow
	for _, r := range side {
		if r.Left >= 0 && r.Right >= 0 {
			paired = r
			break
		}
	}
	assert.Equal(t, RowDelete, rows[paired.Left].Kind)
	assert.Equal(t, RowAdd, rows[paired.Right].Kind)
}
