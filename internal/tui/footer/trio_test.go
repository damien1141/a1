package footer

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/controller"
)

// Degenerate footer states: zero width, hostile job counts, blank statuses and
// an update hint wider than the row must all degrade to blank or clipped
// output, never a panic or garbage.
func TestDraw_DegenerateStates_Negative(t *testing.T) {
	f := NewFooterChrome(components.DefaultTheme(), 0)
	rowAt := func(width int) string {
		return components.SurfaceText(f.Draw(components.DrawContext{Method: xui.WidthUnicode}, width))
	}

	assert.Empty(t, strings.TrimSpace(rowAt(0)), "a zero-width footer paints nothing")

	f.SetLiveJobs(func() int { return -3 })
	assert.NotContains(t, rowAt(40), "job",
		"negative job counts are dropped, not rendered as -3 jobs")

	f.SetLiveJobs(nil)
	f.SetExtensionStatus("   ")
	assert.Empty(t, strings.TrimSpace(rowAt(40)),
		"a whitespace-only extension status renders as an empty row")

	// A hint wider than the remaining row is skipped rather than wrapped or
	// overflowing the surface.
	f.SetExtensionStatus("review")
	f.Apply(controller.FooterMsg{Kind: controller.FooterUpdateAvailable, Latest: "v9.9"})
	narrow := rowAt(30)
	assert.Contains(t, narrow, "review")
	assert.NotContains(t, narrow, "a1 update", "the hint must be dropped when it cannot fit")
}

// Nil-receiver and hostile-usage guards: every accessor must work on a nil
// *FooterChrome and degenerate token numbers must hide the label.
func TestFooterChrome_NilReceiverAndHostileUsage_Negative(t *testing.T) {
	var f *FooterChrome

	assert.Nil(t, f.Spinner())
	assert.Nil(t, f.Activity())
	assert.Equal(t, 0, f.ContextWindow())
	assert.NotPanics(t, func() {
		f.Apply(controller.FooterMsg{Kind: controller.FooterUpdateAvailable, Latest: "v1"})
		f.SyncFromSnap(session.Snapshot{})
		f.UpdateTokenDisplay(session.TokenUsage{PromptTokens: 10})
		f.SetTheme(components.DefaultTheme())
		f.SetWindowSource(func() int { return 1 })
		f.SyncNow()
		f.SetActionsHint("hint")
	})
	surf := f.Draw(components.DrawContext{Method: xui.WidthUnicode}, 40)
	assert.Equal(t, 40, surf.Size.Width)
	assert.Equal(t, 1, surf.Size.Height)
	assert.Empty(t, strings.TrimSpace(components.SurfaceText(surf)))

	// Whitespace-only hint restores the token readout instead of showing blanks.
	live := NewFooterChrome(components.DefaultTheme(), 128000)
	comp := &stubComposer{}
	live.BindComposer(comp)
	live.UpdateTokenDisplay(session.TokenUsage{PromptTokens: 1200, TotalTokens: 1200})
	require.Contains(t, comp.label.Text, "↑1.2k")

	live.SetActionsHint("   ")
	assert.Contains(t, comp.label.Text, "↑1.2k", "a blank hint falls back to the token readout")

	live.SetActionsHint(" copying ")
	assert.Equal(t, "copying", comp.label.Text, "the hint is trimmed before display")

	// Negative usage / window values gate the label off entirely.
	assert.Equal(t, 0.0, contextFillRatio(-10, -10))
	assert.Empty(t, formatContextLabel(session.TokenUsage{TotalTokens: -5}, 128000),
		"negative used hides the label")
	assert.Empty(t, formatContextLabel(session.TokenUsage{TotalTokens: 50}, -128000),
		"negative window hides the label")
}

// naiveUsageStats re-derives the readout from a bucket table: only positive
// buckets appear, Σ falls back to the bucket sum when no total was reported,
// and fragments join with single spaces.
func naiveUsageStats(u session.TokenUsage) string {
	if !u.Reported() {
		return ""
	}
	type bucket struct {
		prefix string
		n      int
	}
	buckets := []bucket{
		{"↑", u.PromptTokens},
		{"↓", u.CompletionTokens},
		{"C", u.CachedTokens},
		{"W", u.CacheWriteTokens},
	}
	var parts []string
	for _, b := range buckets {
		if b.n > 0 {
			parts = append(parts, b.prefix+components.FormatTokens(b.n))
		}
	}
	if t := u.ContextTokens(); t > 0 {
		parts = append(parts, "Σ"+components.FormatTokens(t))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// oracle: naive bucket-table reference (above) over randomized usage mixes;
// formatUsageStats must agree on ordering, gating, and joining for every mix.
func TestFormatUsageStats_MatchesNaiveReference_Differential(t *testing.T) {
	values := []int{0, 0, 5, 42, 800, 1200, 9999, 65432, 1000000, 2500000}
	rng := rand.New(rand.NewSource(7))
	for i := range 250 {
		u := session.TokenUsage{
			PromptTokens:     values[rng.Intn(len(values))],
			CompletionTokens: values[rng.Intn(len(values))],
			CachedTokens:     values[rng.Intn(len(values))],
			CacheWriteTokens: values[rng.Intn(len(values))],
			TotalTokens:      values[rng.Intn(len(values))],
		}
		assert.Equal(t, naiveUsageStats(u), formatUsageStats(u), "case %d: %+v", i, u)
	}

	// Edge mixes the random draw may under-sample.
	allBuckets := session.TokenUsage{PromptTokens: 1200, CompletionTokens: 800, CachedTokens: 900,
		CacheWriteTokens: 100, TotalTokens: 3000}
	for _, u := range []session.TokenUsage{
		{},
		{CacheWriteTokens: 100},
		{CachedTokens: 1, CacheWriteTokens: 2, PromptTokens: 3, CompletionTokens: 4},
		allBuckets,
	} {
		assert.Equal(t, naiveUsageStats(u), formatUsageStats(u), "%+v", u)
	}
}

// oracle: hand-written golden row — left status at column 1, right-aligned
// update hint ending at column width-2, blanks elsewhere, all derived by
// arithmetic on the fixed state rather than by rendering it.
func TestDraw_GoldenRow_Differential(t *testing.T) {
	f := NewFooterChrome(components.DefaultTheme(), 0)
	f.SetExtensionStatus("review")
	f.SetLiveJobs(func() int { return 2 })
	f.Apply(controller.FooterMsg{Kind: controller.FooterUpdateAvailable, Latest: "v9.9"})

	const width = 60
	status := "review · 2 jobs"         // 15 cols, painted from x=1
	hint := "9.9 available · a1 update" // 25 cols, right-aligned to width-2
	hintX := width - len([]rune(hint)) - 1
	fill := hintX - (1 + len([]rune(status)))
	expected := " " + status + strings.Repeat(" ", fill) + hint + " "

	row := strings.TrimSuffix(
		components.SurfaceText(f.Draw(components.DrawContext{Method: xui.WidthUnicode}, width)), "\n")
	assert.Equal(t, expected, row)
}
