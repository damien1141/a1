package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/session"
)

// scheduleFireSession builds an in-memory session whose header carries the
// scheduler's pseudo-parent — the header shape every fire of a schedule
// runs under once the Clock spawns it.
func scheduleFireSession(t *testing.T) *Session {
	t.Helper()
	m, err := session.NewSessionManager(t.TempDir(), session.WithParent("schedule:job-7"))
	require.NoError(t, err)
	return &Session{manager: m}
}

// The engine's client carries the session's resolved scope, so OpenAI-wire
// requests route under the schedule's bucket instead of a fresh one per
// fire.
func TestEngineClientCarriesSessionCacheScope(t *testing.T) {
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:1", APIKey: "x"},
		scheduleFireSession(t),
		WithGate(permission.AllowAll{}),
	)
	require.NoError(t, err)
	assert.Equal(t, "schedule:job-7", engine.client.CacheScope())
}

// Every client rebuild (SetJobs, SetScheduler, model switch, persona swap)
// re-pins the scope — the bucket outlives the rebuild, exactly like the
// credential pool does.
func TestEngineRebindKeepsCacheScope(t *testing.T) {
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:1", APIKey: "x"},
		scheduleFireSession(t),
		WithGate(permission.AllowAll{}),
	)
	require.NoError(t, err)
	engine.SetJobs(nil) // forces a rebind, the client rebuild path
	assert.Equal(t, "schedule:job-7", engine.client.CacheScope())
}

// A plain session scopes to its own id — sub-agent siblings and independent
// conversations each keep their own bucket.
func TestEnginePlainSessionScopesToItself(t *testing.T) {
	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:1", APIKey: "x"},
		sess,
		WithGate(permission.AllowAll{}),
	)
	require.NoError(t, err)
	assert.Equal(t, sess.ID(), engine.client.CacheScope())
}
