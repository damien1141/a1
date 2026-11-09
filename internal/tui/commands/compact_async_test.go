package commands_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components/toast"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/commands"
	"github.com/damien1141/a1/internal/tui/controller"
)

// newCompactTestController builds a real controller against a fake provider,
// isolated under t.TempDir. The engine's client points at serverURL, so the
// fake server fully controls both the turn stream and the summarize
// round-trip.
func newCompactTestController(t *testing.T, serverURL string) (*controller.Bus, *controller.EngineController) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("A1_MODEL", "test-model")
	t.Setenv("A1_API_KEY", "test-key")
	t.Setenv("A1_BASE_URL", serverURL)

	cwd := t.TempDir()
	proj, err := project.Discover(cwd)
	require.NoError(t, err)
	require.NoError(t, proj.LoadConfig())

	bus := controller.NewBus(nil)
	ctrl, err := controller.NewController(bus, proj, cwd)
	require.NoError(t, err)
	return bus, ctrl
}

// waitForIdleTurn polls the replay snapshot until the turn finishes (an
// assistant reply landed and nothing is streaming).
func waitForIdleTurn(t *testing.T, ctrl *controller.EngineController, minMessages int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		snap := ctrl.ReplaySnapshot()
		if len(snap.Messages) >= minMessages && !session.IsStreaming(snap) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("turn did not finish within the timeout (messages: %d)", len(ctrl.ReplaySnapshot().Messages))
}

// TestCompactCommands_AsyncDispatch verifies the freeze fix at the command
// layer: Run toasts "Compacting context…" immediately, returns before the
// summarize round-trip finishes, a second /compact while one runs gets its
// own refusal, and the success toast arrives from the background goroutine.
func TestCompactCommands_AsyncDispatch(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Accept") == "text/event-stream" {
			// A turn stream: one fat assistant reply, then DONE.
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: "+`{"choices":[{"delta":{"role":"assistant","content":"`+
				strings.Repeat("reply payload ", 700)+`"}}]}`+"\n\n")
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		// The compaction summarize call (non-streaming): park until released.
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"summary"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	var releaseOnce sync.Once
	defer func() { releaseOnce.Do(func() { close(release) }) }()

	bus, ctrl := newCompactTestController(t, server.URL)
	defer ctrl.Close()

	// Seed two fat turns so the forced compaction has history to summarize
	// (a single turn leaves nothing before the cut) and actually reaches the
	// parked summarize call.
	for i := range 2 {
		ctrl.StartPrompt(fmt.Sprintf("turn %d ", i)+strings.Repeat("history payload ", 700), nil, nil)
		waitForIdleTurn(t, ctrl, 2*(i+1))
	}

	c := &commands.CompactCommands{
		Bus:          bus,
		Ctrl:         ctrl,
		StreamActive: func() bool { return false },
	}

	returned := make(chan struct{})
	go func() {
		c.Run(context.Background())
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must return before the compaction finishes")
	}

	// The immediate feedback toast — the press registering while the
	// round-trip is still parked on the fake server.
	started := drainToasts(t, bus)
	require.Len(t, started, 1)
	assert.Equal(t, "Compacting context…", started[0].Message)
	assert.Equal(t, toast.ToastInfo, started[0].Kind)
	require.True(t, ctrl.Compacting(), "the compaction is parked in flight")

	// A second /compact while the first is parked: its own refusal, not the
	// generic "reply or command is running" mislabel.
	c.Run(context.Background())
	refused := drainToasts(t, bus)
	require.Len(t, refused, 1)
	assert.Equal(t, "Compaction already running", refused[0].Message)
	assert.Equal(t, toast.ToastWarning, refused[0].Kind)

	// Release and collect the outcome.
	releaseOnce.Do(func() { close(release) })
	var outcome []controller.ToastMsg
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && len(outcome) == 0 {
		outcome = append(outcome, drainToasts(t, bus)...)
		time.Sleep(10 * time.Millisecond)
	}
	require.NotEmpty(t, outcome, "the outcome toast must arrive after the round-trip")
	assert.Equal(t, "Context compacted", outcome[0].Message)
	assert.Equal(t, toast.ToastSuccess, outcome[0].Kind)
}

// TestCompactCommands_NothingToCompactOutcome: a fresh session compacts to a
// no-op, and the outcome toast says so rather than reporting success.
func TestCompactCommands_NothingToCompactOutcome(t *testing.T) {
	bus, ctrl := newCompactTestController(t, "http://127.0.0.1:9")
	defer ctrl.Close()

	c := &commands.CompactCommands{
		Bus:          bus,
		Ctrl:         ctrl,
		StreamActive: func() bool { return false },
	}
	c.Run(context.Background())

	var outcome []controller.ToastMsg
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(outcome) < 2 {
		outcome = append(outcome, drainToasts(t, bus)...)
		time.Sleep(10 * time.Millisecond)
	}
	require.Len(t, outcome, 2, "started + outcome toasts")
	assert.Equal(t, "Compacting context…", outcome[0].Message)
	assert.Equal(t, "Nothing to compact", outcome[1].Message)
	assert.Equal(t, toast.ToastWarning, outcome[1].Kind)
}
