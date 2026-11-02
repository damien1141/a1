package vault_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/vault"
)

func newVault(t *testing.T) *vault.Vault {
	t.Helper()
	v, err := vault.Open(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close() })
	return v
}

func TestVaultOpenAndMeta(t *testing.T) {
	root := t.TempDir()
	v, err := vault.Open(root)
	require.NoError(t, err)
	defer v.Close()

	assert.Equal(t, filepath.Join(root, "a1.db"), v.Path())
	assert.Equal(t, root, v.Root())

	val, err := v.MetaGet(vault.MetaKeyMigrated)
	require.NoError(t, err)
	assert.Empty(t, val, "fresh vault is unmigrated")

	require.NoError(t, v.MetaSet("k", "v1"))
	require.NoError(t, v.MetaSet("k", "v2"))
	val, err = v.MetaGet("k")
	require.NoError(t, err)
	assert.Equal(t, "v2", val)
}

func TestPersonaRowsAndDeleteCascade(t *testing.T) {
	v := newVault(t)

	memOff := false
	p := vault.PersonaRow{
		ID: "sherlock", Name: "Sherlock", Tagline: "detective", Hue: 210,
		Model: "qwen3", MemoryModel: "qwen3:8b", MemoryEnabled: &memOff,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	require.NoError(t, v.PutPersona(p))

	got, ok, err := v.GetPersona("sherlock")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "Sherlock", got.Name)
	assert.Equal(t, "qwen3:8b", got.MemoryModel)
	require.NotNil(t, got.MemoryEnabled)
	assert.False(t, *got.MemoryEnabled)

	_, ok, err = v.GetPersona("missing")
	require.NoError(t, err)
	assert.False(t, ok)

	// Bank rows + a semantic family for the cascade check.
	require.NoError(t, v.AppendBankEntry(vault.PersonaBank("sherlock"), vault.BankEntry{
		Type: "memory", Content: "likes violins", Timestamp: time.Now().UTC(),
	}))
	// The semantic family is created by memstore.OpenIn; exercise the drop
	// path through DeletePersona regardless of whether rows exist.

	require.NoError(t, v.SetActivePersona("sherlock"))
	active, err := v.ActivePersona()
	require.NoError(t, err)
	assert.Equal(t, "sherlock", active)

	// Delete end-to-end: registry row, bank rows, semantic family.
	require.NoError(t, v.DeletePersona("sherlock"))
	_, ok, err = v.GetPersona("sherlock")
	require.NoError(t, err)
	assert.False(t, ok)
	entries, err := v.LoadBank(vault.PersonaBank("sherlock"))
	require.NoError(t, err)
	assert.Empty(t, entries, "persona delete must cascade the memory bank")

	// Idempotent delete.
	require.NoError(t, v.DeletePersona("sherlock"))
}

func TestBankRowsRoundTrip(t *testing.T) {
	v := newVault(t)
	bank := "persona:test"

	for _, c := range []string{"one", "two", "three"} {
		require.NoError(t, v.AppendBankEntry(bank, vault.BankEntry{
			Type: "memory", Content: c, Timestamp: time.Now().UTC(), SessionID: "s1",
		}))
	}
	entries, err := v.LoadBank(bank)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	assert.Equal(t, "one", entries[0].Content)
	assert.Equal(t, "three", entries[2].Content, "seq order is append order")

	// Replace rewrites in order.
	require.NoError(t, v.ReplaceBank(bank, []vault.BankEntry{{
		Type: "memory", Content: "only", Timestamp: time.Now().UTC(),
	}}))
	entries, err = v.LoadBank(bank)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "only", entries[0].Content)

	// Banks are isolated.
	require.NoError(t, v.AppendBankEntry("persona:other", vault.BankEntry{
		Type: "memory", Content: "x", Timestamp: time.Now().UTC(),
	}))
	other, err := v.LoadBank("persona:other")
	require.NoError(t, err)
	require.Len(t, other, 1)
}

func TestEvidenceReceiptRows(t *testing.T) {
	v := newVault(t)
	scope := "/tmp/project-sessions"

	for i := 0; i < 3; i++ {
		require.NoError(t, v.AppendEvidenceReceipt(scope, `{"check":"go-build"}`))
	}
	lines, err := v.LoadEvidenceReceipts(scope)
	require.NoError(t, err)
	require.Len(t, lines, 3)

	require.NoError(t, v.ReplaceEvidenceReceipts(scope, []string{`{"check":"go-build"}`, `{"check":"go-vet"}`}))
	lines, err = v.LoadEvidenceReceipts(scope)
	require.NoError(t, err)
	require.Len(t, lines, 2)

	require.NoError(t, v.DeleteEvidence(scope))
	lines, err = v.LoadEvidenceReceipts(scope)
	require.NoError(t, err)
	assert.Empty(t, lines)
}

func TestSessionRowsAndCascade(t *testing.T) {
	v := newVault(t)

	require.NoError(t, v.PutSession(vault.SessionRow{
		ID: "abc123", Project: "/tmp/p", Ts: "2026-11-01T10-00-00",
		Cwd: "/tmp/p", CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, v.AppendSessionEntries([]vault.SessionEntryRow{
		{SessionID: "abc123", Seq: 0, Kind: "EntrySession", Payload: `{"type":"EntrySession"}`},
		{SessionID: "abc123", Seq: 1, Kind: "EntryMessage", ParentSeq: ptr(int64(0)), Payload: `{"type":"EntryMessage"}`},
	}))

	has, err := v.HasSession("abc123")
	require.NoError(t, err)
	assert.True(t, has)

	lines, err := v.LoadSessionEntries("abc123")
	require.NoError(t, err)
	require.Len(t, lines, 2)

	rows, err := v.ListSessions("/tmp/p")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "abc123", rows[0].ID)

	// FK cascade: deleting the session removes its entries.
	require.NoError(t, v.DeleteSession("abc123"))
	has, err = v.HasSession("abc123")
	require.NoError(t, err)
	assert.False(t, has)
	lines, err = v.LoadSessionEntries("abc123")
	require.NoError(t, err)
	assert.Empty(t, lines)
}

func TestSessionPathSynthetic(t *testing.T) {
	p := vault.SessionPath("/tmp/p", "2026-11-01T10-00-00", "abc")
	assert.Equal(t, filepath.Join("/tmp/p", "2026-11-01T10-00-00_abc.jsonl"), p)
}

func ptr[T any](v T) *T { return &v }
