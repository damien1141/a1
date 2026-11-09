package agent

import (
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/vault"
)

// hasSessionSearch reports whether the engine's core tool list carries
// session_search.
func hasSessionSearch(eng *Engine) bool {
        for _, tl := range eng.buildCoreTools(nil) {
                if tl.Definition.Name == "session_search" {
                        return true
                }
        }
        return false
}

// TestSessionSearchRegistrationFollowsSessionDir pins the registration
// gate: a session that persists under a directory can list its siblings,
// so session_search registers; an in-memory session has nothing to search
// and must not advertise the tool. Both backings count — file and vault.
func TestSessionSearchRegistrationFollowsSessionDir(t *testing.T) {
        dir := t.TempDir()
        sess, err := NewSession(WithSessionDir(dir), WithPersist(true), WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := NewEngine(llm.ModelConfig{Name: "m"}, sess)
        require.NoError(t, err)
        assert.True(t, hasSessionSearch(eng), "persisting file-backed session: tool registered")

        mem, err := NewSession()
        require.NoError(t, err)
        engMem, err := NewEngine(llm.ModelConfig{Name: "m"}, mem)
        require.NoError(t, err)
        assert.False(t, hasSessionSearch(engMem), "in-memory session: nothing to search, no tool advertised")
}

// TestSessionSearchRegistrationVaultManager pins the vault branch of the
// same gate: NewVaultManager must receive the session dir (it flows into
// Manager.SessionDir) or every vault-backed session would silently lose
// the tool the file era got.
func TestSessionSearchRegistrationVaultManager(t *testing.T) {
        v, err := vault.Open(t.TempDir())
        require.NoError(t, err)
        t.Cleanup(func() { _ = v.Close() })
        session.SetDefaultVault(v)
        t.Cleanup(func() { session.SetDefaultVault(nil) })

        dir := t.TempDir()
        sess, err := NewSession(WithSessionDir(dir), WithPersist(true), WithCwd(t.TempDir()))
        require.NoError(t, err)
        assert.NotEmpty(t, sess.SessionDir(), "vault manager must carry the session dir")

        eng, err := NewEngine(llm.ModelConfig{Name: "m"}, sess)
        require.NoError(t, err)
        assert.True(t, hasSessionSearch(eng), "persisting vault-backed session: tool registered")
}
