package job

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The warden tests exercise the full owner-liveness arc with a real
// helper process: a live owner's job is never touched, and the moment
// that owner dies, the RUNNING sweep — not construction-time recovery —
// parks the orphan.

func TestWardenSkipsLiveOwnerThenParksDeadOwner(t *testing.T) {
	root := t.TempDir()

	// The watcher boots FIRST: when the helper's job lands, the
	// manager is already alive — only its warden can see the orphan.
	m, err := New(Options{
		Root:       root,
		Runner:     newRecordingRunner(),
		Recovery:   RecoverRequeue,
		WardenTick: 20 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	// A live helper process owns a running job over the same root
	// (the TUI / a1 serve coexistence). It starts async — wait for the
	// job to land on disk.
	cmd := runHelper(t, root, 1, "linger")
	require.Eventually(t, func() bool {
		return len(listIDsFrom(t, root)) == 1
	}, 10*time.Second, 20*time.Millisecond, "helper job should land on disk")
	ids := listIDsFrom(t, root)
	require.Len(t, ids, 1)
	waitStatusRunning(t, root, ids[0])

	// Many warden ticks pass: the live owner's job is NOT parked.
	time.Sleep(150 * time.Millisecond)
	meta, err := readMetaFrom(root, ids[0])
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, meta.Status, "a live owner's job is left alone")
	assert.NotEmpty(t, meta.Owner, "the helper stamped its owner")

	// The owner dies for real.
	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()

	// The warden's orphan sweep parks it — runtime, not construction.
	require.Eventually(t, func() bool {
		meta, err := readMetaFrom(root, ids[0])
		return err == nil && meta.Status == StatusPending
	}, 3*time.Second, 20*time.Millisecond, "dead owner's job parked by the sweep")
	meta, err = readMetaFrom(root, ids[0])
	require.NoError(t, err)
	assert.Equal(t, 1, meta.Attempt, "sweep bumps the attempt exactly once")
	assert.Empty(t, meta.Owner, "parking clears the dead owner stamp")
}

func TestWardenParksDeadOwnerUnderMarkFailed(t *testing.T) {
	root := t.TempDir()
	runHelper(t, root, 1, "crash")

	m, err := New(Options{
		Root:       root,
		Runner:     newRecordingRunner(),
		Recovery:   RecoverMarkFailed,
		WardenTick: 20 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close(context.Background()) })

	ids := listIDsFrom(t, root)
	require.Len(t, ids, 1)
	require.Eventually(t, func() bool {
		meta, err := readMetaFrom(root, ids[0])
		return err == nil && meta.Status == StatusFailed
	}, 3*time.Second, 20*time.Millisecond, "MarkFailed policy fails the orphan")
}

func TestNestedManagersNeverSweep(t *testing.T) {
	root := t.TempDir()

	// A live linger helper's job over the root.
	runHelper(t, root, 1, "linger")
	require.Eventually(t, func() bool {
		return len(listIDsFrom(t, root)) == 1
	}, 10*time.Second, 20*time.Millisecond, "helper job should land on disk")
	ids := listIDsFrom(t, root)
	require.Len(t, ids, 1)
	waitStatusRunning(t, root, ids[0])

	// A nested manager (RecoverIgnore) boots with a fast would-be
	// tick and must not sweep anything — not at construction, not
	// after.
	_, err := New(Options{
		Root:       root,
		Runner:     newRecordingRunner(),
		Recovery:   RecoverIgnore,
		WardenTick: 20 * time.Millisecond,
	})
	require.NoError(t, err)

	time.Sleep(150 * time.Millisecond)
	meta, err := readMetaFrom(root, ids[0])
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, meta.Status, "nested managers do not sweep")
}
