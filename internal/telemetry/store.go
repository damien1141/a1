package telemetry

import (
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite" // driver name "sqlite"; pure Go, already a module dep
)

// schema is the Bet 07 telemetry DDL. Lazy (CREATE IF NOT EXISTS on first
// use) so an ordinary `a1 run` never pays for tables it will not fill: the
// sink only materializes when a surface attaches a database.
//
// tool_runs is the hot-path table, fed from the global sink. turns is the
// cold-path table: `a1 eval` materializes it from persisted session trees
// (the durable source of truth for per-turn token spend), never from a
// live loop — replaying the JSONL after the fact is cheaper than making
// the engine's turn path durable.
const schema = `
CREATE TABLE IF NOT EXISTS tool_runs (
  id          INTEGER PRIMARY KEY,
  session_id  TEXT,
  tool        TEXT,
  call_id     TEXT,
  ok          INTEGER,
  latency_ms  INTEGER,
  err         TEXT,
  at          TEXT
);
CREATE INDEX IF NOT EXISTS idx_tool_runs_at ON tool_runs(at);
CREATE INDEX IF NOT EXISTS idx_tool_runs_tool ON tool_runs(tool);

CREATE TABLE IF NOT EXISTS turns (
  session_id        TEXT,
  model             TEXT,
  prompt_tokens     INT,
  completion_tokens INT,
  cached_tokens     INT,
  total_tokens      INT,
  at                TEXT,
  PRIMARY KEY (session_id, at)
);
`

// EnsureSchema applies the telemetry DDL to db (idempotent).
func EnsureSchema(db *sql.DB) error {
	if db == nil {
		return errors.New("telemetry: db is required")
	}
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	return nil
}

// ToolRunRow is one persisted tool run (see RecentToolRuns).
type ToolRunRow struct {
	SessionID string
	Tool      string
	CallID    string
	OK        bool
	LatencyMS int64
	Err       string
	At        time.Time
}

// TurnRow is one materialized turn (see MaterializeTurns).
type TurnRow struct {
	Model            string
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	TotalTokens      int
	At               string // RFC3339Nano; part of the primary key
}

// ToolAggregate is the per-tool rollup the dashboard renders.
type ToolAggregate struct {
	Tool        string  `json:"tool"`
	Calls       int     `json:"calls"`
	OK          int     `json:"ok"`
	Errors      int     `json:"errors"`
	MeanLatency float64 `json:"mean_latency_ms"`
}

// TurnSummaryRow is one session's token total (see TurnSummary).
type TurnSummaryRow struct {
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
	Turns     int    `json:"turns"`
	TotalTok  int64  `json:"total_tokens"`
}

// Store is the SQLite telemetry sink: a bounded queue drained by a single
// writer goroutine (the memstore single-writer pattern — one connection, one
// transaction per batch, readers never contend). It implements Sink; Record
// never blocks, events beyond the queue capacity are dropped: telemetry is
// observation, not a critical path.
type Store struct {
	db     *sql.DB
	events chan Event
	done   chan struct{}
	wg     sync.WaitGroup
	once   sync.Once
	// dropped counts events lost to a full queue (observable via DropCount).
	dropped atomic.Int64
}

// queueCapacity bounds buffered events between Record and the writer. 1024
// covers the busiest tool round with room to spare; overflow drops rather
// than blocks.
const queueCapacity = 1024

// NewStore opens a telemetry store on db. The caller owns db; Close stops
// the writer but never closes the handle.
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("telemetry: db is required")
	}
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}
	s := &Store{
		db:     db,
		events: make(chan Event, queueCapacity),
		done:   make(chan struct{}),
	}
	s.wg.Add(1)
	go s.writer()
	return s, nil
}

// Record implements Sink. Non-blocking: a full queue drops the event.
func (s *Store) Record(e Event) {
	if s == nil {
		return
	}
	// Only terminal events carry a row's worth of information; the tool_call
	// pre-event is the in-memory Recorder's concurrency counter, not a row.
	switch e.Kind {
	case EventToolOK, EventToolErr:
	default:
		return
	}
	select {
	case s.events <- e:
	default:
		s.dropped.Add(1)
	}
}

// DropCount reports how many events were dropped by a full queue.
func (s *Store) DropCount() int64 {
	if s == nil {
		return 0
	}
	return s.dropped.Load()
}

// writer drains the queue into tool_runs, one transaction per batch. The
// ack ordering rule from memstore applies: rows commit before any reader
// could observe them via another pooled connection.
func (s *Store) writer() {
	defer s.wg.Done()
	batch := make([]Event, 0, 64)
	for {
		select {
		case e := <-s.events:
			batch = append(batch, e)
			// Drain whatever else is queued without blocking, so one tx
			// amortizes over a burst instead of one-row-per-tx.
			for len(batch) < 64 {
				select {
				case e := <-s.events:
					batch = append(batch, e)
				default:
					goto flush
				}
			}
		flush:
			if err := s.insertBatch(batch); err != nil {
				// Telemetry must never take the process down; a failed batch
				// is dropped on the floor (the next one may recover).
				_ = err
			}
			batch = batch[:0]
		case <-s.done:
			// Final drain: events already queued still land.
			for {
				select {
				case e := <-s.events:
					batch = append(batch, e)
				default:
					if len(batch) > 0 {
						_ = s.insertBatch(batch)
					}
					return
				}
			}
		}
	}
}

