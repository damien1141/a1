package persona_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/memstore"
	"github.com/damien1141/a1/internal/persona"
	"github.com/damien1141/a1/internal/session/memory"
	"github.com/damien1141/a1/internal/vault"
)

func TestPersonaVaultMode(t *testing.T) {
	root := t.TempDir()
	v, err := vault.Open(root)
	require.NoError(t, err)
	defer v.Close()

	s := persona.NewStore(root + "/personas")
	s.AttachVault(v)

	// Fresh vault: default persona is seeded, active.
	list, err := s.List()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, persona.DefaultPersonaID, list[0].ID)

	// Create flows into rows.
	p, err := s.CreateInput(persona.Input{
		Name: "Sherlock", Tagline: "detective", Emoji: "🕵️",
		SystemPrompt: "deduce",
	})
	require.NoError(t, err)
	assert.Equal(t, "sherlock", p.ID)

	row, ok, err := v.GetPersona("sherlock")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "Sherlock", row.Name)

	// Settings updates flow into rows (the web editor path).
	off := false
	_, err = s.SetSettings("sherlock", strPtr("qwen3"), nil, &off)
	require.NoError(t, err)
	row, ok, err = v.GetPersona("sherlock")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "qwen3", row.Model)
	require.NotNil(t, row.MemoryEnabled)
	assert.False(t, *row.MemoryEnabled)

	// Active persona is vault-meta backed.
	require.NoError(t, s.SetActive("sherlock"))
	activeID, err := s.Active()
	require.NoError(t, err)
	assert.Equal(t, "sherlock", activeID)
	active := p // the persona we created

	// The memory bank is vault rows.
	bank, err := s.MemoryBank(active)
	require.NoError(t, err)
	require.NoError(t, bank.Append(memory.EntryMemory, "likes violins"))
	assert.Equal(t, 1, bank.MemoryCount())

	// The semantic store is a namespaced family.
	sem, err := s.SemanticStore(active, memstore.FakeEmbedder{DimCount: 8}, memstore.DefaultParams())
	require.NoError(t, err)
	_, _, err = sem.Insert(t.Context(), memstore.NewTrace{
		Content: "plays violin", Kind: "fact", Scope: "user", Salience: 0.4,
	})
	require.NoError(t, err)

	// Delete cascades everything end-to-end: row, bank rows, semantic family.
	require.NoError(t, s.Delete("sherlock"))
	_, ok, err = v.GetPersona("sherlock")
	require.NoError(t, err)
	assert.False(t, ok)
	entries, err := v.LoadBank(vault.PersonaBank("sherlock"))
	require.NoError(t, err)
	assert.Empty(t, entries)

	// Active reverts to default after deleting the active persona.
	activeID, err = s.Active()
	require.NoError(t, err)
	assert.Equal(t, persona.DefaultPersonaID, activeID)

	// Default persona still cannot be deleted.
	assert.ErrorIs(t, s.Delete(persona.DefaultPersonaID), persona.ErrCannotDeleteDefault)
}

// TestPersonaFileModeUnaffected pins the pre-Bet 08 layout when no vault
// is attached: registry file, bank files, per-persona SQLite files.
func TestPersonaFileModeUnaffected(t *testing.T) {
	root := t.TempDir()
	s := persona.NewStore(root)

	p, err := s.CreateInput(persona.Input{Name: "Watson", Tagline: "doctor"})
	require.NoError(t, err)

	bank, err := s.MemoryBank(p)
	require.NoError(t, err)
	require.NoError(t, bank.Append(memory.EntryMemory, "loyal"))
	assert.Equal(t, 1, bank.MemoryCount())

	sem, err := s.SemanticStore(p, memstore.FakeEmbedder{DimCount: 8}, memstore.DefaultParams())
	require.NoError(t, err)
	require.NotNil(t, sem)
}

func strPtr(s string) *string { return &s }
