package submit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/chat"
	"github.com/damien1141/a1/internal/components/status"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/controller"
	"github.com/damien1141/a1/internal/tui/transcript"
)

// Blank submits (empty or whitespace-only, nothing pending) are dropped before
// the transcript: no phantom user turn, and the composer text is left alone.
func TestSubmitter_Submit_BlankInputIsNoOp_Negative(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	tp := transcript.NewTranscriptPane(th, spin, "A1 test")
	comp := &stubComposer{input: "kept"}
	sub := newTestSubmitter(t, tp, nil, comp, nil)

	sub.Submit("")
	sub.Submit("   \t   ")

	assert.Empty(t, tp.Snapshot().Messages, "blank submits never reach the transcript")
	assert.Equal(t, "kept", comp.input, "a refused submit must not clear the composer")
}

// While a local "!cmd" is in flight, IsBusy refuses new prompts: the text stays
// in the composer instead of being appended mid-run, and flows once idle.
func TestSubmitter_Submit_BusyRefusesNewPrompt_Negative(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	tp := transcript.NewTranscriptPane(th, spin, "A1 test")
	comp := &stubComposer{input: "half-typed"}
	sub := newTestSubmitter(t, tp, nil, comp, nil)

	sub.bash.running.Store(true)
	require.True(t, sub.IsBusy(), "a local bash run counts as busy")

	sub.Submit("hello")
	assert.Empty(t, tp.Snapshot().Messages, "a busy submitter must not gain a user turn")
	assert.Equal(t, "half-typed", comp.input, "the draft survives the refusal")

	sub.bash.running.Store(false)
	sub.Submit("hello")
	require.Len(t, tp.Snapshot().Messages, 1)
	assert.Equal(t, "hello", tp.Snapshot().Messages[0].Text, "the same submit flows once idle")
}

// "!cmd" while the agent stream is active is consumed but refused: a warning
// toast is published and no local bash run is started.
func TestSubmitter_Submit_BangWhileAgentStreamingIsRefused_Negative(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	tp := transcript.NewTranscriptPane(th, spin, "A1 test")
	tp.ApplySession(session.ToolData{Run: session.ToolRun{
		ToolUseID: "t1", Name: "fetch", Status: session.ToolInProgress,
	}})
	require.True(t, tp.IsStreaming(), "an in-flight agent tool makes the pane streaming")

	bus := controller.NewBus(nil)
	sub := NewSubmitter(nil, nil, tp, nil, &stubComposer{}, bus,
		nil, nil, nil, nil, nil, nil, nil)

	sub.Submit("!echo hi")

	assert.False(t, sub.RunningBash(), "no local bash run may start under an active stream")
	var toastMsg string
	for _, m := range bus.Drain() {
		if tm, ok := m.(controller.ToastMsg); ok {
			toastMsg = tm.Message
		}
	}
	assert.Contains(t, toastMsg, "Unable to use shell mode while agent is active")
	for _, run := range tp.Snapshot().Tools {
		assert.False(t, run.Local, "no local bash row was appended (%q)", run.Name)
	}
}

// BashRunner.HandleSubmit refuses degenerate bangs (nothing to run) and a
// second concurrent run, without spawning either.
func TestBashRunner_HandleSubmit_RefusesDegenerateAndConcurrent_Negative(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	tp := transcript.NewTranscriptPane(th, spin, "A1 test")
	bus := controller.NewBus(nil)
	b := newBashRunner(tp, &stubComposer{}, bus)

	// Not a bang, a bare bang, and a whitespace-only bang: none are consumed.
	assert.False(t, b.HandleSubmit("plain text"))
	assert.False(t, b.HandleSubmit("!"))
	assert.False(t, b.HandleSubmit("!\t  "))
	assert.False(t, b.Running(), "no run may have started")

	// A run already in flight: consumed (so the bang never reaches the agent),
	// refused with a toast, and no second run spawned.
	b.running.Store(true)
	assert.True(t, b.HandleSubmit("!echo hi"))
	var toastMsg string
	for _, m := range bus.Drain() {
		if tm, ok := m.(controller.ToastMsg); ok {
			toastMsg = tm.Message
		}
	}
	assert.Contains(t, toastMsg, "A bash command is already running")
	b.running.Store(false)

	// Nil receiver guards: the runner is reachable from teardown paths.
	var nb *BashRunner
	assert.False(t, nb.HandleSubmit("!x"))
	assert.False(t, nb.Running())
	assert.False(t, nb.Cancel())
	nb.SyncBorder("!x") // must not panic
}

// The Submitter is held by the Editor across assembly and teardown; every
// entry point must tolerate a nil receiver.
func TestSubmitter_NilReceiverGuards_Negative(t *testing.T) {
	var s *Submitter
	s.Submit("hello")
	s.Cancel()
	s.SyncBashBorder("!x")
	assert.False(t, s.IsBusy())
	assert.False(t, s.StreamActive())
	assert.False(t, s.RunningBash())
}

// After Close, a late Append must neither publish nor re-arm the trailing
// timer — a straggler chunk from a finished command may still arrive.
func TestBashLiveOutput_AppendAfterCloseIsNoOp_Negative(t *testing.T) {
	updates := make(chan string, 1)
	live := newBashLiveOutput(10*time.Millisecond, func(s string) { updates <- s })

	live.Append("first")
	require.Equal(t, "first", <-updates)

	live.Close()

	live.mu.Lock()
	before := live.lastPublish
	live.mu.Unlock()

	live.Append("-late")

	live.mu.Lock()
	defer live.mu.Unlock()
	require.True(t, live.stopped, "Close is final")
	assert.Nil(t, live.timer, "no trailing timer may be armed after Close")
	assert.Equal(t, before, live.lastPublish, "a late append must not publish")
	select {
	case got := <-updates:
		assert.Fail(t, "post-close append published", "got %q", got)
	default:
	}
}

// Prompt building with hostile empties: whitespace-only text and no
// attachments collapse to nothing, and refs alone still form a prompt.
func TestSubmitter_PromptHelpers_EmptyInputs_Negative(t *testing.T) {
	assert.Empty(t, buildUserDisplay("", nil, nil, nil))
	assert.Empty(t, buildUserDisplay("   \t  ", nil, nil, nil), "whitespace-only text is dropped")
	assert.Empty(t, promptWithRefs("", nil), "no refs, no text: nothing to send")

	ref := chat.Ref{Path: "a.go", Start: 2, End: 3, Lang: "go", Text: "x\ny"}
	assert.Equal(t, ref.Block(), promptWithRefs("   ", []chat.Ref{ref}),
		"blank text with refs sends just the ref blocks")
	assert.Equal(t, "Refs: a.go:2-3", buildUserDisplay("  ", []chat.Ref{ref}, nil, nil),
		"the transcript shows the ref chip, not the blank text")
}
