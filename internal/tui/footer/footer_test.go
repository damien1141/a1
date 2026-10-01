package footer

import (
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/layout"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/controller"
)

type stubComposer struct {
	label       layout.BorderLabel
	set         bool
	topCenter   layout.BorderLabel
	topCenterSet bool
	topLeft     layout.BorderLabel
	topLeftSet  bool
}

func (s *stubComposer) SetBottomLeftLabel(label layout.BorderLabel) {
	s.label = label
	s.set = true
}

func (s *stubComposer) ClearBottomLeftLabel() {
	s.label = layout.BorderLabel{}
	s.set = false
}

func (s *stubComposer) SetTopLeftLabel(label layout.BorderLabel) {
	s.topLeft = label
	s.topLeftSet = true
}

func (s *stubComposer) ClearTopLeftLabel() {
	s.topLeft = layout.BorderLabel{}
	s.topLeftSet = false
}

func (s *stubComposer) SetTopCenterLabel(label layout.BorderLabel) {
	s.topCenter = label
	s.topCenterSet = true
}

func (s *stubComposer) ClearTopCenterLabel() {
	s.topCenter = layout.BorderLabel{}
	s.topCenterSet = false
}

func TestDrawCombinesParts(t *testing.T) {
	f := NewFooterChrome(components.DefaultTheme(), 0)
	f.SetExtensionStatus(" review ")
	f.SetLiveJobs(func() int { return 2 })
	f.Activity().Apply(controller.ActivityCancelled)

	row := components.SurfaceText(f.Draw(components.DrawContext{Method: xui.WidthUnicode}, 40))
	assert.Contains(t, row, "review · 2 jobs")
	assert.NotContains(t, row, "Stopped")
}

func TestDrawSingleJobSingular(t *testing.T) {
	f := NewFooterChrome(components.DefaultTheme(), 0)
	f.SetLiveJobs(func() int { return 1 })

	row := components.SurfaceText(f.Draw(components.DrawContext{Method: xui.WidthUnicode}, 40))
	assert.Contains(t, row, "1 job")
}

func TestDrawEmptyFooter(t *testing.T) {
	f := NewFooterChrome(components.DefaultTheme(), 0)

	row := components.SurfaceText(f.Draw(components.DrawContext{Method: xui.WidthUnicode}, 40))
	assert.Empty(t, strings.TrimSpace(row))
}

func TestUpdateTokenDisplayZeroClearsLabel(t *testing.T) {
	f := NewFooterChrome(components.DefaultTheme(), 128000)
	comp := &stubComposer{}
	f.BindComposer(comp)

	f.UpdateTokenDisplay(session.TokenUsage{PromptTokens: 1200, TotalTokens: 1200})
	require.True(t, comp.set)

	// A session switch to one without usage must drop the stale counts.
	f.UpdateTokenDisplay(session.TokenUsage{})
	assert.False(t, comp.set)
	assert.Empty(t, comp.label.Text)
}

func TestUpdateTokenDisplayTPS(t *testing.T) {
	f := NewFooterChrome(components.DefaultTheme(), 128000)
	comp := &stubComposer{}
	f.BindComposer(comp)

	f.UpdateTokenDisplay(session.TokenUsage{CompletionTokens: 100, TPS: 42.7})
	assert.Equal(t, "43 tok/s", comp.topLeft.Text)
	assert.Equal(t, f.theme.IdentityOrSuccess(), comp.topLeft.Style)

	// Zero TPS clears the label.
	f.UpdateTokenDisplay(session.TokenUsage{CompletionTokens: 100})
	assert.Empty(t, comp.topLeft.Text)
}

func TestClearTokenDisplayDropsLabel(t *testing.T) {
	f := NewFooterChrome(components.DefaultTheme(), 128000)
	comp := &stubComposer{}
	f.BindComposer(comp)

	f.UpdateTokenDisplay(session.TokenUsage{PromptTokens: 1200, TotalTokens: 1200})
	require.True(t, comp.set)

	f.ClearTokenDisplay()
	assert.False(t, comp.set)
}

func TestStatusSlotSwapsActivityAndTokens(t *testing.T) {
	f := NewFooterChrome(components.DefaultTheme(), 128000)
	comp := &stubComposer{}
	f.BindComposer(comp)

	f.UpdateTokenDisplay(session.TokenUsage{
		PromptTokens:     1200,
		CompletionTokens: 800,
		TotalTokens:      2000,
	})
	require.True(t, comp.set)
	assert.Contains(t, comp.label.Text, "↑1.2k")
	assert.Contains(t, comp.label.Text, "↓800")

	f.Activity().Apply(controller.ActivityStreaming)
	require.True(t, comp.set)
	assert.Empty(t, comp.label.Text)
	require.NotEmpty(t, comp.label.Spans)
	var joined strings.Builder
	for _, sp := range comp.label.Spans {
		joined.WriteString(sp.Text)
	}
	assert.Equal(t, "Generating…", joined.String())
	lit := 0
	fg := components.DefaultTheme().Foreground
	for _, sp := range comp.label.Spans {
		if sp.Style == fg {
			lit++
		}
	}
	assert.Positive(t, lit)

	f.Activity().Apply(controller.ActivityIdle)
	assert.Contains(t, comp.label.Text, "↑1.2k")
	assert.Empty(t, comp.label.Spans)
}
