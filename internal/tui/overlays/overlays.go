package overlays

import (
	"fmt"
	"strings"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/chrome"
	"github.com/damien1141/a1/internal/components/layout"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/tui/controller"
)

type overlayComposer interface {
	HideCompleters()
	HidePalette()
}

// Overlays owns permission, continue-ask, and extension-confirm UI that replaces the composer slot.
type Overlays struct {
	theme    components.Theme
	perm     *permAskState
	cont     *continueAskState
	confirm  *confirmAskState
	question *questionAskState
	activity *controller.ActivityHandler
	composer overlayComposer

	focusEditor func()
	focusChat   func()
}

// NewOverlays builds overlay state handlers.
func NewOverlays(
	theme components.Theme,
	activity *controller.ActivityHandler,
	composer overlayComposer,
	focusEditor, focusChat func(),
) *Overlays {
	return &Overlays{
		theme:       theme,
		activity:    activity,
		composer:    composer,
		focusEditor: focusEditor,
		focusChat:   focusChat,
	}
}

// SetTheme updates overlay chrome styling.
func (o *Overlays) SetTheme(th components.Theme) {
	if o != nil {
		o.theme = th
	}
}

// Active reports whether a modal overlay is showing.
func (o *Overlays) Active() bool {
	return o != nil && (o.perm != nil || o.cont != nil || o.confirm != nil)
}

// BlocksComposer reports whether composer input should be disabled.
func (o *Overlays) BlocksComposer() bool {
	return o.Active()
}

// PermissionActive reports whether the permission overlay is showing.
func (o *Overlays) PermissionActive() bool {
	return o != nil && o.perm != nil
}

// ContinueActive reports whether the continue overlay is showing.
func (o *Overlays) ContinueActive() bool {
	return o != nil && o.cont != nil
}

// Apply routes overlay bus messages by Kind.
func (o *Overlays) Apply(msg controller.OverlayMsg) {
	if o == nil {
		return
	}
	switch msg.Kind {
	case controller.OverlayPermissionAsk:
		o.beginPermissionAsk(msg)
	case controller.OverlayPermissionDismiss:
		o.dismissPermission()
	case controller.OverlayContinueAsk:
		o.beginContinueAsk(msg)
	case controller.OverlayContinueDismiss:
		o.dismissContinue()
	case controller.OverlayExtConfirm:
		o.beginExtConfirm(msg)
	case controller.OverlayExtConfirmDismiss:
		o.dismissExtConfirm()
	case controller.OverlayQuestionAsk:
		o.beginQuestionAsk(msg)
	case controller.OverlayQuestionDismiss:
		o.dismissQuestion()
	}
}

// HandleQuestionKey handles keyboard input while a question overlay is active.
func (o *Overlays) HandleQuestionKey(ctx *components.EventContext, e xui.KeyEvent) bool {
	return o != nil && o.question != nil && o.handleQuestionKey(ctx, e)
}

// QuestionActive reports whether a question overlay is capturing keys.
func (o *Overlays) QuestionActive() bool { return o != nil && o.question != nil }

// HandlePermissionKey handles keyboard input while permission ask is active.
func (o *Overlays) HandlePermissionKey(ctx *components.EventContext, e xui.KeyEvent) bool {
	return o != nil && o.perm != nil && o.handlePermissionKey(ctx, e)
}

// HandleContinueKey handles keyboard input while continue ask is active.
func (o *Overlays) HandleContinueKey(ctx *components.EventContext, e xui.KeyEvent) bool {
	return o != nil && o.cont != nil && o.handleContinueKey(ctx, e)
}

// ResolvePermission sends a permission reply and clears the overlay.
func (o *Overlays) ResolvePermission(r controller.AskReply) {
	o.resolvePermission(r)
}

// ResolveContinue sends a continue reply and clears the overlay.
func (o *Overlays) ResolveContinue(r controller.ContinueReply) {
	o.resolveContinue(r)
}

// PreferredBottomHeight estimates rows for the bottom overlay or composer slot.
func (o *Overlays) PreferredBottomHeight(width int, method xui.WidthMethod) (height int, overlay bool) {
	if o == nil {
		return 0, false
	}
	if o.perm != nil {
		return o.perm.preferredAskHeight(width, method), true
	}
	if o.cont != nil {
		return o.cont.preferredAskHeight(), true
	}
	if o.confirm != nil {
		return o.confirm.preferredAskHeight(), true
	}
	if o.question != nil {
		return o.question.preferredQuestionHeight(width, method), true
	}
	return 0, false
}

