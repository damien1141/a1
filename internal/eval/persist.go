package eval

import (
	"database/sql"
	"fmt"

	"github.com/damien1141/a1/internal/skillstats"
	"github.com/damien1141/a1/internal/telemetry"
)

// Persist is the eval scorer's write step: it materializes the scored
// session's turns into the telemetry store (the durable source of truth for
// per-turn token spend — the engine's hot path never writes these rows) and
// feeds the skill-routing bandit's outcome side. Picks were recorded live by
// the executor; outcomes land here, when the scorer knows how the session
// resolved: green session = win for every skill it picked, a governed
// session that never went green = loss. Sessions that picked no skill or
// declared no contract credit nothing.
//
// Idempotency: turn rows are keyed (session, at) and replaced, and a
// session whose turns are already materialized does not re-credit skill
// outcomes — nightly `a1 eval` runs over the same store must not
// double-count. A session that reported no usage cannot be keyed this way
// and credits on every scoring run (golden runs always report usage, so
// this only affects degenerate stored sessions).
func Persist(result EvalResult, db *sql.DB) error {
	if db == nil {
		return nil
	}
	already, err := sessionMaterialized(db, result.SessionID, len(result.TurnRows()))
	if err != nil {
		return fmt.Errorf("eval: materialize check: %w", err)
	}
	if err := telemetry.MaterializeTurns(db, result.SessionID, result.TurnRows()); err != nil {
		return fmt.Errorf("eval: materialize turns: %w", err)
	}
	if already || len(result.SkillsPicked) == 0 {
		return nil
	}
	var won bool
	switch {
	case result.Green:
		won = true
	case len(result.Contract) > 0:
		// Governed attempt that never satisfied its contract.
		won = false
	default:
		return nil // no finish line: nothing to credit or blame
	}
	for _, skill := range result.SkillsPicked {
		if err := skillstats.RecordOutcome(db, skill, won); err != nil {
			return fmt.Errorf("eval: record skill outcome %s: %w", skill, err)
		}
	}
	return nil
}

// sessionMaterialized reports whether the session's turn rows are already
// in the store. want <= 0 means the session reported no usage and cannot be
// keyed (caller credits unconditionally).
func sessionMaterialized(db *sql.DB, sessionID string, want int) (bool, error) {
	if want <= 0 {
		return false, nil
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM turns WHERE session_id = ?`, sessionID).Scan(&n); err != nil {
		return false, err
	}
	return n >= want, nil
}
