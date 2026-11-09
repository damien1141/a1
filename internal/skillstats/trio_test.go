package skillstats

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// ---------- negative ----------

// corruptDB opens a database file that is not a SQLite database at all —
// every schema operation on it must fail with the driver's error.
func corruptDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "corrupt.db")
	require.NoError(t, os.WriteFile(path, []byte("this is definitely not a sqlite database"), 0o644))
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err, "open is lazy; the corruption surfaces on first use")
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestNilDB_Negative(t *testing.T) {
	for name, fn := range map[string]func() error{
		"EnsureSchema":  func() error { return EnsureSchema(nil) },
		"RecordPick":    func() error { return RecordPick(nil, "go-test") },
		"RecordOutcome": func() error { return RecordOutcome(nil, "go-test", true) },
		"Stats":         func() error { _, err := Stats(nil); return err },
	} {
		err := fn()
		require.Error(t, err, "%s(nil)", name)
		assert.EqualError(t, err, "skillstats: db is required", "%s(nil)", name)
	}
}

func TestCorruptStatsFile_Negative(t *testing.T) {
	db := corruptDB(t)

	err := RecordPick(db, "go-test")
	require.Error(t, err, "a corrupt store must fail the write, not panic")
	assert.Contains(t, err.Error(), "file is not a database")

	err = RecordOutcome(db, "go-test", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file is not a database")

	_, err = Stats(db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file is not a database")

	// Score never punishes a broken store: neutral prior, no error path.
	assert.InDelta(t, 0.5, Score(db, "go-test"), 1e-9, "unreadable store stays neutral")
}

func TestPickFromReadPathBoundary_Negative(t *testing.T) {
	cases := []string{
		"SKILL.md",   // no parent directory to name a skill
		"/SKILL.md",  // parent is the root — not a skill name
		"./SKILL.md", // parent cleans to "." — not a skill name
		"   ",        // whitespace-only trims to empty
		"dir/READ ME.md",
	}
	for _, path := range cases {
		_, ok := PickFromReadPath(path)
		assert.False(t, ok, "PickFromReadPath(%q)", path)
	}
}

// TestRecordPickHostileSkillNames_Negative pins the parameterized writes:
// quote-laden and empty skill names round-trip as plain data, never as SQL.
func TestRecordPickHostileSkillNames_Negative(t *testing.T) {
	db := testDB(t)
	hostile := []string{
		"",
		"skill'; DROP TABLE skill_stats;--",
		`quote"name`,
		"unicode-技能",
	}
	for _, skill := range hostile {
		require.NoError(t, RecordPick(db, skill), "skill %q", skill)
	}

	rows, err := Stats(db)
	require.NoError(t, err)
	require.Len(t, rows, len(hostile), "every hostile name became exactly one row")

	got := make(map[string]int, len(rows))
	for _, r := range rows {
		got[r.Skill] = r.Picks
	}
	for _, skill := range hostile {
		assert.Equal(t, 1, got[skill], "skill %q recorded once as data", skill)
	}

	// The table survived the injection attempt and stays queryable.
	require.NoError(t, RecordPick(db, "after"))
	assert.InDelta(t, 1.0/3.0, Score(db, "after"), 1e-9, "one pick, no wins: (0+1)/(1+2)")
}
