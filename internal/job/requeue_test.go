package job

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The requeue tests model real process death with a real dying process:
// the test binary re-execs itself as a helper (TestMain below) that builds
// a Manager, spawns jobs, waits for their RUNNING state to hit disk, and
// then either exits hard (crash mode — no Close, no cleanup) or lingers
// until killed (linger mode — a live owner for the liveness checks).
//
// An in-process "abandon the manager" fiction cannot model either: the
// abandoned manager's goroutines keep running behind the test's back, and
// its owner stamp (this process, alive) makes every liveness check lie.

func TestMain(m *testing.M) {
	if root := os.Getenv("A1_JOB_HELPER_ROOT"); root != "" {
		helperMain(root)
		os.Exit(9) // unreachable; helperMain exits itself
	}
	os.Exit(m.Run())
}

// helperMain is the helper process body. Env:
//
//	A1_JOB_HELPER_ROOT  jobs root (required — its presence selects helper mode)
//	A1_JOB_HELPER_N     job count        (default 1)
//	A1_JOB_HELPER_MODE  "crash" (default) | "linger"
//
// The runner hangs forever on ctx — the helper's death is what ends it.
func helperMain(root string) {
	n := 1
	if v := os.Getenv("A1_JOB_HELPER_N"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			n = p
		}
	}
	hang := RunnerFunc(func(ctx context.Context, env RunEnv) (string, error) {
		<-ctx.Done() // the helper process dies first; this never fires in life
		return "", ctx.Err()
	})
	m, err := New(Options{Root: root, Runner: hang, Recovery: RecoverRequeue})
	if err != nil {
		os.Exit(2)
	}
	for i := 0; i < n; i++ {
		req := SpawnRequest{
			Prompt:     "die mid-run",
			Kind:       KindSchedule,
			ScheduleID: fmt.Sprintf("sch_%d", i+1),
			Contract:   []string{"go-build"},
			Timeout:    5 * time.Minute,
		}
		if _, err := m.Spawn(context.Background(), req); err != nil {
			os.Exit(3)
		}
	}
	// Every job's RUNNING persist must land before the helper goes:
	// the death (or the linger) is mid-run, not mid-create.
	deadline := time.Now().Add(30 * time.Second)
	for {
		running := 0
		if ids, err := m.store.listIDs(); err == nil {
			for _, id := range ids {
				if meta, err := m.store.readMeta(id); err == nil && meta.Status == StatusRunning {
					running++
				}
			}
		}
		if running >= n {
			break
		}
		if time.Now().After(deadline) {
			os.Exit(4)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if os.Getenv("A1_JOB_HELPER_MODE") == "linger" {
		for {
			time.Sleep(time.Hour) // killed from the test
		}
	}
	os.Exit(9) // the crash: hard exit, no Close, no cleanup
}

// runHelper execs the test binary in helper mode and returns when the
// helper has finished (crash mode) — or immediately with the running
// command (linger mode; the test kills it later).
func runHelper(t *testing.T, root string, n int, mode string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(),
		"A1_JOB_HELPER_ROOT="+root,
		fmt.Sprintf("A1_JOB_HELPER_N=%d", n),
		"A1_JOB_HELPER_MODE="+mode,
	)
	if mode == "linger" {
		require.NoError(t, cmd.Start())
		t.Cleanup(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		})
		return cmd
	}
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "crash helper must die, output:\n%s", out)
	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee)
	assert.Equal(t, 9, ee.ExitCode(), "crash helper exit code, output:\n%s", out)
	return cmd
}

// waitStatusRunning blocks until the job's RUNNING state is persisted.
func waitStatusRunning(t *testing.T, root, id string) {
	t.Helper()
	require.Eventually(t, func() bool {
		meta, err := readMetaFrom(root, id)
		return err == nil && meta.Status == StatusRunning
	}, 10*time.Second, 10*time.Millisecond, "helper job %s should reach running", id)
}

// readMetaFrom reads a job's meta.json straight off disk (no manager).
func readMetaFrom(root, id string) (Meta, error) {
	st, err := newStore(root, nil)
	if err != nil {
		return Meta{}, err
	}
	return st.readMeta(id)
}

// listIDsFrom lists job ids straight off disk (no manager).
func listIDsFrom(t *testing.T, root string) []string {
	t.Helper()
	st, err := newStore(root, nil)
	require.NoError(t, err)
	ids, err := st.listIDs()
	require.NoError(t, err)
	return ids
}

