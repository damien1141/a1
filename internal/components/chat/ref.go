package chat

import (
	"strconv"
	"strings"
)

// Ref is a slice of a file the user picked in the code viewer and attached to
// the prompt. It shows as a chip row above the editor, like a pending skill.
type Ref struct {
	Path  string // workspace-relative path
	Start int    // 1-based first line
	End   int    // 1-based last line; == Start for a single line
	Lang  string // fence language for Block(); may be empty
	Text  string // the selected lines, already joined with "\n"
}

// Label is the chip and transcript form: "path:12-18", or "path:12" for one line.
func (r Ref) Label() string {
	start, end := r.span()
	if end > start {
		return r.Path + ":" + strconv.Itoa(start) + "-" + strconv.Itoa(end)
	}
	return r.Path + ":" + strconv.Itoa(start)
}

// Block is the form sent to the model: a "path:12-18" heading followed by a
// fenced code block containing Text. When Lang is empty the fence is bare.
func (r Ref) Block() string {
	var b strings.Builder
	b.WriteString(r.Label())
	b.WriteString("\n```")
	b.WriteString(r.Lang)
	b.WriteByte('\n')
	b.WriteString(r.Text)
	// Text is verbatim, so only close the fence on the next line when the
	// selection does not already end with one.
	if !strings.HasSuffix(r.Text, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```")
	return b.String()
}

// span normalizes the line range: line numbers are 1-based, and a backwards
// range collapses to its start rather than inventing lines.
func (r Ref) span() (int, int) {
	start := max(r.Start, 1)
	return start, max(r.End, start)
}
