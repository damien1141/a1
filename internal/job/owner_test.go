package job

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOwnerIdentityRoundTrip(t *testing.T) {
	cur := currentOwner()
	require.False(t, cur.zero(), "current owner is set")
	assert.True(t, cur.alive(), "this process is alive")

	back := parseOwner(cur.String())
	assert.Equal(t, cur, back, "String/parseOwner round-trip")
	assert.Equal(t, cur.String(), back.String())

	// Degenerate forms.
	assert.True(t, parseOwner("").zero(), "empty stamp is the zero identity")
	assert.True(t, parseOwner("not-a-pid").zero(), "garbage is the zero identity")
	assert.True(t, parseOwner("-4").zero(), "negative pid is the zero identity")
	pidOnly := parseOwner("123")
	assert.Equal(t, 123, pidOnly.pid)
	assert.Equal(t, uint64(0), pidOnly.startTime)
}

func TestOwnerAliveDetectsRecycledPID(t *testing.T) {
	// A start-time mismatch means the pid was recycled: a different
	// process now wears it, so the stamped owner is dead as far as any
	// of its jobs are concerned.
	recycled := parseOwner(fmt.Sprintf("%d:999999999", os.Getpid()))
	assert.False(t, recycled.alive(), "pid with foreign start time is not this owner")
}

func TestOwnerAliveDetectsDeadPID(t *testing.T) {
	// Spawn a short-lived child, reap it, and confirm its pid reads
	// dead. (Pid reuse inside the test's lifetime is a theoretical
	// flake; in practice the allocator does not hand it back this
	// fast, and the loop tries twice.)
	for i := 0; i < 2; i++ {
		cmd := exec.Command("true")
		require.NoError(t, cmd.Start())
		pid := cmd.Process.Pid
		require.NoError(t, cmd.Wait())
		if !parseOwner(fmt.Sprintf("%d", pid)).alive() {
			return
		}
	}
	t.Fatal("a reaped child pid kept reading alive twice in a row")
}
