package calendar

// The RRULE trio. The differential is the hand-computed golden table:
// every expected occurrence list was worked out on paper from the 2026
// calendar (doomsday Saturday: 2026-11-02 is a Monday, the second
// Tuesdays and last Fridays below are the calendar's own), so the oracle
// is independent of the generation algorithm — a wrong week arithmetic
// cannot pass by agreeing with itself. The negatives pin the refusals:
// rules outside recur.go's subset degrade to the first occurrence with a
// warning, malformed pieces are errors, and the step cap keeps an
// anciently-anchored rule from spinning.

import (
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// icsEvent wraps one VEVENT into a minimal .ics document.
func icsEvent(uid string, lines ...string) string {
        doc := "BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nUID:" + uid + "\n"
        for _, ln := range lines {
                doc += ln + "\n"
        }
        return doc + "END:VEVENT\nEND:VCALENDAR\n"
}

// agendaStarts runs Agenda over one event's document and returns the
// occurrence starts (and the full events, for the assertions that need
// more than the instant).
func agendaStarts(t *testing.T, doc, name string, from, to time.Time) ([]time.Time, []Event) {
        t.Helper()
        dir := writeICS(t, name, doc)
        evs, _, err := Agenda(dir, from, to)
        require.NoError(t, err)
        starts := make([]time.Time, 0, len(evs))
        for _, ev := range evs {
                starts = append(starts, ev.Start)
        }
        return starts, evs
}

// utc builds a UTC instant the golden table spells out.
func utc(y int, m time.Month, d, hh, mm int) time.Time {
        return time.Date(y, m, d, hh, mm, 0, 0, time.UTC)
}

// localDay is an all-day anchor in the server's zone (VALUE=DATE parses
// there) with its span reaching next midnight.
func localDay(y int, m time.Month, d int) time.Time {
        return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// ---------- ParseRule units ----------

func TestParseRule_Positive(t *testing.T) {
        t.Run("defaults", func(t *testing.T) {
                r, err := ParseRule("FREQ=DAILY")
                require.NoError(t, err)
                assert.Equal(t, "DAILY", r.Freq)
                assert.Equal(t, 1, r.Interval, "INTERVAL defaults to 1")
                assert.Equal(t, 0, r.Count, "no COUNT by default")
                assert.True(t, r.Until.IsZero() && r.UntilDay == "", "no UNTIL by default")
        })
        t.Run("full subset, case-insensitive tokens, spaced parts", func(t *testing.T) {
                r, err := ParseRule("freq=Weekly; Interval=2; BYDAY=mo,fr; COUNT=10")
                require.NoError(t, err)
                assert.Equal(t, "WEEKLY", r.Freq)
                assert.Equal(t, 2, r.Interval)
                assert.Equal(t, 10, r.Count)
                require.Len(t, r.ByDay, 2)
                assert.Equal(t, ByDayRule{0, time.Monday}, r.ByDay[0])
                assert.Equal(t, ByDayRule{0, time.Friday}, r.ByDay[1])
        })
        t.Run("UNTIL spellings", func(t *testing.T) {
                r, err := ParseRule("FREQ=DAILY;UNTIL=20261104T090000Z")
                require.NoError(t, err)
                assert.True(t, r.Until.Equal(utc(2026, 11, 4, 9, 0)), "DATE-TIME UNTIL resolves to its instant")
                assert.Same(t, time.UTC, r.Until.Location(), "the bound is zone-fixed, never the machine's Local")
                assert.Equal(t, "", r.UntilDay)

                r, err = ParseRule("FREQ=DAILY;UNTIL=20261104")
                require.NoError(t, err)
                assert.Equal(t, "20261104", r.UntilDay, "DATE UNTIL keeps its raw day for per-zone resolution")
                assert.True(t, r.Until.IsZero())

                // A floating DATE-TIME UNTIL is read as UTC — the RFC requires the Z
                // for timezone-aware DTSTARTs, and producers that omit it mean it.
                // The Location pin matters more than the instant: a Local parse is
                // pointer-distinct from UTC even on a UTC machine, where the instants
                // coincide and the bug hides. That is exactly how it shipped.
                r, err = ParseRule("FREQ=DAILY;UNTIL=20261104T090000")
                require.NoError(t, err)
                assert.True(t, r.Until.Equal(utc(2026, 11, 4, 9, 0)), "floating DATE-TIME UNTIL is lenient UTC")
                assert.Same(t, time.UTC, r.Until.Location(), "lenient UTC is UTC itself, not whatever zone this machine sits in")
        })
        t.Run("monthly ordinals, both directions", func(t *testing.T) {
                r, err := ParseRule("FREQ=MONTHLY;BYDAY=2TU,-1FR,WE")
                require.NoError(t, err)
                require.Len(t, r.ByDay, 3)
                assert.Equal(t, ByDayRule{2, time.Tuesday}, r.ByDay[0])
                assert.Equal(t, ByDayRule{-1, time.Friday}, r.ByDay[1])
                assert.Equal(t, ByDayRule{0, time.Wednesday}, r.ByDay[2])
        })
        t.Run("WKST=MO is the accepted default spelling", func(t *testing.T) {
                _, err := ParseRule("FREQ=WEEKLY;WKST=MO")
                assert.NoError(t, err)
        })
}

func TestParseRuleRefusals_Negative(t *testing.T) {
        for _, tc := range []struct{ rule, why string }{
                {"", "no FREQ at all"},
                {"INTERVAL=2", "parts without a FREQ"},
                {"FREQ=YEARLY", "YEARLY is outside the subset"},
                {"FREQ=SECONDLY", "sub-daily frequencies are outside the subset"},
                {"FREQ=DAILY;COUNT=0", "COUNT must be positive"},
                {"FREQ=DAILY;COUNT=-3", "COUNT must be positive"},
                {"FREQ=DAILY;INTERVAL=0", "INTERVAL must be positive"},
                {"FREQ=DAILY;INTERVAL=abc", "INTERVAL must be an integer"},
                {"FREQ=DAILY;COUNT=3;UNTIL=20261104T090000Z", "COUNT and UNTIL together"},
                {"FREQ=DAILY;UNTIL=garbage", "UNTIL must be a date or datetime"},
                {"FREQ=WEEKLY;BYDAY=2TU", "ordinals are MONTHLY-only"},
                {"FREQ=DAILY;BYDAY=2MO", "ordinals are MONTHLY-only (DAILY filter spelling)"},
                {"FREQ=MONTHLY;BYDAY=XX", "two-letter day codes only"},
                {"FREQ=MONTHLY;BYDAY=0TU", "ordinal zero is the plain spelling, not a number"},
                {"FREQ=MONTHLY;BYDAY=+TU", "a bare sign is not an ordinal"},
                {"FREQ=WEEKLY;WKST=SU", "MO is the fixed week start"},
                {"FREQ=MONTHLY;BYSETPOS=1", "BYSETPOS is outside the subset"},
                {"FREQ=MONTHLY;BYMONTHDAY=15", "BYMONTHDAY is outside the subset"},
                {"FREQ=DAILY;BYMONTH=11", "BYMONTH is outside the subset"},
                {"FREQ=DAILY;BYDAY=", "empty BYDAY"},
                {"FREQ=DAILY;RDATE=20261104", "RDATE is outside the subset"},
        } {
                t.Run(tc.rule, func(t *testing.T) {
                        _, err := ParseRule(tc.rule)
                        require.Error(t, err, tc.why)
                })
        }
}

// ---------- the flagship: hand-computed golden expansions ----------

// TestRecurExpansionGolden_Differential walks one .ics per rule through
// Agenda and asserts the exact occurrence starts. Every expectation was
// computed by hand from the 2026 calendar (doomsday is Saturday, so
// 2026-11-02 is a Monday); the derivations sit beside each case.
func TestRecurExpansionGolden_Differential(t *testing.T) {
        for _, tc := range []struct {
                name   string
                doc    string
                from   time.Time
                to     time.Time
                expect []time.Time
        }{
                {
                        "DAILY COUNT=5",
                        icsEvent("d5", "DTSTART:20261102T090000Z", "RRULE:FREQ=DAILY;COUNT=5"),
                        utc(2026, 11, 1, 0, 0), utc(2026, 12, 1, 0, 0),
                        []time.Time{utc(2026, 11, 2, 9, 0), utc(2026, 11, 3, 9, 0), utc(2026, 11, 4, 9, 0), utc(2026, 11, 5, 9, 0), utc(2026, 11, 6, 9, 0)},
                },
                {
                        // COUNT bounds the series: Nov 6 is the 5th and last occurrence,
                        // so a December window sees nothing.
                        "DAILY COUNT=5 exhausted before the window",
                        icsEvent("d5b", "DTSTART:20261102T090000Z", "RRULE:FREQ=DAILY;COUNT=5"),
                        utc(2026, 12, 1, 0, 0), utc(2026, 12, 31, 0, 0),
                        nil,
                },
                {
                        // COUNT counts from DTSTART, not from the window: the 10th and
                        // final occurrence is Nov 11 (Nov 2 + 9 days), so a window opening
                        // Nov 8 sees exactly four of them.
                        "DAILY COUNT=10, window mid-series",
                        icsEvent("d10", "DTSTART:20261102T090000Z", "RRULE:FREQ=DAILY;COUNT=10"),
                        utc(2026, 11, 8, 0, 0), utc(2026, 11, 14, 0, 0),
                        []time.Time{utc(2026, 11, 8, 9, 0), utc(2026, 11, 9, 9, 0), utc(2026, 11, 10, 9, 0), utc(2026, 11, 11, 9, 0)},
                },
                {
                        // Nov 2, 4, 6, 8, 10 — the window's right edge is exclusive, so
                        // the Nov 10 occurrence is the last that starts before it.
                        "DAILY INTERVAL=2",
                        icsEvent("di2", "DTSTART:20261102T090000Z", "RRULE:FREQ=DAILY;INTERVAL=2"),
                        utc(2026, 11, 2, 0, 0), utc(2026, 11, 11, 0, 0),
                        []time.Time{utc(2026, 11, 2, 9, 0), utc(2026, 11, 4, 9, 0), utc(2026, 11, 6, 9, 0), utc(2026, 11, 8, 9, 0), utc(2026, 11, 10, 9, 0)},
                },
                {
                        // UNTIL is inclusive: the Nov 4 09:00Z occurrence equals the
                        // bound exactly and is kept.
                        "DAILY UNTIL on the last occurrence, inclusive",
                        icsEvent("du", "DTSTART:20261102T090000Z", "RRULE:FREQ=DAILY;UNTIL=20261104T090000Z"),
                        utc(2026, 11, 1, 0, 0), utc(2026, 12, 1, 0, 0),
                        []time.Time{utc(2026, 11, 2, 9, 0), utc(2026, 11, 3, 9, 0), utc(2026, 11, 4, 9, 0)},
                },
                {
                        // One second past the Nov 3 occurrence: Nov 3 is in, Nov 4 is cut.
                        "DAILY UNTIL one second past an occurrence",
                        icsEvent("du2", "DTSTART:20261102T090000Z", "RRULE:FREQ=DAILY;UNTIL=20261103T090001Z"),
                        utc(2026, 11, 1, 0, 0), utc(2026, 12, 1, 0, 0),
                        []time.Time{utc(2026, 11, 2, 9, 0), utc(2026, 11, 3, 9, 0)},
                },
                {
                        // All-day: DATE UNTIL bounds at that day's midnight — Nov 4 is
                        // in, Nov 5 is cut. Occurrences stay all-day.
                        "all-day DAILY with DATE UNTIL",
                        icsEvent("ad", "DTSTART;VALUE=DATE:20261102", "RRULE:FREQ=DAILY;UNTIL=20261104"),
                        localDay(2026, 11, 1), localDay(2026, 12, 1),
                        []time.Time{localDay(2026, 11, 2), localDay(2026, 11, 3), localDay(2026, 11, 4)},
                },
                {
                        // DTSTART is a Monday but BYDAY lists only TU,TH: the anchor is
                        // not itself an occurrence (the documented mainstream semantic).
                        // Tuesdays and Thursdays of Nov 2026 from the week of Nov 2:
                        // 3, 5, 10, 12, 17, 19 — the window's edge excludes the 20th+.
                        "WEEKLY BYDAY=TU,TH with a Monday anchor",
                        icsEvent("wtt", "DTSTART:20261102T090000Z", "RRULE:FREQ=WEEKLY;BYDAY=TU,TH"),
                        utc(2026, 11, 2, 0, 0), utc(2026, 11, 20, 0, 0),
                        []time.Time{utc(2026, 11, 3, 9, 0), utc(2026, 11, 5, 9, 0), utc(2026, 11, 10, 9, 0), utc(2026, 11, 12, 9, 0), utc(2026, 11, 17, 9, 0), utc(2026, 11, 19, 9, 0)},
                },
                {
                        // No BYDAY: DTSTART's own weekday is the set — Tuesdays.
                        "WEEKLY plain from a Tuesday",
                        icsEvent("wp", "DTSTART:20261103T090000Z", "RRULE:FREQ=WEEKLY"),
                        utc(2026, 11, 2, 0, 0), utc(2026, 11, 25, 0, 0),
                        []time.Time{utc(2026, 11, 3, 9, 0), utc(2026, 11, 10, 9, 0), utc(2026, 11, 17, 9, 0), utc(2026, 11, 24, 9, 0)},
                },
                {
                        // Fortnights: the weeks of Nov 2 and Nov 16 carry MO,WE,FR —
                        // 2, 4, 6 and 16, 18, 20; the week of Nov 9 is skipped.
                        "WEEKLY INTERVAL=2 BYDAY=MO,WE,FR",
                        icsEvent("wi2", "DTSTART:20261102T090000Z", "RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR"),
                        utc(2026, 11, 2, 0, 0), utc(2026, 11, 27, 0, 0),
                        []time.Time{utc(2026, 11, 2, 9, 0), utc(2026, 11, 4, 9, 0), utc(2026, 11, 6, 9, 0), utc(2026, 11, 16, 9, 0), utc(2026, 11, 18, 9, 0), utc(2026, 11, 20, 9, 0)},
                },
                {
                        // DAILY gated by BYDAY: only MO,WE,FR of the walk — 2, 4, 6, 9,
                        // 11, 13 (the filtered Tue/Thu days never count).
                        "DAILY with a BYDAY filter",
                        icsEvent("df", "DTSTART:20261102T090000Z", "RRULE:FREQ=DAILY;BYDAY=MO,WE,FR"),
                        utc(2026, 11, 2, 0, 0), utc(2026, 11, 14, 0, 0),
                        []time.Time{utc(2026, 11, 2, 9, 0), utc(2026, 11, 4, 9, 0), utc(2026, 11, 6, 9, 0), utc(2026, 11, 9, 9, 0), utc(2026, 11, 11, 9, 0), utc(2026, 11, 13, 9, 0)},
                },
                {
                        // BYDAY is unordered on the wire (FR before MO); emission sorts.
                        // Mondays and Fridays only — Wednesday is not in the set.
                        "DAILY BYDAY in reverse order",
                        icsEvent("dfr", "DTSTART:20261102T090000Z", "RRULE:FREQ=DAILY;BYDAY=FR,MO"),
                        utc(2026, 11, 2, 0, 0), utc(2026, 11, 8, 0, 0),
                        []time.Time{utc(2026, 11, 2, 9, 0), utc(2026, 11, 6, 9, 0)},
                },
                {
                        // The 31st skips the short months: only Jan, Mar, May, Jul, Aug,
                        // Oct, Dec of 2026 carry a 31st day.
                        "MONTHLY by day-of-month from the 31st",
                        icsEvent("m31", "DTSTART:20260131T120000Z", "RRULE:FREQ=MONTHLY"),
                        utc(2026, 1, 1, 0, 0), utc(2027, 1, 1, 0, 0),
                        []time.Time{utc(2026, 1, 31, 12, 0), utc(2026, 3, 31, 12, 0), utc(2026, 5, 31, 12, 0), utc(2026, 7, 31, 12, 0), utc(2026, 8, 31, 12, 0), utc(2026, 10, 31, 12, 0), utc(2026, 12, 31, 12, 0)},
                },
                {
                        // Every second month from January: Jan, Mar, May, Jul, Sep, Nov
                        // — and only Jan, Mar, May, Jul have a 31st.
                        "MONTHLY INTERVAL=2 from the 31st",
                        icsEvent("mi2", "DTSTART:20260131T120000Z", "RRULE:FREQ=MONTHLY;INTERVAL=2"),
                        utc(2026, 1, 1, 0, 0), utc(2027, 1, 1, 0, 0),
                        []time.Time{utc(2026, 1, 31, 12, 0), utc(2026, 3, 31, 12, 0), utc(2026, 5, 31, 12, 0), utc(2026, 7, 31, 12, 0)},
                },
                {
                        // The second Tuesdays of 2026, hand-listed from the calendar:
                        // Jan 13, Feb 10, Mar 10, Apr 14, May 12, Jun 9, Jul 14, Aug 11,
                        // Sep 8, Oct 13, Nov 10, Dec 8.
                        "MONTHLY BYDAY=2TU through 2026",
                        icsEvent("m2tu", "DTSTART:20260113T170000Z", "RRULE:FREQ=MONTHLY;BYDAY=2TU"),
                        utc(2026, 1, 1, 0, 0), utc(2027, 1, 1, 0, 0),
                        []time.Time{
                                utc(2026, 1, 13, 17, 0), utc(2026, 2, 10, 17, 0), utc(2026, 3, 10, 17, 0), utc(2026, 4, 14, 17, 0),
                                utc(2026, 5, 12, 17, 0), utc(2026, 6, 9, 17, 0), utc(2026, 7, 14, 17, 0), utc(2026, 8, 11, 17, 0),
                                utc(2026, 9, 8, 17, 0), utc(2026, 10, 13, 17, 0), utc(2026, 11, 10, 17, 0), utc(2026, 12, 8, 17, 0),
                        },
                },
                {
                        // The last Fridays of 2026: Jan 30, Feb 27, Mar 27, Apr 24,
                        // May 29, Jun 26, Jul 31, Aug 28, Sep 25, Oct 30, Nov 27, Dec 25.
                        "MONTHLY BYDAY=-1FR through 2026",
                        icsEvent("mlfr", "DTSTART:20260130T200000Z", "RRULE:FREQ=MONTHLY;BYDAY=-1FR"),
                        utc(2026, 1, 1, 0, 0), utc(2027, 1, 1, 0, 0),
                        []time.Time{
                                utc(2026, 1, 30, 20, 0), utc(2026, 2, 27, 20, 0), utc(2026, 3, 27, 20, 0), utc(2026, 4, 24, 20, 0),
                                utc(2026, 5, 29, 20, 0), utc(2026, 6, 26, 20, 0), utc(2026, 7, 31, 20, 0), utc(2026, 8, 28, 20, 0),
                                utc(2026, 9, 25, 20, 0), utc(2026, 10, 30, 20, 0), utc(2026, 11, 27, 20, 0), utc(2026, 12, 25, 20, 0),
                        },
                },
                {
                        // Plain FR under MONTHLY is every Friday of the month — all four
                        // of November 2026: 6, 13, 20, 27.
                        "MONTHLY BYDAY=FR is every Friday of the month",
                        icsEvent("mfr", "DTSTART:20261106T090000Z", "RRULE:FREQ=MONTHLY;BYDAY=FR"),
                        utc(2026, 11, 1, 0, 0), utc(2026, 12, 1, 0, 0),
                        []time.Time{utc(2026, 11, 6, 9, 0), utc(2026, 11, 13, 9, 0), utc(2026, 11, 20, 9, 0), utc(2026, 11, 27, 9, 0)},
                },
                {
                        // A series that outlives its year: Mondays of March 2027 are
                        // the 1st, 8th, and 15th — the window's edge keeps the first two.
                        "WEEKLY continuing into 2027",
                        icsEvent("w27", "DTSTART:20261102T090000Z", "RRULE:FREQ=WEEKLY"),
                        utc(2027, 3, 1, 0, 0), utc(2027, 3, 15, 0, 0),
                        []time.Time{utc(2027, 3, 1, 9, 0), utc(2027, 3, 8, 9, 0)},
                },
                {
                        // The window starts mid-series (the series began Nov 2); the
                        // UNTIL is long past, so even the window's Mondays are gone.
                        "series over before the window",
                        icsEvent("over", "DTSTART:20261102T090000Z", "RRULE:FREQ=DAILY;UNTIL=20261105T090000Z"),
                        utc(2026, 12, 1, 0, 0), utc(2026, 12, 31, 0, 0),
                        nil,
                },
                {
                        // The window ends before DTSTART: nothing yet.
                        "window before the series",
                        icsEvent("early", "DTSTART:20261207T090000Z", "RRULE:FREQ=WEEKLY"),
                        utc(2026, 11, 1, 0, 0), utc(2026, 11, 30, 0, 0),
                        nil,
                },
        } {
                t.Run(tc.name, func(t *testing.T) {
                        starts, evs := agendaStarts(t, tc.doc, tc.name+".ics", tc.from, tc.to)
                        if tc.expect == nil {
                                tc.expect = []time.Time{}
                        }
                        require.Len(t, starts, len(tc.expect), "occurrence count")
                        for i := range tc.expect {
                                assert.True(t, starts[i].Equal(tc.expect[i]),
                                        "occurrence %d: got %s want %s", i, starts[i], tc.expect[i])
                        }
                        for _, ev := range evs {
                                assert.True(t, ev.Recurring, "every occurrence carries the recurring marker")
                        }
                })
        }
}

// TestRecurGoldenEventShape_Positive pins what an occurrence IS beyond
// its instant: duration carried from the master, all-day span, and the
// overlap semantics for a multi-day occurrence that began before the
// window.
func TestRecurGoldenEventShape_Positive(t *testing.T) {
        t.Run("two-day weekly occurrences", func(t *testing.T) {
                doc := icsEvent("td",
                        "DTSTART:20261102T090000Z", "DTEND:20261104T090000Z",
                        "RRULE:FREQ=WEEKLY;COUNT=2", "SUMMARY:Two-day sprint")
                // The window opens mid-occurrence: the first occurrence [Nov 2,
                // Nov 4) has not ended when the window starts Nov 3.
                starts, evs := agendaStarts(t, doc, "td.ics", utc(2026, 11, 3, 0, 0), utc(2026, 11, 10, 0, 0))
                require.Len(t, evs, 2, "both occurrences overlap the window")
                assert.True(t, starts[0].Equal(utc(2026, 11, 2, 9, 0)), "first occurrence began before the window")
                assert.True(t, starts[1].Equal(utc(2026, 11, 9, 9, 0)))
                assert.True(t, evs[0].End.Equal(utc(2026, 11, 4, 9, 0)), "duration carries from the master")
                assert.True(t, evs[1].End.Equal(utc(2026, 11, 11, 9, 0)))
                assert.Equal(t, "Two-day sprint", evs[0].Summary, "identity carries from the master")
        })

        t.Run("all-day weekly occurrences span their whole day", func(t *testing.T) {
                doc := icsEvent("adw", "DTSTART;VALUE=DATE:20261103", "RRULE:FREQ=WEEKLY", "SUMMARY:Standup-ish")
                // A window that is only Nov 10 12:00–18:00 still catches the Nov 10
                // occurrence because an all-day event spans [day, next midnight).
                starts, evs := agendaStarts(t, doc, "adw.ics",
                        time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local),
                        time.Date(2026, 11, 10, 18, 0, 0, 0, time.Local))
                require.Len(t, evs, 1)
                assert.True(t, starts[0].Equal(localDay(2026, 11, 10)))
                assert.True(t, evs[0].AllDay, "occurrences stay all-day")
                assert.True(t, evs[0].End.IsZero(), "the master had no DTEND: occurrences keep End zero (the span is window logic)")
        })

        t.Run("wall clock is preserved across the DST boundary", func(t *testing.T) {
                ny, err := time.LoadLocation("America/New_York")
                require.NoError(t, err)
                doc := icsEvent("dst",
                        "DTSTART;TZID=America/New_York:20261026T090000",
                        "RRULE:FREQ=WEEKLY")
                // Oct 26 2026 is EDT (UTC-4): 09:00 NY = 13:00 UTC. The Nov 1
                // fall-back makes Nov 2 EST (UTC-5): 09:00 NY = 14:00 UTC. The
                // expansion must keep 09:00 wall clock, not 13:00 UTC forever.
                starts, _ := agendaStarts(t, doc, "dst.ics", utc(2026, 10, 25, 0, 0), utc(2026, 11, 10, 0, 0))
                require.Len(t, starts, 3, "Oct 26, Nov 2, Nov 9")
                for i, wantDay := range []int{26, 2, 9} {
                        inNY := starts[i].In(ny)
                        assert.Equal(t, 9, inNY.Hour(), "occurrence %d keeps 09:00 New York wall clock", i)
                        assert.Equal(t, wantDay, inNY.Day())
                }
                assert.Equal(t, 13, starts[0].UTC().Hour(), "Oct 26 is EDT: 09:00 NY = 13:00 UTC")
                assert.Equal(t, 14, starts[1].UTC().Hour(), "Nov 2 is EST: 09:00 NY = 14:00 UTC")
        })
}

// TestRecurAgendaDegradation_Negative pins the subset boundary from the
// Agenda side: an unsupported rule is not an error, it is the honest
// first occurrence plus a warning that says why.
func TestRecurAgendaDegradation_Negative(t *testing.T) {
        for _, tc := range []struct{ name, rule string }{
                {"YEARLY degrades", "FREQ=YEARLY"},
                {"BYSETPOS degrades", "FREQ=MONTHLY;BYDAY=TU;BYSETPOS=2"},
                {"COUNT=0 degrades", "FREQ=DAILY;COUNT=0"},
                {"COUNT+UNTIL degrades", "FREQ=DAILY;COUNT=9;UNTIL=20261104T090000Z"},
        } {
                t.Run(tc.name, func(t *testing.T) {
                        doc := icsEvent("deg", "DTSTART:20261102T090000Z", "RRULE:"+tc.rule)
                        starts, evs := agendaStarts(t, doc, "deg.ics", utc(2026, 11, 1, 0, 0), utc(2026, 12, 1, 0, 0))
                        require.Len(t, evs, 1, "the master surfaces, exactly once")
                        assert.True(t, starts[0].Equal(utc(2026, 11, 2, 9, 0)), "at its first occurrence")
                        assert.True(t, evs[0].Recurring, "the marker stays honest")
                        assert.Nil(t, evs[0].rule)
                        found := false
                        for _, w := range evs[0].Warnings {
                                if len(w) > 6 && w[:6] == "RRULE " {
                                        found = true
                                }
                        }
                        assert.True(t, found, "a warning explains the non-expansion: %v", evs[0].Warnings)
                })
        }

        t.Run("the step cap keeps an ancient anchor from spinning", func(t *testing.T) {
                // A daily series anchored in the year 1300 needs ~265k candidate
                // steps to reach 2026 — past the 200k cap. The walk stops and the
                // window sees nothing; absurd input, honest empty, bounded time.
                doc := icsEvent("ancient", "DTSTART:13000101T000000Z", "RRULE:FREQ=DAILY")
                starts, _ := agendaStarts(t, doc, "ancient.ics", utc(2026, 11, 2, 0, 0), utc(2026, 11, 3, 0, 0))
                assert.Empty(t, starts)
        })

        t.Run("UNTIL before DTSTART is an empty series", func(t *testing.T) {
                doc := icsEvent("never", "DTSTART:20261102T090000Z", "RRULE:FREQ=DAILY;UNTIL=20261101T090000Z")
                starts, _ := agendaStarts(t, doc, "never.ics", utc(2026, 11, 1, 0, 0), utc(2026, 12, 1, 0, 0))
                assert.Empty(t, starts, "even the anchor is cut: the series was over before it began")
        })
}

// TestAllDayWindowStartOverlap_Positive pins the overlap fix the sweep
// found: an all-day event on the window's first day is IN the agenda.
// Before chunk 6 the zero-End all-day event compared end == start ==
// from, failed end.After(from), and vanished from its own day.
func TestAllDayWindowStartOverlap_Positive(t *testing.T) {
        doc := icsEvent("today-allday", "DTSTART;VALUE=DATE:20261110", "SUMMARY:Today, all of it")
        starts, evs := agendaStarts(t, doc, "today.ics", localDay(2026, 11, 10), localDay(2026, 11, 11))
        require.Len(t, evs, 1, "an all-day event on the window's first day is included")
        assert.True(t, starts[0].Equal(localDay(2026, 11, 10)))

        // The day after is not: the span is exactly one day.
        starts, _ = agendaStarts(t, doc, "today.ics", localDay(2026, 11, 11), localDay(2026, 11, 12))
        assert.Empty(t, starts, "the next day's window excludes yesterday's all-day event")
}

// TestRecurMemoInteraction_Positive pins that the scan memo and the
// expansion cooperate: the same file scanned twice (mtime+size unchanged)
// yields the same expanded agenda both times.
func TestRecurMemoInteraction_Positive(t *testing.T) {
        doc := icsEvent("memo", "DTSTART:20261102T090000Z", "RRULE:FREQ=WEEKLY;BYDAY=MO")
        dir := writeICS(t, "memo.ics", doc)
        from, to := utc(2026, 11, 2, 0, 0), utc(2026, 11, 30, 0, 0)
        first, _, err := Agenda(dir, from, to)
        require.NoError(t, err)
        second, _, err := Agenda(dir, from, to)
        require.NoError(t, err)
        require.Len(t, first, 4, "the Mondays of Nov 2, 9, 16, 23")
        require.Len(t, second, 4, "the memoized parse expands identically")
        for i := range first {
                assert.True(t, first[i].Start.Equal(second[i].Start), "occurrence %d stable across scans", i)
        }
}
