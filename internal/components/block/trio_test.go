package block

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/status"
)

// oracle: hand-written golden literals — expected full surface text for fixed
// block states, derived from the documented card shapes (title spans, %6d
// gutter, two-column body indent) and chrome grammar, never regenerated from
// the Draw output.
func TestReadBlock_RendersGoldenCard_Differential(t *testing.T) {
	const w = 40
	draw := func(b *ReadBlock) string {
		return components.SurfaceText(b.Draw(components.DrawContext{
			Max:    components.Size{Width: w, Height: 20},
			Method: xui.WidthUnicode,
		}))
	}
	row := func(content string) string {
		return content + strings.Repeat(" ", w-len([]rune(content))) + "\n"
	}

	t.Run("expanded read card", func(t *testing.T) {
		b := &ReadBlock{
			Name: "read", Path: "haiku.txt",
			Body:     "     1|cold bytes on the wire\n     2|a green cursor waits alone",
			Status:   status.ToolDone,
			Expanded: true,
			Theme:    components.DefaultTheme(),
		}
		// Title: "✓ read haiku.txt (2 lines) ▼" (28 columns). Body rows carry a
		// 2-column indent, a 7-column "%6d " gutter, then the text.
		want := row("✓ read haiku.txt (2 lines) ▼") +
			row("  "+"     1 "+"cold bytes on the wire") +
			row("  "+"     2 "+"a green cursor waits alone")
		assert.Equal(t, want, draw(b))
	})

	t.Run("expanded grep card normalizes the gutter", func(t *testing.T) {
		b := &ReadBlock{
			Name:   "search",
			Detail: `search "wire" in .`,
			Body:   "haiku.txt:>> 1|cold bytes on the wire",
			Status: status.ToolDone, Expanded: true,
			Theme: components.DefaultTheme(),
		}
		// The detail's leading verb is folded into the header verb; the
		// "path:>> N|" prefix is stripped and re-poured into the same gutter.
		want := row("✓ search "+`"wire" in .`+" (1 lines) ▼") +
			row("  "+"     1 "+"cold bytes on the wire")
		assert.Equal(t, want, draw(b))
	})

	t.Run("failed read shows its error while collapsed", func(t *testing.T) {
		b := &ReadBlock{
			Name: "read", Path: "missing.go",
			Error:  "file not found",
			Status: status.ToolError,
			Theme:  components.DefaultTheme(),
		}
		// Collapsed (▶ arrow), but the failure is the one body that may not be
		// hidden; no line count for an empty body.
		want := row("✗ read missing.go ▶") +
			row("  "+"Error: file not found")
		assert.Equal(t, want, draw(b))
	})
}

// oracle: naive string reference — the collapsed tail computed with plain
// stdlib ops (strings.Split, slicing, fmt.Sprintf) instead of the wrapped-line
// budget machinery. All lines are short enough that wrapping is the identity,
// so the two must agree row for row.
func TestBashBlock_CollapsedTailMatchesNaiveReference_Differential(t *testing.T) {
	const w = 40
	var out strings.Builder
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&out, "line %02d\n", i)
	}
	b := &BashBlock{
		Command: "seq 12",
		Output:  out.String(),
		Status:  status.ToolDone,
		Theme:   components.DefaultTheme(),
	}

	// Reference: title row, then the last BashCollapsedTailLines source lines
	// plus a "… +N more lines" trailer when the output overran the budget.
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	wantRows := []string{"$ " + "seq 12" + " ▶"}
	kept := lines
	if len(lines) > BashCollapsedTailLines {
		kept = lines[len(lines)-BashCollapsedTailLines:]
		kept = append(kept, fmt.Sprintf("… +%d more lines", len(lines)-BashCollapsedTailLines))
	}
	for _, ln := range kept {
		wantRows = append(wantRows, "  "+ln)
	}

	var want strings.Builder
	for _, r := range wantRows {
		want.WriteString(r)
		want.WriteString(strings.Repeat(" ", w-len([]rune(r))))
		want.WriteByte('\n')
	}

	got := components.SurfaceText(b.Draw(components.DrawContext{
		Max:    components.Size{Width: w, Height: 60},
		Method: xui.WidthUnicode,
	}))
	assert.Equal(t, want.String(), got)
}

// gutterOracle re-derives splitLineNumber with stdlib + regexp machinery: the
// gutter separator is anchored with strings.LastIndex, and the head must be
// exactly (optional "path:>"-style prefix) + optional blanks + digits — the
// shape the read ("  12|text") and grep ("path:>> 12|text") tools emit.
var gutterHead = regexp.MustCompile(`^(?:(.*)[:>])?[ \t>]*([0-9]+)[ \t]*$`)

func gutterOracle(line string) (int, string) {
	i := strings.LastIndex(line, "|")
	if i < 0 {
		return 0, line
	}
	head, tail := line[:i], line[i+1:]
	m := gutterHead.FindStringSubmatch(head)
	if m == nil {
		return 0, line
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return 0, line
	}
	return n, tail
}

// oracle: stdlib/regexp reference (gutterOracle above) on fixed tool-output
// shapes plus randomized hostile lines from the same alphabet.
func TestSplitLineNumber_MatchesStdlibRegexpOracle_Differential(t *testing.T) {
	corpus := []string{
		// read tool gutters
		"     1|cold bytes on the wire", "  12|go test ./...", "1|x", "007|leading zeros",
		// grep tool gutters ("path:>> N|text")
		"haiku.txt:>> 2|a green cursor waits alone", "a.go:>>\t 3|cmd := x || y",
		"path:>7|x", "src/x:>>> 42|deep",
		// no gutter at all
		"plain text", "", "no digits|here", "|tail", "|",
		// boundary digits
		"123456789|rest", "99999999999999999999|overflow",
		// pipes inside the matched text move the anchor to the last pipe
		"  12|a|b", "  12|note: a > b",
	}
	for _, line := range corpus {
		gotNo, gotRest := splitLineNumber(line)
		wantNo, wantRest := gutterOracle(line)
		assert.Equal(t, wantNo, gotNo, "number for %q", line)
		assert.Equal(t, wantRest, gotRest, "rest for %q", line)
	}

	// Randomized hostile lines assembled from the same pieces.
	prefixes := []string{"", "haiku.txt:", "a.go:>>", "src/x:>", ">>", ":>", "cmd :="}
	blanks := []string{"", " ", "\t", "  ", " \t"}
	numbers := []string{"0", "1", "42", "007", "123456", "99999999999999999999"}
	texts := []string{"", "text", "a|b", "x: y", " |> ", "cmd := a || b", "日本語"}
	rng := rand.New(rand.NewSource(42))
	for i := range 300 {
		line := prefixes[rng.Intn(len(prefixes))] +
			blanks[rng.Intn(len(blanks))] +
			numbers[rng.Intn(len(numbers))] + "|" +
			texts[rng.Intn(len(texts))]
		gotNo, gotRest := splitLineNumber(line)
		wantNo, wantRest := gutterOracle(line)
		assert.Equal(t, wantNo, gotNo, "case %d: number for %q", i, line)
		assert.Equal(t, wantRest, gotRest, "case %d: rest for %q", i, line)
	}
}
