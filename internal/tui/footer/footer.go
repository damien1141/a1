package footer

import (
	"fmt"
	"strings"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/layout"
	"github.com/damien1141/a1/internal/components/status"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/controller"
)

type labelComposer interface {
	SetBottomLeftLabel(layout.BorderLabel)
	ClearBottomLeftLabel()
}

// FooterChrome owns the composer status slot (activity ↔ tokens), spinner,
// and the bottom footer row reserved for extension/ambient chrome.
type FooterChrome struct {
	theme         components.Theme
	spin          *status.Spinner
	activity      *controller.ActivityHandler
	contextWindow int
	lastUsage     session.TokenUsage
	updateHint    string
	hookStatus    string
	tick          int

	composer     labelComposer
	labelContext func() session.Snapshot
	liveJobs     func() int
}

// NewFooterChrome builds footer chrome with a fresh spinner and activity handler.
func NewFooterChrome(theme components.Theme, contextWindow int) *FooterChrome {
	spin := status.NewSpinner(theme.ToolName)
	f := &FooterChrome{
		theme:         theme,
		spin:          spin,
		activity:      controller.NewActivityHandler(spin),
		contextWindow: contextWindow,
	}
	f.activity.SetOnChange(f.syncStatusSlot)
	return f
}

// Spinner returns the shared spinner (e.g. for TranscriptPane mapper).
func (f *FooterChrome) Spinner() *status.Spinner {
	if f == nil {
		return nil
	}
	return f.spin
}

// Activity returns the activity handler.
func (f *FooterChrome) Activity() *controller.ActivityHandler {
	if f == nil {
		return nil
	}
	return f.activity
}

// BindComposer wires the composer for status-slot updates.
func (f *FooterChrome) BindComposer(c labelComposer) {
	if f != nil {
		f.composer = c
		f.syncStatusSlot()
	}
}

// SetLabelContext supplies snap for activity status labels.
func (f *FooterChrome) SetLabelContext(fn func() session.Snapshot) {
	if f != nil {
		f.labelContext = fn
	}
}

// SetLiveJobs supplies live sub-agent job count for the footer row.
func (f *FooterChrome) SetLiveJobs(fn func() int) {
	if f != nil {
		f.liveJobs = fn
	}
}

// AdvanceTick drives spinner animation during active work.
func (f *FooterChrome) AdvanceTick() {
	if f == nil {
		return
	}
	f.tick++
	if f.activity.ShowSpinner() && f.tick%4 == 0 && f.spin != nil {
		f.spin.Tick()
		f.syncStatusSlot()
	}
}

// SyncFromSnap refreshes activity from the session snapshot.
func (f *FooterChrome) SyncFromSnap(snap session.Snapshot) {
	if f == nil || f.activity == nil {
		return
	}
	f.activity.SyncFromSnap(snap)
	f.syncStatusSlot()
}

// SetTheme updates footer chrome styling.
func (f *FooterChrome) SetTheme(th components.Theme) {
	if f == nil {
		return
	}
	f.theme = th
	if f.spin != nil {
		f.spin.Style = th.ToolName
	}
	f.syncStatusSlot()
}

// UpdateTokenDisplay stores usage and refreshes the status slot. Zero usage
// clears the label, so a session switch never leaves the previous session's
// counts on screen.
func (f *FooterChrome) UpdateTokenDisplay(usage session.TokenUsage) {
	if f == nil {
		return
	}
	f.lastUsage = usage
	f.syncStatusSlot()
}

// ClearTokenDisplay clears stored usage and refreshes the status slot.
func (f *FooterChrome) ClearTokenDisplay() {
	if f == nil {
		return
	}
	f.UpdateTokenDisplay(session.TokenUsage{})
}

// SetExtensionStatus sets the extension status shown on the bottom footer row.
func (f *FooterChrome) SetExtensionStatus(status string) {
	if f != nil {
		f.hookStatus = status
	}
}

// Apply handles footer bus messages.
func (f *FooterChrome) Apply(msg controller.FooterMsg) {
	if f == nil {
		return
	}
	switch msg.Kind {
	case controller.FooterSetActivity:
		f.activity.Apply(msg.Activity)
	case controller.FooterClearIfActivity:
		if f.activity.Current == msg.If {
			f.activity.Apply(controller.ActivityIdle)
		}
	case controller.FooterUpdateAvailable:
		latest := strings.TrimPrefix(msg.Latest, "v")
		f.updateHint = latest + " available · phi update"
	}
}

// ApplySessionEffects applies toast/status from session lifecycle extensions.
func (f *FooterChrome) ApplySessionEffects(msg controller.ExtSessionEffectsMsg) {
	if f == nil {
		return
	}
	if msg.StatusSet {
		f.hookStatus = msg.Status
	}
}

// syncStatusSlot writes the composer bottom-left label: activity while busy, else tokens.
func (f *FooterChrome) syncStatusSlot() {
	if f == nil || f.composer == nil {
		return
	}
	var snap session.Snapshot
	if f.labelContext != nil {
		snap = f.labelContext()
	}
	if msg := f.activity.Label(snap); msg != "" {
		f.composer.SetBottomLeftLabel(f.activityStatusLabel(msg))
		return
	}
	if !f.lastUsage.Reported() {
		f.composer.ClearBottomLeftLabel()
		return
	}
	label := tokenStatusLabel(f.theme, f.lastUsage, f.contextWindow)
	if !label.Visible() {
		f.composer.ClearBottomLeftLabel()
		return
	}
	f.composer.SetBottomLeftLabel(label)
}

func (f *FooterChrome) activityStatusLabel(msg string) layout.BorderLabel {
	// Ambient chrome + typing-color sheen: one frame dialect, motion without a
	// competing brand hue (no ToolName cyan on the border).
	dim := ChromeLabelStyle(f.theme)
	if !f.activity.ShowSpinner() || f.spin == nil {
		return layout.BorderLabel{Text: msg, Style: dim}
	}
	on := f.theme.Foreground
	spans := make([]layout.BorderSpan, 0, len(msg)+1)
	f.spin.ForEachFlowCell(msg, func(ch string, lit bool) {
		st := dim
		if lit {
			st = on
		}
		spans = append(spans, layout.BorderSpan{Text: ch, Style: st})
	})
	return layout.BorderLabel{Spans: spans}
}

// Draw renders the bottom footer row (extension status, jobs, update hint).
// Activity/spinner live on the composer status slot, not here.
func (f *FooterChrome) Draw(ctx components.DrawContext, width int) components.Surface {
	if f == nil {
		return components.NewSurface(width, 1, nil)
	}
	footer := components.NewSurface(width, 1, nil)
	dim := f.theme.Muted
	var parts []string
	if hs := strings.TrimSpace(f.hookStatus); hs != "" {
		parts = append(parts, hs)
	}
	if f.liveJobs != nil {
		if n := f.liveJobs(); n > 0 {
			jobBit := fmt.Sprintf("%d job", n)
			if n != 1 {
				jobBit += "s"
			}
			parts = append(parts, jobBit)
		}
	}
	msg := strings.Join(parts, " · ")

	x := 1
	if msg != "" {
		footer.Print(x, 0, msg, dim, ctx.Method)
		x += xui.StringWidth(msg, ctx.Method)
	}

	hint := strings.TrimSpace(f.updateHint)
	if hint != "" {
		hw := xui.StringWidth(hint, ctx.Method)
		hx := width - hw - 1
		hx = max(hx, x+2)
		if hx+hw <= width {
			st := f.theme.TitleOrForeground()
			st.Bold = false
			footer.Print(hx, 0, hint, st, ctx.Method)
		}
	}
	return footer
}
