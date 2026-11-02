package persona

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The persona bank must live at <store root>/memory/persona:<id>.jsonl — the
// one location every reader and writer agrees on. Earlier builds opened the
// bank at <store root>/persona:<id>.jsonl on the agent side while the web
// manager read <global>/sessions/memory/persona:<id>.jsonl, so remembered
// facts landed in a file the UI never opened (memories "never saved").
func TestMemoryBankLocation(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	p, err := s.CreateInput(Input{Name: "Sherlock"})
	require.NoError(t, err)

	bank, err := s.MemoryBank(p)
	require.NoError(t, err)
	require.NoError(t, bank.Append("memory", "Clients call at all hours."))
	require.NoError(t, bank.Append("trace", "bash(ls) => ok"))

	want := filepath.Join(dir, "memory", "persona:"+p.ID+".jsonl")
	require.FileExists(t, want, "bank file must live under the store's memory dir")

	// A fresh handle through the same store counts only real memories.
	fresh, err := s.MemoryBank(p)
	require.NoError(t, err)
	assert.Equal(t, 1, fresh.MemoryCount(), "traces must not count as memories")
}

// Banks stranded at the legacy location (written by earlier builds before the
// dir was unified) are adopted into the canonical memory dir on first open.
func TestMemoryBankAdoptsLegacyLocation(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	p, err := s.CreateInput(Input{Name: "Watson"})
	require.NoError(t, err)

	legacy := filepath.Join(dir, "persona:"+p.ID+".jsonl")
	line := `{"session_id":"persona:watson","timestamp":"2025-01-01T00:00:00Z","type":"memory","content":"Old fact from an earlier build."}` + "\n"
	require.NoError(t, os.WriteFile(legacy, []byte(line), 0o644))

	bank, err := s.MemoryBank(p)
	require.NoError(t, err)
	assert.Equal(t, 1, bank.MemoryCount(), "legacy entries must survive the adoption")
	require.NoFileExists(t, legacy, "legacy file should be moved, not copied")
	require.FileExists(t, filepath.Join(dir, "memory", "persona:"+p.ID+".jsonl"))

	// A canonical bank is never overwritten by a stale legacy file.
	require.NoError(t, os.WriteFile(legacy, []byte(line), 0o644))
	bank2, err := s.MemoryBank(p)
	require.NoError(t, err)
	assert.Equal(t, 1, bank2.MemoryCount(), "canonical bank wins over a reappearing legacy file")
}

// The override flag round-trips through the registry, and the tri-state
// update contract holds: nil keeps the stored value, explicit false turns it
// off (a bare bool could not).
func TestOverrideSystemPromptRoundTrip(t *testing.T) {
	s := tempStore(t)
	p, err := s.CreateInput(Input{Name: "Moriarty", Prompt: "You are Moriarty."})
	require.NoError(t, err)
	assert.False(t, p.OverrideSystemPrompt)
	assert.False(t, p.ReplacesSystemPrompt(), "override without a prompt must not replace anything")

	on := true
	p, err = s.UpdateProfile(p.ID, &Input{OverrideSystemPrompt: &on})
	require.NoError(t, err)
	assert.True(t, p.OverrideSystemPrompt)
	assert.True(t, p.ReplacesSystemPrompt(), "override with a prompt replaces the system prompt")

	// nil keeps the stored value.
	p, err = s.UpdateProfile(p.ID, &Input{Tagline: "nudge"})
	require.NoError(t, err)
	assert.True(t, p.OverrideSystemPrompt, "nil flag must keep the stored value")

	// explicit false turns it off.
	off := false
	p, err = s.UpdateProfile(p.ID, &Input{OverrideSystemPrompt: &off})
	require.NoError(t, err)
	assert.False(t, p.OverrideSystemPrompt)
	assert.False(t, p.ReplacesSystemPrompt())

	// Persisted across store reloads.
	s2 := NewStore(s.root)
	got, err := s2.Get(p.ID)
	require.NoError(t, err)
	assert.False(t, got.OverrideSystemPrompt)

	// Created with the flag set from the start.
	m, err := s.CreateInput(Input{Name: "Dominika", Prompt: "You are Dominika.", OverrideSystemPrompt: &on})
	require.NoError(t, err)
	assert.True(t, m.ReplacesSystemPrompt())
}
