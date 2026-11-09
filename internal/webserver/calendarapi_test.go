package webserver

import (
        "encoding/json"
        "fmt"
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "strings"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/calendar"
)

// The calendar endpoints' trio (dashboard chunk 2). The parser's own trio
// lives in internal/calendar; this file pins the HTTP layer: the gate
// composition, the hostile-input fallbacks, and endpoint-vs-library
// parity over the response's own window.

// seedCalendarFixture fills dir with the standard fixture set: one timed
// event now, one all-day event tomorrow, one recurring event, one far
// outside any sane window, one file with a malformed line.
func seedCalendarFixture(t *testing.T, dir string) {
        t.Helper()
        require.NoError(t, os.MkdirAll(dir, 0o755))
        now := time.Now().UTC()
        ics := func(body string) string {
                return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//x//EN\r\n" + body + "END:VCALENDAR\r\n"
        }
        require.NoError(t, os.WriteFile(filepath.Join(dir, "timed.ics"), []byte(ics(
                "BEGIN:VEVENT\r\nUID:timed@t\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:"+
                        now.Format("20060102T150405Z")+"\r\nDTEND:"+now.Add(time.Hour).Format("20060102T150405Z")+
                        "\r\nSUMMARY:Timed today\r\nLOCATION:Somewhere\r\nEND:VEVENT\r\n")), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(dir, "allday.ics"), []byte(ics(
                "BEGIN:VEVENT\r\nUID:allday@t\r\nDTSTART;VALUE=DATE:"+now.AddDate(0, 0, 1).Format("20060102")+
                        "\r\nSUMMARY:All day tomorrow\r\nEND:VEVENT\r\n")), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(dir, "recurring.ics"), []byte(ics(
                "BEGIN:VEVENT\r\nUID:recur@t\r\nDTSTART:"+now.Format("20060102T150405Z")+
                        "\r\nRRULE:FREQ=WEEKLY\r\nSUMMARY:Weekly sync\r\nEND:VEVENT\r\n")), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(dir, "far.ics"), []byte(ics(
                "BEGIN:VEVENT\r\nUID:far@t\r\nDTSTART:20990101T000000Z\r\nSUMMARY:Far future\r\nEND:VEVENT\r\n")), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.ics"), []byte(ics(
                "this line has no colon\r\nBEGIN:VEVENT\r\nUID:ok@t\r\nDTSTART:"+now.Format("20060102T150405Z")+
                        "\r\nSUMMARY:Survivor\r\nEND:VEVENT\r\n")), 0o644))
}

// newCalendarServer builds a fixture project with the personal section
// pointing at the seeded dir.
func newCalendarServer(t *testing.T, dir string) *Server {
        t.Helper()
        return newPersonalFixtureProject(t, fmt.Sprintf("personal:\n  enabled: true\n  calendar_dir: %s\n", dir))
}

func getCalendar(t *testing.T, srv *Server, path string) (int, calendarResponse) {
        t.Helper()
        rec := httptest.NewRecorder()
        srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
        var body calendarResponse
        if rec.Code == http.StatusOK {
                require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
        }
        return rec.Code, body
}

// ---------- positive ----------

func TestCalendarAgendaServesFixture_Positive(t *testing.T) {
        dir := t.TempDir()
        seedCalendarFixture(t, dir)
        srv := newCalendarServer(t, dir)

        code, body := getCalendar(t, srv, "/api/personal/calendar?days=14")
        require.Equal(t, http.StatusOK, code)
        assert.True(t, body.Configured)

        uids := map[string]bool{}
        for _, ev := range body.Events {
                uids[ev.UID] = true
        }
        assert.True(t, uids["timed@t"], "today's timed event is served")
        assert.True(t, uids["allday@t"], "tomorrow's all-day event is served")
        assert.True(t, uids["recur@t"], "a recurring event expands into the window")
        assert.True(t, uids["ok@t"], "the survivor next to the malformed line is served")
        assert.False(t, uids["far@t"], "the far-future event is filtered out")
        assert.False(t, body.Stats.Degraded == false && body.Stats.SkippedLines == 0,
                "the malformed line is counted, not swallowed silently")

        // Ascending order, all-day flagged, location carried. The weekly
        // recur@t (no COUNT, no UNTIL) expands to two occurrences inside a
        // 14-day window: its anchor and the one 7 days later.
        require.Len(t, body.Events, 5)
        assert.LessOrEqual(t, body.Events[0].Start, body.Events[1].Start)
        for _, ev := range body.Events {
                if ev.UID == "allday@t" {
                        assert.True(t, ev.AllDay)
                }
                if ev.UID == "timed@t" {
                        assert.Equal(t, "Somewhere", ev.Location)
                        assert.False(t, ev.AllDay)
                }
                if ev.UID == "recur@t" {
                        assert.True(t, ev.Recurring, "every occurrence carries the recurring marker")
                }
        }
}

