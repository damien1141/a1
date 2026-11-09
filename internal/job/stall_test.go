package job

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStallMonitorFoldsWedgedRunner(t *testing.T) {
	root := t.TempDir()
	// The wedged runner: hangs forever, never reports progress, and
	// only unwinds (late) when cancelled — the exact shape the stall
	// monitor exists for.
	m, err := New(Options{
		Root:        root,
		Runner:      RunnerFunc(func(ctx context.Context, _ RunEnv) (string, error) { <-ctx.Done(); return "", ctx.Err() }),
		Recovery:    RecoverRequeue,
		StallWindow: 60 * time.Millisecond,
		WardenTick:  15 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	info, err := m.Spawn(context.Background(), SpawnRequest{
		Prompt: "wedge", ParentID: "sess_stall", Description: "wedged work",
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		got, err := m.Get(context.Background(), info.ID)
		return err == nil && got.Status == StatusStalled
	}, 3*time.Second, 10*time.Millisecond, "wedged job folds to stalled")

	// The fold's commit-point ordering: result and delivery row were
	// visible the moment the status was.
	res, err := m.Wait(context.Background(), info.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusStalled, res.Info.Status)
	assert.Contains(t, res.Summary, "stalled", "placeholder result landed")

	notices := m.CollectNotices("sess_stall")
	require.Len(t, notices, 1)
	assert.Equal(t, StatusStalled, notices[0].Status)
	assert.Contains(t, notices[0].Error, "no progress")
	assert.Empty(t, m.CollectNotices("sess_stall"), "stall notice presents once")
}

func TestStallMonitorKeepsProgressingRunner(t *testing.T) {
	root := t.TempDir()
	// A slow but ALIVE runner: emits progress tokens well inside the
	// window for far longer than the window itself. Generous margins
	// on purpose — under the race detector and suite load a
	// starved-but-healthy goroutine may miss a beat, and the monitor
	// must not call that wedged.
	m, err := New(Options{
		Root: root,
		Runner: RunnerFunc(func(ctx context.Context, env RunEnv) (string, error) {
			deadline := time.Now().Add(600 * time.Millisecond)
			for time.Now().Before(deadline) {
				env.OnProgress(Progress{Name: "step", Status: "in-progress"})
				time.Sleep(20 * time.Millisecond)
			}
			return "slow but alive", nil
		}),
		Recovery:    RecoverRequeue,
		StallWindow: 400 * time.Millisecond,
		WardenTick:  15 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	info, err := m.Spawn(context.Background(), SpawnRequest{Prompt: "slow"})
	require.NoError(t, err)

	res, err := m.Wait(context.Background(), info.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, res.Info.Status, "a reporting runner is never stalled")
	assert.Equal(t, "slow but alive", res.Summary)
}

func TestStalledFoldSurvivesLateRunnerUnwind(t *testing.T) {
	root := t.TempDir()
	unwind := make(chan struct{})
	m, err := New(Options{
		Root: root,
		Runner: RunnerFunc(func(ctx context.Context, _ RunEnv) (string, error) {
			// Wedged, then cancelled by the fold — but returns
			// a beat LATE, after the stall fold already landed.
			<-ctx.Done()
			<-unwind
			return "", ctx.Err()
		}),
		Recovery:    RecoverRequeue,
		StallWindow: 50 * time.Millisecond,
		WardenTick:  10 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	info, err := m.Spawn(context.Background(), SpawnRequest{Prompt: "late unwind"})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		got, err := m.Get(context.Background(), info.ID)
		return err == nil && got.Status == StatusStalled
	}, 3*time.Second, 10*time.Millisecond, "job folds to stalled")

	// The runner unwinds after the fold: run() must defer to the
	// monitor's terminal state, not overwrite it with "cancelled".
	close(unwind)
	require.Eventually(t, func() bool {
		return m.LiveCount() == 0
	}, 3*time.Second, 10*time.Millisecond, "the unwound goroutine leaves the map")

	got, err := m.Get(context.Background(), info.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusStalled, got.Status, "late cancellation does not overwrite the stalled fold")
}

func TestStallMonitorDisabled(t *testing.T) {
	root := t.TempDir()
	m, err := New(Options{
		Root:        root,
		Runner:      RunnerFunc(func(ctx context.Context, _ RunEnv) (string, error) { <-ctx.Done(); return "", ctx.Err() }),
		Recovery:    RecoverRequeue,
		StallWindow: -1, // off
		WardenTick:  15 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	info, err := m.Spawn(context.Background(), SpawnRequest{Prompt: "wedge forever"})
	require.NoError(t, err)

	// A negative window disables the monitor: no fold within a window
	// that would have folded it many times over.
	time.Sleep(120 * time.Millisecond)
	got, err := m.Get(context.Background(), info.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, got.Status, "stall monitor is off")
}
