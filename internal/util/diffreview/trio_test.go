package diffreview

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// gitOut runs git in dir and returns its combined output, hermetic like
// runGit in git_test.go: inline config neutralizes signing and identity so
// the developer's global config cannot break the run.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	cmd := exec.CommandContext(t.Context(), "git",
		append([]string{
			"-c", "commit.gpgsign=false",
			"-c", "user.name=t", "-c", "user.email=t@t",
			"-c", "color.ui=never",
		}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t",
		"GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Operation not permitted") {
			t.Skipf("sandbox blocked git: %s", out)
		}
		require.NoError(t, err, "%v: %s", args, out)
	}
	return string(out)
}

// fileLines splits file content into lines the way the parser numbers them:
// a trailing newline does not open a phantom line.
func fileLines(s string) []string {
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// checkFileAgainstContents verifies parsed line numbers against the real
// old/new file contents: a context line must appear at both numbered
// positions, a delete at its old position, an add at its new position, and
// the hunk header counts must equal the old/new side line counts in the body.
func checkFileAgainstContents(t *testing.T, f File, oldText, newText string) {
	t.Helper()
	oldLines := fileLines(oldText)
	newLines := fileLines(newText)
	require.NotEmpty(t, f.Hunks, "fixture must produce at least one hunk for %s", f.NewName)
	for _, h := range f.Hunks {
		var oldSeen, newSeen int
		for _, ln := range h.Lines {
			switch ln.Kind {
			case Context:
				require.Equal(t, oldLines[ln.OldLine-1], strings.TrimPrefix(ln.Text, " "),
					"context line must match the old file at its numbered position")
				require.Equal(t, newLines[ln.NewLine-1], strings.TrimPrefix(ln.Text, " "),
					"context line must match the new file at its numbered position")
				oldSeen++
				newSeen++
			case Delete:
				require.Equal(t, oldLines[ln.OldLine-1], ln.Text[1:],
					"deleted line must match the old file at its numbered position")
				oldSeen++
			case Add:
				require.Equal(t, newLines[ln.NewLine-1], ln.Text[1:],
					"added line must match the new file at its numbered position")
				newSeen++
			case NoNewline:
				// A marker, not a file line; nothing to number.
			}
		}
		assert.Equal(t, h.OldCount, oldSeen, "hunk header old count vs old-side body lines")
		assert.Equal(t, h.NewCount, newSeen, "hunk header new count vs new-side body lines")
	}
}

// oracle: the materialized files themselves — a git working-tree diff is
// parsed, and every line number the parser assigned is used to index into
// the actual old and new file contents on disk. The files come from git and
// the literals below, a completely different path than the parser's
// header-derived counters.
func TestParse_LineNumbersMatchFileContents_Differential(t *testing.T) {
	dir := t.TempDir()
	gitOut(t, dir, "init", "--template=")

	oldA := "alpha\nbeta\ngamma\ndelta\nepsilon\nzeta\neta\ntheta\niota\nkappa\nlambda\nmu\n"
	// No trailing newline: git answers with a "\ No newline" marker line.
	newA := "alpha\nbeta\ngamma\nDELTA-ONE\nDELTA-TWO\nepsilon\nzeta\neta\ntheta\niota\nOMEGA"
	oldB := "keep\nxx\n"
	newB := "keep\nyy\n"
	newC := "fresh\nfile\nlines\n"

	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(dir+"/"+name, []byte(content), 0o644))
	}
	write("a.txt", oldA)
	write("b.txt", oldB)
	gitOut(t, dir, "add", "a.txt", "b.txt")
	gitOut(t, dir, "commit", "-m", "init")

	write("a.txt", newA)
	write("b.txt", newB)
	write("c.txt", newC)
	// Intent-to-add so the new file shows up in the plain working-tree diff.
	gitOut(t, dir, "add", "-N", "c.txt")

	diff := gitOut(t, dir, "diff")
	doc, err := Parse(diff)
	require.NoError(t, err)
	require.Len(t, doc.Files, 3, "three changed files: %s", diff)

	byNewName := map[string]File{}
	for _, f := range doc.Files {
		byNewName[f.NewName] = f
	}

	checkFileAgainstContents(t, byNewName["b/a.txt"], oldA, newA)
	checkFileAgainstContents(t, byNewName["b/b.txt"], oldB, newB)
	checkFileAgainstContents(t, byNewName["b/c.txt"], "", newC)
	assert.Equal(t, "/dev/null", byNewName["b/c.txt"].OldName, "a new file diffs against /dev/null")

	sawNoNewline := false
	for _, f := range doc.Files {
		for _, h := range f.Hunks {
			for _, ln := range h.Lines {
				if ln.Kind == NoNewline {
					sawNoNewline = true
				}
			}
		}
	}
	require.True(t, sawNoNewline, "fixture must exercise the no-newline marker")
}