// DrawBottom renders the overlay panel when active.
func (o *Overlays) DrawBottom(ctx components.DrawContext, width, height int) (components.Surface, bool) {
	if o == nil {
		return components.Surface{}, false
	}
	if o.perm != nil {
		return o.drawPermissionAsk(ctx, width, height), true
	}
	if o.cont != nil {
		return o.drawContinueAsk(ctx, width, height), true
	}
	if o.confirm != nil {
		return o.drawExtConfirm(ctx, width, height), true
	}
	if o.question != nil {
		return o.drawQuestionAsk(ctx, width, height), true
	}
	return components.Surface{}, false
}

func (o *Overlays) beginPermissionAsk(msg controller.OverlayMsg) {
	if o.perm != nil {
		o.resolvePermission(controller.AskReply{})
	}
	if o.cont != nil {
		o.resolveContinue(controller.ContinueReply{})
	}
	if o.confirm != nil {
		o.resolveExtConfirm(controller.ExtConfirmReply{})
	}
	// A question supersedes a live permission ask, not the other way round: the
	// agent asks before it acts, so the answer is what unblocks the turn.
	if o.question != nil {
		o.resolveQuestion(controller.QuestionReply{Cancelled: true})
	}
	if o.composer != nil {
		o.composer.HideCompleters()
		o.composer.HidePalette()
	}
	o.perm = newPermAskState(msg.Request, msg.Reason, msg.PermReply)
	o.activity.Apply(controller.ActivityAwaitingApproval)
	if o.focusEditor != nil {
		o.focusEditor()
	}
}

func (o *Overlays) dismissPermission() {
	wasAsk := o.perm != nil
	o.perm = nil
	if !wasAsk {
		return
	}
	if o.activity != nil && o.activity.Current == controller.ActivityAwaitingApproval {
		o.activity.Apply(controller.ActivityTools)
	}
	if o.focusChat != nil {
		o.focusChat()
	}
}

func (o *Overlays) resolvePermission(r controller.AskReply) {
	st := o.perm
	if st == nil {
		return
	}
	o.perm = nil
	if o.activity != nil && o.activity.Current == controller.ActivityAwaitingApproval {
		o.activity.Apply(controller.ActivityTools)
	}
	if o.focusChat != nil {
		o.focusChat()
	}
	if st.reply != nil {
		select {
		case st.reply <- r:
		default:
		}
	}
}

func (o *Overlays) beginContinueAsk(msg controller.OverlayMsg) {
	if o.cont != nil {
		o.resolveContinue(controller.ContinueReply{})
	}
	if o.perm != nil {
		o.resolvePermission(controller.AskReply{})
	}
	if o.confirm != nil {
		o.resolveExtConfirm(controller.ExtConfirmReply{})
	}
	if o.composer != nil {
		o.composer.HideCompleters()
		o.composer.HidePalette()
	}
	o.cont = newContinueAskState(msg.MaxRounds, msg.ContReply)
	o.activity.Apply(controller.ActivityAwaitingApproval)
	if o.focusEditor != nil {
		o.focusEditor()
	}
}

func (o *Overlays) dismissContinue() {
	wasAsk := o.cont != nil
	o.cont = nil
	if !wasAsk {
		return
	}
	if o.activity != nil && o.activity.Current == controller.ActivityAwaitingApproval {
		o.activity.Apply(controller.ActivityTools)
	}
	if o.focusChat != nil {
		o.focusChat()
	}
}

func (o *Overlays) resolveContinue(r controller.ContinueReply) {
	st := o.cont
	if st == nil {
		return
	}
	o.cont = nil
	if o.activity != nil && o.activity.Current == controller.ActivityAwaitingApproval {
		o.activity.Apply(controller.ActivityTools)
	}
	if o.focusChat != nil {
		o.focusChat()
	}
	if st.reply != nil {
		select {
		case st.reply <- r:
		default:
		}
	}
}

