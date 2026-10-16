package controller

import (
	"time"

	"github.com/damien1141/a1/internal/components/toast"
	"github.com/damien1141/a1/internal/job"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/session"
)

// Msg is a UI-thread message. Producers send; Editor.Update applies.
// Share memory by communicating — not the other way around.
type Msg interface {
	isMsg()
}

// SubmitMsg asks the UI to accept a user prompt.
type SubmitMsg struct{ Text string }

func (SubmitMsg) isMsg() {}

// CancelStreamMsg aborts the in-flight agent stream.
type CancelStreamMsg struct{}

func (CancelStreamMsg) isMsg() {}

// SessionEventMsg carries a session model event from the agent pipeline.
type SessionEventMsg struct{ Event session.Event }

func (SessionEventMsg) isMsg() {}

// FooterKind discriminates variants of FooterMsg.
type FooterKind int

const (
	FooterSetActivity FooterKind = iota
	FooterClearIfActivity
	FooterUpdateAvailable
	FooterTurnEnd
)

// FooterMsg drives footer activity status and update hints.
type FooterMsg struct {
	Kind FooterKind

	Activity Activity // FooterSetActivity
	If       Activity // FooterClearIfActivity — Idle only when current still matches

	Latest  string // FooterUpdateAvailable
	Current string
	Label   string // FooterTurnEnd
}

func (FooterMsg) isMsg() {}

// ToastMsg asks the Editor to show a transient overlay notification.
// Producers Publish this instead of holding a toast callback.
type ToastMsg struct {
	Message  string
	Kind     toast.ToastKind
	Duration time.Duration
}

func (ToastMsg) isMsg() {}

// ThemeMsg asks the Editor to switch the UI chrome theme by name.
type ThemeMsg struct{ Name string }

func (ThemeMsg) isMsg() {}

// MentionResultsMsg delivers async @-file search results to the UI goroutine.
type MentionResultsMsg struct {
	Gen   int
	Query string
	Paths []string
	// Truncated reports that more matches exist than Paths holds, so the
	// picker can say the list is partial instead of looking complete.
	Truncated bool
	ErrText   string
}

func (MentionResultsMsg) isMsg() {}

// OverlayKind discriminates ask/dismiss variants of OverlayMsg.
type OverlayKind int

const (
	OverlayPermissionAsk OverlayKind = iota
	OverlayPermissionDismiss
	OverlayContinueAsk
	OverlayContinueDismiss
	OverlayExtConfirm
	OverlayExtConfirmDismiss
	OverlayQuestionAsk
	OverlayQuestionDismiss
)

// OverlayMsg drives permission / continue / extension-confirm UI.
// Kind selects which fields are meaningful; reply chans must be buffered(1).
type OverlayMsg struct {
	Kind OverlayKind

	// Permission ask
	Request   permission.Request
	Reason    string
	PermReply chan AskReply

	// Continue ask
	MaxRounds int
	ContReply chan ContinueReply

	// Question ask (AskUserQuestion tool)
	Question   *Question
	QuestReply chan QuestionReply

	// Extension confirm
	Title        string
	Message      string
	Yes          string
	No           string
	Danger       bool
	ConfirmReply chan ExtConfirmReply
}

func (OverlayMsg) isMsg() {}

// Question is one structural question the agent asked the human, with the
// choices offered. It is deliberately not a permission.Request: nothing is being
// approved or denied, an answer is being collected.
type Question struct {
	// Header is a short chip label (<=12 runes) naming the question.
	Header string
	// Question is the full text.
	Question string
	// Options are the choices, in the order the agent ranked them. A "(Recommended)"
	// suffix in Label is the agent steering, not a different option type.
	Options []QuestionOption
	// MultiSelect allows picking several options; Enter submits them all.
	MultiSelect bool
}

// QuestionOption is one answer choice.
type QuestionOption struct {
	Label       string
	Description string
}

// QuestionReply is the human's answer. Choice is the selected label, or the
// typed text when the user picked "Other" and wrote their own.
type QuestionReply struct {
	Choice string
	// Choices holds every picked label when the question was MultiSelect.
	Choices []string
	// Cancelled reports that the user dismissed the question without answering.
	Cancelled bool
}

// ExtCommandResultMsg delivers the result of an extension slash command.
type ExtCommandResultMsg struct {
	Gen       uint64
	Submit    string
	Toast     string
	Status    string
	StatusSet bool
	Err       string
}

func (ExtCommandResultMsg) isMsg() {}

// ExtSessionEffectsMsg applies toast/status from session lifecycle extensions.
type ExtSessionEffectsMsg struct {
	Toast     string
	Status    string
	StatusSet bool
}

func (ExtSessionEffectsMsg) isMsg() {}

// JobProgressMsg carries a live sub-agent tool update for the nested tree UI.
type JobProgressMsg struct {
	Progress job.Progress
}

func (JobProgressMsg) isMsg() {}

// BranchLabelMsg refreshes the path label's git branch after an external
// checkout (e.g. from another terminal or editor).
type BranchLabelMsg struct {
	Text string
}

func (BranchLabelMsg) isMsg() {}

// ModelChangeMsg carries a model-related update. Kind distinguishes the target:
// "model" → model name change, "think_level" → thinking level change.
type ModelChangeMsg struct {
	Kind  string
	Value string
}

func (ModelChangeMsg) isMsg() {}
