package palette

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

// ---------- negative ----------

// Hostile queries against the matcher: empty labels never match, queries
// longer than the label fail the subsequence walk, whitespace-only queries
// degrade to match-all, and out-of-order runes are rejected.
func TestFuzzyMatchHostileQueries_Negative(t *testing.T) {
	ok, score := fuzzyMatch("", "")
	require.True(t, ok)
	assert.InDelta(t, 1.0, score, 1e-9)

	ok, _ = fuzzyMatch("a", "")
	assert.False(t, ok, "an empty label never matches a non-empty query")

	ok, _ = fuzzyMatch(strings.Repeat("x", 40), "short")
	assert.False(t, ok, "query longer than the label cannot be a subsequence")

	ok, score = fuzzyMatch("   ", "anything")
	require.True(t, ok, "whitespace-only query trims to match-all")
	assert.InDelta(t, 1.0, score, 1e-9)

	ok, _ = fuzzyMatch("axc", "abc")
	assert.False(t, ok, "a rune missing from the label kills the subsequence")

	ok, _ = fuzzyMatch("cb", "abc")
	assert.False(t, ok, "out-of-order runes are not a subsequence")

	// A zero command has an empty label, so nothing ever matches it.
	assert.Empty(t, PaletteCommand{}.Label())
	ok, _ = fuzzyMatch("x", PaletteCommand{}.Label())
	assert.False(t, ok)
}

// Enter on a disabled command or an empty/overshooting selection is a
// rejected operation: the palette stays open and nothing runs.
func TestPaletteAcceptDisabledAndEmpty_Negative(t *testing.T) {
	ran := false
	p := &CommandPalette{
		Theme: components.DefaultTheme(),
		Commands: []PaletteCommand{
			{ID: "d", Verb: "disabled one", Disabled: true, Run: func() { ran = true }},
		},
	}
	p.Show()
	ctx := &components.EventContext{}
	p.Handle(ctx, xui.KeyEvent{Code: xui.KeyEnter, Press: true})
	assert.True(t, p.Open, "disabled command must not close the palette")
	assert.False(t, ran, "disabled command must not run")
	assert.True(t, ctx.Consume, "the keypress is still consumed")

	// No matches: Enter is a no-op that keeps the palette open.
	p2 := &CommandPalette{
		Theme:    components.DefaultTheme(),
		Commands: []PaletteCommand{{ID: "1", Verb: "help"}},
	}
	p2.Show()
	for _, r := range "zzz" {
		p2.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: r, Press: true})
	}
	require.Empty(t, p2.filtered)
	p2.Handle(ctx, xui.KeyEvent{Code: xui.KeyEnter, Press: true})
	assert.True(t, p2.Open, "accept with no matches must not close the palette")

	// A hostile selection index never reaches Commands.
	p3 := &CommandPalette{
		Theme:    components.DefaultTheme(),
		Commands: []PaletteCommand{{ID: "1", Verb: "help", Run: func() { ran = true }}},
	}
	p3.Show()
	p3.Selected = 99
	p3.Handle(ctx, xui.KeyEvent{Code: xui.KeyEnter, Press: true})
	assert.True(t, p3.Open)
	assert.False(t, ran, "out-of-range selection must not run anything")
}

