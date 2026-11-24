package submit

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/damien1141/a1/internal/components/chat"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/commands"
	"github.com/damien1141/a1/internal/tui/composer"
	"github.com/damien1141/a1/internal/tui/controller"
	"github.com/damien1141/a1/internal/tui/transcript"
	imgutil "github.com/damien1141/a1/internal/util/image"
)

// Submitter owns submit / cancel / slash dispatch and coordinates bash runs.
type Submitter struct {
	ctrl       *controller.EngineController
	commands   *commands.CommandRegistry
	transcript *transcript.TranscriptPane
	activity   *controller.ActivityHandler
	composer   composer.Input
	bash       *BashRunner

	commandContext func() commands.Context
	bus            *controller.Bus

	// turnInFlight marks the window between Submit and the first agent event.
	//
	// Without it the first Ctrl-C quit the TUI instead of stopping the turn:
	// IsBusy() derives everything from the transcript, and between "user pressed
	// enter" and "first assistant token or tool row" the transcript is idle —
	// the model is still thinking, nothing is streaming, no tool is running. A
	// user reaching for Ctrl-C during a slow first token got kicked out of the
	// app instead of cancelling.
	turnInFlight atomic.Bool

	permissionActive  func() bool
	continueActive    func() bool
	confirmActive     func() bool
	resolvePermission func(controller.AskReply)
	resolveContinue   func(controller.ContinueReply)
	resolveConfirm    func(controller.ExtConfirmReply)
}

// NewSubmitter builds a Submitter from explicit collaborators (no *Editor back-pointer).
// BashRunner is composed internally from transcript/composer/bus.
func NewSubmitter(
	ctrl *controller.EngineController,
	commands *commands.CommandRegistry,
	transcript *transcript.TranscriptPane,
	activity *controller.ActivityHandler,
	composer composer.Input,
	bus *controller.Bus,
	commandContext func() commands.Context,
	permissionActive func() bool,
	continueActive func() bool,
	confirmActive func() bool,
	resolvePermission func(controller.AskReply),
	resolveContinue func(controller.ContinueReply),
	resolveConfirm func(controller.ExtConfirmReply),
) *Submitter {
	return &Submitter{
		ctrl:              ctrl,
		commands:          commands,
		transcript:        transcript,
		activity:          activity,
		composer:          composer,
		bash:              newBashRunner(transcript, composer, bus),
		commandContext:    commandContext,
		bus:               bus,
		permissionActive:  permissionActive,
		continueActive:    continueActive,
		confirmActive:     confirmActive,
		resolvePermission: resolvePermission,
		resolveContinue:   resolveContinue,
		resolveConfirm:    resolveConfirm,
	}
}

// SyncBashBorder updates composer chrome for "!cmd" prefix.
func (s *Submitter) SyncBashBorder(text string) {
	if s == nil || s.bash == nil {
		return
	}
	s.bash.SyncBorder(text)
}

// Submit handles a user prompt from the composer (agent, slash, or bash).
func (s *Submitter) Submit(text string) {
	if s == nil {
		return
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "!") {
		if s.bash != nil && s.bash.HandleSubmit(text) {
			return
		}
	}
	if strings.HasPrefix(text, "/") {
		if insert, ok := s.incompleteSlash(text); ok {
			s.composer.HideCompleters()
			s.composer.SetInput(insert)
			s.composer.SyncBashBorder("")
			return
		}
		if s.dispatchSlash(text) {
			s.composer.HideCompleters()
			s.composer.ClearInput()
			s.composer.ClearPendingImages()
			s.composer.SyncBashBorder("")
			return
		}
	}
	s.handleUserInput(text)
}