func TestCalendarRangeServesWindow_Positive(t *testing.T) {
        dir := t.TempDir()
        seedCalendarFixture(t, dir)
        srv := newCalendarServer(t, dir)

        from := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
        to := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
        code, body := getCalendar(t, srv, "/api/personal/calendar/range?from="+from+"&to="+to)
        require.Equal(t, http.StatusOK, code)
        // The window's to-bound is the local midnight of the 7th day out, and
        // the second weekly occurrence lands later that day — so the range
        // sees the recur@t anchor only, plus timed, allday, and the survivor.
        assert.Len(t, body.Events, 4, "the range window sees the same near-now set")
}

// ---------- negative ----------

func TestCalendarGatedOff_Negative(t *testing.T) {
        dir := t.TempDir()
        seedCalendarFixture(t, dir)
        // Enabled explicitly false: the gate must 403 before any scan.
        srv := newPersonalFixtureProject(t, fmt.Sprintf("personal:\n  enabled: false\n  calendar_dir: %s\n", dir))
        for _, path := range []string{
                "/api/personal/calendar",
                "/api/personal/calendar?days=14",
                "/api/personal/calendar/range?from=2026-01-01&to=2026-02-01",
        } {
                code, _ := getCalendar(t, srv, path)
                assert.Equal(t, http.StatusForbidden, code, "%s", path)
        }
}

func TestCalendarHostileInputsFallBack_Negative(t *testing.T) {
        dir := t.TempDir()
        seedCalendarFixture(t, dir)
        srv := newCalendarServer(t, dir)

        for _, raw := range []string{"", "abc", "0", "-5", "99999999999999999999", "10000"} {
                code, body := getCalendar(t, srv, "/api/personal/calendar?days="+raw)
                require.Equal(t, http.StatusOK, code, "days=%q never errors", raw)
                require.NotEmpty(t, body.Events, "days=%q still serves the fixture", raw)
        }

        // Hostile range bounds fall back to a sane window, never 5xx. The
        // inverted and absurd-span cases normalize to windows the fixture
        // doesn't intersect — what's pinned is the normalization, not the
        // event count.
        for _, tc := range []struct {
                q    string
                from time.Time
                to   time.Time
        }{
                {"?from=nonsense&to=alsononsense", time.Now(), time.Now().AddDate(0, 0, 30)},
                {"?from=", time.Now(), time.Now().AddDate(0, 0, 30)},
                {"?to=2030-01-01", time.Now(), time.Now().AddDate(0, 0, 400)}, // to parses but the 400-day cap applies
                {"?from=2030-01-01&to=2020-01-01", time.Date(2030, 1, 1, 0, 0, 0, 0, time.Local), time.Date(2030, 1, 2, 0, 0, 0, 0, time.Local)},
                // The absurd span hits the 400-day cap: 1980-01-01 + 400d = 1981-02-04
                // (1980 is a leap year; the arithmetic differs from a non-leap anchor).
                // The anchor stays in the modern era on purpose: pre-1972 zone history
                // can carry sub-minute LMT offsets (Madrid's was -00:14:44), and RFC3339
                // serializes offsets to minute precision, so an exact-instant pin on a
                // 1900 date flakes on every non-UTC machine whose zone reaches that far
                // back — the string round-trips 44 seconds short of the true instant.
                {"?from=1980-01-01&to=2900-01-01", time.Date(1980, 1, 1, 0, 0, 0, 0, time.Local), time.Date(1981, 2, 4, 0, 0, 0, 0, time.Local)}, // 400-day cap
                {"?from=%00%01&to=" + string([]byte{0xff}), time.Now(), time.Now().AddDate(0, 0, 30)},
        } {
                code, body := getCalendar(t, srv, "/api/personal/calendar/range"+tc.q)
                require.Equal(t, http.StatusOK, code, "range %q", tc.q)
                gotFrom, err := time.Parse(time.RFC3339, body.From)
                require.NoError(t, err)
                gotTo, err := time.Parse(time.RFC3339, body.To)
                require.NoError(t, err)
                assert.True(t, gotFrom.Equal(calendar.LocalMidnight(tc.from)), "%q: from %s", tc.q, body.From)
                assert.True(t, gotTo.Equal(calendar.LocalMidnight(tc.to)), "%q: to %s", tc.q, body.To)
                assert.True(t, gotTo.After(gotFrom), "%q: window is never empty or inverted", tc.q)
                if tc.q == "?from=nonsense&to=alsononsense" || tc.q == "?from=" || strings.HasPrefix(tc.q, "?to=") || strings.HasPrefix(tc.q, "?from=%") {
                        assert.NotEmpty(t, body.Events, "%q: a default window still serves the fixture", tc.q)
                }
        }
}

