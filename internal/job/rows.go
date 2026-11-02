package job

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// jobsDDL creates the jobs table lazily on the caller's handle — the vault
// pattern for subsystem-owned rows (see memstore.OpenIn). The table mirrors
// meta.json so the vault is a queryable record of every job the fleet ever
// ran (status transitions upsert on every persist); meta.json stays the
// working store and recovery source.
const jobsDDL = `
CREATE TABLE IF NOT EXISTS jobs (
  id           TEXT PRIMARY KEY,
  kind         TEXT NOT NULL DEFAULT '',
  parent_id    TEXT NOT NULL DEFAULT '',
  parent_depth INTEGER NOT NULL DEFAULT 0,
  role         TEXT NOT NULL DEFAULT '',
  prompt       TEXT NOT NULL,
  description  TEXT NOT NULL DEFAULT '',
  workdir      TEXT NOT NULL DEFAULT '',
  warm_from    TEXT NOT NULL DEFAULT '',
  schedule_id  TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL,
  attempt      INTEGER NOT NULL DEFAULT 0,
  resume       INTEGER NOT NULL DEFAULT 0,
  timeout_sec  INTEGER NOT NULL DEFAULT 0,
  contract     TEXT NOT NULL DEFAULT '[]',
  error        TEXT NOT NULL DEFAULT '',
  dir          TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL DEFAULT '',
  started_at   TEXT NOT NULL DEFAULT '',
  finished_at  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status, created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_schedule ON jobs(schedule_id) WHERE schedule_id != '';
`

// EnsureJobRows creates the jobs table on the handle when missing. Called
// by the store constructor (Options.DB) and by the vault's one-shot
// importer, which writes rows for pre-vault job dirs.
func EnsureJobRows(db *sql.DB) error {
	if db == nil {
		return nil
	}
	if _, err := db.Exec(jobsDDL); err != nil {
		return fmt.Errorf("job: ensure rows: %w", err)
	}
	return nil
}

// PutJobRow upserts a job's meta as a row (used by the store on every
// persist, and by the vault importer for legacy job dirs).
func PutJobRow(db *sql.DB, m Meta) error {
	if db == nil {
		return nil
	}
	contract, err := json.Marshal(m.Contract)
	if err != nil {
		return fmt.Errorf("job: marshal contract: %w", err)
	}
	boolInt := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	ts := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	_, err = db.Exec(`INSERT INTO jobs (
                id, kind, parent_id, parent_depth, role, prompt, description, workdir,
                warm_from, schedule_id, status, attempt, resume, timeout_sec, contract,
                error, dir, created_at, started_at, finished_at
        ) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
        ON CONFLICT(id) DO UPDATE SET
                kind=excluded.kind, parent_id=excluded.parent_id, parent_depth=excluded.parent_depth,
                role=excluded.role, prompt=excluded.prompt, description=excluded.description,
                workdir=excluded.workdir, warm_from=excluded.warm_from, schedule_id=excluded.schedule_id,
                status=excluded.status, attempt=excluded.attempt, resume=excluded.resume,
                timeout_sec=excluded.timeout_sec, contract=excluded.contract, error=excluded.error,
                dir=excluded.dir, started_at=excluded.started_at, finished_at=excluded.finished_at`,
		m.ID, m.Kind, m.ParentID, m.ParentDepth, string(m.Role), m.Prompt, m.Description,
		m.WorkDir, m.WarmFrom, m.ScheduleID, string(m.Status), m.Attempt, boolInt(m.Resume),
		m.TimeoutSec, string(contract), m.Error, m.Dir,
		ts(m.CreatedAt), ts(m.StartedAt), ts(m.FinishedAt))
	if err != nil {
		return fmt.Errorf("job: put row %s: %w", m.ID, err)
	}
	return nil
}

// DeleteJobRow removes a job's row (the spawn-rejected path removes the
// dir before any lifecycle existed).
func DeleteJobRow(db *sql.DB, id string) error {
	if db == nil {
		return nil
	}
	if _, err := db.Exec(`DELETE FROM jobs WHERE id = ?`, id); err != nil {
		return fmt.Errorf("job: delete row %s: %w", id, err)
	}
	return nil
}

// JobRow is one read-back row (ListJobRows).
type JobRow struct {
	ID         string
	Kind       string
	ParentID   string
	Role       string
	Status     string
	ScheduleID string
	Attempt    int
	Prompt     string
	CreatedAt  time.Time
	StartedAt  time.Time
	FinishedAt time.Time
	Error      string
}

// ListJobRows reads every job row newest-first — the durable query surface
// for dashboards and eval joins.
func ListJobRows(db *sql.DB) ([]JobRow, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.Query(`SELECT
                id, kind, parent_id, role, status, schedule_id, attempt, prompt,
                created_at, started_at, finished_at, error
                FROM jobs ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("job: list rows: %w", err)
	}
	defer rows.Close()
	var out []JobRow
	for rows.Next() {
		var r JobRow
		var created, started, finished string
		if err := rows.Scan(&r.ID, &r.Kind, &r.ParentID, &r.Role, &r.Status, &r.ScheduleID,
			&r.Attempt, &r.Prompt, &created, &started, &finished, &r.Error); err != nil {
			return nil, fmt.Errorf("job: scan row: %w", err)
		}
		r.CreatedAt = parseRowTs(created)
		r.StartedAt = parseRowTs(started)
		r.FinishedAt = parseRowTs(finished)
		out = append(out, r)
	}
	return out, rows.Err()
}

func parseRowTs(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