func TestRecoverRequeueParksDeadOwnerInsteadOfFailing(t *testing.T) {
	root := t.TempDir()
	runHelper(t, root, 1, "crash")

	// A real dead process owned that job: the new manager parks it as
	// pending instead of failing it, with the attempt bumped and the
	// spawn metadata intact.
	m, err := New(Options{Root: root, Runner: newRecordingRunner(), Recovery: RecoverRequeue})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	list, err := m.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, StatusPending, list[0].Status)
	assert.Equal(t, 1, list[0].Attempt, "requeue bumps the attempt counter")
	assert.Equal(t, KindSchedule, list[0].Kind)
	assert.Equal(t, "sch_1", list[0].ScheduleID)

	// Wait reports the parked state instead of a zombie hang.
	_, err = m.Wait(context.Background(), list[0].ID)
	assert.ErrorIs(t, err, ErrNotRunning)

	// Cancel finalizes a parked job in place.
	require.NoError(t, m.Cancel(context.Background(), list[0].ID))
	got, err := m.Get(context.Background(), list[0].ID)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, got.Status)
}

func TestResumeRunsAgainWithResumeFlag(t *testing.T) {
	root := t.TempDir()
	runHelper(t, root, 1, "crash")
	ids := listIDsFrom(t, root)
	require.Len(t, ids, 1)

	r := newEnvRecorder()
	m, err := New(Options{Root: root, Runner: r, Recovery: RecoverRequeue})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	// Resume re-runs the SAME job id: the runner sees Resume=true, the
	// persisted contract and timeout survived the crash.
	resumed, err := m.Resume(context.Background(), ids[0])
	require.NoError(t, err)
	assert.Equal(t, ids[0], resumed.ID)

	// The recording runner finishes instantly — "running" is not a
	// stable state to observe. The env snapshot is the real signal:
	// wait until the resumed run actually happened.
	require.Eventually(t, func() bool {
		return len(r.snapshot()) == 1
	}, 5*time.Second, 5*time.Millisecond, "resumed job should run")

	seen := r.snapshot()
	require.Len(t, seen, 1)
	assert.True(t, seen[0].Job.Resume, "resumed run carries the resume flag")
	assert.Equal(t, []string{"go-build"}, seen[0].Job.Contract)
	assert.Equal(t, 5*time.Minute, time.Duration(seen[0].Job.TimeoutSec)*time.Second)

	// The resumer re-stamped the owner: this manager's own stamp
	// (read back after the run settled on disk).
	require.Eventually(t, func() bool {
		got, err := m.Get(context.Background(), ids[0])
		return err == nil && got.Status.Terminal()
	}, 5*time.Second, 5*time.Millisecond, "resumed job should finish")
	got, err := m.Get(context.Background(), ids[0])
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, got.Status)
	assert.Equal(t, m.ownerStr, got.Owner)

	// Resuming a terminal job errors.
	_, err = m.Resume(context.Background(), ids[0])
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestResumePendingFiresAllParkedJobs(t *testing.T) {
	root := t.TempDir()
	runHelper(t, root, 3, "crash")

	// A gated runner holds every resumed job live until the test
	// releases it — an instant runner completes before LiveCount can
	// observe the fleet.
	gate := make(chan struct{})
	r := RunnerFunc(func(ctx context.Context, env RunEnv) (string, error) {
		select {
		case <-gate:
			return "resumed: " + env.Job.ID, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	m, err := New(Options{Root: root, Runner: r, Recovery: RecoverRequeue})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	list, err := m.List(context.Background())
	require.NoError(t, err)
	parked := 0
	for _, info := range list {
		if info.Status == StatusPending {
			parked++
		}
	}
	require.Equal(t, 3, parked)

	require.NoError(t, m.ResumePending(context.Background()))
	require.Eventually(t, func() bool { return m.LiveCount() == 3 }, 5*time.Second, 5*time.Millisecond)

	// Drain: release the gate so Close does not wait on live jobs.
	close(gate)
	for _, info := range list {
		_, err := m.Wait(context.Background(), info.ID)
		require.NoError(t, err)
	}
}

// recordingRunner finishes immediately with a canned summary.
type recordingRunner struct{}

func (recordingRunner) Run(_ context.Context, env RunEnv) (string, error) {
	return "recorded: " + env.Job.ID, nil
}

func newRecordingRunner() Runner { return recordingRunner{} }

// envRecorder records every RunEnv it sees (mutex-guarded: concurrent
// jobs run in parallel) and finishes immediately.
type envRecorder struct {
	mu   sync.Mutex
	seen []RunEnv
}

func newEnvRecorder() *envRecorder { return &envRecorder{} }

func (r *envRecorder) Run(_ context.Context, env RunEnv) (string, error) {
	r.mu.Lock()
	r.seen = append(r.seen, env)
	r.mu.Unlock()
	return "recorded: " + env.Job.ID, nil
}

func (r *envRecorder) snapshot() []RunEnv {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RunEnv(nil), r.seen...)
}
