package vault_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/vault"
)

// ---------- negative ----------

func TestOpenGuards_Negative(t *testing.T) {
	_, err := vault.Open("")
	require.Error(t, err)
	assert.EqualError(t, err, "vault: root is required")

	_, err = vault.OpenDB(nil, "/tmp/root")
	require.Error(t, err)
	assert.EqualError(t, err, "vault: db is required")

	err = vault.Migrate(nil, "/tmp/root")
	require.Error(t, err)
	assert.EqualError(t, err, "vault: nil handle")
}

// TestOpenCorruptDB_Negative pins what happens when a1.db exists but is not
// a database: the schema exec fails with a wrapped error, never a panic.
func TestOpenCorruptDB_Negative(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a1.db"),
		[]byte("definitely not a sqlite file"), 0o644))

	_, err := vault.Open(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault: schema:")
	assert.Contains(t, err.Error(), "file is not a database")

	_, err = vault.Boot(root)
	require.Error(t, err, "Boot must fail the same way Open does")
	assert.Contains(t, err.Error(), "vault: schema:")
}

// TestInvalidPersonaRows_Negative writes rows that violate the Go-side row
// shape directly via SQL and pins the scan failures they produce.
func TestInvalidPersonaRows_Negative(t *testing.T) {
	v := newVault(t)

	// hue is TEXT in the DDL but scanned into an int: a non-numeric value
	// makes every read of the row fail with a scan error.
	_, err := v.DB().Exec(`INSERT INTO personas (
                id, name, description, tagline, emoji, hue, system_prompt,
                override_system_prompt, avatar_ext, builtin, model, memory_model,
                memory_enabled, permission_mode, created_at, updated_at)
                VALUES ('badhue', 'n', '', '', '', 'not-a-number', '', 0, '', 0, '', '', NULL, '', '', '')`)
	require.NoError(t, err)

	_, _, err = v.GetPersona("badhue")
	require.Error(t, err, "a persona row with a non-numeric hue must fail the scan")
	assert.Contains(t, err.Error(), "vault: scan persona")

	_, err = v.ListPersonas()
	require.Error(t, err, "one poisoned row poisons the listing scan")
	assert.Contains(t, err.Error(), "vault: scan persona")

	// Garbage timestamps, by contrast, degrade to the zero time — the row
	// still reads back without an error.
	_, err = v.DB().Exec(`INSERT INTO personas (
                id, name, description, tagline, emoji, hue, system_prompt,
                override_system_prompt, avatar_ext, builtin, model, memory_model,
                memory_enabled, permission_mode, created_at, updated_at)
                VALUES ('badtime', 'n', '', '', '', '210', '', 0, '', 0, '', '', NULL, '', 'garbage-time', 'garbage-time')`)
	require.NoError(t, err)
	got, ok, err := v.GetPersona("badtime")
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, got.CreatedAt.IsZero(), "unparseable timestamps degrade to the zero time")
	assert.True(t, got.UpdatedAt.IsZero())
}

func TestBankAndScopeGuards_Negative(t *testing.T) {
	v := newVault(t)

	err := v.AppendBankEntry("", vault.BankEntry{Type: "memory", Content: "x", Timestamp: time.Now().UTC()})
	require.Error(t, err)
	assert.EqualError(t, err, "vault: bank key is required")

	err = v.AppendEvidenceReceipt("", "{}")
	require.Error(t, err)
	assert.EqualError(t, err, "vault: evidence scope is required")
}

// TestSessionEntriesFK_Negative pins the tree invariant: entries for a
// session the vault does not hold are rejected by the foreign key, not
// silently orphaned.
func TestSessionEntriesFK_Negative(t *testing.T) {
	v := newVault(t)

	err := v.AppendSessionEntries([]vault.SessionEntryRow{
		{SessionID: "ghost", Seq: 0, Kind: "EntrySession", Payload: `{"type":"EntrySession"}`},
	})
	require.Error(t, err, "entries without their session row must be refused")
	assert.Contains(t, err.Error(), "FOREIGN KEY constraint")
}