func TestCalendarDirMissingAndHostile_Negative(t *testing.T) {
        // A configured-but-missing directory is an empty calendar, not an
        // error — the widget distinguishes "not configured" via status.
        missing := filepath.Join(t.TempDir(), "does-not-exist")
        srv := newCalendarServer(t, missing)
        code, body := getCalendar(t, srv, "/api/personal/calendar")
        require.Equal(t, http.StatusOK, code)
        assert.True(t, body.Configured)
        assert.Empty(t, body.Events)
        assert.False(t, body.Stats.Degraded, "a missing dir is empty, not degraded")

        // A hostile "directory" degrades honestly: a dangling symlink named
        // *.ics makes the walk's stat fail — counted, not fatal. (A plain FILE
        // at the path is not an error at all: if it ends in .ics it is simply
        // parsed as a one-file calendar, which is reasonable.)
        hostile := t.TempDir()
        require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "nowhere"), filepath.Join(hostile, "dangling.ics")))
        srv2 := newCalendarServer(t, hostile)
        code, body = getCalendar(t, srv2, "/api/personal/calendar")
        require.Equal(t, http.StatusOK, code, "a hostile path never 500s")
        assert.Empty(t, body.Events)
        assert.True(t, body.Stats.Degraded, "the unreadable entry is counted")
        assert.Equal(t, 1, body.Stats.SkippedFiles)
}

func TestCalendarDirEmptiedAtRuntime_Negative(t *testing.T) {
        dir := t.TempDir()
        seedCalendarFixture(t, dir)
        srv := newCalendarServer(t, dir)

        code, body := getCalendar(t, srv, "/api/personal/calendar")
        require.Equal(t, http.StatusOK, code)
        require.NotEmpty(t, body.Events)

        entries, err := os.ReadDir(dir)
        require.NoError(t, err)
        for _, e := range entries {
                require.NoError(t, os.Remove(filepath.Join(dir, e.Name())))
        }
        code, body = getCalendar(t, srv, "/api/personal/calendar")
        require.Equal(t, http.StatusOK, code)
        assert.Empty(t, body.Events, "an emptied dir serves an empty agenda, not a stale one")
        assert.Equal(t, 0, body.Stats.Files)
}

// ---------- differential ----------

// TestCalendarEndpointMatchesLibrary_Differential: the endpoint must be a
// faithful projection of calendar.Agenda over the response's OWN window —
// the same fixtures through two implementations (HTTP handler vs direct
// library call), compared event-by-event.
func TestCalendarEndpointMatchesLibrary_Differential(t *testing.T) {
        dir := t.TempDir()
        seedCalendarFixture(t, dir)
        srv := newCalendarServer(t, dir)

        for _, q := range []string{
                "/api/personal/calendar",
                "/api/personal/calendar?days=3",
                "/api/personal/calendar?days=90",
        } {
                t.Run(q, func(t *testing.T) {
                        code, body := getCalendar(t, srv, q)
                        require.Equal(t, http.StatusOK, code)

                        from, err := time.Parse(time.RFC3339, body.From)
                        require.NoError(t, err)
                        to, err := time.Parse(time.RFC3339, body.To)
                        require.NoError(t, err)

                        want, _, err := calendar.Agenda(dir, from, to)
                        require.NoError(t, err)
                        require.Len(t, body.Events, len(want), "endpoint and library agree on the count")

                        for i, ev := range body.Events {
                                assert.Equal(t, want[i].UID, ev.UID)
                                assert.Equal(t, want[i].Summary, ev.Summary)
                                assert.Equal(t, want[i].Location, ev.Location)
                                assert.Equal(t, want[i].AllDay, ev.AllDay)
                                assert.Equal(t, want[i].Recurring, ev.Recurring)
                                gotStart, err := time.Parse(time.RFC3339, ev.Start)
                                require.NoError(t, err)
                                assert.True(t, want[i].Start.Equal(gotStart), "start: %s vs %s", ev.Start, want[i].Start)
                                if want[i].End.IsZero() {
                                        assert.Empty(t, ev.End)
                                } else {
                                        gotEnd, err := time.Parse(time.RFC3339, ev.End)
                                        require.NoError(t, err)
                                        assert.True(t, want[i].End.Equal(gotEnd), "end: %s vs %s", ev.End, want[i].End)
                                }
                        }

                        // Days clamping is part of the contract: an oversize days
                        // answers exactly what the capped window answers.
                        if q == "/api/personal/calendar?days=90" {
                                code2, capped := getCalendar(t, srv, "/api/personal/calendar?days=10000")
                                require.Equal(t, http.StatusOK, code2)
                                assert.Equal(t, body.To, capped.To, "days=10000 clamps to the same window as days=90")
                        }
                })
        }
}