// Cursor and rune guards: bounds hold, control runes and chords never edit
// the query, releases are ignored, and hostile pastes are sanitized.
func TestPaletteEditingGuards_Negative(t *testing.T) {
	p := &CommandPalette{
		Theme:    components.DefaultTheme(),
		Commands: []PaletteCommand{{ID: "1", Verb: "help"}},
	}
	p.Show()
	ctx := &components.EventContext{}
	press := func(code xui.KeyCode, r rune) {
		p.Handle(ctx, xui.KeyEvent{Code: code, Rune: r, Press: true})
	}

	// Backspace on an empty query is a no-op.
	press(xui.KeyBackspace, 0)
	assert.Equal(t, "", p.Query)
	assert.Equal(t, 0, p.Cursor)

	// Control runes, Ctrl chords, and Alt chords never insert.
	press(xui.KeyRune, 0x01)
	p.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'x', Press: true, Mods: xui.ModCtrl})
	p.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'y', Press: true, Mods: xui.ModAlt})
	assert.Equal(t, "", p.Query)

	// Key releases are ignored entirely.
	p.Handle(ctx, xui.KeyEvent{Code: xui.KeyRune, Rune: 'a', Press: false})
	assert.Equal(t, "", p.Query)

	// Cursor motion clamps at both ends of the query.
	for _, r := range "ab" {
		press(xui.KeyRune, r)
	}
	require.Equal(t, "ab", p.Query)
	press(xui.KeyEnd, 0)
	press(xui.KeyRight, 0)
	assert.Equal(t, 2, p.Cursor, "right at the end stays put")
	press(xui.KeyHome, 0)
	press(xui.KeyLeft, 0)
	assert.Equal(t, 0, p.Cursor, "left at the start stays put")
	press(xui.KeyBackspace, 0)
	assert.Equal(t, "ab", p.Query, "backspace at column 0 removes nothing")

	// A hostile paste is sanitized: newlines and CRs become spaces. The
	// cursor sits at column 0 after Home, so the paste lands in front.
	p.Handle(ctx, xui.PasteEvent{Text: "x\ny\rz"})
	assert.Equal(t, "x y zab", p.Query)
	assert.Equal(t, len("x y z"), p.Cursor)

	// A closed palette ignores keys and pastes without consuming.
	p.Hide()
	ctx2 := &components.EventContext{}
	p.Handle(ctx2, xui.KeyEvent{Code: xui.KeyEnter, Press: true})
	p.Handle(ctx2, xui.PasteEvent{Text: "nope"})
	assert.False(t, ctx2.Consume)
}

// Degenerate geometry and empty command lists.
func TestPaletteDrawDegenerate_Negative(t *testing.T) {
	// Zero-size context falls back to the 80x24 default canvas.
	p := &CommandPalette{
		Theme:    components.DefaultTheme(),
		Commands: []PaletteCommand{{ID: "1", Noun: "app", Verb: "help"}},
	}
	p.Show()
	s := p.Draw(components.DrawContext{Max: components.Size{}, Method: xui.WidthUnicode})
	require.Len(t, s.Children, 1)
	assert.Equal(t, components.Size{Width: 80, Height: 24}, s.Size)
	assert.Equal(t, 60, s.Children[0].Surface.Size.Width, "auto width is 3/4 of the canvas")

	// An empty command list renders the empty-state row instead of panicking.
	e := &CommandPalette{Theme: components.DefaultTheme()}
	e.Show()
	se := e.Draw(components.DrawContext{Max: components.Size{Width: 80, Height: 24}, Method: xui.WidthUnicode})
	require.Len(t, se.Children, 1)
	assert.Contains(t, components.SurfaceText(se.Children[0].Surface), "No matching commands")

	// A closed palette draws a bare overlay surface.
	c := &CommandPalette{Theme: components.DefaultTheme()}
	sc := c.Draw(components.DrawContext{Max: components.Size{Width: 80, Height: 24}, Method: xui.WidthUnicode})
	assert.Empty(t, sc.Children)
}

// ---------- differential ----------

// oracleFuzzyMatch rebuilds the match decision from stdlib string relations
// (ToLower/TrimSpace/HasPrefix/Contains) plus a two-pointer subsequence scan
// with strings.IndexRune. Corpus labels stay within 24 runes, so a full
// subsequence match always scores at least 1-24*0.02 = 0.52 > 0.15 and the
// threshold never flips an outcome.
func oracleFuzzyMatch(query, label string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	l := strings.ToLower(label)
	if q == "" || l == q || strings.HasPrefix(l, q) || strings.Contains(l, q) {
		return true
	}
	rest := l
	for _, r := range q {
		idx := strings.IndexRune(rest, r)
		if idx < 0 {
			return false
		}
		rest = rest[idx+utf8.RuneLen(r):]
	}
	return true
}

func paletteRandomString(rng *rand.Rand, maxRunes int) string {
	letters := []rune("abcd ")
	var b strings.Builder
	for n := rng.Intn(maxRunes + 1); n > 0; n-- {
		b.WriteRune(letters[rng.Intn(len(letters))])
	}
	return b.String()
}

