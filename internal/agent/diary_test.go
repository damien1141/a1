package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/vault"
)

// TestUpdateSessionValence pins the Bet 06 mood computation: regressions
// pull the session's valence down far enough to arm selectBundle's §3.5
// mood-congruency override (-0.3 threshold), certificates pull it up.
func TestUpdateSessionValence(t *testing.T) {
	engine := newContractTestEngine(t)

	// Neutral by default.
	assert.Equal(t, 0.0, engine.loadSessionValence())

	// One protected regression: -0.2.
	engine.evidence.ObserveExit("go test ./...", "ok  \tgithub.com/x/auth\t0.1s\nPASS", 0, false, time.Now())
	engine.evidence.ObserveExit("go test ./...", "FAIL\tgithub.com/x/auth\t0.1s\nFAIL", 1, true, time.Now())
	engine.turnContract.declare([]string{"go-build"}) // not green: no evidence
	engine.updateSessionValence()
	assert.InDelta(t, -0.2, engine.loadSessionValence(), 1e-9)

	// More regressions cross the -0.3 override threshold.
	engine.evidence.ObserveExit("go vet ./...", "ok  \tgithub.com/x/auth", 0, false, time.Now())
	engine.evidence.ObserveExit("go vet ./...", "# github.com/x/auth\nx.go:1: unused import", 1, true, time.Now())
	engine.updateSessionValence()
	assert.InDelta(t, -0.4, engine.loadSessionValence(), 1e-9)
	assert.Less(t, engine.loadSessionValence(), -0.3, "the mood override is armed")
}

// TestRecallEpisodesInjects pins the episodic store's recall path: a
// recorded episode for this project rides into the inlet block, other
// projects' episodes do not.
func TestRecallEpisodesInjects(t *testing.T) {
	engine := newContractTestEngine(t)

	v, err := vault.Open(t.TempDir())
	require.NoError(t, err)
	session.SetDefaultVault(v)
	t.Cleanup(func() {
		session.SetDefaultVault(nil)
		_ = v.Close()
	})

	require.NoError(t, v.PutEpisode("/tmp/proj", "compaction", "fixed the flaky auth test by pinning the clock"))
	require.NoError(t, v.PutEpisode("/tmp/other", "compaction", "unrelated project"))

	got := recallEpisodes(engine, "/tmp/proj")
	require.NotEmpty(t, got)
	assert.Contains(t, got, "flaky auth test")
	assert.NotContains(t, got, "unrelated project")
	assert.Contains(t, got, "<episode")

	// Unknown project: nothing.
	assert.Empty(t, recallEpisodes(engine, "/tmp/elsewhere"))
}

// TestRecordEpisodeNoVaultIsNoop pins the chat-inert guarantee: without
// a live vault nothing records and nothing breaks.
func TestRecordEpisodeNoVaultIsNoop(t *testing.T) {
	engine := newContractTestEngine(t)
	assert.Equal(t, "", engine.SessionFile()) // in-memory session
	recordEpisode(engine, "summary")
}

// TestMemoryConsolidationClientWithoutPersona pins the fallback: with no
// persona store attached, consolidation uses the engine's main client.
func TestMemoryConsolidationClientWithoutPersona(t *testing.T) {
	engine := newContractTestEngine(t)
	assert.Same(t, engine.client, engine.memoryConsolidationClient())
}
