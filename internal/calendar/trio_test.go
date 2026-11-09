package calendar

import (
        "os"
        "path/filepath"
        "strings"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// ---------- positive ----------

// TestParseGoldenFixture_Positive pins the subset's happy paths on the
// committed golden fixture: folded lines reassemble, escapes decode, the
// VALARM's DESCRIPTION stays inside the alarm, stats count honestly.
func TestParseGoldenFixture_Positive(t *testing.T) {
        evs, st := Parse(goldenICS(t))

        require.Len(t, evs, 10, "ten well-formed events survive")
        assert.Equal(t, 10, st.Events)
        assert.Equal(t, 1, st.SkippedLines, "the colonless line is counted")
        assert.Equal(t, 2, st.SkippedEvents, "no-DTSTART ghost + unterminated tail")
        assert.True(t, st.Degraded())

        folded := findEvent(t, evs, "folded-summary@example")
        assert.Equal(t, "Quarterly planning — continued on a folded line", folded.Summary)
        assert.Equal(t, "Big Room, floor 2", folded.Location, "\\, unescapes to a comma")
        assert.Equal(t, "Agenda: one;two;three\nBring notes", folded.Description, "\\; and \\n unescape")
        assert.Equal(t, time.Date(2026, 11, 3, 9, 0, 0, 0, time.Local), folded.Start, "floating time anchors in the server's zone")

        alarm := findEvent(t, evs, "valarm@example")
        assert.Equal(t, "", alarm.Description, "the VALARM's DESCRIPTION must not leak into the event")
        assert.Equal(t, "Event with alarm", alarm.Summary)

        dur := findEvent(t, evs, "duration@example")
        assert.True(t, dur.End.Equal(dur.Start.Add(90*time.Minute)), "DURATION resolves against DTSTART")

        rr := findEvent(t, evs, "rrule@example")
        assert.True(t, rr.Recurring, "RRULE is recognized")
        assert.True(t, rr.End.IsZero(), "Parse returns the master: expansion is Agenda's window-scoped job")
        assert.NotNil(t, rr.rule, "the WEEKLY;BYDAY=MO rule is inside recur.go's subset")
}

// TestScanDirectory_Positive walks a real directory: recursion into
// subfolders, non-.ics files ignored, events sorted by start.
func TestScanDirectory_Positive(t *testing.T) {
        dir := t.TempDir()
        require.NoError(t, os.MkdirAll(filepath.Join(dir, "work"), 0o755))
        require.NoError(t, os.WriteFile(filepath.Join(dir, "work", "b.ics"), []byte(crlf(goldenICS(t))), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("not a calendar"), 0o644))

        evs, st, err := Scan(dir)
        require.NoError(t, err)
        assert.Equal(t, 1, st.Files, "only the .ics file counts")
        assert.Len(t, evs, 10)
        for i := 1; i < len(evs); i++ {
                assert.False(t, evs[i].Start.Before(evs[i-1].Start), "sorted by start: %s before %s", evs[i-1].UID, evs[i].UID)
        }
        assert.Equal(t, filepath.Join(dir, "work", "b.ics"), evs[0].File, "the source file rides along")
}

// TestUpdateSurgery_Preservation_Positive is the write side's promise:
// patching one property leaves every line the subset does not understand
// byte-identical — RRULE, VALARM blocks, custom X- properties, and the
// sibling event in the same file.
func TestUpdateSurgery_Preservation_Positive(t *testing.T) {
        const file = "BEGIN:VCALENDAR\r\n" +
                "VERSION:2.0\r\n" +
                "BEGIN:VEVENT\r\n" +
                "UID:keep-me\r\n" +
                "DTSTAMP:20260101T000000Z\r\n" +
                "DTSTART:20261105T140000Z\r\n" +
                "DTEND:20261105T150000Z\r\n" +
                "SUMMARY:Old title\r\n" +
                "LOCATION:Old room\r\n" +
                "RRULE:FREQ=WEEKLY;BYDAY=TH\r\n" +
                "X-CUSTOM-PROP:with a value that must survive\r\n" +
                "BEGIN:VALARM\r\n" +
                "ACTION:DISPLAY\r\n" +
                "TRIGGER:-PT10M\r\n" +
                "DESCRIPTION:Alarm description, not the event's\r\n" +
                "END:VALARM\r\n" +
                "END:VEVENT\r\n" +
                "BEGIN:VEVENT\r\n" +
                "UID:sibling\r\n" +
                "DTSTART:20261201T100000Z\r\n" +
                "SUMMARY:Sibling stays\r\n" +
                "END:VEVENT\r\n" +
                "END:VCALENDAR\r\n"
        dir := writeICS(t, "surgery.ics", file)

        newTitle := "New title, with punctuation"
        start := time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC)
        updated, err := Update(dir, "keep-me", Patch{Summary: &newTitle, Start: &start})
        require.NoError(t, err)
        assert.Equal(t, newTitle, updated.Summary)
        assert.True(t, updated.Start.Equal(start))
        assert.True(t, updated.Recurring, "the RRULE survived the surgery")
        assert.Equal(t, "Old room", updated.Location)

        raw, err := os.ReadFile(filepath.Join(dir, "surgery.ics"))
        require.NoError(t, err)
        content := string(raw)
        assert.Contains(t, content, "RRULE:FREQ=WEEKLY;BYDAY=TH\r\n", "unknown property preserved verbatim")
        assert.Contains(t, content, "X-CUSTOM-PROP:with a value that must survive\r\n")
        assert.Contains(t, content, "DESCRIPTION:Alarm description, not the event's\r\n",
                "the VALARM's DESCRIPTION survives byte-identical — line surgery never re-escapes what it did not write")
        assert.Contains(t, content, "SUMMARY:Sibling stays\r\n")
        assert.NotContains(t, content, "Old title")

        // The DTSTAMP must have been bumped off 2026-01-01.
        assert.NotContains(t, content, "DTSTAMP:20260101T000000Z")
}

