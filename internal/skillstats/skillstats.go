// Package skillstats is the bandit ledger behind skill routing (Bet 07).
//
// Skills used to be static files: same catalog every turn, no memory of
// whether a trigger keyword ever led anywhere. This package turns routing
// into a learnable problem with the smallest honest loop available:
//
//   - PICKS live in the agent executor: a successful `read` of a skill's
//     SKILL.md records a pick for that skill's directory name. The executor
//     is the only place that sees the model actually consulting a skill.
//   - OUTCOMES land here from `a1 eval`'s scorer: when it materializes a
//     session's turns, a green session counts as a win for every skill
//     picked in it (a coarse, session-granular credit assignment —
//     deliberate v1 honesty: finer attribution would need the engine's turn
//     bookkeeping, which stays untouched).
//   - DEMOTION happens in the skilltool: list_skills sinks skills whose
//     Laplace-smoothed win rate collapsed (Score < 0.35 with picks >= 5) to
//     the bottom of the catalog with a "(recently unhelpful)" note, so the
//     model stops reaching for triggers that correlate with failed turns.
//
// Storage is the vault's SQLite handle with lazy DDL, so the feature costs
// nothing until someone reads or writes a row.
package skillstats

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/llm/skills"
)

const schema = `
CREATE TABLE IF NOT EXISTS skill_stats (
  skill      TEXT PRIMARY KEY,
  picks      INT  NOT NULL DEFAULT 0,
  wins       INT  NOT NULL DEFAULT 0,
  losses     INT  NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);
`

// PickFromReadPath extracts the skill a `read` call landed on. A skill file
// is <skill root>/<skill name>/SKILL.md, so the parent directory names the
// skill without needing the (config-owned, executor-invisible) root. The
// empty string / false return means "not a skill read" and records nothing.
func PickFromReadPath(path string) (string, bool) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", false
	}
	if filepath.Base(filepath.Clean(trimmed)) != skills.SkillFileName {
		return "", false
	}
	dir := filepath.Base(filepath.Dir(filepath.Clean(trimmed)))
	if dir == "" || dir == "." || dir == string(filepath.Separator) {
		return "", false
	}
	return dir, true
}

// EnsureSchema applies the skill-stats DDL to db (idempotent).
func EnsureSchema(db *sql.DB) error {
	if db == nil {
		return errors.New("skillstats: db is required")
	}
	_, err := db.Exec(schema)
	return err
}

// Score is the Laplace-smoothed win rate: (wins+1)/(picks+2). The +1/+2
// priors keep a skill with no history at 0.5 (neutral) instead of 0 (dead),
// and require several losses before demotion — one bad session must not
// bury a skill nobody has picked often.
func Score(db *sql.DB, skill string) float64 {
	if db == nil {
		return 0.5
	}
	if err := EnsureSchema(db); err != nil {
		return 0.5 // unreadable store: neutral, never punitive
	}
	var picks, wins int
	err := db.QueryRow(`SELECT picks, wins FROM skill_stats WHERE skill = ?`, skill).Scan(&picks, &wins)
	if err != nil {
		return 0.5 // no row: neutral
	}
	return float64(wins+1) / float64(picks+2)
}

// RecordPick increments the pick counter for skill.
func RecordPick(db *sql.DB, skill string) error {
	if db == nil {
		return errors.New("skillstats: db is required")
	}
	if err := EnsureSchema(db); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO skill_stats (skill, picks, wins, losses, updated_at)
                VALUES (?, 1, 0, 0, ?)
                ON CONFLICT(skill) DO UPDATE SET picks = picks + 1, updated_at = excluded.updated_at`,
		skill, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return nil
}

// RecordOutcome records a win or loss for skill. Picks are bumped to keep
// picks >= wins+losses even when an outcome arrives without a recorded pick
// (e.g. the eval scorer attributes a skill whose picks predate the vault);
// the Laplace prior stays well-formed either way.
func RecordOutcome(db *sql.DB, skill string, won bool) error {
	if db == nil {
		return errors.New("skillstats: db is required")
	}
	if err := EnsureSchema(db); err != nil {
		return err
	}
	column := "losses"
	if won {
		column = "wins"
	}
	// picks = MAX(picks, wins + losses) after the increment, computed in SQL
	// so the invariant holds without a read-modify-write race.
	if _, err := db.Exec(`INSERT INTO skill_stats (skill, picks, wins, losses, updated_at)
                VALUES (?, 1, ?, ?, ?)
                ON CONFLICT(skill) DO UPDATE SET
                  `+column+` = `+column+` + 1,
                  picks = MAX(picks, wins + losses),
                  updated_at = excluded.updated_at`,
		skill, boolToInt(won), boolToInt(!won), time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return nil
}

// Row is one skill's ledger.
type Row struct {
	Skill     string
	Picks     int
	Wins      int
	Losses    int
	UpdatedAt string
}

// Stats returns every tracked skill, highest score first.
func Stats(db *sql.DB) ([]Row, error) {
	if db == nil {
		return nil, errors.New("skillstats: db is required")
	}
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT skill, picks, wins, losses, updated_at FROM skill_stats`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.Skill, &r.Picks, &r.Wins, &r.Losses, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Sort by score in Go: the Laplace formula is trivial and keeping it in
	// one place avoids SQL/Go drift.
	sortByScore(out)
	return out, nil
}

func sortByScore(rows []Row) {
	// Insertion sort — the table is small (dozens of skills).
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0; j-- {
			if laplace(rows[j]) > laplace(rows[j-1]) {
				rows[j], rows[j-1] = rows[j-1], rows[j]
				continue
			}
			break
		}
	}
}

func laplace(r Row) float64 {
	return float64(r.Wins+1) / float64(r.Picks+2)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
