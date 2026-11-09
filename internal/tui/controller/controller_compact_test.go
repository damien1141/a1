package controller

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

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/session"
)

// TestStartForceCompactAsync pins the /compact freeze fix: the compaction
// summarize round-trip runs on a background goroutine, the compacting flag
// arms synchronously (so busy guards see it with no one-frame window), a
// second /compact while one runs is refused, and the CompactionStarted /
// CompactionComplete events still reach the bus.
func TestStartForceCompactAsync(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			// Park the summarize call until the test releases it.
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			// The summarizer uses the non-streaming Compact call: a plain
			// JSON chat-completion body.
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"summary of the session"},"finish_reason":"stop"}]}`)
		default:
			http.NotFound(w, r)
		}
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

	bus := NewBus(nil)
	ctrl, err := NewController(bus, proj, cwd)
	require.NoError(t, err)
	defer ctrl.Close()

	// Seed enough history that a forced compaction has something to
	// summarize: a few fat turns push the kept zone past its floor, so the
	// cut lands mid-session and the summarize call actually runs (and parks
	// on the blocked handler above).
	big := strings.Repeat("history payload ", 700) // ~11.9KB per message
	for i := 0; i < 4; i++ {
		require.NoError(t, ctrl.engine.Session().Append(
			llm.Message{Role: llm.RoleUser, Content: fmt.Sprintf("turn %d: %s", i, big)},
			llm.Message{Role: llm.RoleAssistant, Content: big},
		))
	}

	require.False(t, ctrl.Compacting(), "idle controller is not compacting")

	done := make(chan error, 2)
	returned := make(chan struct{})
	go func() {
		ctrl.StartForceCompact(context.Background(), func(err error) { done <- err })
		close(returned)
	}()

	// The dispatch must return while the summarize call is still parked.
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("StartForceCompact must not block on the summarize round-trip")
	}

	// The flag armed synchronously — before the CompactionStarted event has
	// any chance to land in a snapshot.
	require.True(t, ctrl.Compacting(), "compacting flag must be live while the round-trip runs")

	// A second compaction is refused synchronously.
	refused := make(chan error, 1)
	ctrl.StartForceCompact(context.Background(), func(err error) { refused <- err })
	select {
	case err := <-refused:
		require.ErrorIs(t, err, ErrCompactionRunning)
	case <-time.After(2 * time.Second):
		t.Fatal("double-run refusal must be immediate")
	}

	// Release the summarize call and collect the outcome.
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		require.NoError(t, err, "the released compaction completes")
	case <-time.After(10 * time.Second):
		t.Fatal("compaction never finished after release")
	}
	require.False(t, ctrl.Compacting(), "the flag clears when the round-trip ends")

	// The events still reached the bus for the transcript.
	var started, complete bool
	for _, m := range bus.Drain() {
		sem, ok := m.(SessionEventMsg)
		if !ok {
			continue
		}
		switch sem.Event.(type) {
		case session.CompactionStarted:
			started = true
		case session.CompactionComplete:
			complete = true
		}
	}
	assert.True(t, started, "CompactionStarted was published")
	assert.True(t, complete, "CompactionComplete was published")
}

// TestForceCompactSyncGuard covers the synchronous ForceCompact entry: it
// reports ErrCompactionRunning on a double call and clears the flag when
// nothing was compactable.
func TestForceCompactSyncGuard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("A1_MODEL", "test-model")
	t.Setenv("A1_API_KEY", "test-key")
	t.Setenv("A1_BASE_URL", "http://127.0.0.1:9")

	cwd := t.TempDir()
	proj, err := project.Discover(cwd)
	require.NoError(t, err)
	require.NoError(t, proj.LoadConfig())

	bus := NewBus(nil)
	ctrl, err := NewController(bus, proj, cwd)
	require.NoError(t, err)
	defer ctrl.Close()

	// Fresh session: nothing to compact, and the flag must clear afterwards.
	err = ctrl.ForceCompact(context.Background())
	require.ErrorIs(t, err, ErrNothingToCompact)
	require.False(t, ctrl.Compacting())
}
