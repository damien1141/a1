package webserver

import (
        "encoding/json"
        "fmt"
        "net/http"
        "net/http/httptest"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/telemetry"
        "github.com/damien1141/a1/internal/vault"
)

// usage_test.go is the chunk-1 dashboard heatmap's trio. The flagship is
// the differential: the SQL rollup (production) against the naive Go scan
// (oracle + fallback) over a fixture vault seeded with the cases that
// make naive implementations lie — non-UTC entry timestamps, tool-role
// rows, runtime riders, a corrupt payload, an out-of-window row, a NULL
// tool_run timestamp — across several window sizes.

// usageFixture seeds a booted server's vault with a deterministic activity
// spread relative to today (UTC) and returns today's date so assertions
// can address days symbolically. Seeded activity:
//
//      day T-300: one session            (inside the 371 window, outside 14)
//      day T-3  : one session, one user message, one tool call
//      day T-1  : one user message       (timestamp "T00:30+01:00" — a naive
//                                        substr day-key counts it on T)
//      day T    : one session, two assistant messages w/ usage (2000 tokens
//                total), one tool call, plus rows that must NOT count
//      day T-500: one user message       (outside both windows; rides the T-300 session)
//      never    : tool-role entry, runtime rider, corrupt payload,
//                EntryCompaction row, NULL tool_runs.at
func usageFixture(t *testing.T) (*Server, string) {
        t.Helper()
        srv := newPersonalFixtureProject(t, "")
        require.NotNil(t, srv.vault, "the fixture needs a booted vault")
        db := srv.vault.DB()
        require.NoError(t, telemetry.EnsureSchema(db))

        today := time.Now().UTC().Format("2006-01-02")
        day := func(offset int) string {
                return time.Now().UTC().AddDate(0, 0, offset).Format("2006-01-02")
        }

        // Sessions.
        for _, s := range []vault.SessionRow{
                {ID: "s-today", Project: "usage-fixture", Ts: "x", Cwd: "/x", CreatedAt: time.Now().UTC()},
                {ID: "s-3", Project: "usage-fixture", Ts: "x", Cwd: "/x", CreatedAt: time.Now().UTC().AddDate(0, 0, -3)},
                {ID: "s-300", Project: "usage-fixture", Ts: "x", Cwd: "/x", CreatedAt: time.Now().UTC().AddDate(0, 0, -300)},
        } {
                require.NoError(t, srv.vault.PutSession(s))
        }

        // EntryMessage payload lines (hand-written so the timestamp spellings —
        // the +01:00 offset, the Z suffix, the nanos — are exactly the shapes
        // the vault stores in the wild).
        msg := func(id, ts, role string, runtime bool, tokens string) string {
                line := fmt.Sprintf(`{"type":"EntryMessage","id":%q,"parentID":null,"timestamp":%q,"message":{"role":%q,"content":"x"`,
                        id, ts, role)
                if runtime {
                        line += `,"runtime":true`
                }
                line += `}`
                if tokens != "" {
                        line += `,"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":` + tokens + `}`
                }
                return line + `}`
        }
        entries := []vault.SessionEntryRow{
                // The offset case: "T00:30:00+01:00" is 23:30 UTC on T-1. Counted
                // on T-1 — a substr(…,1,10) implementation counts it on T.
                {SessionID: "s-today", Seq: 0, Kind: "EntryMessage", Payload: msg("e1", day(0)+"T00:30:00+01:00", "user", false, "")},
                {SessionID: "s-today", Seq: 1, Kind: "EntryMessage", Payload: msg("e2", day(0)+"T12:00:00Z", "assistant", false, "1500")},
                {SessionID: "s-today", Seq: 2, Kind: "EntryMessage", Payload: msg("e3", day(0)+"T12:00:05.123456789Z", "assistant", false, "500")},
                // Tool results are machinery, not conversation.
                {SessionID: "s-today", Seq: 3, Kind: "EntryMessage", Payload: msg("e4", day(0)+"T12:00:10Z", "tool", false, "")},
                // Runtime riders are ambient context, not conversation.
                {SessionID: "s-today", Seq: 4, Kind: "EntryMessage", Payload: msg("e5", day(0)+"T12:00:15Z", "user", true, "")},
                // Corrupt payload: skipped, never fatal.
                {SessionID: "s-today", Seq: 5, Kind: "EntryMessage", Payload: `{"type":"EntryMessage","id":"e6","timestamp":`},
                // Non-message entry kinds are ignored wholesale.
                {SessionID: "s-today", Seq: 6, Kind: "EntryCompaction", Payload: `{"type":"EntryCompaction","id":"e7"}`},
                {SessionID: "s-3", Seq: 0, Kind: "EntryMessage", Payload: msg("e8", day(-3)+"T09:00:00Z", "user", false, "")},
                // Outside both windows.
                {SessionID: "s-300", Seq: 0, Kind: "EntryMessage", Payload: msg("e9", day(-500)+"T09:00:00Z", "user", false, "")},
        }
        require.NoError(t, srv.vault.AppendSessionEntries(entries))

        // Tool runs. The telemetry writer always stamps at; a hostile NULL is
        // seeded alongside to prove it is skipped, not counted, not fatal.
        for _, r := range []struct {
                session string
                at      string
        }{
                {"s-today", day(0) + "T12:00:20Z"},
                {"s-3", day(-3) + "T09:05:00Z"},
        } {
                _, err := db.Exec(`INSERT INTO tool_runs (session_id, tool, call_id, ok, latency_ms, err, at)
                        VALUES (?,?,?,?,?,?,?)`, r.session, "bash", "c"+r.session, 1, 5, nil, r.at)
                require.NoError(t, err)
        }
        _, err := db.Exec(`INSERT INTO tool_runs (session_id, tool, call_id, ok, latency_ms, err, at)
                VALUES (?,?,?,?,?,?,?)`, "s-today", "read", "c-nil", 1, 5, nil, nil)
        require.NoError(t, err)

        usageResetCache()
        return srv, today
}