// oracle: stdlib string relations plus a two-pointer subsequence scan
// (oracleFuzzyMatch above) on randomized query/label pairs, plus the fixed
// spec scores for exact, prefix, substring, and gapped-subsequence matches.
func TestFuzzyMatchMatchesStdlibOracle_Differential(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	for i := range 400 {
		label := paletteRandomString(rng, 24)
		query := paletteRandomString(rng, 8)
		got, _ := fuzzyMatch(query, label)
		assert.Equal(t, oracleFuzzyMatch(query, label), got, "case %d: fuzzyMatch(%q, %q)", i, query, label)
	}

	// Fixed spec scores: exact, prefix, substring, subsequence with gaps.
	ok, score := fuzzyMatch("help", "help")
	require.True(t, ok)
	assert.InDelta(t, 1.0, score, 1e-9)
	ok, score = fuzzyMatch("he", "help")
	require.True(t, ok)
	assert.InDelta(t, 0.95, score, 1e-9)
	ok, score = fuzzyMatch("lp", "help")
	require.True(t, ok)
	assert.InDelta(t, 0.75, score, 1e-9)
	ok, score = fuzzyMatch("hp", "help")
	require.True(t, ok)
	assert.InDelta(t, 0.96, score, 1e-9, "full subsequence with two gap runes: 1-2*0.02")
}

func paletteCommandIDs(cmds []PaletteCommand) []string {
	ids := make([]string, len(cmds))
	for i, c := range cmds {
		ids[i] = c.ID
	}
	return ids
}

// oracle: the pre-Push state snapshot itself — Push saves the current page
// and Pop must restore it through the inverse code path, so the round trip is
// checked against the captured state, not against the push machinery.
func TestPalettePushPopRoundTrip_Differential(t *testing.T) {
	titles := []string{"", "Root", "Command Palette"}
	verbs := []string{"theme", "help", "reload", "quit"}

	rng := rand.New(rand.NewSource(11))
	for trial := range 25 {
		p := &CommandPalette{Theme: components.DefaultTheme()}
		p.Show()

		nCmds := rng.Intn(4)
		p.Commands = make([]PaletteCommand, nCmds)
		for i := range p.Commands {
			p.Commands[i] = PaletteCommand{ID: fmt.Sprintf("root-%d", i), Verb: verbs[rng.Intn(len(verbs))]}
		}
		p.Title = titles[rng.Intn(len(titles))]
		p.Query = paletteRandomString(rng, 6)
		p.Cursor = rng.Intn(len(p.Query) + 1)
		sel := rng.Intn(6) - 2 // hostile: negative or past the end
		p.Selected = sel

		savedTitle, savedQuery, savedCursor := p.Title, p.Query, p.Cursor
		savedIDs := paletteCommandIDs(p.Commands)

		depth := 1 + rng.Intn(3)
		for d := range depth {
			p.Push(fmt.Sprintf("sub-%d", d), []PaletteCommand{{ID: fmt.Sprintf("sub-%d-cmd", d), Verb: "pick"}})
			assert.Equal(t, fmt.Sprintf("sub-%d", d), p.Title, "trial %d: pushed title", trial)
			assert.Empty(t, p.Query, "trial %d: pushed page starts with an empty query", trial)
			assert.Equal(t, 0, p.Selected, "trial %d: pushed page starts at the top", trial)
		}
		for range depth {
			require.True(t, p.Pop(), "trial %d: pop within the stack succeeds", trial)
		}
		assert.False(t, p.Pop(), "trial %d: pop past the root reports failure", trial)

		assert.Equal(t, savedTitle, p.Title, "trial %d: title restored", trial)
		assert.Equal(t, savedQuery, p.Query, "trial %d: query restored", trial)
		assert.Equal(t, savedCursor, p.Cursor, "trial %d: cursor restored", trial)
		assert.Equal(t, savedIDs, paletteCommandIDs(p.Commands), "trial %d: commands restored", trial)

		// Selected comes back through refilter's clamp against the number of
		// commands the restored query still matches.
		matched := 0
		if strings.TrimSpace(savedQuery) == "" {
			matched = len(p.Commands)
		} else {
			for _, c := range p.Commands {
				if oracleFuzzyMatch(savedQuery, c.Label()) {
					matched++
				}
			}
		}
		wantSel := 0
		if matched > 0 {
			wantSel = min(max(sel, 0), matched-1)
		}
		assert.Equal(t, wantSel, p.Selected, "trial %d: selection restored through the clamp", trial)
	}
}
