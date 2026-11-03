package job

import (
        "context"
        "database/sql"
        "path/filepath"
        "sync"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        _ "modernc.org/sqlite"
)

// blockingRunner hangs until released, so its jobs are always mid-flight
// when the "process dies" (the manager is abandoned without Close).
type blockingRunner struct {
        mu      sync.Mutex
        release map[string]chan struct{}
        seen    []RunEnv
}

func newBlockingRunner() *blockingRunner {
        return &blockingRunner{release: map[string]chan struct{}{}}
}

func (r *blockingRunner) Run(ctx context.Context, env RunEnv) (string, error) {
        r.mu.Lock()
        ch, ok := r.release[env.Job.ID]
        if !ok {
                ch = make(chan struct{})
                r.release[env.Job.ID] = ch
        }
        r.seen = append(r.seen, env)
        r.mu.Unlock()
        select {
        case <-ch:
                return "done: " + env.Job.ID, nil
        case <-ctx.Done():
                return "", ctx.Err()
        }
}

func TestRecoverRequeueParksInsteadOfFailing(t *testing.T) {
        root := t.TempDir()
        r1 := newBlockingRunner()
        m1, err := New(Options{Root: root, Runner: r1, Recovery: RecoverRequeue})
        require.NoError(t, err)

        info, err := m1.Spawn(context.Background(), SpawnRequest{Prompt: "long work", Kind: KindSchedule, ScheduleID: "sch_1"})
        require.NoError(t, err)
        require.Eventually(t, func() bool {
                got, err := m1.Get(context.Background(), info.ID)
                return err == nil && got.Status == StatusRunning
        }, 2*time.Second, 5*time.Millisecond, "job should reach running")

        // Abandon the manager without Close — the process "died" mid-run.
        // A new manager over the same root requeues the leftover as pending.
        m2, err := New(Options{Root: root, Runner: r1, Recovery: RecoverRequeue})
        require.NoError(t, err)
        got, err := m2.Get(context.Background(), info.ID)
        require.NoError(t, err)
        assert.Equal(t, StatusPending, got.Status)
        assert.Equal(t, 1, got.Attempt, "requeue bumps the attempt counter")
        assert.Equal(t, KindSchedule, got.Kind)
        assert.Equal(t, "sch_1", got.ScheduleID)

        // Wait reports the parked state instead of a zombie hang.
        _, err = m2.Wait(context.Background(), info.ID)
        assert.ErrorIs(t, err, ErrNotRunning)

        // Cancel finalizes a parked job in place.
        require.NoError(t, m2.Cancel(context.Background(), info.ID))
        got, err = m2.Get(context.Background(), info.ID)
        require.NoError(t, err)
        assert.Equal(t, StatusCancelled, got.Status)
}

func TestResumeRunsAgainWithResumeFlag(t *testing.T) {
        root := t.TempDir()
        // Separate runners, unlike TestRecoverRequeueParksInsteadOfFailing:
        // releasing a SHARED runner's channels would also wake the abandoned
        // m1's goroutine, whose completion persist then races m2's on the same
        // meta.json. A real process death leaves no goroutine behind (the
        // requeue tests' own rule), so m1's runner is never released — its
        // goroutine stays parked exactly like a dead process's would.
        r1 := newBlockingRunner()
        r2 := newBlockingRunner()
        m1, err := New(Options{Root: root, Runner: r1, Recovery: RecoverRequeue})
        require.NoError(t, err)

        info, err := m1.Spawn(context.Background(), SpawnRequest{
                Prompt: "restart me", Contract: []string{"go-build"}, Timeout: 5 * time.Second,
        })
        require.NoError(t, err)
        require.Eventually(t, func() bool {
                got, _ := m1.Get(context.Background(), info.ID)
                return got.Status == StatusRunning
        }, 2*time.Second, 5*time.Millisecond)

        // Crash + requeue.
        m2, err := New(Options{Root: root, Runner: r2, Recovery: RecoverRequeue})
        require.NoError(t, err)

        // Resume re-runs the SAME job id: the runner sees Resume=true, the
        // persisted contract and timeout survived.
        resumed, err := m2.Resume(context.Background(), info.ID)
        require.NoError(t, err)
        assert.Equal(t, info.ID, resumed.ID)

        require.Eventually(t, func() bool {
                got, _ := m2.Get(context.Background(), info.ID)
                return got.Status == StatusRunning
        }, 2*time.Second, 5*time.Millisecond)

        r1.mu.Lock()
        require.Len(t, r1.seen, 1)
        assert.False(t, r1.seen[0].Job.Resume, "first attempt is a fresh run")
        r1.mu.Unlock()
        r2.mu.Lock()
        require.Len(t, r2.seen, 1)
        assert.True(t, r2.seen[0].Job.Resume, "resumed run carries the resume flag")
        assert.Equal(t, []string{"go-build"}, r2.seen[0].Job.Contract)
        for _, ch := range r2.release {
                close(ch)
        }
        r2.mu.Unlock()

        closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
        defer closeCancel()
        require.NoError(t, m2.Close(closeCtx))

        // Resuming a terminal job errors.
        _, err = m2.Resume(context.Background(), info.ID)
        assert.ErrorIs(t, err, ErrInvalid)
}

