package block

import (
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

// TestBodyLineClass_ClassifiesByLeadingToken_Differential pins the three-rule
// classifier against hand-written expectations. Output used to render as ONE dim
// span, so a test run's two failures looked exactly like its hundred passes.
func TestBodyLineClass_ClassifiesByLeadingToken_Differential(t *testing.T) {
	cases := map[string]bodyClass{
		"FAIL github.com/x/y":             bodyErr,
		"--- FAIL: TestFoo":               bodyErr,
		"error: cannot find symbol":       bodyErr,
		"panic: runtime error":            bodyErr,
		"npm ERR! missing script":         bodyErr,
		"dial tcp: connection refused":    bodyErr,
		"ok  \tgithub.com/x/y\t0.4s":      bodyOK,
		"PASS":                            bodyOK,
		"--- PASS: TestFoo":               bodyOK,
		"✓ all good":                      bodyOK,
		"WARN deprecated":                 bodyWarn,
		"--- SKIP: TestFoo":               bodyWarn,
		"total 42":                        bodyPlain,
		"the error was handled correctly": bodyPlain, // mid-line mention: NOT an error line
		"":                                bodyPlain,
	}
	for line, want := range cases {
		assert.Equal(t, want, bodyLineClass(line), "classification of %q", line)
	}
}

// TestClassifyBodySpans_ColoursFailuresDifferently is the differential against
// the old single-span renderer: a failing line and a passing line must no
// longer share one style.
func TestClassifyBodySpans_ColoursFailuresDifferently(t *testing.T) {
	th := components.DefaultTheme()
	output := "ok  \tgithub.com/x/a\t0.1s\nFAIL github.com/x/b\nok  \tgithub.com/x/c\t0.2s"

	var okStyle, errStyle xui.Style
	var sawOK, sawErr bool
	for _, sp := range classifyBodySpans(output, th) {
		switch {
		case strings.Contains(sp.Text, "FAIL"):
			errStyle, sawErr = sp.Style, true
		case strings.HasPrefix(sp.Text, "ok  "):
			okStyle, sawOK = sp.Style, true
		}
	}
	require.True(t, sawErr, "the failure line is classified")
	require.True(t, sawOK, "the success line is classified")
	assert.NotEqual(t, okStyle, errStyle, "a failing line must not look like a passing one")
	assert.Equal(t, th.Destructive, errStyle)
	assert.Equal(t, th.Success, okStyle)
}

// TestBashBlock_BodyWrapsInsideTheMargin_Negative pins the off-by-two: the body
// is painted at x=2 but used to wrap to width-2, so every wrapped line hung two
// columns past the right edge and reflowed at the wrong place.
func TestBashBlock_BodyWrapsInsideTheMargin_Negative(t *testing.T) {
	const width = 40
	long := strings.Repeat("word ", 40)
	b := &BashBlock{Command: "echo", Output: long, Theme: components.DefaultTheme(), Expanded: true}

	surf := b.Draw(components.DrawContext{Max: components.Size{Width: width, Height: 100}})
	for y := 0; y < surf.Size.Height; y++ {
		line := strings.TrimRight(rowText(surf, y, width), " ")
		assert.LessOrEqual(t, xui.StringWidth(line, xui.WidthUnicode), width,
			"row %d must not exceed the surface width", y)
	}
}

// rowText reads one painted row back as a string.
func rowText(s components.Surface, y, width int) string {
	var b strings.Builder
	for x := 0; x < width; x++ {
		if len(s.Buffer) == 0 {
			return ""
		}
		c := s.Buffer[y*s.Size.Width+x]
		if c.Char == "" {
			b.WriteString(" ")
			continue
		}
		b.WriteString(c.Char)
	}
	return b.String()
}
