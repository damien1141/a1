package agent_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/agent"
	"github.com/damien1141/a1/internal/job"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/session"
)

func TestSpecForRoleAtDepthBudgets(t *testing.T) {
	// Depth 0 (direct child) matches the historical budgets.
	assert.Equal(t, 20, agent.SpecForRoleAt(job.RoleWorker, 0).Confinement.MaxSteps)
	assert.Equal(t, 15, agent.SpecForRoleAt(job.RoleReview, 0).Confinement.MaxSteps)
	assert.Equal(t, 10, agent.SpecForRoleAt(job.RoleExplore, 0).Confinement.MaxSteps)
	assert.Equal(t, agent.SpecForRole(job.RoleWorker).Confinement.MaxSteps,
		agent.SpecForRoleAt(job.RoleWorker, 0).Confinement.MaxSteps)

	// Grandchildren are cheaper than children; tables clamp at the tail.
	assert.Equal(t, 14, agent.SpecForRoleAt(job.RoleWorker, 1).Confinement.MaxSteps)
	assert.Equal(t, 10, agent.SpecForRoleAt(job.RoleWorker, 2).Confinement.MaxSteps)
	assert.Equal(t, 10, agent.SpecForRoleAt(job.RoleWorker, 9).Confinement.MaxSteps, "clamped")
	assert.Equal(t, 7, agent.SpecForRoleAt(job.RoleExplore, 1).Confinement.MaxSteps)
	assert.Equal(t, 5, agent.SpecForRoleAt(job.RoleExplore, 9).Confinement.MaxSteps, "clamped")

	// Negative depth is treated as 0; confinement values are fresh per call.
	a := agent.SpecForRoleAt(job.RoleWorker, -1).Confinement
	b := agent.SpecForRoleAt(job.RoleWorker, 1).Confinement
	assert.Equal(t, 20, a.MaxSteps)
	assert.NotSame(t, a, b, "specs must not share confinement state")
}

func jobEventLog(t *testing.T, mgr *job.Manager, id string) string {
	t.Helper()
	events, err := mgr.Log(context.Background(), id, 0)
	require.NoError(t, err)
	out := ""
	for _, ev := range events {
		out += ev.Message + "\n"
	}
	return out
}

// TestEngineRunnerWritesHandoffAndNests drives a worker child end-to-end
// through the job manager and checks the Bet 04 delivery mechanisms: the
// structured handoff lands in the job dir, and the runner logs the child's
// fleet depth and the inherited contract seed.
func TestEngineRunnerWritesHandoffAndNests(t *testing.T) {
	srv := textOnlySSEServer("implemented the thing; tests pass")
	defer srv.Close()

	jobsRoot := t.TempDir()
	workdir := t.TempDir()

	runner := agent.EngineRunner{
		Model:     llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x"},
		JobsRoot:  jobsRoot,
		MaxRounds: 4,
	}
	mgr, err := job.New(job.Options{Root: jobsRoot, Runner: runner})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	info, err := mgr.Spawn(context.Background(), job.SpawnRequest{
		Prompt:   "implement the scoped thing",
		Role:     job.RoleWorker,
		WorkDir:  workdir,
		Contract: []string{"go-build"},
	})
	require.NoError(t, err)

	res, err := mgr.Wait(context.Background(), info.ID)
	require.NoError(t, err)
	require.Equal(t, job.StatusCompleted, res.Info.Status, "worker child should complete")
	assert.Contains(t, res.Summary, "implemented the thing")

	// Structured handoff landed next to result.md.
	h, ok := job.ReadHandoff(info.Dir)
	require.True(t, ok, "worker child must write handoff.json")
	assert.Contains(t, h.Summary, "implemented the thing")

	// The runner logs the child's fleet depth (worker, depth 1).
	assert.Contains(t, jobEventLog(t, mgr, info.ID), "role=worker depth=1")
}