// naiveHunkNumbers parses a hunk header by hand: slice off the "@@ -"
// prefix, split at " +", stop the new side at " @@", and default an omitted
// count to 1. No regex, no shared code with the parser.
func naiveHunkNumbers(header string) (oldStart, oldCount, newStart, newCount int) {
	rest := strings.TrimPrefix(header, "@@ -")
	plus := strings.Index(rest, " +")
	if plus < 0 {
		return 0, 0, 0, 0
	}
	oldPart, newPart := rest[:plus], rest[plus+2:]
	if end := strings.Index(newPart, " @@"); end >= 0 {
		newPart = newPart[:end]
	}
	parse := func(part string) (start, count int) {
		if c := strings.Index(part, ","); c >= 0 {
			s, _ := strconv.Atoi(part[:c])
			n, _ := strconv.Atoi(part[c+1:])
			return s, n
		}
		s, _ := strconv.Atoi(part)
		return s, 1
	}
	oldStart, oldCount = parse(oldPart)
	newStart, newCount = parse(newPart)
	return
}

// hunkBody emits exactly the lines the header promises: one shared context
// line (when both sides exist), then the old-side extras as deletes and the
// new-side extras as adds.
func hunkBody(oldCount, newCount int) []string {
	ctx := 0
	if oldCount > 0 && newCount > 0 {
		ctx = 1
	}
	var lines []string
	if ctx == 1 {
		lines = append(lines, " ctx")
	}
	for i := 0; i < oldCount-ctx; i++ {
		lines = append(lines, "-old"+strconv.Itoa(i))
	}
	for i := 0; i < newCount-ctx; i++ {
		lines = append(lines, "+new"+strconv.Itoa(i))
	}
	return lines
}

// oracle: naive strings.Split-based reference — each hunk header in a golden
// corpus is parsed by hand-slicing, and the parser's starts, counts, and
// body line numbers must all match that reference, including the omitted
// ",count" form and trailing function context.
func TestParse_HunkNumbersMatchNaiveSliceReference_Differential(t *testing.T) {
	headers := []string{
		"@@ -12,3 +13,5 @@",
		"@@ -1 +1 @@",
		"@@ -0,0 +1,3 @@",
		"@@ -3,7 +3,7 @@ func main() {",
		"@@ -10,4 +9,0 @@",
		"@@ -200,1 +200,2 @@",
	}
	for _, header := range headers {
		t.Run(header, func(t *testing.T) {
			oldStart, oldCount, newStart, newCount := naiveHunkNumbers(header)
			text := "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n" +
				header + "\n" + strings.Join(hunkBody(oldCount, newCount), "\n") + "\n"

			doc, err := Parse(text)
			require.NoError(t, err)
			require.Len(t, doc.Files, 1)
			require.Len(t, doc.Files[0].Hunks, 1)
			h := doc.Files[0].Hunks[0]

			assert.Equal(t, oldStart, h.OldStart)
			assert.Equal(t, oldCount, h.OldCount)
			assert.Equal(t, newStart, h.NewStart)
			assert.Equal(t, newCount, h.NewCount)

			var oldNums, newNums []int
			for _, ln := range h.Lines {
				switch ln.Kind {
				case Context:
					oldNums = append(oldNums, ln.OldLine)
					newNums = append(newNums, ln.NewLine)
				case Delete:
					oldNums = append(oldNums, ln.OldLine)
				case Add:
					newNums = append(newNums, ln.NewLine)
				case NoNewline:
				}
			}
			require.Len(t, oldNums, oldCount)
			require.Len(t, newNums, newCount)
			for i, n := range oldNums {
				assert.Equal(t, oldStart+i, n, "old-side numbering must run consecutively from the start")
			}
			for i, n := range newNums {
				assert.Equal(t, newStart+i, n, "new-side numbering must run consecutively from the start")
			}
		})
	}
}
