// Recurrence — the RRULE subset this package expands, and nothing more.
//
// Scope (deliberately the common personal-calendar shapes): FREQ=DAILY /
// WEEKLY / MONTHLY, INTERVAL, COUNT, UNTIL (both spellings), and BYDAY —
// plain weekdays for DAILY (a limit filter) and WEEKLY (the day set), and
// weekday-plus-ordinal for MONTHLY (2TU the second Tuesday, -1FR the last
// Friday, plain FR every Friday of the month). WKST is accepted only as
// the default MO. Everything else — YEARLY, BYSETPOS, BYMONTH, RDATE,
// RECURRENCE-ID overrides — is NOT supported: such a rule parses to an
// error, the event keeps surfacing at its first occurrence with a warning
// in Warnings, and the doc you are reading is the claim. Full RFC 5545
// compliance is explicitly not claimed.
//
// Semantics chosen where the RFC leaves room:
//   - Occurrences are generated from the rule, restricted to instants at
//     or after DTSTART. A DTSTART that does not itself match the pattern
//     (Tuesday start with BYDAY=MO,WE — malformed per the RFC's SHOULD)
//     is not force-included; the first occurrence is the first generated
//     match. This matches the mainstream producers.
//   - COUNT counts occurrences of the whole series from the first match,
//     not the window's slice of it — a window that starts mid-series
//     still consumes the earlier occurrences against the budget.
//   - UNTIL is inclusive in both spellings. A DATE-form UNTIL bounds a
//     DATE (all-day) DTSTART at that day's midnight in the event's zone;
//     for a timed DTSTART it bounds at the end of that day (a DATE UNTIL
//     with a DATE-TIME DTSTART is invalid input; the tolerant reading is
//     the whole last day, the way mainstream producers behave).
//   - Expansion happens in DTSTART's own zone: the wall clock is
//     preserved across DST boundaries the way a calendar app shows it
//     (09:00 stays 09:00 local; the UTC instant shifts).
//   - WKST is fixed at MO; INTERVAL>1 weeks with BYDAY that straddle a
//     non-default week start are unsupported rather than wrong.
package calendar

import (
        "errors"
        "fmt"
        "sort"
        "strconv"
        "strings"
        "time"
)

// Rule is one parsed RRULE within the supported subset. The zero value is
// not a usable rule; ParseRule is the constructor.
type Rule struct {
        Freq     string // "DAILY" | "WEEKLY" | "MONTHLY"
        Interval int    // >= 1
        Count    int    // 0 = no COUNT (unbounded or UNTIL-bounded)
        // Until is the resolved bound for DATE-TIME spellings (zero when
        // absent). A DATE spelling lands in UntilDay and resolves against
        // the event's zone at expansion time.
        Until    time.Time
        UntilDay string // raw YYYYMMDD when UNTIL was DATE-valued, else ""
        ByDay    []ByDayRule
}

// ByDayRule is one BYDAY entry: a weekday, plus an ordinal that is only
// meaningful (and only accepted) under MONTHLY. Ordinal 0 means "every
// such weekday" — under MONTHLY plain FR is every Friday of the month;
// under DAILY/WEEKLY the ordinal must be 0.
type ByDayRule struct {
        Ordinal int
        Weekday time.Weekday
}

// maxRuleSteps caps candidate generation for one expansion: a hostile or
// anciently-anchored rule must not spin. 200k daily steps is ~548 years —
// beyond any real personal calendar, and the surfaces' 400-day window cap
// makes the limit unreachable in practice; it is a guard, not a feature.
const maxRuleSteps = 200_000

