package chrome_test

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/chrome"
	"github.com/damien1141/a1/internal/components/status"
)

func TestChromeDegenerateFallbacks_Negative(t *testing.T) {
	th := components.DefaultTheme()

	// A zero Theme has no colors: every style picker must fall back to its
	// documented second choice instead of painting nothing.
	assert.Equal(t, components.Theme{}.Success, chrome.DecisionPrimary(components.Theme{}))
	assert.Equal(t, th.ToolName, chrome.DecisionPrimary(th), "a real theme keeps its ToolName accent")
	assert.Equal(t, components.Theme{}.Border, chrome.ModalBorder(components.Theme{}))
	assert.Equal(t, th.Title, chrome.ModalBorder(th))
	// A zero Theme degrades to a bold default-style title; a theme with only
	// a Foreground promotes that (bolded) instead.
	assert.Equal(t, xui.Style{Bold: true}, chrome.PanelTitle(components.Theme{}))
	assert.Equal(t, xui.Style{Fg: th.Foreground.Fg, Bold: true},
		chrome.PanelTitle(components.Theme{Foreground: th.Foreground}))

	// All-empty hint fragments join to nothing — no dangling separator.
	assert.Equal(t, "", chrome.AskHint("", "", ""))

	// Unknown status values degrade to the done glyphs, not garbage.
	icon, st := chrome.ToolIcon(status.ToolStatus(42), th, nil)
	assert.Equal(t, chrome.Ok, icon)
	assert.Equal(t, th.Success, st)
	assert.Equal(t, chrome.Ok, chrome.ChildIcon(status.ToolStatus(42)))
	assert.Equal(t, "", chrome.StatusSuffix(status.ToolStatus(42)))
}

func TestOptionLineDegenerateWidth_Negative(t *testing.T) {
	th := components.DefaultTheme()
	primary := chrome.DecisionPrimary(th)

	// innerW 0 clamps to one column: one grapheme per row, nothing lost.
	lines := chrome.OptionLine(th, primary, "Approve", true, 0, xui.WidthUnicode)
	require.NotEmpty(t, lines)
	var joined strings.Builder
	for _, line := range lines {
		for _, sp := range line {
			joined.WriteString(sp.Text)
		}
	}
	assert.Equal(t, "▸● Approve", joined.String(), "zero width must not drop or duplicate text")
	require.Len(t, lines, 10, "arrow + dot + 8 label graphemes, one per row")

	unselected := chrome.OptionLine(th, primary, "note", false, -5, xui.WidthUnicode)
	var joined2 strings.Builder
	for _, line := range unselected {
		for _, sp := range line {
			joined2.WriteString(sp.Text)
		}
	}
	assert.Equal(t, " ○ note", joined2.String(), "negative width clamps like zero")
}

// oracle: strings.Join over the non-empty fragments — the byline contract
// (separator between real fragments only) restated with the stdlib, plus the
// product invariant that chrome.Sep and components.BylineSeparator are the
// same "one separator for the whole product" definition.
func TestHintsMatchJoinedParts_Differential(t *testing.T) {
	assert.Equal(t, components.BylineSeparator, chrome.Sep, "one separator for the whole product")
	assert.Equal(t, " · ", chrome.Sep)

	actions := []string{"", "select", "switch branch", "日本語", "x · y"}
	rng := rand.New(rand.NewSource(19))
	for i := range 150 {
		action := actions[rng.Intn(len(actions))]
		eff := action
		if eff == "" {
			eff = "select"
		}
		assert.Equal(t,
			strings.Join([]string{" ↑↓ move", "⏎ " + eff, "esc close "}, chrome.Sep),
			chrome.ListHint(action),
			"case %d: ListHint(%q)", i, action)
		assert.Equal(t,
			strings.Join([]string{" ⏎ " + eff, "esc close "}, chrome.Sep),
			chrome.ListHintShort(action),
			"case %d: ListHintShort(%q)", i, action)

		nav := actions[rng.Intn(len(actions))]
		dismiss := actions[rng.Intn(len(actions))]
		var parts []string
		if nav != "" {
			parts = append(parts, nav)
		}
		if action != "" {
			parts = append(parts, "Enter "+action)
		}
		if dismiss != "" {
			parts = append(parts, "Esc "+dismiss)
		}
		assert.Equal(t, strings.Join(parts, chrome.Sep), chrome.AskHint(nav, action, dismiss),
			"case %d: AskHint(%q, %q, %q)", i, nav, action, dismiss)
	}
}

