package submit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/project"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/chat"
	"github.com/damien1141/a1/internal/components/status"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/commands"
	"github.com/damien1141/a1/internal/tui/controller"
	"github.com/damien1141/a1/internal/tui/transcript"
	imgutil "github.com/damien1141/a1/internal/util/image"
)

type stubComposer struct {
	skills []string
	images []imgutil.Attachment
	input  string
}

func (stubComposer) HideCompleters()                       {}
func (s *stubComposer) ClearInput()                        { s.input = "" }
func (s *stubComposer) SetInput(text string)               { s.input = text }
func (s stubComposer) PendingSkills() []string             { return s.skills }
func (s stubComposer) PendingImages() []imgutil.Attachment { return s.images }
func (stubComposer) PendingRefs() []chat.Ref               { return nil }
func (stubComposer) ClearPendingSkills()                   {}
func (stubComposer) ClearPendingImages()                   {}
func (stubComposer) ClearPendingRefs()                     {}
func (stubComposer) SyncBashBorder(string)                 {}
func (stubComposer) CloseMentionSlash()                    {}
func (stubComposer) SetBashBorderActive(bool)              {}

func newTestSubmitter(
	t *testing.T,
	tp *transcript.TranscriptPane,
	activity *controller.ActivityHandler,
	composer *stubComposer,
	cmds *commands.CommandRegistry,
) *Submitter {
	t.Helper()
	if composer == nil {
		composer = &stubComposer{}
	}
	return NewSubmitter(
		nil,
		cmds,
		tp,
		activity,
		composer,
		nil,
		func() commands.Context { return commands.NewContext(nil, nil) },
		nil, nil, nil,
		nil, nil, nil,
	)
}

func TestSubmitter_IsBusy(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	tp := transcript.NewTranscriptPane(th, spin, "A1 test")
	sub := newTestSubmitter(t, tp, nil, nil, nil)
	assert.False(t, sub.IsBusy())
}

func TestSubmitter_StreamActive_activity(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	activity := controller.NewActivityHandler(spin)
	sub := newTestSubmitter(t, transcript.NewTranscriptPane(th, spin, "A1 test"), activity, nil, nil)
	activity.Apply(controller.ActivityWaiting)
	assert.True(t, sub.StreamActive())
}

func TestSubmitter_Submit_unknownSlashFallsThroughToAgent(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	tp := transcript.NewTranscriptPane(th, spin, "A1 test")
	sub := newTestSubmitter(t, tp, nil, nil, commands.NewCommandRegistry())
	sub.Submit("/not-a-real-command")
	require.Len(t, tp.Snapshot().Messages, 1)
	assert.Equal(t, "/not-a-real-command", tp.Snapshot().Messages[0].Text)
	assert.Equal(t, session.RoleUser, tp.Snapshot().Messages[0].Role)
}

func TestSubmitter_Submit_bareBangFallsThroughToAgent(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	tp := transcript.NewTranscriptPane(th, spin, "A1 test")
	sub := newTestSubmitter(t, tp, nil, nil, nil)
	sub.Submit("!")
	require.Len(t, tp.Snapshot().Messages, 1)
	assert.Equal(t, "!", tp.Snapshot().Messages[0].Text)
}

func TestSubmitter_Submit_needsArgsRefillsComposer(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	tp := transcript.NewTranscriptPane(th, spin, "A1 test")
	comp := &stubComposer{}
	reg := commands.NewCommandRegistry()
	reg.Register(commands.Command{
		Name:      "plan",
		Slash:     true,
		NeedsArgs: true,
		Run:       func(commands.Context, []string) error { return nil },
	})
	sub := newTestSubmitter(t, tp, nil, comp, reg)
	sub.Submit("/plan")
	assert.Equal(t, "/plan ", comp.input)
	assert.Empty(t, tp.Snapshot().Messages)
}

func TestSubmitter_Submit_withImagesOnly(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	tp := transcript.NewTranscriptPane(th, spin, "A1 test")
	images := []imgutil.Attachment{
		{Label: "a.png", Result: imgutil.Result{Data: []byte("abc"), MimeType: "image/png"}},
	}
	sub := newTestSubmitter(t, tp, nil, &stubComposer{images: images}, nil)
	sub.Submit("")
	require.Len(t, tp.Snapshot().Messages, 1)
	msg := tp.Snapshot().Messages[0]
	assert.Equal(t, session.RoleUser, msg.Role)
	require.Len(t, msg.Images, 1)
}

// TestSubmitter_IsBusyWhileCompacting pins the busy wiring for the async
// /compact: while the controller's compacting flag is armed, IsBusy (and
// with it StreamActive) must report busy, so an Enter during the background
// round-trip keeps its text instead of appending a turn mid-compaction.
//
// The flag is armed by parking the compaction's window probe (GET /props,
// the first thing runCompact does) on a blocked handler - no seeded history
// needed for an empty session to reach it.
func TestSubmitter_IsBusyWhileCompacting(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/props" {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	var releaseOnce sync.Once
	defer func() { releaseOnce.Do(func() { close(release) }) }()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("A1_MODEL", "test-model")
	t.Setenv("A1_API_KEY", "test-key")
	t.Setenv("A1_BASE_URL", server.URL)

	cwd := t.TempDir()
	proj, err := project.Discover(cwd)
	require.NoError(t, err)
	require.NoError(t, proj.LoadConfig())
	bus := controller.NewBus(nil)
	ctrl, err := controller.NewController(bus, proj, cwd)
	require.NoError(t, err)
	defer ctrl.Close()

	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	sub := NewSubmitter(
		ctrl,
		nil,
		transcript.NewTranscriptPane(th, spin, "A1 test"),
		nil,
		&stubComposer{},
		nil,
		func() commands.Context { return commands.NewContext(nil, nil) },
		nil, nil, nil,
		nil, nil, nil,
	)
	require.False(t, sub.IsBusy())

	ctrl.StartForceCompact(context.Background(), func(error) {})
	require.True(t, ctrl.Compacting(), "the probe is parked, the flag is armed")
	assert.True(t, sub.IsBusy(), "a background compaction counts as busy")
	assert.True(t, sub.StreamActive(), "and blocks new prompts")

	releaseOnce.Do(func() { close(release) })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && ctrl.Compacting() {
		time.Sleep(10 * time.Millisecond)
	}
	require.False(t, ctrl.Compacting(), "the no-op compaction finished after release")
	assert.False(t, sub.IsBusy())
}