// ParseRule decodes an RRULE value (the part after "RRULE:") into a Rule.
// Every piece must be within the supported subset; anything else is an
// error, because a recurrence shown wrong is worse than one shown at its
// first occurrence with a warning.
func ParseRule(s string) (*Rule, error) {
        r := &Rule{Interval: 1}
        sawFreq, sawCount, sawUntil := false, false, false
        for _, part := range strings.Split(s, ";") {
                part = strings.TrimSpace(part)
                if part == "" {
                        continue // stray ';' between parts: tolerated
                }
                eq := strings.IndexByte(part, '=')
                if eq < 0 {
                        return nil, fmt.Errorf("rule part %q has no '='", part)
                }
                key := strings.ToUpper(strings.TrimSpace(part[:eq]))
                val := part[eq+1:]
                switch key {
                case "FREQ":
                        if sawFreq {
                                return nil, errors.New("FREQ appears twice")
                        }
                        f := strings.ToUpper(strings.TrimSpace(val))
                        switch f {
                        case "DAILY", "WEEKLY", "MONTHLY":
                                r.Freq = f
                        default:
                                return nil, fmt.Errorf("unsupported FREQ %q", f)
                        }
                        sawFreq = true
                case "INTERVAL":
                        n, err := strconv.Atoi(strings.TrimSpace(val))
                        if err != nil || n < 1 {
                                return nil, fmt.Errorf("invalid INTERVAL %q", val)
                        }
                        r.Interval = n
                case "COUNT":
                        if sawCount {
                                return nil, errors.New("COUNT appears twice")
                        }
                        n, err := strconv.Atoi(strings.TrimSpace(val))
                        if err != nil || n < 1 {
                                return nil, fmt.Errorf("invalid COUNT %q", val)
                        }
                        r.Count = n
                        sawCount = true
                case "UNTIL":
                        if sawUntil {
                                return nil, errors.New("UNTIL appears twice")
                        }
                        v := strings.TrimSpace(val)
                        if len(v) == 8 && !strings.ContainsAny(v, "Tt") {
                                if _, err := time.Parse("20060102", v); err != nil {
                                        return nil, fmt.Errorf("invalid UNTIL %q", val)
                                }
                                r.UntilDay = v
                        } else {
                                t, ok := parseUntilDateTime(v)
                                if !ok {
                                        return nil, fmt.Errorf("invalid UNTIL %q", val)
                                }
                                r.Until = t
                        }
                        sawUntil = true
                case "BYDAY":
                        entries, err := parseByDay(val, r.Freq)
                        if err != nil {
                                return nil, err
                        }
                        r.ByDay = entries
                case "WKST":
                        if !strings.EqualFold(strings.TrimSpace(val), "MO") {
                                return nil, fmt.Errorf("unsupported WKST %q (MO is the fixed week start)", val)
                        }
                default:
                        return nil, fmt.Errorf("unsupported rule part %q", key)
                }
        }
        if !sawFreq {
                return nil, errors.New("no FREQ")
        }
        if sawCount && sawUntil {
                return nil, errors.New("COUNT and UNTIL together (the RFC forbids the pair)")
        }
        return r, nil
}

// parseUntilDateTime decodes a DATE-TIME UNTIL. Both spellings resolve in
// UTC: Z-suffixed is UTC by the RFC, and a floating value is read as UTC
// (the RFC requires the Z for timezone-aware DTSTARTs; producers that omit
// it mean it anyway). Never time.Local — a series bound that moved with
// the machine's zone would end the recurrence an hour early or late
// depending on where it runs.
func parseUntilDateTime(v string) (time.Time, bool) {
        if strings.HasSuffix(v, "Z") || strings.HasSuffix(v, "z") {
                v = v[:len(v)-1]
        }
        if t, ok := parseDateTimeBody(v, time.UTC); ok {
                return t, true
        }
        return time.Time{}, false
}

// parseByDay decodes a comma list of [ordinal]weekday entries. Ordinals
// are only legal under MONTHLY; under DAILY and WEEKLY they are malformed
// (the RFC assigns them no meaning there) and rejected rather than
// half-honored.
func parseByDay(val, freq string) ([]ByDayRule, error) {
        var out []ByDayRule
        for _, tok := range strings.Split(val, ",") {
                tok = strings.TrimSpace(tok)
                if tok == "" {
                        return nil, errors.New("empty BYDAY entry")
                }
                // Split the optional leading integer from the two-letter day code.
                i := 0
                for i < len(tok) && (tok[i] == '-' || (tok[i] >= '0' && tok[i] <= '9')) {
                        i++
                }
                num, code := tok[:i], tok[i:]
                var wd time.Weekday
                switch strings.ToUpper(code) {
                case "MO":
                        wd = time.Monday
                case "TU":
                        wd = time.Tuesday
                case "WE":
                        wd = time.Wednesday
                case "TH":
                        wd = time.Thursday
                case "FR":
                        wd = time.Friday
                case "SA":
                        wd = time.Saturday
                case "SU":
                        wd = time.Sunday
                default:
                        return nil, fmt.Errorf("invalid BYDAY entry %q", tok)
                }
                e := ByDayRule{Weekday: wd}
                if num != "" && num != "+" && num != "-" {
                        n, err := strconv.Atoi(num)
                        if err != nil || n == 0 {
                                return nil, fmt.Errorf("invalid BYDAY ordinal in %q", tok)
                        }
                        if freq != "MONTHLY" {
                                return nil, fmt.Errorf("BYDAY ordinal in %q is only valid with FREQ=MONTHLY", tok)
                        }
                        e.Ordinal = n
                } else if num == "+" || num == "-" {
                        return nil, fmt.Errorf("invalid BYDAY ordinal in %q", tok)
                }
                out = append(out, e)
        }
        if len(out) == 0 {
                return nil, errors.New("empty BYDAY")
        }
        return out, nil
}