// ---------- negative ----------

// TestScanHostileInputs_Negative: a missing dir is an empty calendar,
// an oversized file is skipped-with-count, a garbage file contributes
// nothing but stats, and the walk never errors out.
func TestScanHostileInputs_Negative(t *testing.T) {
        evs, st, err := Scan(filepath.Join(t.TempDir(), "does-not-exist"))
        require.NoError(t, err, "a missing dir is empty, not an error")
        assert.Empty(t, evs)
        assert.False(t, st.Degraded(), "nothing was skipped — there was nothing to skip")

        dir := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(dir, "huge.ics"), make([]byte, maxFileBytes+1), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(dir, "garbage.ics"), []byte("total garbage\nno calendars here\n"), 0o644))
        evs, st, err = Scan(dir)
        require.NoError(t, err)
        assert.Empty(t, evs)
        assert.Equal(t, 2, st.Files)
        assert.Equal(t, 1, st.SkippedFiles, "the oversized file is skipped with a count")
        assert.True(t, st.Degraded())
}

// TestCreateRejectsBadEvents_Negative: the writer refuses events no
// calendar should hold — no summary, no start, end before start.
func TestCreateRejectsBadEvents_Negative(t *testing.T) {
        dir := t.TempDir()
        _, err := Create(dir, Event{Start: time.Now()})
        assert.ErrorContains(t, err, "summary")
        _, err = Create(dir, Event{Summary: "x"})
        assert.ErrorContains(t, err, "start")
        _, err = Create(dir, Event{Summary: "x", Start: time.Now(), End: time.Now().Add(-time.Hour)})
        assert.ErrorContains(t, err, "before start")
        entries, _ := os.ReadDir(dir)
        assert.Empty(t, entries, "a refused create writes nothing")
}

// TestUpdateHostileUIDs_Negative: empty UID, unknown UID, and a patch
// that would invert the event's timing all refuse without writing.
func TestUpdateHostileUIDs_Negative(t *testing.T) {
        dir := writeICS(t, "one.ics", "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:u1\r\nDTSTART:20261105T100000Z\r\nSUMMARY:keep\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")

        _, err := Update(dir, "", Patch{})
        assert.ErrorContains(t, err, "uid")

        _, err = Update(dir, "ghost", Patch{})
        assert.ErrorContains(t, err, "no event with UID")

        end := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
        _, err = Update(dir, "u1", Patch{End: &end})
        assert.ErrorContains(t, err, "end precede start")

        raw, err := os.ReadFile(filepath.Join(dir, "one.ics"))
        require.NoError(t, err)
        assert.Contains(t, string(raw), "SUMMARY:keep\r\n", "a refused update writes nothing")
}

// ---------- differential ----------