func (o *Overlays) handlePermissionKey(ctx *components.EventContext, e xui.KeyEvent) bool {
	st := o.perm
	if st == nil || !e.Press {
		return false
	}

	if st.feedbackMode {
		return o.handlePermissionFeedbackKey(ctx, e)
	}

	switch e.Code {
	case xui.KeyEscape:
		o.resolvePermission(controller.AskReply{})
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyUp, xui.KeyLeft:
		if st.selected > 0 {
			st.selected--
		} else {
			st.selected = len(askOptionLabels) - 1
		}
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyDown, xui.KeyRight, xui.KeyTab:
		st.selected = (st.selected + 1) % len(askOptionLabels)
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyEnter:
		o.acceptPermissionOption(askOption(st.selected))
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyRune:
		if e.Mods.Has(xui.ModCtrl) || e.Mods.Has(xui.ModAlt) {
			ctx.ConsumeAndRedraw()
			return true
		}
		switch e.Rune {
		case 'k', 'K', 'h', 'H':
			if st.selected > 0 {
				st.selected--
			}
			ctx.ConsumeAndRedraw()
			return true
		case 'j', 'J', 'l', 'L':
			st.selected = (st.selected + 1) % len(askOptionLabels)
			ctx.ConsumeAndRedraw()
			return true
		}
	}
	ctx.ConsumeAndRedraw()
	return true
}

func (o *Overlays) acceptPermissionOption(opt askOption) {
	st := o.perm
	if st == nil {
		return
	}
	switch opt {
	case askOptApprove:
		o.resolvePermission(controller.AskReply{Approved: true})
	case askOptAllowForever:
		pattern := allowlistPatternForRequest(st.req)
		o.resolvePermission(controller.AskReply{Approved: true, AllowPersistent: true, AllowlistPattern: pattern})
	case askOptDeny:
		o.resolvePermission(controller.AskReply{})
	}
}

func allowlistPatternForRequest(req permission.Request) string {
	switch req.Action {
	case permission.ActionBash:
		cmd := strings.TrimSpace(req.Command)
		if cmd != "" {
			return cmd
		}
	}
	return req.Tool
}

func (o *Overlays) handlePermissionFeedbackKey(ctx *components.EventContext, e xui.KeyEvent) bool {
	st := o.perm
	if st == nil {
		return false
	}
	switch e.Code {
	case xui.KeyEscape:
		st.feedbackMode = false
		st.feedback = ""
		st.feedbackCur = 0
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyEnter:
		fb := strings.TrimSpace(st.feedback)
		o.resolvePermission(controller.AskReply{Feedback: fb})
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyBackspace:
		runes := []rune(st.feedback)
		if st.feedbackCur > 0 && st.feedbackCur <= len(runes) {
			st.feedback = string(append(runes[:st.feedbackCur-1], runes[st.feedbackCur:]...))
			st.feedbackCur--
		}
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyLeft:
		if st.feedbackCur > 0 {
			st.feedbackCur--
		}
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyRight:
		if st.feedbackCur < len([]rune(st.feedback)) {
			st.feedbackCur++
		}
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyRune:
		if e.Mods.Has(xui.ModCtrl) || e.Mods.Has(xui.ModAlt) {
			ctx.ConsumeAndRedraw()
			return true
		}
		runes := []rune(st.feedback)
		ch := string(e.Rune)
		st.feedback = string(append(runes[:st.feedbackCur], append([]rune(ch), runes[st.feedbackCur:]...)...))
		st.feedbackCur++
		ctx.ConsumeAndRedraw()
		return true
	}
	ctx.ConsumeAndRedraw()
	return true
}

func (o *Overlays) handleContinueKey(ctx *components.EventContext, e xui.KeyEvent) bool {
	st := o.cont
	if st == nil || !e.Press {
		return false
	}

	switch e.Code {
	case xui.KeyEscape:
		o.resolveContinue(controller.ContinueReply{})
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyUp, xui.KeyLeft:
		if st.selected > 0 {
			st.selected--
		} else {
			st.selected = len(continueOptionLabels) - 1
		}
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyDown, xui.KeyRight, xui.KeyTab:
		st.selected = (st.selected + 1) % len(continueOptionLabels)
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyEnter:
		o.acceptContinueOption(st.selected)
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyRune:
		if e.Mods.Has(xui.ModCtrl) || e.Mods.Has(xui.ModAlt) {
			ctx.ConsumeAndRedraw()
			return true
		}
		switch e.Rune {
		case 'k', 'K', 'h', 'H':
			if st.selected > 0 {
				st.selected--
			}
			ctx.ConsumeAndRedraw()
			return true
		case 'j', 'J', 'l', 'L':
			st.selected = (st.selected + 1) % len(continueOptionLabels)
			ctx.ConsumeAndRedraw()
			return true
		}
	}
	ctx.ConsumeAndRedraw()
	return true
}