// untilBound resolves the effective inclusive bound on occurrence starts.
// The second return is false when the rule has no UNTIL.
func (r *Rule) untilBound(start time.Time, allDay bool) (time.Time, bool) {
        if !r.Until.IsZero() {
                return r.Until, true // DATE-TIME: instant, inclusive
        }
        if r.UntilDay != "" {
                d, err := time.ParseInLocation("20060102", r.UntilDay, start.Location())
                if err != nil {
                        return time.Time{}, false // unreachable: validated at parse
                }
                if allDay {
                        return d, true // the day's midnight, inclusive
                }
                // A DATE UNTIL under a timed DTSTART: the whole last day.
                return d.AddDate(0, 0, 1).Add(-time.Nanosecond), true
        }
        return time.Time{}, false
}

// expandOccurrenceStarts generates the occurrence starts of the series
// anchored at ev.Start that could overlap [lo, hi), in chronological
// order. Generation always walks from DTSTART — COUNT and UNTIL bound the
// whole series, not the window — so a series whose budget ran out before
// the window correctly yields nothing.
func (r *Rule) expandOccurrenceStarts(ev Event, lo, hi time.Time) []time.Time {
        start := ev.Start
        until, hasUntil := r.untilBound(start, ev.AllDay)

        var out []time.Time
        steps, generated := 0, 0
        count := r.Count

        // emit folds one candidate into the output. isOcc is false for days a
        // BYDAY filter removes under DAILY: such a day is neither an occurrence
        // nor budget-consuming, but it still counts toward the step cap and the
        // window/until cut-offs, which are what terminate the walk.
        emit := func(c time.Time, isOcc bool) bool {
                steps++
                if steps > maxRuleSteps {
                        return false
                }
                if !c.Before(hi) {
                        return false // candidates are chronological: the walk is done
                }
                if hasUntil && c.After(until) {
                        return false // the series is over: the walk is done
                }
                if !isOcc || c.Before(start) {
                        return true // before DTSTART (first partial week): not an occurrence
                }
                generated++
                if count > 0 && generated > count {
                        return false // budget exhausted
                }
                if overlapsWindow(ev, c, lo, hi) {
                        out = append(out, c)
                }
                return true
        }

        switch r.Freq {
        case "DAILY":
                var filter map[time.Weekday]bool
                if len(r.ByDay) > 0 {
                        filter = make(map[time.Weekday]bool, len(r.ByDay))
                        for _, e := range r.ByDay {
                                filter[e.Weekday] = true
                        }
                }
                for n := 0; ; n++ {
                        c := start.AddDate(0, 0, n*r.Interval)
                        if !emit(c, filter == nil || filter[c.Weekday()]) {
                                return out
                        }
                }

        case "WEEKLY":
                days := r.weeklyDays(start)
                base := mondayOf(start)
                for n := 0; ; n++ {
                        week := base.AddDate(0, 0, 7*n*r.Interval)
                        for _, wd := range days {
                                c := atDate(start, week.AddDate(0, 0, weekdayOffset(wd)))
                                if !emit(c, true) {
                                        return out
                                }
                        }
                }

        default: // MONTHLY
                sy, sm, _ := start.Date()
                for n := 0; ; n++ {
                        y, m := monthAdd(sy, sm, n*r.Interval)
                        for _, d := range r.monthlyDays(y, m, start.Day()) {
                                c := atDate(start, time.Date(y, m, d, 0, 0, 0, 0, start.Location()))
                                if !emit(c, true) {
                                        return out
                                }
                        }
                }
        }
}