func buildUserDisplay(text string, refs []chat.Ref, skills []string, images []imgutil.Attachment) string {
	var parts []string
	if len(refs) > 0 {
		labels := make([]string, len(refs))
		for i, r := range refs {
			labels[i] = r.Label()
		}
		parts = append(parts, "Refs: "+strings.Join(labels, " "))
	}
	if len(skills) > 0 {
		parts = append(parts, "Skills: "+strings.Join(skills, ", "))
	}
	if len(images) > 0 {
		labels := make([]string, len(images))
		for i, att := range images {
			labels[i] = att.Label
		}
		parts = append(parts, "Images: "+strings.Join(labels, " "))
	}
	text = strings.TrimSpace(text)
	if text != "" {
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n")
}

// promptWithRefs expands attached references into fenced blocks ahead of the
// typed prompt. The model gets the selected lines; the transcript keeps the
// chip labels from buildUserDisplay.
func promptWithRefs(text string, refs []chat.Ref) string {
	if len(refs) == 0 {
		return text
	}
	blocks := make([]string, len(refs))
	for i, r := range refs {
		blocks[i] = r.Block()
	}
	expanded := strings.Join(blocks, "\n\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return expanded
	}
	return expanded + "\n\n" + text
}

func (s *Submitter) handleUserInput(text string) {
	pendingRefs := s.composer.PendingRefs()
	pendingSkills := s.composer.PendingSkills()
	pendingImages := s.composer.PendingImages()
	if (text == "" && len(pendingRefs) == 0 && len(pendingSkills) == 0 && len(pendingImages) == 0) || s.IsBusy() {
		return
	}

	s.composer.CloseMentionSlash()

	llmImages := make([]llm.Image, 0, len(pendingImages))
	for _, att := range pendingImages {
		llmImages = append(llmImages, imgutil.ToLLM(att.Result))
	}

	s.activity.Apply(controller.ActivitySubmitting)
	// Mark the turn in flight BEFORE anything is handed to the engine, so the
	// window between submit and the first streamed token is covered.
	s.turnInFlight.Store(true)

	display := buildUserDisplay(text, pendingRefs, pendingSkills, pendingImages)
	s.transcript.ApplySession(session.UserAppend{Text: display, Images: llmImages})
	s.transcript.Sync()
	s.transcript.StickToBottom()

	s.activity.Apply(controller.ActivityWaiting)

	s.composer.ClearInput()
	s.composer.ClearPendingSkills()
	s.composer.ClearPendingImages()
	s.composer.ClearPendingRefs()

	if s.ctrl != nil {
		s.ctrl.StartPrompt(promptWithRefs(text, pendingRefs), pendingSkills, llmImages)
	}
}

// Cancel aborts overlays, bash, or the in-flight agent stream.
func (s *Submitter) Cancel() {
	// A cancelled turn is no longer in flight; the next Ctrl-C must be able to
	// reach the quit path again.
	if s != nil {
		s.turnInFlight.Store(false)
	}
	if s == nil {
		return
	}
	if s.resolvePermission != nil && s.permissionActive != nil && s.permissionActive() {
		s.resolvePermission(controller.AskReply{})
	}
	if s.resolveContinue != nil && s.continueActive != nil && s.continueActive() {
		s.resolveContinue(controller.ContinueReply{})
	}
	if s.resolveConfirm != nil && s.confirmActive != nil && s.confirmActive() {
		s.resolveConfirm(controller.ExtConfirmReply{})
	}
	if s.bash != nil && s.bash.Cancel() {
		return
	}
	if s.ctrl != nil {
		s.ctrl.Cancel()
	}
	s.transcript.ApplySession(session.CancelStreaming{})
	s.transcript.Sync()
	s.activity.Apply(controller.ActivityCancelled)
	time.AfterFunc(1200*time.Millisecond, func() {
		s.bus.Publish(controller.FooterMsg{Kind: controller.FooterClearIfActivity, If: controller.ActivityCancelled})
	})
}

// RunningBash reports whether a local "!cmd" is in flight.
func (s *Submitter) RunningBash() bool {
	if s == nil || s.bash == nil {
		return false
	}
	return s.bash.Running()
}

// IsBusy reports agent stream, background compaction, or local bash activity.
// The controller's compacting flag covers the same-frame window after
// /compact fires, before the CompactionStarted event has landed in the
// snapshot - without it, a fast Enter could append a turn mid-compaction.
func (s *Submitter) IsBusy() bool {
	if s == nil {
		return false
	}
	// The submit-to-first-token gap: nothing is streaming yet, so every other
	// signal below reads idle. Ctrl-C during a slow first token must stop the
	// turn, not the app.
	if s.turnInFlight.Load() {
		return true
	}
	if s.ctrl != nil && s.ctrl.Compacting() {
		return true
	}
	if s.transcript != nil && s.transcript.IsStreaming() {
		return true
	}
	return s.bash != nil && s.bash.Running()
}

// NoteAgentEvent is called for every agent session event. It retires the
// in-flight bridge as soon as the transcript has something to report.
//
// The flag exists only to cover the submit-to-first-token gap. Once the stream
// is visible (a streaming assistant message or a running tool), IsBusy derives
// the answer from the transcript again — leaving the flag set would pin every
// later turn to "busy" and make the second Ctrl-C unable to quit.
func (s *Submitter) NoteAgentEvent() {
	if s == nil || !s.turnInFlight.Load() {
		return
	}
	if s.transcript != nil && s.transcript.IsStreaming() {
		s.turnInFlight.Store(false)
	}
}

// AbandonTurn clears the in-flight bridge without cancelling. Used when a turn
// ends before it ever streamed (an immediate error, or a refused submit) so the
// flag cannot leak into the next turn.
func (s *Submitter) AbandonTurn() {
	if s != nil {
		s.turnInFlight.Store(false)
	}
}

// StreamActive reports whether user input should be blocked for stream/overlays.
func (s *Submitter) StreamActive() bool {
	if s == nil {
		return false
	}
	if s.IsBusy() ||
		(s.permissionActive != nil && s.permissionActive()) ||
		(s.continueActive != nil && s.continueActive()) {
		return true
	}
	if s.activity == nil {
		return false
	}
	switch s.activity.Current {
	case controller.ActivitySubmitting,
		controller.ActivityWaiting,
		controller.ActivityStreaming,
		controller.ActivityTools,
		controller.ActivityCompacting,
		controller.ActivityAwaitingApproval,
		controller.ActivityRetrying:
		return true
	default:
		return false
	}
}

func (s *Submitter) dispatchSlash(text string) bool {
	if s.commands == nil || s.commandContext == nil {
		return false
	}
	return s.commands.DispatchSlash(text, s.commandContext())
}

func (s *Submitter) incompleteSlash(text string) (string, bool) {
	if s == nil || s.commands == nil {
		return "", false
	}
	return s.commands.IncompleteSlash(text)
}