func (o *Overlays) acceptContinueOption(idx int) {
	switch idx {
	case 0:
		o.resolveContinue(controller.ContinueReply{Continue: true})
	default:
		o.resolveContinue(controller.ContinueReply{})
	}
}

func (o *Overlays) drawPermissionAsk(ctx components.DrawContext, width, height int) components.Surface {
	st := o.perm
	if st == nil {
		return components.NewSurface(width, height, nil)
	}
	th := o.theme
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = st.preferredAskHeight(width, ctx.Method)
	}
	innerW := width - 4
	if innerW < 10 {
		innerW = width
	}

	primary := chrome.DecisionPrimary(th)

	var body []components.RichLine
	add := func(spans ...components.Span) {
		body = append(body, components.WrapSpans(spans, innerW, ctx.Method)...)
	}

	if st.header != "" || st.detail != "" || st.reason != "" {
		if st.header != "" {
			add(components.Span{Text: st.header, Style: th.Foreground})
		}
		body = append(body, st.detailLines(th, innerW, ctx.Method)...)
		if st.reason != "" {
			add(components.Span{Text: "(" + st.reason + ")", Style: th.Muted})
		}
		body = append(body, components.RichLine{})
	} else {
		add(components.Span{Text: permission.Summarize(st.req), Style: th.Foreground})
		body = append(body, components.RichLine{})
	}

	if st.feedbackMode {
		body = append(body, st.feedbackLines(th, primary, innerW, ctx.Method)...)
	} else {
		body = append(body, st.optionLines(th, primary, innerW, ctx.Method)...)
	}

	return paintAskPanel(body, width, height, chrome.ModalBorder(th), ctx.Method)
}

func (o *Overlays) drawContinueAsk(ctx components.DrawContext, width, height int) components.Surface {
	st := o.cont
	if st == nil {
		return components.NewSurface(width, height, nil)
	}
	th := o.theme
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = st.preferredAskHeight()
	}
	innerW := width - 4
	if innerW < 10 {
		innerW = width
	}

	primary := chrome.DecisionPrimary(th)

	var body []components.RichLine
	body = append(body, components.WrapSpans([]components.Span{
		{
			Text:  fmt.Sprintf("Reached max tool rounds (%d). Continue for another %d?", st.maxRounds, st.maxRounds),
			Style: th.Foreground,
		},
	}, innerW, ctx.Method)...)
	body = append(body, components.RichLine{})

	for i, label := range continueOptionLabels {
		body = append(body, chrome.OptionLine(th, primary, label, i == st.selected, innerW, ctx.Method)...)
	}
	body = append(body, components.WrapSpans([]components.Span{
		{Text: chrome.AskHint("↑↓ move", "select", "stop"), Style: th.Muted},
	}, innerW, ctx.Method)...)

	return paintAskPanel(body, width, height, chrome.ModalBorder(th), ctx.Method)
}

