package job_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/job"
)

func TestCancelQueuedJobNeverRuns(t *testing.T) {
	var ran bool
	block := make(chan struct{})
	m := newMgr(t, job.RunnerFunc(func(_ context.Context, env job.RunEnv) (string, error) {
		if env.Job.Prompt == "2" {
			ran = true
		}
		<-block
		return "ok", nil
	}), job.Options{MaxConcurrent: 1})

	ctx := t.Context()
	info1, err := m.Spawn(ctx, job.SpawnRequest{Prompt: "1"})
	require.NoError(t, err)
	info2, err := m.Spawn(ctx, job.SpawnRequest{Prompt: "2"})
	require.NoError(t, err)
	require.Equal(t, job.StatusQueued, info2.Status)

	// Cancel the queued job: it must finalize without ever running.
	require.NoError(t, m.Cancel(ctx, info2.ID))
	res, err := m.Wait(ctx, info2.ID)
	require.NoError(t, err)
	assert.Equal(t, job.StatusCancelled, res.Info.Status)
	assert.Contains(t, res.Info.Error, "queued")

	close(block)
	_, err = m.Wait(ctx, info1.ID)
	require.NoError(t, err)
	assert.False(t, ran, "cancelled queued job must not run")
}

func TestCloseCancelsQueuedJobs(t *testing.T) {
	block := make(chan struct{})
	m := newMgr(t, job.RunnerFunc(func(ctx context.Context, _ job.RunEnv) (string, error) {
		select {
		case <-block:
			return "ok", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}), job.Options{MaxConcurrent: 1})

	ctx := t.Context()
	_, err := m.Spawn(ctx, job.SpawnRequest{Prompt: "1"})
	require.NoError(t, err)
	queued, err := m.Spawn(ctx, job.SpawnRequest{Prompt: "2"})
	require.NoError(t, err)
	require.Equal(t, job.StatusQueued, queued.Status)

	close(block)
	require.NoError(t, m.Close(ctx))

	res, err := m.Wait(ctx, queued.ID)
	require.NoError(t, err)
	assert.True(t, res.Info.Status.Terminal())
}

func TestQueuedJobContractForwardedToRunner(t *testing.T) {
	block := make(chan struct{})
	var got []string
	var mu sync.Mutex
	m := newMgr(t, job.RunnerFunc(func(_ context.Context, env job.RunEnv) (string, error) {
		if env.Job.Prompt == "1" {
			<-block
			return "ok", nil
		}
		mu.Lock()
		got = env.Contract
		mu.Unlock()
		return "ok", nil
	}), job.Options{MaxConcurrent: 1})

	ctx := t.Context()
	info1, err := m.Spawn(ctx, job.SpawnRequest{Prompt: "1"})
	require.NoError(t, err)
	info2, err := m.Spawn(ctx, job.SpawnRequest{
		Prompt:   "2",
		Contract: []string{"go-build", "go-test:..."},
	})
	require.NoError(t, err)
	close(block)

	for _, id := range []string{info1.ID, info2.ID} {
		res, err := m.Wait(ctx, id)
		require.NoError(t, err)
		require.Equal(t, job.StatusCompleted, res.Info.Status)
	}
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"go-build", "go-test:..."}, got)
}

func TestHandoffRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Missing handoff: ok=false, no error semantics.
	_, ok := job.ReadHandoff(dir)
	assert.False(t, ok)

	h := job.Handoff{
		Summary:       "did the thing",
		FilesRead:     []string{"a.go", "b.go"},
		FilesModified: []string{"c.go"},
		Checks: []job.HandoffCheck{
			{Check: "go-build", Status: "pass"},
			{Check: "go-test:pkg", Status: "fail", Detail: "exit 1"},
		},
	}
	require.NoError(t, job.WriteHandoff(dir, h))

	got, ok := job.ReadHandoff(dir)
	require.True(t, ok)
	assert.Equal(t, h, got)
}

func TestHandoffPathEmptyDir(t *testing.T) {
	assert.Empty(t, job.HandoffPath(""))
	assert.Equal(t, "x"+string(rune(filepath.Separator))+"handoff.json", job.HandoffPath("x"))
	// Writing with no dir is an error, not a panic.
	require.Error(t, job.WriteHandoff("", job.Handoff{}))
}