// weeklyDays is the weekday set for WEEKLY: BYDAY's weekdays sorted
// Monday-first, or DTSTART's own weekday when BYDAY is absent.
func (r *Rule) weeklyDays(start time.Time) []time.Weekday {
        if len(r.ByDay) == 0 {
                return []time.Weekday{start.Weekday()}
        }
        out := make([]time.Weekday, len(r.ByDay))
        for i, e := range r.ByDay {
                out[i] = e.Weekday
        }
        sort.Slice(out, func(i, j int) bool { return weekdayOffset(out[i]) < weekdayOffset(out[j]) })
        return out
}

// monthlyDays is the day-of-month set for one MONTHLY iteration: BYDAY
// entries (ordinals and all) resolved against the month, or DTSTART's
// day-of-month when BYDAY is absent (months lacking that day contribute
// nothing — the 31st skips February). Sorted ascending.
func (r *Rule) monthlyDays(y int, m time.Month, anchorDay int) []int {
        if len(r.ByDay) == 0 {
                if anchorDay <= daysInMonth(y, m) {
                        return []int{anchorDay}
                }
                return nil
        }
        var out []int
        for _, e := range r.ByDay {
                switch {
                case e.Ordinal == 0:
                        for d := 1; d <= daysInMonth(y, m); d++ {
                                if time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Weekday() == e.Weekday {
                                        out = append(out, d)
                                }
                        }
                case e.Ordinal > 0:
                        // The nth <weekday> from the top: first match, plus (n-1) weeks.
                        first := 1 + ((int(e.Weekday)-int(firstWeekdayOf(y, m))+7)%7)
                        if d := first + (e.Ordinal-1)*7; d <= daysInMonth(y, m) {
                                out = append(out, d)
                        }
                default:
                        // The nth <weekday> from the end: last match, minus (n-1) weeks.
                        dim := daysInMonth(y, m)
                        last := dim - ((int(lastWeekdayOf(y, m))-int(e.Weekday)+7)%7)
                        if d := last + (e.Ordinal+1)*7; d >= 1 {
                                out = append(out, d)
                        }
                }
        }
        sort.Ints(out)
        return out
}

// ---------- small date helpers ----------

// weekdayOffset is wd's Monday-first index (MO=0 .. SU=6).
func weekdayOffset(wd time.Weekday) int { return (int(wd) + 6) % 7 }

// mondayOf is the midnight starting t's Monday-to-Sunday week, in t's zone.
func mondayOf(t time.Time) time.Time {
        y, m, d := t.Date()
        mid := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
        return mid.AddDate(0, 0, -weekdayOffset(mid.Weekday()))
}

// atDate rebuilds t's clock (hh:mm:ss) on day's calendar date, in t's own
// zone — the wall-clock-preserving step that keeps 09:00 at 09:00 across
// DST boundaries.
func atDate(t, day time.Time) time.Time {
        y, m, d := day.Date()
        hh, mm, ss := t.Clock()
        return time.Date(y, m, d, hh, mm, ss, 0, t.Location())
}

// monthAdd steps k months from (y, m), normalizing year rollover.
func monthAdd(y int, m time.Month, k int) (int, time.Month) {
        t := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).AddDate(0, k, 0)
        return t.Year(), t.Month()
}

// daysInMonth is the number of days in (y, m).
func daysInMonth(y int, m time.Month) int {
        return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func firstWeekdayOf(y int, m time.Month) time.Weekday {
        return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).Weekday()
}

func lastWeekdayOf(y int, m time.Month) time.Weekday {
        dim := daysInMonth(y, m)
        return time.Date(y, m, dim, 0, 0, 0, 0, time.UTC).Weekday()
}

// overlapsWindow is Agenda's window test: does an occurrence of ev
// starting at start overlap the half-open [from, to)? An event with a
// resolved end (DTEND or DURATION) spans [start, start+duration); an
// all-day event with neither spans [its day, next midnight) — calendar
// day arithmetic, not 24 hours, so DST cannot shave an all-day event
// short; a timed instant event is its own point.
func overlapsWindow(ev Event, start, from, to time.Time) bool {
        if !start.Before(to) {
                return false
        }
        var end time.Time
        switch {
        case !ev.End.IsZero():
                end = start.Add(ev.End.Sub(ev.Start))
        case ev.AllDay:
                end = start.AddDate(0, 0, 1)
        default:
                end = start
        }
        return end.After(from)
}