// TestParseGoldenInstants_Differential is the flagship: every timed
// event in the golden fixture is cross-checked against TWO independent
// oracles — (a) a time.Date construction in the named zone, and (b) a
// hand-computed UTC literal. (a) pins that the parser resolves zones
// through the timezone database like every other Go program; (b) pins
// the actual wall-clock truth, including the DST edge cases:
//
//   - 2026-07-04 12:00 America/New_York is EDT (UTC-4)      → 16:00Z
//   - 2026-03-08 02:30 does not exist (spring forward). The Go
//     zone database resolves the gap with the post-transition
//     offset (02:30 “EDT”), an instant that renders back as 01:30
//     EST                                            → 06:30Z
//   - 2026-11-01 02:30 is EST (UTC-5, after fall-back)      → 07:30Z
//
// A parser that applied a fixed offset, or did its own gap arithmetic,
// diverges from at least one oracle. (The 06:30Z gap literal was
// confirmed empirically against both time.Date and ParseInLocation —
// they agree, and the parser must agree with both.)
func TestParseGoldenInstants_Differential(t *testing.T) {
        evs, _ := Parse(goldenICS(t))
        ny, err := time.LoadLocation("America/New_York")
        require.NoError(t, err)

        for _, tc := range []struct {
                uid        string
                wantUTC    string // oracle (b): the hand-computed literal
                wantEndUTC string
                warning    string
        }{
                {uid: "utc-z@example", wantUTC: "2026-11-05T15:30:00Z", wantEndUTC: "2026-11-05T16:30:00Z"},
                {uid: "tzid-ny@example", wantUTC: "2026-07-04T16:00:00Z", wantEndUTC: "2026-07-04T17:00:00Z"},
                {uid: "dst-gap@example", wantUTC: "2026-03-08T06:30:00Z"},
                {uid: "dst-fall@example", wantUTC: "2026-11-01T07:30:00Z", wantEndUTC: "2026-11-01T08:30:00Z"},
                {uid: "unknown-tzid@example", wantUTC: "2026-11-05T10:00:00Z", warning: `unknown TZID "Mars/Olympus" interpreted as UTC`},
        } {
                ev := findEvent(t, evs, tc.uid)
                assert.Equal(t, tc.wantUTC, ev.Start.UTC().Format(time.RFC3339), "%s: start matches the hand-computed literal", tc.uid)
                if tc.wantEndUTC != "" {
                        assert.Equal(t, tc.wantEndUTC, ev.End.UTC().Format(time.RFC3339), "%s: end matches the literal", tc.uid)
                }
                if tc.warning != "" {
                        require.NotEmpty(t, ev.Warnings, "%s: the fallback is surfaced, not silent", tc.uid)
                        assert.Contains(t, strings.Join(ev.Warnings, "; "), tc.warning)
                }
        }

        // Oracle (a): zone-database constructions for the New York events.
        picnic := findEvent(t, evs, "tzid-ny@example")
        assert.True(t, picnic.Start.Equal(time.Date(2026, 7, 4, 12, 0, 0, 0, ny)), "TZID resolves through LoadLocation")
        gap := findEvent(t, evs, "dst-gap@example")
        assert.True(t, gap.Start.Equal(time.Date(2026, 3, 8, 2, 30, 0, 0, ny)), "the gap instant normalizes exactly as time.Date does")
        fall := findEvent(t, evs, "dst-fall@example")
        assert.True(t, fall.Start.Equal(time.Date(2026, 11, 1, 2, 30, 0, 0, ny)), "post-fall-back offsets come from the zone db")

        // All-day boundaries: the fixture's offsite spans Nov 10–12 (end
        // exclusive at local midnight).
        allday := findEvent(t, evs, "allday@example")
        assert.True(t, allday.AllDay)
        assert.Equal(t, time.Date(2026, 11, 10, 0, 0, 0, 0, time.Local), allday.Start)
        assert.Equal(t, time.Date(2026, 11, 12, 0, 0, 0, 0, time.Local), allday.End)
}