// drawQuestionAsk renders the question overlay: a chip header, the question,
// each option with its description, and the always-present "Other" row.
func (o *Overlays) drawQuestionAsk(ctx components.DrawContext, width, height int) components.Surface {
	st := o.question
	if st == nil || st.question == nil {
		return components.NewSurface(width, height, nil)
	}
	th := o.theme
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = st.preferredQuestionHeight(width, ctx.Method)
	}
	innerW := width - 4
	if innerW < 10 {
		innerW = width
	}

	primary := chrome.DecisionPrimary(th)

	var body []components.RichLine
	add := func(spans ...components.Span) {
		body = append(body, components.WrapSpans(spans, innerW, ctx.Method)...)
	}

	if st.question.Header != "" {
		add(components.Span{Text: " " + st.question.Header + " ", Style: xui.Style{Bold: true, Fg: primary.Fg}})
	}
	add(components.Span{Text: st.question.Question, Style: th.Foreground})
	body = append(body, components.RichLine{})

	if st.customOn {
		body = append(body, st.customLines(th, primary, innerW, ctx.Method)...)
		return paintAskPanel(body, width, height, chrome.ModalBorder(th), ctx.Method)
	}

	for i, opt := range st.question.Options {
		selected := st.selected == i
		// The option line already carries a selection dot, so MultiSelect gets
		// its checkbox on the same row rather than a second prefix column.
		if st.question.MultiSelect {
			// The checkbox shares the row with the option label, on the same
			// span set — emitting it separately put it on its own line.
			mark := "[ ] "
			markSt := th.Muted
			if st.picked[i] {
				mark = "[x] "
				markSt = th.Success
			}
			body = append(
				body,
				st.optionRowMulti(th, primary, opt.Label, selected, mark, markSt, innerW, ctx.Method)...)
		} else {
			body = append(body, chrome.OptionLine(th, primary, opt.Label, selected, innerW, ctx.Method)...)
		}
		if opt.Description != "" {
			descSt := th.Muted
			if selected {
				descSt = th.Foreground
			}
			add(components.Span{Text: "    " + opt.Description, Style: descSt})
		}
	}

	// "Other" is never optional: a question with no free-text path would trap
	// the user inside the agent's framing.
	body = append(body, chrome.OptionLine(
		th, primary, "Other", st.selected == st.optionCount(), innerW, ctx.Method,
	)...)
	body = append(body, components.WrapSpans([]components.Span{
		{Text: st.hint(), Style: th.Muted},
	}, innerW, ctx.Method)...)

	return paintAskPanel(body, width, height, chrome.ModalBorder(th), ctx.Method)
}

type askOption int

const (
	askOptApprove askOption = iota
	askOptAllowForever
	askOptDeny
)

var askOptionLabels = []string{
	"Allow once",
	"Allow forever",
	"Reject",
}

var continueOptionLabels = []string{
	"Continue",
	"Stop",
}

// questionAskState is one in-flight AskUserQuestion prompt.
type questionAskState struct {
	question *controller.Question
	reply    chan controller.QuestionReply

	selected int
	// picked holds the checked indices while MultiSelect is on.
	picked map[int]bool
	// custom is the "Other" free-text answer.
	custom    string
	customCur int
	customOn  bool
}

// otherOptionIndex is the always-present escape hatch: a question without a
// free-text path would trap the user into the agent's framing.
const otherOptionIndex = -1

func newQuestionAskState(q *controller.Question, reply chan controller.QuestionReply) *questionAskState {
	return &questionAskState{question: q, reply: reply, picked: map[int]bool{}}
}

// optionCount counts the offered choices; the "Other" row is not among them,
// because it is not a choice the agent made.
func (s *questionAskState) optionCount() int {
	if s.question == nil {
		return 0
	}
	return len(s.question.Options)
}

type permAskState struct {
	req    permission.Request
	reason string
	reply  chan controller.AskReply

	header       string
	detail       string
	selected     int
	feedbackMode bool
	feedback     string
	feedbackCur  int
}

type continueAskState struct {
	maxRounds int
	reply     chan controller.ContinueReply
	selected  int
}

func formatAskHeader(req permission.Request) (header, detail string) {
	switch req.Action {
	case permission.ActionBash:
		cmd := req.Command
		lines := strings.Split(cmd, "\n")
		if len(lines) > 3 {
			cmd = strings.Join(lines[:3], "\n") + "\n..."
		}
		return "Run this command?", cmd
	case permission.ActionEdit:
		path := ""
		if len(req.Paths) > 0 {
			path = req.Paths[0]
		}
		return "Allow editing file:", path
	case permission.ActionWrite:
		path := ""
		if len(req.Paths) > 0 {
			path = req.Paths[0]
		}
		return "Allow creating file:", path
	default:
		return fmt.Sprintf("Invoke tool %s?", req.Tool), permission.Summarize(req)
	}
}

func newPermAskState(req permission.Request, reason string, reply chan controller.AskReply) *permAskState {
	h, d := formatAskHeader(req)
	return &permAskState{
		req:      req,
		reason:   reason,
		reply:    reply,
		header:   h,
		detail:   d,
		selected: 0,
	}
}

func newContinueAskState(maxRounds int, reply chan controller.ContinueReply) *continueAskState {
	return &continueAskState{
		maxRounds: maxRounds,
		reply:     reply,
		selected:  0,
	}
}

