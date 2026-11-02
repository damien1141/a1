package session_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/vault"
)

func withVault(t *testing.T) *vault.Vault {
	t.Helper()
	v, err := vault.Open(t.TempDir())
	require.NoError(t, err)
	session.SetDefaultVault(v)
	t.Cleanup(func() {
		session.SetDefaultVault(nil)
		_ = v.Close()
	})
	return v
}

// TestVaultSessionRoundTrip covers the full Bet 08 session lifecycle:
// create (rows), append, reopen by synthetic path, append again, list.
func TestVaultSessionRoundTrip(t *testing.T) {
	v := withVault(t)
	dir := t.TempDir()

	m, err := session.NewVaultManager("/tmp/proj", dir, "", v)
	require.NoError(t, err)

	// Nothing hits the store until the first assistant message (the
	// file-era flush gate).
	has, err := v.HasSession(m.ID())
	require.NoError(t, err)
	assert.False(t, has, "unflushed session leaves no rows")

	_, err = m.Append(llm.Message{Role: llm.RoleUser, Content: "hi"})
	require.NoError(t, err)
	has, err = v.HasSession(m.ID())
	require.NoError(t, err)
	assert.False(t, has, "still nothing before an assistant message")

	_, err = m.Append(llm.Message{Role: llm.RoleAssistant, Content: "hello"})
	require.NoError(t, err)
	has, err = v.HasSession(m.ID())
	require.NoError(t, err)
	assert.True(t, has, "first assistant message flushes the session")

	// Reopen via the synthetic path (the resume path every caller uses).
	path := m.File()
	m2, err := session.OpenSession(path)
	require.NoError(t, err)
	assert.Equal(t, m.ID(), m2.ID())
	assert.Equal(t, path, m2.File())
	require.Equal(t, 3, m2.Len(), "header + user + assistant survive replay")

	// Appends continue into the same vault session.
	_, err = m2.Append(llm.Message{Role: llm.RoleUser, Content: "again"})
	require.NoError(t, err)
	lines, err := v.LoadSessionEntries(m.ID())
	require.NoError(t, err)
	require.Len(t, lines, 4)

	// Listing merges the vault row and reports the synthetic path.
	metas, err := session.ListSessions(dir)
	require.NoError(t, err)
	require.Len(t, metas, 1)
	assert.Equal(t, m.ID(), metas[0].ID)
	assert.Equal(t, path, metas[0].File)

	// Resume by id resolves through the vault.
	resolved, err := session.FindSessionFile(dir, m.ID())
	require.NoError(t, err)
	assert.Equal(t, path, resolved)

	// Prefix resolution works too.
	resolved, err = session.FindSessionFile(dir, m.ID()[:8])
	require.NoError(t, err)
	assert.Equal(t, path, resolved)

	// Delete cascades the entries.
	require.NoError(t, v.DeleteSession(m.ID()))
	metas, err = session.ListSessions(dir)
	require.NoError(t, err)
	assert.Empty(t, metas)
}

// TestVaultSessionForkKeepsBacking pins Fork on a vault-backed session.
func TestVaultSessionForkKeepsBacking(t *testing.T) {
	v := withVault(t)
	dir := t.TempDir()

	m, err := session.NewVaultManager("/tmp/proj", dir, "", v)
	require.NoError(t, err)
	_, err = m.Append(llm.Message{Role: llm.RoleUser, Content: "hi"})
	require.NoError(t, err)
	_, err = m.Append(llm.Message{Role: llm.RoleAssistant, Content: "hello"})
	require.NoError(t, err)

	fork, err := m.Fork()
	require.NoError(t, err)
	assert.NotEqual(t, m.ID(), fork.ID())

	has, err := v.HasSession(fork.ID())
	require.NoError(t, err)
	assert.True(t, has, "fork flushes into the vault")

	_, err = fork.Append(llm.Message{Role: llm.RoleUser, Content: "branch"})
	require.NoError(t, err)
	lines, err := v.LoadSessionEntries(fork.ID())
	require.NoError(t, err)
	require.Len(t, lines, 4, "header + replayed pair + branch append")
}

// TestFileModeUnaffected pins that with no default vault installed the
// JSONL behavior is byte-identical to pre-Bet 08.
func TestFileModeUnaffected(t *testing.T) {
	dir := t.TempDir()
	m, err := session.NewSessionManager("/tmp/proj",
		session.WithSessionDir(dir), session.WithShouldFlush(true))
	require.NoError(t, err)
	_, err = m.Append(llm.Message{Role: llm.RoleUser, Content: "hi"})
	require.NoError(t, err)
	_, err = m.Append(llm.Message{Role: llm.RoleAssistant, Content: "hello"})
	require.NoError(t, err)

	metas, err := session.ListSessions(dir)
	require.NoError(t, err)
	require.Len(t, metas, 1)

	m2, err := session.OpenSession(metas[0].File)
	require.NoError(t, err)
	require.Equal(t, 3, m2.Len())
}
