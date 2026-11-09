package webserver

import (
        "database/sql"
        "encoding/json"
        "fmt"
        "net/http"
        "sort"
        "strconv"
        "sync"
        "time"

        "github.com/damien1141/a1/internal/telemetry"
)

// usageapi.go serves GET /api/usage/daily — the dashboard's commit-style
// heatmap feed. It aggregates the vault's own durable rows (sessions,
// session_entries, tool_runs) into per-day counts; it writes nothing, so it
// needs no gate beyond the loopback binding every other read enjoys.
//
// Day semantics: every metric is bucketed on the UTC calendar day. The two
// timestamp columns written as UTC (sessions.created_at, tool_runs.at) and
// the entry timestamps (marshaled with the writer's local offset) are all
// normalized by SQLite's date(), which parses the RFC3339 offset and folds
// it to UTC — a naive substr(ts,1,10) would bucket a +01:00 entry into the
// wrong day, and the differential test pins that this implementation does
// not.

// usageDay is one day of activity. Zero-valued fields are meaningful (an
// empty day still ships as a row) so the heatmap never has to infer gaps.
type usageDay struct {
        Date      string `json:"date"`
        Sessions  int64  `json:"sessions"`
        Messages  int64  `json:"messages"`
        ToolCalls int64  `json:"toolCalls"`
        Tokens    int64  `json:"tokens"`
}

// usageResponse is the endpoint payload: the resolved window plus one row
// per day, oldest first, zero-filled.
type usageResponse struct {
        Days  int        `json:"days"`
        Daily []usageDay `json:"daily"`
}

const (
        // usageDefaultDays is the 53-week heatmap window (53×7 = 371).
        usageDefaultDays = 371
        // usageMaxDays caps a hostile ?days= value at ~11 years of zero-filled
        // rows — plenty for a heatmap, bounded for the response body.
        usageMaxDays = 4000
        // usageCacheTTL bounds how stale a cached aggregate may be.
        usageCacheTTL = 60 * time.Second
)

// usageCache is the in-memory TTL cache: the aggregate scans every entry
// payload, and a dashboard left open would otherwise re-scan on every
// mount. One entry (the last computed window) is plenty for one heatmap;
// a different ?days= simply recomputes.
type usageCache struct {
        mu      sync.Mutex
        days    int
        value   []usageDay
        expires time.Time
}

var usageTTL usageCache

// handleUsageDaily serves the per-day usage rollup. ?days=N is clamped
// (<=0 or unparsable ⇒ the 371-day default, >4000 ⇒ 4000); the degraded
// vault (nil) answers an honest zero-filled window rather than an error,
// matching how /api/telemetry renders its empty state.
func (s *Server) handleUsageDaily(w http.ResponseWriter, r *http.Request) {
        days := usageParseDays(r.URL.Query().Get("days"))

        usageTTL.mu.Lock()
        cached := days == usageTTL.days && time.Now().Before(usageTTL.expires)
        value := usageTTL.value
        usageTTL.mu.Unlock()
        if cached {
                writeJSON(w, http.StatusOK, usageResponse{Days: days, Daily: value})
                return
        }

        db := s.telemetryDB() // ensures tool_runs exists (telemetry's lazy DDL)
        var daily []usageDay
        if db == nil {
                daily = usageZeroFilled(days)
        } else {
                var err error
                daily, err = usageDaily(db, days)
                if err != nil {
                        writeStatus(w, http.StatusInternalServerError, "usage: "+err.Error())
                        return
                }
        }

        usageTTL.mu.Lock()
        usageTTL.days, usageTTL.value, usageTTL.expires = days, daily, time.Now().Add(usageCacheTTL)
        usageTTL.mu.Unlock()
        writeJSON(w, http.StatusOK, usageResponse{Days: days, Daily: daily})
}

// usageParseDays resolves the ?days= query: unparsable or non-positive
// falls back to the default window, oversized values clamp. A hostile
// value can only select a valid window, never blow up the response.
func usageParseDays(raw string) int {
        n, err := strconv.Atoi(raw)
        if err != nil || n < 1 {
                return usageDefaultDays
        }
        if n > usageMaxDays {
                return usageMaxDays
        }
        return n
}

