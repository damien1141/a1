package block_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/block"
	"github.com/damien1141/a1/internal/components/status"
)

const readOutput = "@file haiku.txt\n     1\tcold bytes on the wire\n     2\ta green cursor waits alone\n     3\tthe build passes at last"

// strip drops the tool's leading "@file path" line, the same way the Mapper's
// stripReadHeader does before handing the body to ReadBlock.
func strip(body string) string {
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "@file ") {
			return strings.TrimLeft(
				strings.Join(append(append([]string(nil), lines[:i]...), lines[i+1:]...), "\n"),
				"\n",
			)
		}
	}
	return body
}

func readCard(t *testing.T, w *block.ReadBlock) string {
	t.Helper()
	return components.SurfaceText(w.Draw(components.DrawContext{Max: components.Size{Width: 74, Height: 40}}))
}

func TestReadBlockHeaderNamesToolAndPath(t *testing.T) {
	d := &block.ReadBlock{
		Name: "read", Path: "haiku.txt", Body: strip(readOutput),
		Status: status.ToolDone, Expanded: true, Theme: components.DefaultTheme(),
	}
	got := readCard(t, d)
	assert.Contains(t, got, "read haiku.txt (3 lines)", "the header matches the edit-card shape")
	// The @file line is the tool's, not the card's: it must not double up.
	assert.NotContains(t, got, "@file", "no duplicated @file header inside the card")
}

func TestReadBlockGrepHeaderKeepsPattern(t *testing.T) {
	d := &block.ReadBlock{
		Name: "grep", Detail: `grep "cursor" in .`, Body: "a green cursor waits alone",
		Status: status.ToolDone, Expanded: true, Theme: components.DefaultTheme(),
	}
	got := readCard(t, d)
	assert.Contains(t, got, `grep "cursor" in . (1 lines)`)
	assert.NotContains(t, got, "grep grep", "the header prints one verb, not two")
}

func TestReadBlockStripsGrepLinePrefix(t *testing.T) {
	// grep emits "path:>>  12|text"; ReadBlock normalizes it to the same gutter
	// read uses, so both row types look identical.
	d := &block.ReadBlock{
		Name: "grep", Detail: `grep "cursor" in .`, Body: "haiku.txt:>>\t 2|a green cursor waits alone",
		Status: status.ToolDone, Expanded: true, Theme: components.DefaultTheme(),
	}
	got := readCard(t, d)
	assert.Contains(t, got, "a green cursor waits alone")
	assert.NotContains(t, got, "haiku.txt:>>", "the path:>> prefix is stripped")
	assert.NotContains(t, got, "2|", "the | separator is stripped")
}

func TestReadBlockFailedShowsError(t *testing.T) {
	d := &block.ReadBlock{
		Name: "read", Path: "missing.go", Error: "file not found",
		Status: status.ToolError, Theme: components.DefaultTheme(),
	}
	require.True(t, d.HasBody(), "a failed read has a body: the error")
	assert.Contains(t, readCard(t, d), "Error:")
}

func TestReadBlockCopyText(t *testing.T) {
	d := &block.ReadBlock{Name: "read", Path: "haiku.txt", Body: strip(readOutput)}
	text := d.CopyText()
	assert.Contains(t, text, "haiku.txt (3 lines)")
	assert.Contains(t, text, "cold bytes on the wire")
}

func TestReadBlockLineCountIgnoresBlanks(t *testing.T) {
	d := &block.ReadBlock{Name: "read", Body: "a\n\n\nb\n"}
	assert.Equal(t, 2, d.LineCount(), "blank lines are not content")
}