func (st *permAskState) preferredAskHeight(width int, method xui.WidthMethod) int {
	if st == nil {
		return 8
	}
	innerW := width - 4
	innerW = max(innerW, 20)
	h := 2
	h++
	if st.detail != "" {
		h += strings.Count(st.detail, "\n") + 1
	}
	if st.reason != "" {
		h++
	}
	h++
	if st.feedbackMode {
		h += 3
	} else {
		h += len(askOptionLabels)
		h++
	}
	h++
	if h < 8 {
		h = 8
	}
	_ = method
	_ = innerW
	return h
}

func (*continueAskState) preferredAskHeight() int {
	h := 2 + 1 + 1 + len(continueOptionLabels) + 1 + 1
	h = max(h, 8)
	return h
}

func (st *permAskState) detailLines(th components.Theme, innerW int, method xui.WidthMethod) []components.RichLine {
	if st.detail == "" {
		return nil
	}
	id := th.IdentityOrSuccess()
	lines := strings.Split(st.detail, "\n")
	out := make([]components.RichLine, 0, len(lines))
	for i, line := range lines {
		var spans []components.Span
		switch {
		case st.req.Action == permission.ActionBash && i == 0:
			spans = []components.Span{
				{Text: chrome.BashPrompt, Style: xui.Style{Bold: true, Fg: id.Fg}},
				{Text: line, Style: th.Foreground},
			}
		case st.req.Action == permission.ActionBash:
			spans = []components.Span{{Text: "  " + line, Style: th.Foreground}}
		default:
			spans = []components.Span{{Text: line, Style: xui.Style{Bold: true, Fg: th.Foreground.Fg}}}
		}
		out = append(out, components.WrapSpans(spans, innerW, method)...)
	}
	return out
}

func (st *permAskState) optionLines(
	th components.Theme,
	primary xui.Style,
	innerW int,
	method xui.WidthMethod,
) []components.RichLine {
	out := make([]components.RichLine, 0, len(askOptionLabels)+1)
	for i, label := range askOptionLabels {
		out = append(out, chrome.OptionLine(th, primary, label, i == st.selected, innerW, method)...)
	}
	out = append(out, components.WrapSpans([]components.Span{
		{Text: chrome.AskHint("↑↓ move", "select", "cancel"), Style: th.Muted},
	}, innerW, method)...)
	return out
}

func (st *permAskState) feedbackLines(
	th components.Theme,
	primary xui.Style,
	innerW int,
	method xui.WidthMethod,
) []components.RichLine {
	runes := []rune(st.feedback)
	if st.feedbackCur > len(runes) {
		st.feedbackCur = len(runes)
	}
	shown := string(runes[:st.feedbackCur]) + "▎" + string(runes[st.feedbackCur:])
	var out []components.RichLine
	out = append(out, components.WrapSpans([]components.Span{
		{Text: chrome.Err + " ", Style: th.Destructive},
		{Text: "Denied", Style: xui.Style{Bold: true, Fg: th.Destructive.Fg}},
		{Text: " — tell a1 what to do instead", Style: th.Muted},
	}, innerW, method)...)
	out = append(out, components.WrapSpans([]components.Span{
		{Text: chrome.SoftPrompt, Style: xui.Style{Bold: true, Fg: primary.Fg}},
		{Text: shown, Style: th.Foreground},
	}, innerW, method)...)
	out = append(out, components.WrapSpans([]components.Span{
		{Text: chrome.FeedbackHint(), Style: th.Muted},
	}, innerW, method)...)
	return out
}

func paintAskPanel(
	body []components.RichLine,
	width, height int,
	border xui.Style,
	method xui.WidthMethod,
) components.Surface {
	panel := components.NewSurface(width, height, nil)
	layout.DrawRoundedBorder(&panel, layout.BorderRounded, border, nil, nil, nil, nil, nil, method)
	y := 1
	for _, line := range body {
		if y >= height-1 {
			break
		}
		components.PaintSpans(&panel, 2, y, line, method)
		y++
	}
	return panel
}

