package footer

import (
	"fmt"
	"strings"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/chrome"
	"github.com/damien1141/a1/internal/components/layout"
	"github.com/damien1141/a1/internal/components/status"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/controller"
)

type labelComposer interface {
	SetTopCenterLabel(layout.BorderLabel)
	ClearTopCenterLabel()
	SetBottomLeftLabel(layout.BorderLabel)
	ClearBottomLeftLabel()
	SetTopLeftLabel(layout.BorderLabel)
	ClearTopLeftLabel()
}

// FooterChrome owns the composer status slot (activity ↔ tokens), spinner,
// and the bottom footer row reserved for extension/ambient chrome.
type FooterChrome struct {
	theme         components.Theme
	spin          *status.Spinner
	activity      *controller.ActivityHandler
	contextWindow int
	// windowFn is a live context-window source (the engine's resolved
	// runtime window). When set it takes precedence over the fixed
	// constructor value, so the ctx label's denominator tracks model switches
	// and learned windows without rebuilding the footer.
	windowFn   func() int
	lastUsage  session.TokenUsage
	updateHint string
	hookStatus string
	tick       int
	// actionsHint overrides the status slot while the transcript cursor is
	// active. The activity label and token readout both yield to it, because the
	// keys that leave the mode matter more than either.
	actionsHint string

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
	if f.composer != nil {
		if usage.TPS > 0 {
			f.composer.SetTopLeftLabel(layout.BorderLabel{
				Text:  fmt.Sprintf("%.0f tok/s", usage.TPS),
				Style: f.theme.IdentityOrSuccess(),
			})
		} else {
			f.composer.ClearTopLeftLabel()
		}
	}
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
		if msg.Activity == controller.ActivityStreaming {
			f.composer.ClearTopCenterLabel()
		}
	case controller.FooterClearIfActivity:
		if f.activity.Current == msg.If {
			f.activity.Apply(controller.ActivityIdle)
			f.composer.ClearTopCenterLabel()
		}
	case controller.FooterUpdateAvailable:
		latest := strings.TrimPrefix(msg.Latest, "v")
		f.updateHint = latest + " available · a1 update"
	case controller.FooterTurnEnd:
		if f.composer == nil {
			break
		}
		text := ""
		if strings.TrimSpace(msg.Label) != "" {
			text = strings.TrimSpace(msg.Label)
		} else {
			snap := session.Snapshot{}
			if f.labelContext != nil {
				snap = f.labelContext()
			}
			for i := len(snap.Messages) - 1; i >= 0; i-- {
				if snap.Messages[i].Role == session.RoleAssistant {
					if snap.Messages[i].State != session.StateStreaming {
						text = snap.Messages[i].State.String()
					}
					break
				}
			}
		}
		if text == "" {
			break
		}
		// A glyph and a real colour, not the bare state word in success
		// green: a failed turn and a clean one used to look identical at
		// the exact spot the eye checks. Throughput is NOT repeated here
		// — it already lives next to the ctx label via tokenStatusLabel,
		// and printing it twice in two formats made the corner noisy.
		glyph, style, word := chrome.TurnEndLabel(text, f.theme)
		label := layout.BorderLabel{Text: word, Style: style}
		if glyph != "" {
			label = layout.BorderLabel{Spans: []layout.BorderSpan{
				{Text: glyph + " ", Style: style},
				{Text: word, Style: style},
			}}
		}
		f.composer.SetTopLeftLabel(label)
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

// ContextWindow returns the model's usable context window, or 0 when unknown.
// A live window source (SetWindowSource) takes precedence over the fixed
// constructor value.
func (f *FooterChrome) ContextWindow() int {
	return f.currentWindow()
}

// SetWindowSource supplies a live context-window source (e.g. the engine's
// resolved runtime window: config → probe → assumed → learned). When set it
// takes precedence over the constructor value, so the ctx label re-scales on
// model switches without rebuilding the footer.
func (f *FooterChrome) SetWindowSource(fn func() int) {
	if f == nil {
		return
	}
	f.windowFn = fn
	f.syncStatusSlot()
}

// SyncNow repaints the status slot — used when a model switch changed the
// ctx denominator without a new usage event to trigger a repaint.
func (f *FooterChrome) SyncNow() {
	if f == nil {
		return
	}
	f.syncStatusSlot()
}

// currentWindow resolves the display window: the live source when it yields
// a positive value, else the constructor value.
func (f *FooterChrome) currentWindow() int {
	if f == nil {
		return 0
	}
	if f.windowFn != nil {
		if w := f.windowFn(); w > 0 {
			return w
		}
	}
	return f.contextWindow
}

// SetActionsHint shows transcript-cursor copy in the status slot. Empty restores
// the activity / token readout.
func (f *FooterChrome) SetActionsHint(hint string) {
	if f == nil {
		return
	}
	f.actionsHint = strings.TrimSpace(hint)
	f.syncStatusSlot()
}

// syncStatusSlot writes the composer bottom-left label.
//
// The slot used to be a strict mux — cursor hint OR activity OR tokens OR
// nothing — which meant context occupancy, the one number that tells you
// whether the turn will survive, disappeared the moment anything streamed, the
// moment the transcript cursor was active, and entirely on a fresh session.
// Transient information was evicting persistent information.
//
// Now there are two halves: the PERSISTENT ctx readout (never yields), and one
// TRANSIENT message beside it, in priority order — cursor hint, activity, then
// the fuller token stats. When nothing transient is happening the slot still
// says something useful instead of going blank.
func (f *FooterChrome) syncStatusSlot() {
	if f == nil || f.composer == nil {
		return
	}
	ctxLabel := f.contextReadout()

	if f.actionsHint != "" {
		f.composer.SetBottomLeftLabel(f.joinLabels(ctxLabel, f.activityStatusLabel(f.actionsHint)))
		return
	}
	var snap session.Snapshot
	if f.labelContext != nil {
		snap = f.labelContext()
	}
	if msg := f.activity.Label(snap); msg != "" {
		// While busy, say how to stop it. There was no keyboard affordance
		// anywhere in the streaming state, at the exact moment the user most
		// wants one.
		transient := f.activityStatusLabel(msg + chrome.Sep + "esc interrupt")
		f.composer.SetBottomLeftLabel(f.joinLabels(ctxLabel, transient))
		return
	}
	if !f.lastUsage.Reported() {
		// Idle with nothing measured. Emit the ctx readout when there is one,
		// and otherwise NOTHING — an explicit ClearTokenDisplay() must be able
		// to drop the slot entirely, so this cannot fabricate a placeholder.
		if ctxLabel.Visible() {
			f.composer.SetBottomLeftLabel(ctxLabel)
			return
		}
		f.composer.ClearBottomLeftLabel()
		return
	}
	stats := f.statReadout()
	f.composer.SetBottomLeftLabel(f.joinLabels(ctxLabel, stats))
}

// contextReadout is the always-present ctx occupancy label, or an empty label
// when the window is unknown (a fresh session before the first usage event).
func (f *FooterChrome) contextReadout() layout.BorderLabel {
	if f == nil {
		return layout.BorderLabel{}
	}
	text := formatContextLabel(f.lastUsage, f.currentWindow())
	if text == "" {
		return layout.BorderLabel{}
	}
	return layout.BorderLabel{
		Text:  text,
		Style: contextPressureStyle(f.theme, f.lastUsage, f.currentWindow()),
	}
}

// statReadout is the fuller token breakdown, shown only in the idle case where
// the ctx readout alone would be the whole story.
func (f *FooterChrome) statReadout() layout.BorderLabel {
	if f == nil {
		return layout.BorderLabel{}
	}
	stats := formatUsageStats(f.lastUsage)
	if stats == "" {
		return layout.BorderLabel{}
	}
	return layout.BorderLabel{Text: stats, Style: ChromeLabelStyle(f.theme)}
}

// joinLabels puts two border labels side by side with the product separator,
// dropping either when it is empty. Spans from both are preserved so a spinner
// keeps its per-cell styling.
func (f *FooterChrome) joinLabels(parts ...layout.BorderLabel) layout.BorderLabel {
	var spans []layout.BorderSpan
	for _, p := range parts {
		if !p.Visible() {
			continue
		}
		if len(spans) > 0 {
			spans = append(spans, layout.BorderSpan{Text: chrome.Sep, Style: ChromeLabelStyle(f.theme)})
		}
		if len(p.Spans) > 0 {
			spans = append(spans, p.Spans...)
			continue
		}
		spans = append(spans, layout.BorderSpan{Text: p.Text, Style: p.Style})
	}
	if len(spans) == 0 {
		return layout.BorderLabel{}
	}
	return layout.BorderLabel{Spans: spans}
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
	msg := strings.Join(parts, chrome.Sep)

	// Reserve the right-aligned update hint BEFORE laying out the left cluster.
	// The old order placed the left side first, then clamped the hint to start
	// two columns past it — so a long extension status made the update notice
	// vanish rather than truncate, silently losing an actionable message.
	hint := strings.TrimSpace(f.updateHint)
	hintW := 0
	if hint != "" {
		hintW = xui.StringWidth(hint, ctx.Method)
	}
	// The hint yields when keeping it would squeeze the left cluster below a
	// readable width. Reserved-but-cramped was worse than dropped: at width 30
	// the hint alone is 27 columns, leaving "re" for the status — technically
	// both present, useless to read.
	const minLeftWidth = 12
	leftLimit := width
	if hintW > 0 && width-hintW-2 >= minLeftWidth {
		leftLimit = width - hintW - 2
	} else {
		hintW = 0 // no room to show it without gutting the left cluster
	}

	x := 1
	if msg != "" {
		msg = layout.TruncateToWidth(msg, max(leftLimit-x, 1), ctx.Method)
		footer.Print(x, 0, msg, dim, ctx.Method)
		x += xui.StringWidth(msg, ctx.Method)
	}

	if hint != "" {
		hx := width - hintW - 1
		hx = max(hx, x+2)
		// The hint is skipped, never wrapped or overlapped, when the row cannot
		// hold both. Reserving its width above means this only happens when the
		// terminal is genuinely too narrow — the old code lost the notice to any
		// long extension status, which is what made it look flaky.
		if hx >= 0 && hx+hintW <= width {
			st := f.theme.TitleOrForeground()
			st.Bold = false
			footer.Print(hx, 0, hint, st, ctx.Method)
		}
	}
	return footer
}
