package permission

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEffectLogCommitAndHas(t *testing.T) {
	l := NewEffectLog()
	l.Commit("bash", "ticket_created")
	l.Commit("write", "ticket_created")
	l.Commit("bash", "email_sent")

	assert.True(t, l.Has("ticket_created"))
	assert.True(t, l.Has("email_sent"))
	assert.False(t, l.Has("missing"))
	assert.Equal(t, 2, l.Count("ticket_created"))
	assert.Equal(t, 1, l.Count("email_sent"))
}

func TestEffectLogIgnoresEmptyToken(t *testing.T) {
	l := NewEffectLog()
	l.Commit("bash", "")
	assert.False(t, l.Has(""))
	assert.Equal(t, 0, l.Count(""))
	assert.Empty(t, l.Entries())
}

func TestEffectLogCheckRequiredEffects(t *testing.T) {
	l := NewEffectLog()
	l.Commit("bash", "ticket_created")

	require.NoError(t, l.CheckRequiredEffects(nil))
	require.NoError(t, l.CheckRequiredEffects([]string{}))
	require.NoError(t, l.CheckRequiredEffects([]string{"ticket_created"}))
	require.Error(t, l.CheckRequiredEffects([]string{"ticket_created", "missing"}))
}

func TestEffectLogCheckNoPrior(t *testing.T) {
	l := NewEffectLog()
	l.Commit("bash", "ticket_created")

	require.NoError(t, l.CheckNoPrior("missing"))
	require.NoError(t, l.CheckNoPrior(""))
	require.Error(t, l.CheckNoPrior("ticket_created"))
}

func TestEffectLogEntriesSnapshot(t *testing.T) {
	l := NewEffectLog()
	l.Commit("bash", "a")
	l.Commit("write", "b")

	entries := l.Entries()
	require.Len(t, entries, 2)
	assert.Equal(t, "bash", entries[0].Tool)
	assert.Equal(t, "a", entries[0].EffectToken)
}

func TestEffectLogConcurrentAccess(t *testing.T) {
	l := NewEffectLog()
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				l.Commit("bash", "token")
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	assert.Equal(t, 1000, l.Count("token"))
}