// beginQuestionAsk opens a question overlay, superseding anything else up.
func (o *Overlays) beginQuestionAsk(msg controller.OverlayMsg) {
	if msg.Question == nil {
		return
	}
	if o.perm != nil {
		o.resolvePermission(controller.AskReply{})
	}
	if o.cont != nil {
		o.resolveContinue(controller.ContinueReply{})
	}
	if o.confirm != nil {
		o.resolveExtConfirm(controller.ExtConfirmReply{})
	}
	if o.question != nil {
		o.resolveQuestion(controller.QuestionReply{Cancelled: true})
	}
	if o.composer != nil {
		o.composer.HideCompleters()
		o.composer.HidePalette()
	}
	o.question = newQuestionAskState(msg.Question, msg.QuestReply)
	o.activity.Apply(controller.ActivityAwaitingApproval)
	if o.focusEditor != nil {
		o.focusEditor()
	}
}

func (o *Overlays) dismissQuestion() {
	wasAsk := o.question != nil
	o.question = nil
	if !wasAsk {
		return
	}
	if o.activity != nil && o.activity.Current == controller.ActivityAwaitingApproval {
		o.activity.Apply(controller.ActivityIdle)
	}
}

// resolveQuestion sends the answer and clears the overlay.
func (o *Overlays) resolveQuestion(r controller.QuestionReply) {
	st := o.question
	if st == nil {
		return
	}
	o.question = nil
	if o.activity != nil && o.activity.Current == controller.ActivityAwaitingApproval {
		o.activity.Apply(controller.ActivityTools)
	}
	if o.focusChat != nil {
		o.focusChat()
	}
	if st.reply != nil {
		select {
		case st.reply <- r:
		default:
		}
	}
}

// handleQuestionKey drives the question overlay: arrows move, Enter picks,
// Esc cancels, and typing while on "Other" collects free text.
func (o *Overlays) handleQuestionKey(ctx *components.EventContext, e xui.KeyEvent) bool {
	st := o.question
	if st == nil || !e.Press {
		return false
	}
	if st.customOn {
		return o.handleQuestionCustomKey(ctx, e)
	}

	switch e.Code {
	case xui.KeyEscape:
		o.resolveQuestion(controller.QuestionReply{Cancelled: true})
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyUp, xui.KeyLeft:
		if st.selected > 0 {
			st.selected--
		} else {
			st.selected = st.optionCount()
		}
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyDown, xui.KeyRight, xui.KeyTab:
		st.selected = (st.selected + 1) % (st.optionCount() + 1)
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyEnter:
		if st.selected == otherOptionIndex || st.selected == st.optionCount() {
			st.customOn = true
			st.customCur = len([]rune(st.custom))
			ctx.ConsumeAndRedraw()
			return true
		}
		o.acceptQuestionOption(st.selected)
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyRune:
		if e.Mods.Has(xui.ModCtrl) || e.Mods.Has(xui.ModAlt) {
			ctx.ConsumeAndRedraw()
			return true
		}
		switch e.Rune {
		case 'k', 'K', 'h', 'H':
			if st.selected > 0 {
				st.selected--
			} else {
				st.selected = st.optionCount()
			}
			ctx.ConsumeAndRedraw()
			return true
		case 'j', 'J', 'l', 'L':
			st.selected = (st.selected + 1) % (st.optionCount() + 1)
			ctx.ConsumeAndRedraw()
			return true
		case ' ':
			if st.question.MultiSelect {
				if st.selected >= 0 && st.selected < st.optionCount() {
					if st.picked[st.selected] {
						delete(st.picked, st.selected)
					} else {
						st.picked[st.selected] = true
					}
				}
				ctx.ConsumeAndRedraw()
				return true
			}
		}
	}
	ctx.ConsumeAndRedraw()
	return true
}

// handleQuestionCustomKey collects the "Other" free-text answer.
func (o *Overlays) handleQuestionCustomKey(ctx *components.EventContext, e xui.KeyEvent) bool {
	st := o.question
	switch e.Code {
	case xui.KeyEscape:
		st.customOn = false
		st.custom = ""
		st.customCur = 0
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyEnter:
		answer := strings.TrimSpace(st.custom)
		if answer == "" {
			// An empty "Other" is a cancel, not an answer.
			o.resolveQuestion(controller.QuestionReply{Cancelled: true})
			ctx.ConsumeAndRedraw()
			return true
		}
		o.resolveQuestion(controller.QuestionReply{Choice: answer, Choices: []string{answer}})
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyBackspace:
		runes := []rune(st.custom)
		if st.customCur > 0 && st.customCur <= len(runes) {
			st.custom = string(append(runes[:st.customCur-1], runes[st.customCur:]...))
			st.customCur--
		}
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyLeft:
		if st.customCur > 0 {
			st.customCur--
		}
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyRight:
		if st.customCur < len([]rune(st.custom)) {
			st.customCur++
		}
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyRune:
		if e.Mods.Has(xui.ModCtrl) || e.Mods.Has(xui.ModAlt) {
			ctx.ConsumeAndRedraw()
			return true
		}
		runes := []rune(st.custom)
		ch := string(e.Rune)
		st.custom = string(append(runes[:st.customCur], append([]rune(ch), runes[st.customCur:]...)...))
		st.customCur++
		ctx.ConsumeAndRedraw()
		return true
	}
	ctx.ConsumeAndRedraw()
	return true
}

