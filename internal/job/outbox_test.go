package job

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *store {
	t.Helper()
	st, err := newStore(t.TempDir(), nil)
	require.NoError(t, err)
	return st
}

func testMeta(id, parent string) Meta {
	return Meta{
		ID:          id,
		ParentID:    parent,
		Description: "find auth config",
		Role:        Role("explore"),
		Status:      StatusCompleted,
		FinishedAt:  time.Now().UTC(),
		Dir:         "/jobs/" + id,
	}
}

func TestDeliveryPublishListClaimAck(t *testing.T) {
	st := newTestStore(t)

	// No parent, no row — schedule-fired work is pulled, not pushed.
	require.NoError(t, st.publishDelivery(testMeta("job_a", "")))
	rows, err := st.listDeliveries("")
	require.NoError(t, err)
	assert.Empty(t, rows)

	// With a parent, the row lands and lists under that parent.
	require.NoError(t, st.publishDelivery(testMeta("job_a", "sess_1")))
	require.NoError(t, st.publishDelivery(testMeta("job_b", "sess_2")))
	rows, err = st.listDeliveries("sess_1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "job_a", rows[0].JobID)
	assert.Equal(t, StatusCompleted, rows[0].Status)

	// Claim wins once; the fixed-name mutex blocks the second claimant.
	now := time.Now()
	assert.True(t, st.claimDelivery(rows[0], time.Minute, now))
	assert.False(t, st.claimDelivery(rows[0], time.Minute, now), "second claim on a held row fails")

	// Ack removes row and claim.
	st.ackDelivery("job_a")
	rows, err = st.listDeliveries("sess_1")
	require.NoError(t, err)
	assert.Empty(t, rows)
	_, err = os.Stat(filepath.Join(st.outboxPath(), "job_a"+claimExt))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestDeliveryReleaseKeepsRow(t *testing.T) {
	st := newTestStore(t)
	require.NoError(t, st.publishDelivery(testMeta("job_a", "sess_1")))
	row := mustListOne(t, st, "sess_1")

	assert.True(t, st.claimDelivery(row, time.Minute, time.Now()))
	st.releaseDelivery("job_a")
	// Row survives a release — a later turn re-presents.
	rows, err := st.listDeliveries("sess_1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	// And the row is claimable again.
	assert.True(t, st.claimDelivery(rows[0], time.Minute, time.Now()))
}

func TestDeliveryClaimStealsStaleClaims(t *testing.T) {
	st := newTestStore(t)
	require.NoError(t, st.publishDelivery(testMeta("job_a", "sess_1")))
	row := mustListOne(t, st, "sess_1")

	// A dead claimant's leftover: claim file back-dated past the stale
	// horizon gets stolen, not honored.
	assert.True(t, st.claimDelivery(row, time.Minute, time.Now()))
	claimPath := filepath.Join(st.outboxPath(), "job_a"+claimExt)
	past := time.Now().Add(-2 * time.Minute)
	require.NoError(t, os.Chtimes(claimPath, past, past))

	assert.True(t, st.claimDelivery(row, time.Minute, time.Now()), "stale claim is stolen")
}

func TestDeliverySweepReleasesAndDrops(t *testing.T) {
	st := newTestStore(t)

	// Fresh claim on a fresh row: untouched by the sweep.
	require.NoError(t, st.publishDelivery(testMeta("job_fresh", "sess_1")))
	fresh := mustListOne(t, st, "sess_1")
	assert.True(t, st.claimDelivery(fresh, time.Minute, time.Now()))

	// Aged claim on a fresh row: released, row survives.
	require.NoError(t, st.publishDelivery(testMeta("job_claim_old", "sess_1")))
	claimPath := filepath.Join(st.outboxPath(), "job_claim_old"+claimExt)
	require.NoError(t, os.WriteFile(claimPath, nil, 0o644))
	past := time.Now().Add(-10 * time.Minute)
	require.NoError(t, os.Chtimes(claimPath, past, past))

	// Aged row nobody collected: dropped outright.
	require.NoError(t, st.publishDelivery(testMeta("job_row_old", "sess_2")))
	rowPath := filepath.Join(st.outboxPath(), "job_row_old"+deliveryExt)
	require.NoError(t, os.Chtimes(rowPath, past, past))

	st.sweepDeliveries(time.Minute, 5*time.Minute, time.Now())

	_, err := os.Stat(claimPath)
	assert.ErrorIs(t, err, os.ErrNotExist, "aged claim released")
	_, err = os.Stat(rowPath)
	assert.ErrorIs(t, err, os.ErrNotExist, "aged row dropped")

	// The fresh pair is untouched.
	_, err = os.Stat(filepath.Join(st.outboxPath(), "job_fresh"+deliveryExt))
	require.NoError(t, err)
	rows, err := st.listDeliveries("")
	require.NoError(t, err)
	assert.Len(t, rows, 2, "fresh row and released-claim row survive")
}

func TestCollectNoticesPresentsOnceAndAckDelivery(t *testing.T) {
	root := t.TempDir()
	m, err := New(Options{Root: root, Runner: newRecordingRunner(), Recovery: RecoverRequeue})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	info, err := m.Spawn(context.Background(), SpawnRequest{
		Prompt: "notify me", ParentID: "sess_parent", Description: "the job",
	})
	require.NoError(t, err)
	res, err := m.Wait(context.Background(), info.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, res.Info.Status)

	// The outbox presents the finished job exactly once.
	first := m.CollectNotices("sess_parent")
	require.Len(t, first, 1)
	assert.Equal(t, info.ID, first[0].JobID)
	assert.Equal(t, StatusCompleted, first[0].Status)
	assert.Equal(t, "the job", first[0].Description)
	assert.Contains(t, first[0].Summary, info.ID, "summary is the job's result.md")

	assert.Empty(t, m.CollectNotices("sess_parent"), "presented once, then acked")
	assert.Empty(t, m.CollectNotices("sess_other"), "other parents see nothing")

	// AckDelivery on a missing row is a no-op.
	m.AckDelivery(info.ID)
	m.AckDelivery("")
}

func TestConcurrentClaimsElectOneWinner(t *testing.T) {
	st := newTestStore(t)
	require.NoError(t, st.publishDelivery(testMeta("job_race", "sess_1")))

	const n = 8
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := st.listDeliveries("sess_1")
			if err != nil || len(rows) == 0 {
				return
			}
			if st.claimDelivery(rows[0], time.Minute, time.Now()) {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), wins.Load(), "exactly one claimant wins the fixed-name mutex")
}

func TestWardenDeliversQueuedCancelAndAgedDrop(t *testing.T) {
	root := t.TempDir()
	// One slot, held by a gated blocker: the victim joins the FIFO
	// queue, so Cancel finalizes it as queued-cancelled — the fold
	// that must still publish a delivery row for the parent.
	gate := make(chan struct{})
	blocker := RunnerFunc(func(ctx context.Context, env RunEnv) (string, error) {
		select {
		case <-gate:
			return "blocker done", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	m, err := New(Options{
		Root:          root,
		Runner:        blocker,
		Recovery:      RecoverRequeue,
		MaxConcurrent: 1,
		WardenTick:    20 * time.Millisecond,
		DropAge:       60 * time.Millisecond,
	})
	require.NoError(t, err)
	// No gate-close here: Close (below) cancels the blocker directly,
	// so a mid-test require failure still drains cleanly.
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	hold, err := m.Spawn(context.Background(), SpawnRequest{Prompt: "hold the slot"})
	require.NoError(t, err)

	info, err := m.Spawn(context.Background(), SpawnRequest{
		Prompt: "cancelled while queued", ParentID: "sess_q",
	})
	require.NoError(t, err)
	require.NoError(t, m.Cancel(context.Background(), info.ID))

	got, err := m.Get(context.Background(), info.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, got.Status, "victim cancels while queued")

	rows, err := m.store.listDeliveries("sess_q")
	require.NoError(t, err)
	require.Len(t, rows, 1, "cancelled job still notifies its parent")
	assert.Equal(t, StatusCancelled, rows[0].Status)

	// The warden's aged pass drops it once the drop horizon passes.
	require.Eventually(t, func() bool {
		rows, err := m.store.listDeliveries("sess_q")
		return err == nil && len(rows) == 0
	}, 3*time.Second, 20*time.Millisecond, "warden drops the uncollected row past DropAge")

	// Release the blocker so Close drains cleanly.
	close(gate)
	_, err = m.Wait(context.Background(), hold.ID)
	require.NoError(t, err)
}

func mustListOne(t *testing.T, st *store, parent string) deliveryRow {
	t.Helper()
	rows, err := st.listDeliveries(parent)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return rows[0]
}
