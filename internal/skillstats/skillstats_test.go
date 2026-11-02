package skillstats

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "stats.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRoundTrip(t *testing.T) {
	db := testDB(t)
	for range 3 {
		require.NoError(t, RecordPick(db, "go-test"))
	}
	require.NoError(t, RecordOutcome(db, "go-test", true))
	require.NoError(t, RecordOutcome(db, "go-test", true))

	rows, err := Stats(db)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	r := rows[0]
	assert.Equal(t, "go-test", r.Skill)
	assert.Equal(t, 3, r.Picks)
	assert.Equal(t, 2, r.Wins)
	assert.Equal(t, 0, r.Losses)

	// Laplace: (2+1)/(3+2) = 0.6.
	assert.InDelta(t, 0.6, Score(db, "go-test"), 1e-9)
}

func TestScoreNeutralWithoutHistory(t *testing.T) {
	db := testDB(t)
	assert.InDelta(t, 0.5, Score(db, "never-picked"), 1e-9, "no row: neutral prior")
	assert.InDelta(t, 0.5, Score(nil, "anything"), 1e-9, "no store: neutral prior")
}

func TestOutcomeWithoutPickKeepsInvariant(t *testing.T) {
	db := testDB(t)
	require.NoError(t, RecordOutcome(db, "legacy", true))
	rows, err := Stats(db)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, r0(rows).Picks, "picks >= wins+losses even without a live pick")
	assert.Equal(t, 1, r0(rows).Wins)
	assert.InDelta(t, 2.0/3.0, Score(db, "legacy"), 1e-9)
}

func TestStatsSortedByScore(t *testing.T) {
	db := testDB(t)
	// good: 4 wins of 5 picks → (4+1)/(5+2) ≈ 0.71.
	for range 5 {
		require.NoError(t, RecordPick(db, "good"))
	}
	for i := 0; i < 4; i++ {
		require.NoError(t, RecordOutcome(db, "good", true))
	}
	require.NoError(t, RecordOutcome(db, "good", false))
	// bad: 6 picks, all losses → (0+1)/(6+2) = 0.125.
	for range 6 {
		require.NoError(t, RecordPick(db, "bad"))
	}
	for i := 0; i < 6; i++ {
		require.NoError(t, RecordOutcome(db, "bad", false))
	}

	rows, err := Stats(db)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "good", rows[0].Skill, "highest score first")
	assert.Equal(t, "bad", rows[1].Skill)
}

func TestPickFromReadPath(t *testing.T) {
	skill, ok := PickFromReadPath("/home/u/.a1/skills/go-test/SKILL.md")
	require.True(t, ok)
	assert.Equal(t, "go-test", skill)

	skill, ok = PickFromReadPath("skills/rust-debug/SKILL.md")
	require.True(t, ok)
	assert.Equal(t, "rust-debug", skill)

	_, ok = PickFromReadPath("/home/u/project/main.go")
	assert.False(t, ok, "an ordinary file is not a skill pick")

	_, ok = PickFromReadPath("/home/u/.a1/skills/go-test/README.md")
	assert.False(t, ok, "only SKILL.md identifies the skill")

	_, ok = PickFromReadPath("")
	assert.False(t, ok)
}

func r0(rows []Row) Row {
	if len(rows) == 0 {
		return Row{}
	}
	return rows[0]
}