// TestPutSessionImmutable_Negative pins INSERT OR IGNORE: a second put of
// the same id is a silent no-op, so the first write wins.
func TestPutSessionImmutable_Negative(t *testing.T) {
	v := newVault(t)
	require.NoError(t, v.PutSession(vault.SessionRow{
		ID: "dup", Project: "/first", Ts: "t", Cwd: "/first", CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, v.PutSession(vault.SessionRow{
		ID: "dup", Project: "/second", Ts: "t2", Cwd: "/second", CreatedAt: time.Now().UTC(),
	}), "the conflicting insert is ignored, not an error")

	got, ok, err := v.GetSession("dup")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "/first", got.Project, "sessions are immutable once created")
}

// TestMigrateCorruptLegacyFiles_Negative feeds the importer garbage in every
// legacy format: the migration must still succeed and stamp, importing only
// the records that parse.
func TestMigrateCorruptLegacyFiles_Negative(t *testing.T) {
	root := t.TempDir()

	// Corrupt personas.json registry.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "personas", "memory", "semantic"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "personas", "personas.json"),
		[]byte("}}this is not json{{"), 0o644))

	// A bank with one garbage line and one good one.
	require.NoError(t, os.WriteFile(filepath.Join(root, "personas", "memory", "persona:sherlock.jsonl"),
		[]byte("this line is garbage\n"+
			`{"type":"memory","content":"likes violins","timestamp":"2026-10-02T00:00:00Z"}`+"\n"), 0o644))

	// A semantic store that is not a SQLite file.
	require.NoError(t, os.WriteFile(filepath.Join(root, "personas", "memory", "semantic", "sherlock.sqlite"),
		[]byte("garbage bytes pretending to be sqlite"), 0o644))

	// A job dir with a corrupt meta.json.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "jobs", "j1"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "jobs", "j1", "meta.json"), []byte("{nope"), 0o644))

	v, err := vault.Boot(root)
	require.NoError(t, err, "garbage legacy files never fail the migration")
	t.Cleanup(func() { _ = v.Close() })

	stamp, err := v.MetaGet(vault.MetaKeyMigrated)
	require.NoError(t, err)
	assert.NotEmpty(t, stamp, "migration stamped despite the garbage")

	personas, err := v.ListPersonas()
	require.NoError(t, err)
	assert.Empty(t, personas, "the corrupt registry imported nothing")

	entries, err := v.LoadBank(vault.PersonaBank("sherlock"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the parseable bank line landed")
	assert.Equal(t, "likes violins", entries[0].Content)
}

// TestMigrateGarbageSessionTree_Negative pins how the session-dir importer
// treats hostile trees: evidence lines are byte-faithful (never JSON-checked),
// session JSONL lines that do not parse are dropped, and files without a
// usable header or filename shape are skipped entirely.
func TestMigrateGarbageSessionTree_Negative(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "session", "proj")
	require.NoError(t, os.MkdirAll(proj, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(proj, "evidence.jsonl"),
		[]byte("garbage-line-one\ngarbage-line-two\n"), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(proj, "20261103T00-00-00_abc.jsonl"),
		[]byte(`{"type":"EntrySession","id":"abc","cwd":"/w"}`+"\n"+
			"not json at all\n"+
			`{"type":"EntryMessage","id":"m1"}`+"\n"), 0o644))

	// No "_" in the filename: unusable identity, skipped even though valid.
	require.NoError(t, os.WriteFile(filepath.Join(proj, "badname.jsonl"),
		[]byte(`{"type":"EntrySession","id":"zzz"}`+"\n"), 0o644))

	// "_" present but no EntrySession header: no session row.
	require.NoError(t, os.WriteFile(filepath.Join(proj, "1_noheader.jsonl"),
		[]byte("garbage only\n"), 0o644))

	v, err := vault.Boot(root)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close() })

	// Evidence receipts keep garbage verbatim — the ratchet replays bytes.
	receipts, err := v.LoadEvidenceReceipts(proj)
	require.NoError(t, err)
	assert.Equal(t, []string{"garbage-line-one", "garbage-line-two"}, receipts)

	has, err := v.HasSession("abc")
	require.NoError(t, err)
	require.True(t, has)
	lines, err := v.LoadSessionEntries("abc")
	require.NoError(t, err)
	require.Len(t, lines, 2, "the unparseable line is dropped, the rest kept")
	assert.Contains(t, lines[0], "EntrySession")

	for _, id := range []string{"zzz", "noheader"} {
		has, err := v.HasSession(id)
		require.NoError(t, err)
		assert.False(t, has, "session %q must not be imported", id)
	}
}

// TestMigrateRegistryShapeEdge_Negative: a registry that parses but carries
// an empty-ID persona skips it, while the active pointer is still applied —
// even when it names a persona that was never imported.
func TestMigrateRegistryShapeEdge_Negative(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "personas"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "personas", "personas.json"),
		[]byte(`{"active":"ghost","personas":[{"id":"","name":"nameless"},{"id":"real","name":"Real"}]}`), 0o644))

	v, err := vault.Boot(root)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close() })

	personas, err := v.ListPersonas()
	require.NoError(t, err)
	require.Len(t, personas, 1)
	assert.Equal(t, "real", personas[0].ID, "the empty-ID persona is skipped")

	active, err := v.ActivePersona()
	require.NoError(t, err)
	assert.Equal(t, "ghost", active, "the active pointer is applied verbatim")

	_, ok, err := v.GetPersona("ghost")
	require.NoError(t, err)
	assert.False(t, ok, "active can dangle — the registry import does not validate it")
}