func TestResumePendingFiresAllParkedJobs(t *testing.T) {
        root := t.TempDir()
        r := newBlockingRunner()
        m1, err := New(Options{Root: root, Runner: r, Recovery: RecoverRequeue})
        require.NoError(t, err)
        ids := make([]string, 3)
        for i := range ids {
                info, err := m1.Spawn(context.Background(), SpawnRequest{Prompt: "parked"})
                require.NoError(t, err)
                ids[i] = info.ID
        }
        // Wait for each job's RUNNING state to be PERSISTED before abandoning
        // the manager: a real process death leaves no goroutine behind to race
        // the requeue's meta write, and the test must model that — polling
        // LiveCount alone can observe `starting` before run()'s first persist.
        for _, id := range ids {
                require.Eventually(t, func() bool {
                        got, err := m1.Get(context.Background(), id)
                        return err == nil && got.Status == StatusRunning
                }, 2*time.Second, 5*time.Millisecond, "job %s should persist running", id)
        }

        m2, err := New(Options{Root: root, Runner: r, Recovery: RecoverRequeue})
        require.NoError(t, err)
        list, err := m2.List(context.Background())
        require.NoError(t, err)
        parked := 0
        for _, info := range list {
                if info.Status == StatusPending {
                        parked++
                }
        }
        require.Equal(t, 3, parked)

        require.NoError(t, m2.ResumePending(context.Background()))
        require.Eventually(t, func() bool { return m2.LiveCount() == 3 }, 2*time.Second, 5*time.Millisecond)

        // Drain: release the runners and close the manager so t.TempDir's
        // cleanup does not race live event writes.
        r.mu.Lock()
        for _, ch := range r.release {
                close(ch)
        }
        r.mu.Unlock()
        closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
        defer closeCancel()
        require.NoError(t, m2.Close(closeCtx))
}

func TestJobRowsMirrorPersist(t *testing.T) {
        dir := t.TempDir()
        db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "jobs.db"))
        require.NoError(t, err)
        t.Cleanup(func() { db.Close() })

        var ran []Meta
        m, err := New(Options{
                Root:     filepath.Join(dir, "jobs"),
                Runner:   RunnerFunc(func(_ context.Context, env RunEnv) (string, error) { ran = append(ran, env.Job); return "ok", nil }),
                DB:       db,
                Recovery: RecoverRequeue,
        })
        require.NoError(t, err)

        info, err := m.Spawn(context.Background(), SpawnRequest{
                Prompt: "row mirror", Kind: KindSchedule, ScheduleID: "sch_x",
        })
        require.NoError(t, err)
        _, err = m.Wait(context.Background(), info.ID)
        require.NoError(t, err)
        require.Equal(t, StatusRunning, ran[0].Status, "RunEnv snapshot is taken at run start")

        rows, err := ListJobRows(db)
        require.NoError(t, err)
        require.Len(t, rows, 1)
        assert.Equal(t, info.ID, rows[0].ID)
        assert.Equal(t, KindSchedule, rows[0].Kind)
        assert.Equal(t, "sch_x", rows[0].ScheduleID)
        assert.Equal(t, string(StatusCompleted), rows[0].Status)
        assert.False(t, rows[0].CreatedAt.IsZero())
}