// acceptQuestionOption answers with the picked option, or for MultiSelect with
// every checked one.
func (o *Overlays) acceptQuestionOption(idx int) {
	st := o.question
	if st == nil || idx < 0 || idx >= st.optionCount() {
		return
	}
	opt := st.question.Options[idx]
	if st.question.MultiSelect {
		var labels []string
		for i, o2 := range st.question.Options {
			if i == idx || st.picked[i] {
				labels = append(labels, o2.Label)
			}
		}
		o.resolveQuestion(controller.QuestionReply{Choice: opt.Label, Choices: labels})
		return
	}
	o.resolveQuestion(controller.QuestionReply{Choice: opt.Label, Choices: []string{opt.Label}})
}

// optionRowMulti renders one multi-select row: the checkbox, the selection
// arrow/dot, then the label. chrome.OptionLine cannot be reused directly
// because it owns the leading arrow column.
func (s *questionAskState) optionRowMulti(
	th components.Theme,
	primary xui.Style,
	label string,
	selected bool,
	mark string,
	markSt xui.Style,
	innerW int,
	method xui.WidthMethod,
) []components.RichLine {
	const (
		selectArrow, selectDot = "▸", "●"
		idleArrow, idleDot     = " ", "○"
	)
	arrow, dot := idleArrow, idleDot
	labelSt, dotSt := th.Foreground, th.Muted
	if selected {
		arrow, dot = selectArrow, selectDot
		labelSt = xui.Style{Bold: true, Fg: primary.Fg}
		dotSt = primary
	}
	return components.WrapSpans([]components.Span{
		{Text: arrow, Style: primary},
		{Text: dot, Style: dotSt},
		{Text: " " + mark, Style: markSt},
		{Text: label, Style: labelSt},
	}, innerW, method)
}

// hint names the keys that work here, which differ between single and multi
// select — space checks a box only when several answers are allowed.
func (s *questionAskState) hint() string {
	if s.question != nil && s.question.MultiSelect {
		return "↑↓ move" + chrome.Sep + "space check" + chrome.Sep + "Enter submit" + chrome.Sep + "Esc skip"
	}
	return chrome.AskHint("↑↓ move", "select", "skip")
}

// customLines renders the "Other" text field.
func (s *questionAskState) customLines(
	th components.Theme,
	primary xui.Style,
	innerW int,
	method xui.WidthMethod,
) []components.RichLine {
	runes := []rune(s.custom)
	// Render the caret inline so the field behaves like the permission
	// feedback input rather than a static line.
	head := string(runes[:min(s.customCur, len(runes))])
	tail := string(runes[min(s.customCur, len(runes)):])
	style := th.Foreground
	return components.WrapSpans([]components.Span{
		{Text: "Your answer: ", Style: th.Muted},
		{Text: head, Style: style},
		{Text: caretGlyph, Style: xui.Style{Bold: true, Fg: primary.Fg}},
		{Text: tail, Style: style},
	}, innerW, method)
}

const caretGlyph = "\u258d"

// preferredQuestionHeight sizes the panel to its content.
func (s *questionAskState) preferredQuestionHeight(width int, method xui.WidthMethod) int {
	innerW := width - 4
	if innerW < 10 {
		innerW = width
	}
	h := 1 // header
	if s.question != nil && s.question.Header != "" {
		h++
	}
	if s.question != nil {
		h += len(components.WrapSpans([]components.Span{
			{Text: s.question.Question, Style: components.DefaultTheme().Foreground},
		}, innerW, method))
	}
	h++ // blank
	for _, opt := range s.question.Options {
		h++
		if opt.Description != "" {
			h++
		}
	}
	h++ // Other
	h++ // hint
	return h + 4
}