// TestEngineRunnerExploreDoesNotNest pins the no-nesting default for
// explore children even when the runner carries a jobs root.
func TestEngineRunnerExploreDoesNotNest(t *testing.T) {
	srv := textOnlySSEServer("found it")
	defer srv.Close()

	jobsRoot := t.TempDir()
	runner := agent.EngineRunner{
		Model:     llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x"},
		JobsRoot:  jobsRoot,
		MaxRounds: 4,
	}
	mgr, err := job.New(job.Options{Root: jobsRoot, Runner: runner})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	info, err := mgr.Spawn(context.Background(), job.SpawnRequest{
		Prompt:  "look around",
		Role:    job.RoleExplore,
		WorkDir: t.TempDir(),
	})
	require.NoError(t, err)
	res, err := mgr.Wait(context.Background(), info.ID)
	require.NoError(t, err)
	assert.Equal(t, job.StatusCompleted, res.Info.Status)
	assert.Contains(t, jobEventLog(t, mgr, info.ID), "role=explore depth=1")
}

// TestWarmSessionResumesPriorWorker pins the warm-pool mechanism: a job
// with WarmFrom copies the source job's newest session file and resumes it.
func TestWarmSessionResumesPriorWorker(t *testing.T) {
	srv := textOnlySSEServer("warm worker reply")
	defer srv.Close()

	jobsRoot := t.TempDir()
	runner := agent.EngineRunner{
		Model: llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x"},
	}
	mgr, err := job.New(job.Options{Root: jobsRoot, Runner: runner})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	first, err := mgr.Spawn(context.Background(), job.SpawnRequest{
		Prompt:  "first run",
		Role:    job.RoleWorker,
		WorkDir: t.TempDir(),
	})
	require.NoError(t, err)
	res, err := mgr.Wait(context.Background(), first.ID)
	require.NoError(t, err)
	require.Equal(t, job.StatusCompleted, res.Info.Status)

	// Warm spawn: resumes the first job's session.
	second, err := mgr.Spawn(context.Background(), job.SpawnRequest{
		Prompt:   "second run",
		Role:     job.RoleWorker,
		WorkDir:  t.TempDir(),
		WarmFrom: first.ID,
	})
	require.NoError(t, err)
	res2, err := mgr.Wait(context.Background(), second.ID)
	require.NoError(t, err)
	require.Equal(t, job.StatusCompleted, res2.Info.Status)

	// The second job's session dir contains a copy of the first's session
	// file (same basename), and events record no fallback.
	firstMetas, err := session.ListSessions(filepath.Join(first.Dir, "session"))
	require.NoError(t, err)
	require.Len(t, firstMetas, 1)
	secondMetas, err := session.ListSessions(filepath.Join(second.Dir, "session"))
	require.NoError(t, err)
	require.Len(t, secondMetas, 1)
	assert.Equal(t, filepath.Base(firstMetas[0].File), filepath.Base(secondMetas[0].File),
		"warm resume copies the source session file")
	assert.Equal(t, firstMetas[0].ID, secondMetas[0].ID, "resumed session keeps its id")

	assert.NotContains(t, jobEventLog(t, mgr, second.ID), "warm resume unavailable",
		"source session exists; resume must succeed")
}

// TestWarmSessionMissingSourceFallsBack pins the never-a-hard-requirement
// rule: an unknown WarmFrom job id starts a fresh session.
func TestWarmSessionMissingSourceFallsBack(t *testing.T) {
	srv := textOnlySSEServer("fresh anyway")
	defer srv.Close()

	jobsRoot := t.TempDir()
	runner := agent.EngineRunner{
		Model: llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x"},
	}
	mgr, err := job.New(job.Options{Root: jobsRoot, Runner: runner})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	info, err := mgr.Spawn(context.Background(), job.SpawnRequest{
		Prompt:   "warm from nothing",
		Role:     job.RoleWorker,
		WorkDir:  t.TempDir(),
		WarmFrom: "job_20990101T000000_0000000000000000",
	})
	require.NoError(t, err)
	res, err := mgr.Wait(context.Background(), info.ID)
	require.NoError(t, err)
	assert.Equal(t, job.StatusCompleted, res.Info.Status)
	assert.Contains(t, jobEventLog(t, mgr, info.ID), "warm resume unavailable",
		"missing source must log the fallback")
}