// usageDaily is the production aggregate: the SQL rollup, falling back to
// the Go-side scan if the query itself fails (a SQLite build without the
// JSON functions would rather degrade than 500 the dashboard). tool_runs
// is telemetry's lazily-created table — EnsureSchema here (idempotent,
// like every telemetry read) guarantees it exists on THIS db, because the
// telemetryDB() once-guard only primes the first handle in the process
// and tests boot many vaults.
func usageDaily(db *sql.DB, days int) ([]usageDay, error) {
        if err := telemetry.EnsureSchema(db); err != nil {
                return nil, err
        }
        cutoff := usageCutoff(days)
        out, err := usageDailySQL(db, cutoff)
        if err != nil {
                out, err = usageDailyScan(db, cutoff)
                if err != nil {
                        return nil, err
                }
        }
        return usageZeroFill(out, days), nil
}

// usageCutoff is the oldest day (inclusive) in the window, as a UTC date.
func usageCutoff(days int) string {
        return time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
}

// usageDailySQL rolls the three sources up in one UNION ALL pass:
//
//   - sessions: one count per sessions row, keyed date(created_at);
//   - messages: EntryMessage rows whose message.role is user or assistant
//     (tool results are machinery, not conversation) and whose runtime
//     rider flag is clear (riders are ambient context, not conversation),
//     keyed date(json_extract $.timestamp); usage.total_tokens rides the
//     same rows, so tokens land on the day the turn completed;
//   - tool_calls: one count per tool_runs row, keyed date(at).
//
// Every payload extraction sits behind a CASE WHEN json_valid(...) guard:
// json_extract RAISES on malformed JSON (a corrupt row must be skipped,
// never fatal), and while SQLite's planner happens to evaluate the ANDed
// WHERE terms in written order today, that is an implementation detail —
// CASE evaluation order is the documented contract the guard hangs on.
// date() returns NULL for unparseable timestamps, which the day IS NOT
// NULL filter then excludes.
//
// The session_entries scan is a full table walk (kind has no index); it is
// C-side and TTL-cached, which is fine for a dashboard feed — the naive
// scan below is the fallback precisely because it is slower.
func usageDailySQL(db *sql.DB, cutoff string) ([]usageDay, error) {
        if db == nil {
                return nil, fmt.Errorf("usage: db is required")
        }
        rows, err := db.Query(`
                SELECT day, SUM(sessions), SUM(messages), SUM(tool_calls), SUM(tokens) FROM (
                        SELECT date(created_at) AS day, 1 AS sessions, 0 AS messages, 0 AS tool_calls, 0 AS tokens
                                FROM sessions
                                WHERE date(created_at) >= ?
                        UNION ALL
                        SELECT date(CASE WHEN json_valid(payload) THEN json_extract(payload, '$.timestamp') END) AS day,
                                        0, 1, 0,
                                        coalesce(CASE WHEN json_valid(payload) THEN json_extract(payload, '$.usage.total_tokens') END, 0)
                                FROM session_entries
                                WHERE kind = 'EntryMessage'
                                        AND coalesce(CASE WHEN json_valid(payload) THEN json_extract(payload, '$.message.runtime') END, 0) = 0
                                        AND CASE WHEN json_valid(payload) THEN json_extract(payload, '$.message.role') END IN ('user', 'assistant')
                                        AND date(CASE WHEN json_valid(payload) THEN json_extract(payload, '$.timestamp') END) >= ?
                        UNION ALL
                        SELECT date(at) AS day, 0, 0, 1, 0
                                FROM tool_runs
                                WHERE date(at) >= ?
                )
                WHERE day IS NOT NULL
                GROUP BY day
                ORDER BY day`, cutoff, cutoff, cutoff)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        var out []usageDay
        for rows.Next() {
                var d usageDay
                if err := rows.Scan(&d.Date, &d.Sessions, &d.Messages, &d.ToolCalls, &d.Tokens); err != nil {
                        return nil, err
                }
                out = append(out, d)
        }
        return out, rows.Err()
}

// usageScanEntry is the payload shape the naive scan decodes — the same
// fields the SQL path extracts, no more.
type usageScanEntry struct {
        Timestamp time.Time `json:"timestamp"`
        Message   struct {
                Role    string `json:"role"`
                Runtime bool   `json:"runtime"`
        } `json:"message"`
        Usage struct {
                TotalTokens int64 `json:"total_tokens"`
        } `json:"usage"`
}