// oracle: hand-written golden table — every ToolStatus (plus one invalid
// value) mapped to its glyph and theme role, with a deterministic spinner
// frame (frame 4 of the braille cycle) for the running states.
func TestToolIconGoldenMap_Differential(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	spin.Frame = 4 // braille cycle index 4: ⠋ ⠙ ⠹ ⠸ ⠼ …

	type iconCase struct {
		st    status.ToolStatus
		icon  string
		style xui.Style
	}
	cases := []iconCase{
		{status.ToolDone, chrome.Ok, th.Success},
		{status.ToolRunning, "⠼", th.ToolName},
		{status.ToolQueued, "⠼", th.ToolName},
		{status.ToolError, chrome.Err, th.Destructive},
		{status.ToolCancelled, chrome.Stop, th.Muted},
		{status.ToolRejected, chrome.Stop, th.Destructive},
	}
	for _, tc := range cases {
		icon, st := chrome.ToolIcon(tc.st, th, spin)
		assert.Equal(t, tc.icon, icon, "ToolIcon(%v)", tc.st)
		assert.Equal(t, tc.style, st, "ToolIcon(%v) style", tc.st)
	}

	// Without a spinner the running states fall back to the busy glyph.
	for _, st := range []status.ToolStatus{status.ToolRunning, status.ToolQueued} {
		icon, style := chrome.ToolIcon(st, th, nil)
		assert.Equal(t, chrome.Busy, icon, "ToolIcon(%v, nil spin)", st)
		assert.Equal(t, th.ToolName, style)
	}

	childWant := map[status.ToolStatus]string{
		status.ToolDone:      chrome.Ok,
		status.ToolRunning:   chrome.Busy,
		status.ToolError:     chrome.Err,
		status.ToolCancelled: chrome.Stop,
		status.ToolQueued:    chrome.Busy,
		status.ToolRejected:  chrome.Stop,
	}
	for st, want := range childWant {
		assert.Equal(t, want, chrome.ChildIcon(st), "ChildIcon(%v)", st)
	}

	suffixWant := map[status.ToolStatus]string{
		status.ToolDone:      "",
		status.ToolRunning:   "",
		status.ToolError:     "",
		status.ToolCancelled: " (cancelled)",
		status.ToolRejected:  " (rejected)",
	}
	for st, want := range suffixWant {
		assert.Equal(t, want, chrome.StatusSuffix(st), "StatusSuffix(%v)", st)
	}
}

// oracle: hand-written golden literal of the rendered decision row for one
// fixed state — expected surface text derived by hand ("▸● Approve" occupies
// columns 0..9 of a 40-column row), never regenerated from OptionLine.
func TestOptionLineGoldenRender_Differential(t *testing.T) {
	th := components.DarkTheme()
	primary := chrome.DecisionPrimary(th)

	selected := chrome.OptionLine(th, primary, "Approve", true, 40, xui.WidthUnicode)
	sel := components.PaintRichLines(40, selected, xui.WidthUnicode, nil)
	assert.Equal(t, "▸● Approve"+strings.Repeat(" ", 30)+"\n", components.SurfaceText(sel))

	idle := chrome.OptionLine(th, primary, "Approve", false, 40, xui.WidthUnicode)
	idleSurf := components.PaintRichLines(40, idle, xui.WidthUnicode, nil)
	assert.Equal(t, " ○ Approve"+strings.Repeat(" ", 30)+"\n", components.SurfaceText(idleSurf))

	// Selected rows carry the product's arrow/dot glyphs on the same columns.
	require.Len(t, selected, 1)
	assert.Equal(t, chrome.SelectArrow, selected[0][0].Text)
	assert.Equal(t, chrome.DotOn, selected[0][1].Text)
	require.Len(t, idle, 1)
	assert.Equal(t, " ", idle[0][0].Text)
	assert.Equal(t, chrome.DotOff, idle[0][1].Text)
}
