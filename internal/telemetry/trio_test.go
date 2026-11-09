package telemetry

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- negative ----------

// openRaw opens a SQLite handle at an arbitrary path (testDB always makes a
// fresh file; the corrupt-db cases need to point at one we poisoned).
func openRaw(path string) (*sql.DB, error) {
	return sql.Open("sqlite", path)
}

// corruptDBFile writes a non-SQLite file and opens it — the driver accepts
// the open lazily, so the corruption surfaces on the first Exec/Query.
func corruptDBFile(t *testing.T) (path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "corrupt.db")
	require.NoError(t, os.WriteFile(path, []byte("garbage, not a database"), 0o644))
	return path
}

func TestNilDB_Negative(t *testing.T) {
	calls := map[string]func() error{
		"EnsureSchema": func() error { return EnsureSchema(nil) },
		"NewStore":     func() error { _, err := NewStore(nil); return err },
		"MaterializeTurns": func() error {
			return MaterializeTurns(nil, "s", []TurnRow{{Model: "m", TotalTokens: 1, At: "t"}})
		},
		"RecentToolRuns": func() error { _, err := RecentToolRuns(nil, 10); return err },
		"ToolAggregates": func() error { _, err := ToolAggregates(nil); return err },
		"RecentErrors":   func() error { _, err := RecentErrors(nil, 10); return err },
		"TurnSummary":    func() error { _, err := TurnSummary(nil); return err },
	}
	for name, fn := range calls {
		err := fn()
		require.Error(t, err, "%s(nil)", name)
		assert.EqualError(t, err, "telemetry: db is required", "%s(nil)", name)
	}
}

func TestCorruptDB_Negative(t *testing.T) {
	db, err := openRaw(corruptDBFile(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = NewStore(db)
	require.Error(t, err, "a corrupt database must fail store creation, not panic")
	assert.Contains(t, err.Error(), "file is not a database")

	for name, fn := range map[string]func() error{
		"ToolAggregates": func() error { _, err := ToolAggregates(db); return err },
		"RecentToolRuns": func() error { _, err := RecentToolRuns(db, 10); return err },
		"MaterializeTurns": func() error {
			return MaterializeTurns(db, "s", []TurnRow{{Model: "m", TotalTokens: 1, At: "t"}})
		},
	} {
		err := fn()
		require.Error(t, err, "%s on corrupt db", name)
		assert.Contains(t, err.Error(), "file is not a database", "%s", name)
	}
}

func TestClosedDB_Negative(t *testing.T) {
	db := testDB(t)
	st, err := NewStore(db)
	require.NoError(t, err)
	st.Close()
	require.NoError(t, db.Close())

	_, err = ToolAggregates(db)
	require.Error(t, err, "queries on a closed handle must error, not panic")
	assert.Contains(t, err.Error(), "database is closed")

	_, err = RecentToolRuns(db, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database is closed")
}

// TestUnknownEventKind_Negative pins how the recorder classifies payloads it
// does not know: unknown kinds are stored but counted nowhere, and the
// last-tool map still updates (it is keyed by tool, not kind).
func TestUnknownEventKind_Negative(t *testing.T) {
	r := NewRecorder()
	when := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	r.Record(Event{Kind: "bogus_kind", Tool: "bash", When: when})
	r.Record(Event{Kind: "", Tool: "read", When: when})

	assert.Equal(t, 0, r.ToolCalls(), "unknown kinds are not tool calls")
	assert.Equal(t, 0, r.ToolOK())
	assert.Equal(t, 0, r.ToolErrors())

	snap := r.Snapshot()
	assert.Equal(t, 0, snap.TotalCalls)
	assert.Equal(t, 0, snap.TotalOK)
	assert.Equal(t, 0, snap.TotalErrors)
	assert.Empty(t, snap.ByTool, "unknown kinds never reach the per-tool rollup")

	got, ok := r.LastToolAt("bash")
	require.True(t, ok, "LastToolAt is keyed by tool regardless of kind")
	assert.Equal(t, when, got)

	r.Reset()
	_, ok = r.LastToolAt("bash")
	assert.False(t, ok, "reset clears the last-tool map")
}

// TestAttachDBNil_Negative pins the degraded-vault contract: a nil db is a
// documented no-op, and a db that cannot hold the schema fails without ever
// installing a sink. This test must stay the only AttachDB consumer in the
// package: the first call (success or failure) burns the process-wide once.
func TestAttachDBNil_Negative(t *testing.T) {
	require.NoError(t, AttachDB(nil), "nil db: no-op, never an error")
	assert.Nil(t, GlobalSink(), "nil db attaches nothing")

	db, err := openRaw(corruptDBFile(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	err = AttachDB(db)
	require.Error(t, err, "corrupt db must surface its schema error")
	assert.Contains(t, err.Error(), "file is not a database")
	assert.Nil(t, GlobalSink(), "a failed attach installs no sink")
}
