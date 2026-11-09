package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// A session with no pin and no schedule marker scopes to itself: the
// physical session id is the bucket.
func TestCacheScopeDefaultsToSessionID(t *testing.T) {
	sm, err := NewSessionManager(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, sm.ID(), sm.CacheScope())
	assert.NotEmpty(t, sm.CacheScope())
}

// Fires of one schedule share the schedule's pseudo-parent as their scope,
// so repeat fires re-read one warm bucket instead of each minting a cold
// one. Two fires, same scope.
func TestCacheScopeScheduleFireSharesScheduleBucket(t *testing.T) {
	sm, err := NewSessionManager(t.TempDir(), WithParent("schedule:job-7"))
	require.NoError(t, err)
	assert.Equal(t, "schedule:job-7", sm.CacheScope())

	sibling, err := NewSessionManager(t.TempDir(), WithParent("schedule:job-7"))
	require.NoError(t, err)
	assert.Equal(t, sm.CacheScope(), sibling.CacheScope())
}

// A hex parent id is a real session (a sub-agent's spawner), not a schedule
// marker: each child keeps its own bucket. Walking real parents would
// collapse sibling sub-agents onto one bucket and let their divergent
// request streams evict each other's cached prefixes.
func TestCacheScopeRealParentNotWalked(t *testing.T) {
	sm, err := NewSessionManager(t.TempDir(), WithParent("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	assert.Equal(t, sm.ID(), sm.CacheScope())
	assert.NotEqual(t, "0123456789abcdef0123456789abcdef", sm.CacheScope())
}

// Fork pins the parent's RESOLVED scope on the child's header: the branch
// replays the parent's entries, so its first request re-reads the parent's
// cached prefix instead of minting a cold bucket under the fresh session id.
// A fork of the fork chains to the same root.
func TestForkStampsParentResolvedScope(t *testing.T) {
	dir := t.TempDir()
	root, err := NewSessionManager(dir, WithSessionDir(dir), WithShouldFlush(true))
	require.NoError(t, err)
	_, err = root.Append(llm.Message{Role: llm.RoleUser, Content: "hello"})
	require.NoError(t, err)

	fork, err := root.Fork()
	require.NoError(t, err)
	require.NotEqual(t, root.ID(), fork.ID(), "the branch is a new physical session")
	assert.Equal(t, root.ID(), fork.CacheScope(), "but routes under the root's bucket")

	// The pin lives on the header itself, not a runtime memo.
	header, ok := fork.entries[0].(SessionHeader)
	require.True(t, ok)
	assert.Equal(t, root.ID(), header.CacheScope)

	grand, err := fork.Fork()
	require.NoError(t, err)
	assert.Equal(t, root.ID(), grand.CacheScope(), "a fork of the fork keeps the root bucket")
}

// A branch taken inside a schedule fire keeps the schedule's scope — the
// parent's resolved scope is the marker, and that is what gets pinned.
func TestForkOfScheduleFireKeepsScheduleScope(t *testing.T) {
	dir := t.TempDir()
	fire, err := NewSessionManager(dir, WithSessionDir(dir), WithShouldFlush(true), WithParent("schedule:nightly"))
	require.NoError(t, err)

	fork, err := fire.Fork()
	require.NoError(t, err)
	assert.Equal(t, "schedule:nightly", fork.CacheScope())
}

// The scope rides the session header through persist and reload — both the
// pinned (forked) form and the derived (schedule-marker) form.
func TestCacheScopeSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	root, err := NewSessionManager(dir, WithSessionDir(dir), WithShouldFlush(true))
	require.NoError(t, err)
	// An assistant message is what makes the file land on disk.
	_, err = root.Append(llm.Message{Role: llm.RoleUser, Content: "hi"})
	require.NoError(t, err)
	_, err = root.Append(llm.Message{Role: llm.RoleAssistant, Content: "hello"})
	require.NoError(t, err)

	fork, err := root.Fork()
	require.NoError(t, err)

	reloaded, err := OpenSession(fork.File())
	require.NoError(t, err)
	assert.Equal(t, root.ID(), reloaded.CacheScope(), "the pinned scope rides the header through reload")

	sched, err := NewSessionManager(dir, WithSessionDir(dir), WithShouldFlush(true), WithParent("schedule:daily"))
	require.NoError(t, err)
	_, err = sched.Append(llm.Message{Role: llm.RoleUser, Content: "hi"})
	require.NoError(t, err)
	_, err = sched.Append(llm.Message{Role: llm.RoleAssistant, Content: "run"})
	require.NoError(t, err)

	reSched, err := OpenSession(sched.File())
	require.NoError(t, err)
	assert.Equal(t, "schedule:daily", reSched.CacheScope(), "the derived scope re-derives after reload")
}
