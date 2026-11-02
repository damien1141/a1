package vault_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/memstore"
	"github.com/damien1141/a1/internal/vault"
)

// buildLegacyLayout writes a miniature pre-vault ~/.a1 tree.
func buildLegacyLayout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// personas.json registry.
	personas := `{
  "active": "sherlock",
  "personas": [
    {"id": "default", "name": "a1", "created": "2026-10-01T00:00:00Z", "updated": "2026-10-01T00:00:00Z"},
    {"id": "sherlock", "name": "Sherlock", "tagline": "detective", "hue": 210, "model": "qwen3",
     "created": "2026-10-02T00:00:00Z", "updated": "2026-10-02T00:00:00Z"}
  ]
}`
	require.NoError(t, os.MkdirAll(filepath.Join(root, "personas"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "personas", "personas.json"), []byte(personas), 0o644))

	// Persona memory bank (canonical location).
	require.NoError(t, os.MkdirAll(filepath.Join(root, "personas", "memory"), 0o755))
	bank := `{"session_id":"persona:sherlock","type":"memory","content":"likes violins"}
{"session_id":"persona:sherlock","type":"decision","content":"always deduce"}
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "personas", "memory", "persona:sherlock.jsonl"), []byte(bank), 0o644))

	// Per-persona semantic store (one SQLite file).
	semDir := filepath.Join(root, "personas", "memory", "semantic")
	require.NoError(t, os.MkdirAll(semDir, 0o755))
	sem, err := memstore.Open(filepath.Join(semDir, "sherlock.sqlite"), memstore.FakeEmbedder{DimCount: 8}, memstore.DefaultParams())
	require.NoError(t, err)
	_, _, err = sem.Insert(t.Context(), memstore.NewTrace{
		Content: "plays violin at night", Kind: "fact", Scope: "user",
		Salience: 0.4,
	})
	require.NoError(t, err)
	require.NoError(t, sem.Close())

	// Shared engine store.
	agentDir := filepath.Join(root, "agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	shared, err := memstore.Open(filepath.Join(agentDir, "memories.db"), memstore.FakeEmbedder{DimCount: 8}, memstore.DefaultParams())
	require.NoError(t, err)
	_, _, err = shared.Insert(t.Context(), memstore.NewTrace{
		Content: "user prefers dark mode", Kind: "fact", Scope: "user",
		Salience: 0.3,
	})
	require.NoError(t, err)
	require.NoError(t, shared.Close())

	// Project session dir with a session file + evidence receipts.
	projDir := filepath.Join(root, "session", "--tmp-proj--")
	require.NoError(t, os.MkdirAll(projDir, 0o755))
	sessionJSONL := `{"type":"EntrySession","version":1,"id":"sess001","timestamp":"2026-10-03T10:00:00","cwd":"/tmp/proj"}
{"type":"EntryMessage","id":"aaaa","parentID":null,"timestamp":"2026-10-03T10:00:01","message":{"role":"user","content":"hi"}}
{"type":"EntryMessage","id":"bbbb","parentID":"aaaa","timestamp":"2026-10-03T10:00:02","message":{"role":"assistant","content":"hello"}}
`
	require.NoError(t, os.WriteFile(filepath.Join(projDir, "2026-10-03T10-00-00_sess001.jsonl"), []byte(sessionJSONL), 0o644))
	evidenceJSONL := `{"check":"go-build","status":"pass","at":"2026-10-03T10:01:00","detail":"","command":"go build ./..."}