func (s *Store) insertBatch(batch []Event) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO tool_runs (session_id, tool, call_id, ok, latency_ms, err, at)
		VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, e := range batch {
		ok := 0
		if e.Kind == EventToolOK {
			ok = 1
		}
		if _, err := stmt.Exec(nullIfEmpty(e.SessionID), e.Tool, nullIfEmpty(e.CallID),
			ok, e.DurationMS, nullIfEmpty(e.Err), e.When.UTC().Format(time.RFC3339Nano)); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Close stops the writer after a final drain. Idempotent; the handle stays
// open (the caller owns it).
func (s *Store) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		close(s.done)
		s.wg.Wait()
	})
}

// globalStoreOnce guards AttachDB: only the first attached database wins, so
// a second surface (webserver handler after `a1 eval` in the same process)
// cannot silently re-point persistence at a different file.
var globalStoreOnce sync.Once

// AttachDB installs a SQLite telemetry sink on db as the process-global sink
// (creating the tables when missing). It is safe to call from anywhere; only
// the first call takes effect. A nil db is a no-op so surfaces can call it
// unconditionally on a degraded vault.
func AttachDB(db *sql.DB) error {
	if db == nil {
		return nil
	}
	var err error
	globalStoreOnce.Do(func() {
		var st *Store
		st, err = NewStore(db)
		if err != nil {
			return
		}
		SetGlobalSink(st)
	})
	return err
}

// MaterializeTurns writes one session's per-turn token rows. Rows are keyed
// (session_id, at) and REPLACEd, so re-running the scorer over an evolving
// session is idempotent. This is the eval scorer's path, not a hot path.
func MaterializeTurns(db *sql.DB, sessionID string, rows []TurnRow) error {
	if db == nil {
		return errors.New("telemetry: db is required")
	}
	if err := EnsureSchema(db); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO turns
		(session_id, model, prompt_tokens, completion_tokens, cached_tokens, total_tokens, at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(session_id, at) DO UPDATE SET
		  model=excluded.model, prompt_tokens=excluded.prompt_tokens,
		  completion_tokens=excluded.completion_tokens, cached_tokens=excluded.cached_tokens,
		  total_tokens=excluded.total_tokens`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.Exec(sessionID, r.Model, r.PromptTokens, r.CompletionTokens,
			r.CachedTokens, r.TotalTokens, r.At); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// RecentToolRuns returns the newest limit tool-run rows (newest first).
func RecentToolRuns(db *sql.DB, limit int) ([]ToolRunRow, error) {
	if db == nil {
		return nil, errors.New("telemetry: db is required")
	}
	if limit <= 0 {
		limit = 20
	}
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT session_id, tool, call_id, ok, latency_ms, err, at
		FROM tool_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToolRunRow
	for rows.Next() {
		var r ToolRunRow
		var ok int
		var sid, cid, errMsg, at sql.NullString
		if err := rows.Scan(&sid, &r.Tool, &cid, &ok, &r.LatencyMS, &errMsg, &at); err != nil {
			return nil, err
		}
		r.SessionID = sid.String
		r.CallID = cid.String
		r.Err = errMsg.String
		r.OK = ok != 0
		if at.Valid {
			r.At, _ = time.Parse(time.RFC3339Nano, at.String)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ToolAggregates rolls tool_runs up per tool: calls (completed runs), ok /
// error splits, and mean latency over runs that reported one.
func ToolAggregates(db *sql.DB) ([]ToolAggregate, error) {
	if db == nil {
		return nil, errors.New("telemetry: db is required")
	}
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT tool,
		COUNT(*), SUM(ok), SUM(1 - ok),
		AVG(CASE WHEN latency_ms > 0 THEN latency_ms END)
		FROM tool_runs GROUP BY tool ORDER BY COUNT(*) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToolAggregate
	for rows.Next() {
		var a ToolAggregate
		var okSum, errSum sql.NullInt64
		var mean sql.NullFloat64
		if err := rows.Scan(&a.Tool, &a.Calls, &okSum, &errSum, &mean); err != nil {
			return nil, err
		}
		a.OK = int(okSum.Int64)
		a.Errors = int(errSum.Int64)
		a.MeanLatency = mean.Float64
		out = append(out, a)
	}
	return out, rows.Err()
}

// RecentErrors returns the newest limit failed tool runs (newest first).
func RecentErrors(db *sql.DB, limit int) ([]ToolRunRow, error) {
	if db == nil {
		return nil, errors.New("telemetry: db is required")
	}
	if limit <= 0 {
		limit = 10
	}
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT session_id, tool, call_id, ok, latency_ms, err, at
		FROM tool_runs WHERE ok = 0 ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToolRunRow
	for rows.Next() {
		var r ToolRunRow
		var ok int
		var sid, cid, errMsg, at sql.NullString
		if err := rows.Scan(&sid, &r.Tool, &cid, &ok, &r.LatencyMS, &errMsg, &at); err != nil {
			return nil, err
		}
		r.SessionID = sid.String
		r.CallID = cid.String
		r.Err = errMsg.String
		r.OK = ok != 0
		if at.Valid {
			r.At, _ = time.Parse(time.RFC3339Nano, at.String)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TurnSummary returns per-session token totals from the materialized turns
// table, newest session first.
func TurnSummary(db *sql.DB) ([]TurnSummaryRow, error) {
	if db == nil {
		return nil, errors.New("telemetry: db is required")
	}
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT session_id, model, COUNT(*), SUM(total_tokens)
		FROM turns GROUP BY session_id, model ORDER BY MAX(at) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TurnSummaryRow
	for rows.Next() {
		var r TurnSummaryRow
		var total sql.NullInt64
		if err := rows.Scan(&r.SessionID, &r.Model, &r.Turns, &total); err != nil {
			return nil, err
		}
		r.TotalTok = total.Int64
		out = append(out, r)
	}
	return out, rows.Err()
}