// usageDailyScan is the naive Go oracle: read every raw row, decode the
// payloads with encoding/json, bucket in maps. It is the differential
// test's reference implementation for the SQL rollup, and the runtime
// fallback when the SQL path errors. Identical semantics by contract:
// UTC days, user+assistant non-rider messages only, corrupt rows skipped
// without failing the batch.
func usageDailyScan(db *sql.DB, cutoff string) ([]usageDay, error) {
        if db == nil {
                return nil, fmt.Errorf("usage: db is required")
        }
        buckets := map[string]*usageDay{}
        bucket := func(day string) *usageDay {
                b, ok := buckets[day]
                if !ok {
                        b = &usageDay{Date: day}
                        buckets[day] = b
                }
                return b
        }

        // Sessions: one row = one session on its creation day. Unparseable
        // timestamps (not written by this codebase, but a hostile fixture is
        // cheap to survive) are skipped.
        sessRows, err := db.Query(`SELECT created_at FROM sessions`)
        if err != nil {
                return nil, err
        }
        defer sessRows.Close()
        for sessRows.Next() {
                var createdAt string
                if err := sessRows.Scan(&createdAt); err != nil {
                        return nil, err
                }
                if t, ok := usageParseTime(createdAt); ok {
                        if day, in := usageDayOf(t, cutoff); in {
                                bucket(day).Sessions++
                        }
                }
        }
        if err := sessRows.Err(); err != nil {
                return nil, err
        }

        // Messages + tokens: decode each EntryMessage payload; a corrupt
        // payload is skipped (matching the SQL path's json_valid guard).
        entryRows, err := db.Query(`SELECT payload FROM session_entries WHERE kind = 'EntryMessage'`)
        if err != nil {
                return nil, err
        }
        defer entryRows.Close()
        for entryRows.Next() {
                var payload string
                if err := entryRows.Scan(&payload); err != nil {
                        return nil, err
                }
                var e usageScanEntry
                if err := json.Unmarshal([]byte(payload), &e); err != nil {
                        continue
                }
                if e.Message.Role != "user" && e.Message.Role != "assistant" {
                        continue
                }
                if e.Message.Runtime {
                        continue
                }
                if day, in := usageDayOf(e.Timestamp, cutoff); in {
                        bucket(day).Messages++
                        bucket(day).Tokens += e.Usage.TotalTokens
                }
        }
        if err := entryRows.Err(); err != nil {
                return nil, err
        }

        // Tool calls: one row = one call on its day. `at` is nullable in the
        // telemetry DDL, so NULLs (no timestamp) are skipped.
        toolRows, err := db.Query(`SELECT at FROM tool_runs`)
        if err != nil {
                return nil, err
        }
        defer toolRows.Close()
        for toolRows.Next() {
                var at sql.NullString
                if err := toolRows.Scan(&at); err != nil {
                        return nil, err
                }
                if t, ok := usageParseTime(at.String); ok {
                        if day, in := usageDayOf(t, cutoff); in {
                                bucket(day).ToolCalls++
                        }
                }
        }
        if err := toolRows.Err(); err != nil {
                return nil, err
        }

        out := make([]usageDay, 0, len(buckets))
        for _, b := range buckets {
                out = append(out, *b)
        }
        // Sorted for stable comparison; the caller zero-fills into day order.
        sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
        return out, nil
}

// usageParseTime parses one RFC3339(Nano) timestamp; ok is false for
// garbage (skip, not fail).
func usageParseTime(raw string) (time.Time, bool) {
        if raw == "" {
                return time.Time{}, false
        }
        t, err := time.Parse(time.RFC3339Nano, raw)
        if err != nil {
                return time.Time{}, false
        }
        return t, true
}

// usageDayOf normalizes a timestamp to its UTC day and reports whether it
// falls inside the window (day >= cutoff, both 'YYYY-MM-DD' so the string
// compare is the calendar compare).
func usageDayOf(t time.Time, cutoff string) (string, bool) {
        day := t.UTC().Format("2006-01-02")
        return day, day >= cutoff
}

// usageZeroFill expands the sparse aggregates into one row per day for the
// trailing days-window ending today (UTC), oldest first. A heatmap should
// never have to guess whether a missing date means zero or unknown.
func usageZeroFill(sparse []usageDay, days int) []usageDay {
        byDate := make(map[string]usageDay, len(sparse))
        for _, d := range sparse {
                byDate[d.Date] = d
        }
        return usageWindow(byDate, days)
}

// usageZeroFilled builds the empty window for the degraded vault.
func usageZeroFilled(days int) []usageDay {
        return usageWindow(nil, days)
}

// usageWindow materializes the trailing days-window with values from byDate.
func usageWindow(byDate map[string]usageDay, days int) []usageDay {
        today := time.Now().UTC()
        out := make([]usageDay, 0, days)
        for i := days - 1; i >= 0; i-- {
                date := today.AddDate(0, 0, -i).Format("2006-01-02")
                d := byDate[date] // zero value when absent
                d.Date = date
                out = append(out, d)
        }
        return out
}