`
	require.NoError(t, os.WriteFile(filepath.Join(projDir, "evidence.jsonl"), []byte(evidenceJSONL), 0o644))

	// Session memory bank.
	sessMemDir := filepath.Join(root, "sessions", "memory")
	require.NoError(t, os.MkdirAll(sessMemDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sessMemDir, "sess001.jsonl"),
		[]byte(`{"session_id":"sess001","type":"memory","content":"session note"}`), 0o644))

	return root
}

func TestMigrateImportsLegacyLayout(t *testing.T) {
	root := buildLegacyLayout(t)

	v, err := vault.Open(root)
	require.NoError(t, err)
	defer v.Close()
	require.NoError(t, vault.Migrate(v, root))

	// Personas.
	rows, err := v.ListPersonas()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	var sherlock *vault.PersonaRow
	for i, r := range rows {
		if r.ID == "sherlock" {
			sherlock = &rows[i]
		}
	}
	require.NotNil(t, sherlock)
	assert.Equal(t, "Sherlock", sherlock.Name)
	assert.Equal(t, "qwen3", sherlock.Model)
	active, err := v.ActivePersona()
	require.NoError(t, err)
	assert.Equal(t, "sherlock", active)

	// Persona bank rows.
	entries, err := v.LoadBank(vault.PersonaBank("sherlock"))
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "likes violins", entries[0].Content)

	// Session bank rows.
	entries, err = v.LoadBank("sess001")
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// Session tree rows.
	has, err := v.HasSession("sess001")
	require.NoError(t, err)
	assert.True(t, has)
	lines, err := v.LoadSessionEntries("sess001")
	require.NoError(t, err)
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "EntrySession")

	// Evidence receipts keyed by the project dir scope.
	receipts, err := v.LoadEvidenceReceipts(filepath.Join(root, "session", "--tmp-proj--"))
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	assert.Contains(t, receipts[0], "go-build")

	// Semantic stores copied into namespaced families: the persona ns and
	// the shared ns both hold their memory.
	pStore, err := memstore.OpenIn(v.DB(), vault.PersonaNS("sherlock"), memstore.FakeEmbedder{DimCount: 8}, memstore.DefaultParams())
	require.NoError(t, err)
	defer pStore.Close()
	traces, err := pStore.Retrieve(t.Context(), "violin", memstore.ModeCompanion, memstore.Cues{}, time.Now())
	require.NoError(t, err)
	require.Len(t, traces, 1)

	shared, err := memstore.OpenIn(v.DB(), vault.SharedNS(), memstore.FakeEmbedder{DimCount: 8}, memstore.DefaultParams())
	require.NoError(t, err)
	defer shared.Close()
	traces, err = shared.Retrieve(t.Context(), "dark mode", memstore.ModeCompanion, memstore.Cues{}, time.Now())
	require.NoError(t, err)
	require.Len(t, traces, 1)

	// The stamp is set and re-migration is a no-op.
	stamp, err := v.MetaGet(vault.MetaKeyMigrated)
	require.NoError(t, err)
	assert.NotEmpty(t, stamp)
	require.NoError(t, vault.Migrate(v, root))
}

func TestMigrateFreshRootIsStampOnly(t *testing.T) {
	root := t.TempDir()
	v, err := vault.Open(root)
	require.NoError(t, err)
	defer v.Close()

	require.NoError(t, vault.Migrate(v, root))
	stamp, err := v.MetaGet(vault.MetaKeyMigrated)
	require.NoError(t, err)
	assert.NotEmpty(t, stamp)
	rows, err := v.ListPersonas()
	require.NoError(t, err)
	assert.Empty(t, rows, "no personas.json to import → no rows")
}

func TestMemstoreOpenInNamespaceIsolation(t *testing.T) {
	v, err := vault.Open(t.TempDir())
	require.NoError(t, err)
	defer v.Close()

	a, err := memstore.OpenIn(v.DB(), vault.PersonaNS("alpha"), memstore.FakeEmbedder{DimCount: 8}, memstore.DefaultParams())
	require.NoError(t, err)
	defer a.Close()
	b, err := memstore.OpenIn(v.DB(), vault.PersonaNS("beta"), memstore.FakeEmbedder{DimCount: 8}, memstore.DefaultParams())
	require.NoError(t, err)
	defer b.Close()

	_, _, err = a.Insert(t.Context(), memstore.NewTrace{
		Content: "alpha only", Kind: "fact", Scope: "user", Salience: 0.3,
	})
	require.NoError(t, err)

	traces, err := b.Retrieve(t.Context(), "alpha", memstore.ModeCompanion, memstore.Cues{}, time.Now())
	require.NoError(t, err)
	assert.Empty(t, traces, "namespaces must not leak")

	traces, err = a.Retrieve(t.Context(), "alpha", memstore.ModeCompanion, memstore.Cues{}, time.Now())
	require.NoError(t, err)
	require.Len(t, traces, 1)

	// Close does not close the shared vault handle.
	a.Close()
	b.Close()
	_, err = v.DB().Exec(`SELECT 1`)
	require.NoError(t, err)
}