// usageResetCache clears the TTL cache between tests so a cached window
// from one fixture can never answer another's.
func usageResetCache() {
        usageTTL.mu.Lock()
        usageTTL.days, usageTTL.value, usageTTL.expires = 0, nil, time.Time{}
        usageTTL.mu.Unlock()
}

// usageGet drives the handler and decodes the payload.
func usageGet(t *testing.T, srv *Server, days string) usageResponse {
        t.Helper()
        rec := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/api/usage/daily?days="+days, nil)
        srv.handleUsageDaily(rec, req)
        require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
        var out usageResponse
        require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
        return out
}

// usageByDate indexes the daily rows for assertions.
func usageByDate(t *testing.T, res usageResponse) map[string]usageDay {
        t.Helper()
        require.NotEmpty(t, res.Daily)
        out := make(map[string]usageDay, len(res.Daily))
        for _, d := range res.Daily {
                out[d.Date] = d
        }
        return out
}

// ---------- positive ----------

func TestUsageDailyRollsUpVaultRows_Positive(t *testing.T) {
        srv, today := usageFixture(t)
        res := usageGet(t, srv, "14")

        assert.Equal(t, 14, res.Days)
        require.Len(t, res.Daily, 14)
        assert.Equal(t, today, res.Daily[len(res.Daily)-1].Date, "the window ends today")
        assert.Equal(t, time.Now().UTC().AddDate(0, 0, -13).Format("2006-01-02"), res.Daily[0].Date)

        byDate := usageByDate(t, res)
        assert.Equal(t, usageDay{Date: today, Sessions: 1, Messages: 2, ToolCalls: 1, Tokens: 2000}, byDate[today],
                "today: one session, two assistant messages (tool-role and rider excluded), 1500+500 tokens, one tool call")
        assert.Equal(t, usageDay{Date: time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"), Messages: 1},
                byDate[time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")],
                "the +01:00 entry counts on its UTC day (T-1), not its substr day (T)")
        assert.Equal(t, usageDay{Date: time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02"), Sessions: 1, Messages: 1, ToolCalls: 1},
                byDate[time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02")])

        // Days with no seeded activity are present and zero, not missing.
        emptyCount := 0
        for _, d := range res.Daily {
                if d == (usageDay{Date: d.Date}) {
                        emptyCount++
                }
        }
        assert.Equal(t, 11, emptyCount, "14 window days minus the 3 active days are explicit zeros")

        // The 371-day window also sees the 300-day-old session, but not the
        // 500-day-old message.
        res371 := usageGet(t, srv, "371")
        assert.Equal(t, 371, res371.Days)
        assert.Equal(t, int64(1), usageByDate(t, res371)[time.Now().UTC().AddDate(0, 0, -300).Format("2006-01-02")].Sessions)
}

func TestUsageDailyServesCachedWindow_Positive(t *testing.T) {
        srv, _ := usageFixture(t)
        first := usageGet(t, srv, "14")
        second := usageGet(t, srv, "14")
        require.Equal(t, first.Daily, second.Daily, "the second call rides the TTL cache and answers identically")
}

// ---------- negative ----------

func TestUsageDailyHostileDays_Negative(t *testing.T) {
        srv, _ := usageFixture(t)
        for _, tc := range []struct {
                raw  string
                want int
        }{
                {"0", 371},     // non-positive → default
                {"-5", 371},    // negative → default
                {"abc", 371},   // unparsable → default
                {"", 371},      // absent → default
                {"4001", 4000}, // oversized → clamp
                {"14", 14},     // sane values pass through untouched
        } {
                t.Run("days="+tc.raw, func(t *testing.T) {
                        usageResetCache()
                        res := usageGet(t, srv, tc.raw)
                        assert.Equal(t, tc.want, res.Days)
                        assert.Len(t, res.Daily, tc.want)
                })
        }
}

func TestUsageDailyCorruptRowsSkippedNotFatal_Negative(t *testing.T) {
        srv, today := usageFixture(t)
        res := usageGet(t, srv, "14")
        byDate := usageByDate(t, res)
        // The corrupt payload (e6) contributed nothing and the request did not
        // 500 — today's message count is exactly the two decodable ones.
        assert.Equal(t, int64(2), byDate[today].Messages)
}

func TestUsageDailyEmptyAndDegradedVaults_Negative(t *testing.T) {
        // Empty vault: an honest zero-filled window, not an error.
        empty := newPersonalFixtureProject(t, "")
        usageResetCache()
        res := usageGet(t, empty, "7")
        assert.Equal(t, 7, res.Days)
        for _, d := range res.Daily {
                assert.Equal(t, usageDay{Date: d.Date}, d, "every day is an explicit zero")
        }

        // Degraded vault (nil — the file layout): same honest zeros.
        empty.vault = nil
        usageResetCache()
        res = usageGet(t, empty, "3")
        assert.Equal(t, 3, res.Days)
        assert.Equal(t, usageDay{Date: res.Daily[0].Date}, res.Daily[0])
}

// ---------- differential ----------

// TestUsageDailySQLMatchesNaiveScan_Differential is the flagship oracle:
// over the fixture's full spread (offset timestamps, tool roles, riders,
// corrupt payloads, out-of-window rows, NULL tool timestamps), the SQL
// rollup and the naive Go scan must agree exactly, at every window size —
// the scan is the runtime fallback, so any divergence is a real bug in
// one of them.
func TestUsageDailySQLMatchesNaiveScan_Differential(t *testing.T) {
        srv, _ := usageFixture(t)
        db := srv.vault.DB()
        for _, window := range []int{1, 2, 7, 14, 60, 371, 400} {
                t.Run(fmt.Sprintf("window=%d", window), func(t *testing.T) {
                        cutoff := usageCutoff(window)
                        sqlOut, err := usageDailySQL(db, cutoff)
                        require.NoError(t, err)
                        scanOut, err := usageDailyScan(db, cutoff)
                        require.NoError(t, err)
                        assert.Equal(t, sqlOut, scanOut,
                                "SQL rollup and naive Go scan disagree at window %d", window)
                })
        }
}
