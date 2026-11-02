package schedule

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Schedule is one durable wake-up. The zero Schedule is not usable; build
// via ParseTrigger + Scheduler.Upsert (which assigns IDs and fire times).
type Schedule struct {
	ID          string
	Prompt      string
	Description string
	Trigger     Trigger
	// WorkDir defaults to the spawning session's cwd at create time.
	WorkDir string
	// Role is the engine role the wake-up runs as: explore | review |
	// worker. Empty = worker, the unattended default — worker children run
	// headless-strict (Ask folds to Deny), which is what an overnight
	// wake-up wants unless configured otherwise.
	Role string
	// Contract seeds the wake-up's task contract (Bet 04 finish line) so
	// scheduled work is held to machine-checked done-criteria.
	Contract []string
	// Timeout bounds each fired run (0 = no timeout).
	Timeout time.Duration
	Enabled bool
	// Source is "config" (declared in config.yaml) or "tool" (created via
	// the schedule tool). Config schedules win id conflicts on Sync.
	Source string

	CreatedAt time.Time
	UpdatedAt time.Time
	LastRunAt time.Time
	// NextRunAt is the scheduler's computed next fire (informational for
	// listings; due-ness is recomputed from the trigger each pass).
	NextRunAt time.Time
}

// schedulesDDL creates the schedules table lazily on the caller's handle —
// the vault pattern for subsystem-owned rows (see memstore.OpenIn). Keeping
// the DDL here (not in the vault core schema) means the job package owns
// its migration story end to end.
const schedulesDDL = `
CREATE TABLE IF NOT EXISTS schedules (
  id          TEXT PRIMARY KEY,
  prompt      TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  kind        TEXT NOT NULL,
  cron        TEXT NOT NULL DEFAULT '',
  fixed_rate  INTEGER NOT NULL DEFAULT 0,
  one_time    TEXT NOT NULL DEFAULT '',
  workdir     TEXT NOT NULL DEFAULT '',
  role        TEXT NOT NULL DEFAULT '',
  contract    TEXT NOT NULL DEFAULT '[]',
  timeout_sec INTEGER NOT NULL DEFAULT 0,
  enabled     INTEGER NOT NULL DEFAULT 1,
  source      TEXT NOT NULL DEFAULT 'tool',
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  last_run_at TEXT NOT NULL DEFAULT '',
  next_run_at TEXT NOT NULL DEFAULT ''
);
`

type store struct {
	db *sql.DB // nil = memory-only (degraded, vault unavailable)
}

func newStore(db *sql.DB) (*store, error) {
	if db == nil {
		return &store{}, nil
	}
	if _, err := db.Exec(schedulesDDL); err != nil {
		return nil, fmt.Errorf("schedule: ensure table: %w", err)
	}
	return &store{db: db}, nil
}

func (s *store) put(sc Schedule) error {
	if s.db == nil {
		return nil
	}
	contract, err := json.Marshal(sc.Contract)
	if err != nil {
		return fmt.Errorf("schedule: marshal contract: %w", err)
	}
	_, err = s.db.Exec(`INSERT INTO schedules (
                id, prompt, description, kind, cron, fixed_rate, one_time,
                workdir, role, contract, timeout_sec, enabled, source,
                created_at, updated_at, last_run_at, next_run_at
        ) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
        ON CONFLICT(id) DO UPDATE SET
                prompt=excluded.prompt, description=excluded.description,
                kind=excluded.kind, cron=excluded.cron, fixed_rate=excluded.fixed_rate,
                one_time=excluded.one_time, workdir=excluded.workdir, role=excluded.role,
                contract=excluded.contract, timeout_sec=excluded.timeout_sec,
                enabled=excluded.enabled, source=excluded.source,
                updated_at=excluded.updated_at, last_run_at=excluded.last_run_at,
                next_run_at=excluded.next_run_at`,
		sc.ID, sc.Prompt, sc.Description, sc.Trigger.Kind(), sc.Trigger.Cron,
		int64(sc.Trigger.FixedRate.Seconds()), tsOrEmpty(sc.Trigger.OneTime),
		sc.WorkDir, sc.Role, string(contract), int64(sc.Timeout.Seconds()),
		boolToInt(sc.Enabled), sc.Source,
		sc.CreatedAt.UTC().Format(time.RFC3339), sc.UpdatedAt.UTC().Format(time.RFC3339),
		tsOrEmpty(sc.LastRunAt), tsOrEmpty(sc.NextRunAt))
	if err != nil {
		return fmt.Errorf("schedule: put %s: %w", sc.ID, err)
	}
	return nil
}

func (s *store) delete(id string) error {
	if s.db == nil {
		return nil
	}
	if _, err := s.db.Exec(`DELETE FROM schedules WHERE id = ?`, id); err != nil {
		return fmt.Errorf("schedule: delete %s: %w", id, err)
	}
	return nil
}

func (s *store) list() ([]Schedule, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT
                id, prompt, description, kind, cron, fixed_rate, one_time,
                workdir, role, contract, timeout_sec, enabled, source,
                created_at, updated_at, last_run_at, next_run_at
                FROM schedules ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("schedule: list: %w", err)
	}
	defer rows.Close()
	var out []Schedule
	for rows.Next() {
		var (
			sc                                    Schedule
			kind, cron                            string
			contract                              string
			fixedRate, timeout                    int64
			enabled                               int
			oneTime, last, next, created, updated string
		)
		if err := rows.Scan(&sc.ID, &sc.Prompt, &sc.Description, &kind, &cron,
			&fixedRate, &oneTime, &sc.WorkDir, &sc.Role, &contract, &timeout, &enabled,
			&sc.Source, &created, &updated, &last, &next); err != nil {
			return nil, fmt.Errorf("schedule: scan: %w", err)
		}
		switch kind {
		case KindCron:
			sc.Trigger.Cron = cron
		case KindFixedRate:
			sc.Trigger.FixedRate = time.Duration(fixedRate) * time.Second
		case KindOneTime:
			sc.Trigger.OneTime = parseTs(oneTime)
		}
		_ = json.Unmarshal([]byte(contract), &sc.Contract)
		sc.Timeout = time.Duration(timeout) * time.Second
		sc.Enabled = enabled != 0
		sc.LastRunAt = parseTs(last)
		sc.NextRunAt = parseTs(next)
		sc.CreatedAt = parseTs(created)
		sc.UpdatedAt = parseTs(updated)
		out = append(out, sc)
	}
	return out, rows.Err()
}

func tsOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseTs(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// newScheduleID mirrors the job id shape: timestamped, collision-safe.
func newScheduleID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("sch_%s_%s", time.Now().UTC().Format("20060102T150405"), hex.EncodeToString(b[:])), nil
}