// TestAgendaWindows_Differential runs the Agenda filter against
// hand-enumerated membership over the golden fixture: for each window,
// the expected UID list was worked out on paper (overlap is
// [start < to) ∧ (end > from), all-day ends exclusive at local
// midnight; recurring events expand — every occurrence counts, same
// UID per occurrence). Windows touching all-day events anchor on LOCAL
// wall time so the enumeration holds in any server timezone; windows
// over UTC events anchor on UTC instants. Real zone offsets (≤ ±14h)
// can never move a Nov 3 or Nov 10 local instant across a Nov 5 UTC
// boundary, so the two anchor styles don't interact.
func TestAgendaWindows_Differential(t *testing.T) {
        dir := writeICS(t, "golden.ics", goldenICS(t))
        mustParse := func(s string) time.Time {
                ts, err := time.Parse(time.RFC3339, s)
                require.NoError(t, err)
                return ts
        }
        local := func(day, hour int) time.Time {
                return time.Date(2026, 11, day, hour, 0, 0, 0, time.Local)
        }

        for _, tc := range []struct {
                name    string
                from    time.Time
                to      time.Time
                wantUID []string
        }{
                {"utc window inside nov 5", mustParse("2026-11-05T00:00:00Z"), mustParse("2026-11-06T00:00:00Z"),
                        []string{"utc-z@example", "unknown-tzid@example"}},
                // The all-day offsite [Nov 10, Nov 12) local overlaps a local-noon
                // window on the 11th — it began before the window, has not ended.
                {"all-day mid-span", local(11, 12), local(11, 18),
                        []string{"allday@example"}},
                // End-exclusive boundary: the utc event exactly fills the window.
                {"boundary touch at utc-z start", mustParse("2026-11-05T15:30:00Z"), mustParse("2026-11-05T16:30:00Z"),
                        []string{"utc-z@example"}},
                // A window that ends exactly when utc-z starts excludes it.
                {"window ends at event start", mustParse("2026-11-05T00:00:00Z"), mustParse("2026-11-05T15:30:00Z"),
                        []string{"unknown-tzid@example"}},
                {"wide window", mustParse("2026-11-01T00:00:00Z"), mustParse("2026-11-30T00:00:00Z"),
                        []string{"folded-summary@example", "utc-z@example", "dst-fall@example", "unknown-tzid@example",
                                "allday@example", "duration@example", "rrule@example", "rrule@example",
                                "rrule@example", "rrule@example", "valarm@example"}},
                // December holds no one-off events, but the weekly Monday series
                // (no COUNT, no UNTIL) keeps going: Dec 7, 14, 21, 28 — the Mondays
                // of 2026-12, each 09:00 UTC, inside this window.
                {"december: only the recurring series", mustParse("2026-12-01T00:00:00Z"), mustParse("2026-12-31T00:00:00Z"),
                        []string{"rrule@example", "rrule@example", "rrule@example", "rrule@example"}},
        } {
                t.Run(tc.name, func(t *testing.T) {
                        got, _, err := Agenda(dir, tc.from, tc.to)
                        require.NoError(t, err)
                        uids := make([]string, 0, len(got))
                        for _, ev := range got {
                                uids = append(uids, ev.UID)
                        }
                        want := tc.wantUID
                        if want == nil {
                                want = []string{}
                        }
                        assert.ElementsMatch(t, want, uids, "hand-enumerated membership")
                })
        }
}

// TestCreateScanRoundTrip_Differential is the writer/reader agreement:
// events Create writes must Scan back equal — instants, flags, and the
// escaped text fields — across the grid of cases that make serializers
// lie (escapes, newlines, multi-byte runs, long folded descriptions,
// all-day forms).
func TestCreateScanRoundTrip_Differential(t *testing.T) {
        dir := t.TempDir()
        multiByte := strings.Repeat("会議の予定 — ", 40) // 440 octets: forces folding
        want := []Event{
                {
                        Summary:     "Team sync; reschedule, maybe",
                        Location:    "Room 5; floor 3, wing B",
                        Description: "Line one\nLine two with a \\ backslash",
                        Start:       time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC),
                        End:         time.Date(2026, 11, 12, 10, 30, 0, 0, time.UTC),
                },
                {
                        Summary:     "全 day offsite with a very long description " + multiByte,
                        Start:       time.Date(2026, 11, 20, 0, 0, 0, 0, time.Local),
                        End:         time.Date(2026, 11, 22, 0, 0, 0, 0, time.Local),
                        AllDay:      true,
                },
                {
                        Summary: "No end, no location",
                        Start:   time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC),
                },
        }

        stored := make([]Event, len(want))
        for i, ev := range want {
                got, err := Create(dir, ev)
                require.NoError(t, err, "create %q", ev.Summary)
                require.NotEmpty(t, got.UID)
                stored[i] = got
        }

        back, st, err := Scan(dir)
        require.NoError(t, err)
        assert.Equal(t, 3, st.Files, "one file per created event")
        assert.False(t, st.Degraded())

        for i, ev := range stored {
                got := findEvent(t, back, ev.UID)
                want := want[i]
                // The writer trims leading/trailing whitespace on text fields —
                // a deliberate serialization rule the round-trip must reflect.
                assert.Equal(t, strings.TrimSpace(want.Summary), got.Summary, "summary round-trips through escaping + folding")
                assert.Equal(t, strings.TrimSpace(want.Location), got.Location)
                assert.Equal(t, strings.TrimSpace(want.Description), got.Description)
                assert.True(t, got.Start.Equal(want.Start), "start instant: %s", got.UID)
                wantEnd := want.End
                if wantEnd.IsZero() {
                        wantEnd = want.Start // RFC 5545 §3.6.1: no DTEND ⇒ ends when it starts
                }
                assert.True(t, got.End.Equal(wantEnd), "end instant: %s", got.UID)
                assert.Equal(t, want.AllDay, got.AllDay, "all-day flag: %s", got.UID)
        }
}
