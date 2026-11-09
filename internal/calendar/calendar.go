// Package calendar reads and writes a local directory of RFC 5545 (.ics)
// files — the personal-data surface behind the dashboard's agenda widget
// (dashboard chunk 2) and the agent's personal_calendar tool. It is
// deliberately a SUBSET parser: the properties a personal agenda needs
// (VEVENT identity + timing + the big three text fields), with everything
// else tolerated. Recurrence is expanded over the subset recur.go rules
// (FREQ=DAILY/WEEKLY/MONTHLY, INTERVAL, COUNT, UNTIL, BYDAY); a rule
// outside the subset surfaces at its first occurrence with a warning —
// full RFC 5545 compliance is not claimed, and recur.go's doc is the
// honest scope statement.
//
// The doctrine throughout: malformed content is skipped-with-count, never
// fatal — a calendar dir is user data, and one bad file must not take the
// agenda down.
package calendar

import (
        "errors"
        "fmt"
        "io/fs"
        "os"
        "path/filepath"
        "sort"
        "strconv"
        "strings"
        "sync"
        "time"

        // The embedded tzdata database makes TZID= lookups work on scratch
        // containers without /usr/share/zoneinfo (Go only consults it when
        // the system copy is missing, so hosts with zoneinfo are unaffected).
        _ "time/tzdata"
)

// Event is one VEVENT parsed out of an .ics file. Times are absolute
// instants: DATE-TIME values with a Z suffix are UTC, TZID-qualified
// values resolve through time.LoadLocation (unknown zones fall back to
// UTC with a warning), floating values resolve in the server's local
// zone, and DATE values anchor at local midnight.
type Event struct {
        UID         string
        Summary     string
        Location    string
        Description string
        Start       time.Time
        // End is the exclusive end instant. Zero when the source VEVENT had
        // neither DTEND nor DURATION — treat as an instant at Start.
        End time.Time
        // AllDay marks a VALUE=DATE event (no time of day).
        AllDay bool
        // Recurring marks an event whose source carried an RRULE. Every
        // occurrence carries it, expanded or not: an unsupported rule surfaces
        // at its first occurrence only (recur.go's subset is the boundary).
        Recurring bool
        // rule is the parsed RRULE when it is within recur.go's supported
        // subset (nil otherwise — including when there is no RRULE at all).
        // Agenda consults it to expand occurrences; it never serializes.
        rule *Rule
        // Warnings are non-fatal parse notes (unknown TZID, malformed DTEND
        // ignored, ...). Surfaces may show them; they never drop the event.
        Warnings []string
        // File is the source path Scan found the event in.
        File string
}

// Stats reports what a Scan saw: the honest accounting for surfaces that
// want to say "3 files, 1 malformed line skipped" instead of lying by
// omission.
type Stats struct {
        Files         int // .ics files considered
        Events        int // VEVENTs parsed into the result
        SkippedLines  int // content lines that failed to parse
        SkippedEvents int // VEVENTs without a usable DTSTART (or unterminated)
        SkippedFiles  int // unreadable or over the size cap
}

// Degraded reports whether anything was skipped — a signal that the
// directory is imperfect, not broken.
func (s Stats) Degraded() bool {
        return s.SkippedLines+s.SkippedEvents+s.SkippedFiles > 0
}

// maxFileBytes caps a single .ics file: a personal calendar file lives
// far below this, and a hostile "calendar" that is really a disk dump
// must not be slurped whole.
const maxFileBytes = 4 << 20

// Parse decodes one .ics document into its events plus per-file stats.
// It never fails on content — malformed pieces are counted in Stats, and
// everything parseable still surfaces.
func Parse(content string) ([]Event, Stats) {
        var st Stats
        lines, orphans := unfold(content)
        st.SkippedLines += orphans

        var events []Event
        var cur *eventBuilder
        var stack []string // open component names, innermost last

        // inEvent reports whether the innermost open component is the event
        // being built — a property inside a nested component (VALARM's
        // DESCRIPTION, say) must never land on the event.
        inEvent := func() bool {
                return len(stack) > 0 && stack[len(stack)-1] == "VEVENT"
        }

        for _, ln := range lines {
                if ln == "" {
                        continue // blank line: sloppy editors emit them; harmless
                }
                cl, err := parseContentLine(ln)
                if err != nil {
                        st.SkippedLines++
                        continue
                }
                switch cl.name {
                case "BEGIN":
                        comp := strings.ToUpper(strings.TrimSpace(cl.value))
                        if comp == "VEVENT" && cur != nil {
                                // BEGIN:VEVENT inside a VEVENT: malformed. Count the
                                // half-built one as skipped and start over.
                                st.SkippedEvents++
                        }
                        stack = append(stack, comp)
                        if comp == "VEVENT" {
                                cur = &eventBuilder{}
                        }
                case "END":
                        comp := strings.ToUpper(strings.TrimSpace(cl.value))
                        idx := -1
                        for i := len(stack) - 1; i >= 0; i-- {
                                if stack[i] == comp {
                                        idx = i
                                        break
                                }
                        }
                        if idx < 0 {
                                continue // END with no matching BEGIN: tolerated
                        }
                        if comp == "VEVENT" && cur != nil && idx == len(stack)-1 {
                                if ev, err := cur.build(); err != nil {
                                        st.SkippedEvents++
                                } else {
                                        events = append(events, ev)
                                }
                                cur = nil
                        }
                        stack = stack[:idx]
                default:
                        if cur != nil && inEvent() {
                                if w := cur.set(cl); w != "" {
                                        cur.warn(w)
                                }
                        }
                        // Lines outside a VEVENT or inside a nested component
                        // (VCALENDAR props, VTIMEZONE innards, VALARM bodies) are
                        // valid-but-unknown to this subset: ignored silently.
                }
        }
        if cur != nil {
                st.SkippedEvents++ // unterminated VEVENT at EOF
        }
        st.Events = len(events)
        return events, st
}

// ---------- content lines ----------

// contentLine is one unfolded logical content line: NAME;PARAM=V:VALUE.
type contentLine struct {
        name   string
        params map[string]string
        value  string
}

// parseContentLine splits an unfolded line at the first ':' that is not
// inside a double-quoted param value (RFC 5545 §3.1), then splits the
// head on unquoted ';'. Property and param names are case-insensitive;
// values are case-sensitive.
func parseContentLine(ln string) (contentLine, error) {
        colon := -1
        inQuote := false
        for i := 0; i < len(ln); i++ {
                switch ln[i] {
                case '"':
                        inQuote = !inQuote
                case ':':
                        if !inQuote {
                                colon = i
                        }
                }
                if colon >= 0 {
                        break
                }
        }
        if colon < 0 {
                return contentLine{}, errors.New("calendar: content line has no colon")
        }
        head, value := ln[:colon], ln[colon+1:]

        parts := splitUnquoted(head, ';')
        name := strings.ToUpper(strings.TrimSpace(parts[0]))
        if name == "" {
                return contentLine{}, errors.New("calendar: content line has no property name")
        }
        var params map[string]string
        if len(parts) > 1 {
                params = make(map[string]string, len(parts)-1)
                for _, p := range parts[1:] {
                        p = strings.TrimSpace(p)
                        if p == "" {
                                continue
                        }
                        eq := strings.IndexByte(p, '=')
                        if eq < 0 {
                                continue // param without '=': tolerated and dropped
                        }
                        v := p[eq+1:]
                        if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
                                v = v[1 : len(v)-1]
                        }
                        params[strings.ToUpper(strings.TrimSpace(p[:eq]))] = v
                }
        }
        return contentLine{name: name, params: params, value: value}, nil
}

// splitUnquoted splits s on sep, ignoring separators inside double quotes.
func splitUnquoted(s string, sep byte) []string {
        var out []string
        start, inQuote := 0, false
        for i := 0; i < len(s); i++ {
                switch s[i] {
                case '"':
                        inQuote = !inQuote
                case sep:
                        if !inQuote {
                                out = append(out, s[start:i])
                                start = i + 1
                        }
                }
        }
        return append(out, s[start:])
}

// unfold normalizes newlines and applies RFC 5545 §3.1 line unfolding: a
// line break followed by one space or tab is a continuation, not a new
// line. Returns the logical lines plus the count of orphan continuations
// (a continuation with nothing to continue — malformed, counted, kept as
// its own line so the rest of the document stays aligned).
func unfold(content string) ([]string, int) {
        content = strings.ReplaceAll(content, "\r\n", "\n")
        content = strings.ReplaceAll(content, "\r", "\n")
        raw := strings.Split(content, "\n")
        if n := len(raw); n > 0 && raw[n-1] == "" {
                raw = raw[:n-1] // trailing newline, not a blank line
        }
        lines := make([]string, 0, len(raw))
        orphans := 0
        for _, l := range raw {
                if l == "" {
                        continue
                }
                if l[0] == ' ' || l[0] == '\t' {
                        if len(lines) > 0 {
                                lines[len(lines)-1] += l[1:]
                                continue
                        }
                        orphans++
                }
                lines = append(lines, l)
        }
        return lines, orphans
}

// ---------- event building ----------

// eventBuilder buffers one VEVENT's properties; property order is not
// guaranteed by the format, so timing is resolved at build time.
type eventBuilder struct {
        uid         string
        summary     string
        location    string
        description string
        rrule       bool
        rruleRaw    string
        dtstart     *propTime
        dtend       *propTime
        duration    string
        warnings    []string
        sub         int // nested component depth (VALARM & friends)
}

// propTime is a parsed DTSTART/DTEND: the instant plus whether it was a
// DATE (all-day) value.
type propTime struct {
        t      time.Time
        isDate bool
}

func (b *eventBuilder) warn(msg string) { b.warnings = append(b.warnings, msg) }

// set folds one property into the builder; the return value is a warning
// to record ("" when clean).
func (b *eventBuilder) set(cl contentLine) string {
        switch cl.name {
        case "UID":
                if b.uid == "" { // first UID wins; duplicates are malformed
                        b.uid = cl.value // raw: UIDs are matched verbatim, never unescaped
                }
        case "SUMMARY":
                b.summary = UnescapeText(cl.value)
        case "LOCATION":
                b.location = UnescapeText(cl.value)
        case "DESCRIPTION":
                b.description = UnescapeText(cl.value)
        case "RRULE":
                b.rrule = true
                b.rruleRaw = cl.value
        case "DURATION":
                b.duration = cl.value
        case "DTSTART":
                pt, warn := parsePropTime(cl)
                if pt == nil {
                        return fmt.Sprintf("DTSTART %q is unparseable; event unusable", cl.value)
                }
                b.dtstart = pt
                return warn
        case "DTEND":
                pt, warn := parsePropTime(cl)
                if pt != nil {
                        b.dtend = pt
                }
                return warn // a bad DTEND downgrades to Start-only, never fatal
        }
        return ""
}

// build resolves the buffered properties into an Event.
func (b *eventBuilder) build() (Event, error) {
        if b.dtstart == nil {
                return Event{}, errors.New("calendar: VEVENT without a usable DTSTART")
        }
        ev := Event{
                UID:         b.uid,
                Summary:     b.summary,
                Location:    b.location,
                Description: b.description,
                Start:       b.dtstart.t,
                AllDay:      b.dtstart.isDate,
                Recurring:   b.rrule,
                Warnings:    b.warnings,
        }
        switch {
        case b.dtend != nil:
                ev.End = b.dtend.t
        case b.duration != "":
                d, err := ParseDuration(b.duration)
                if err != nil {
                        ev.Warnings = append(ev.Warnings, fmt.Sprintf("DURATION %q unparseable; ignored", b.duration))
                } else {
                        ev.End = ev.Start.Add(d)
                }
        }
        // The RRULE resolves into ev.rule when it is within recur.go's
        // subset; anything else keeps the first-occurrence behavior and
        // says so in Warnings — a recurrence shown wrong is worse than one
        // shown at its anchor.
        if b.rruleRaw != "" {
                if rule, err := ParseRule(b.rruleRaw); err != nil {
                        ev.Warnings = append(ev.Warnings,
                                fmt.Sprintf("RRULE %q not expanded: %v", b.rruleRaw, err))
                } else {
                        ev.rule = rule
                }
        }
        return ev, nil
}

// ---------- value parsing ----------

// parsePropTime decodes a DTSTART/DTEND value with its params. Returns
// (nil, "") for values neither parser accepts — the caller decides
// whether that is fatal (DTSTART) or a downgrade (DTEND).
func parsePropTime(cl contentLine) (*propTime, string) {
        v := strings.TrimSpace(cl.value)
        if v == "" {
                return nil, ""
        }

        // DATE form: explicit VALUE=DATE, or a bare YYYYMMDD with no 'T'.
        if strings.EqualFold(cl.params["VALUE"], "DATE") || (len(v) == 8 && !strings.ContainsAny(v, "Tt")) {
                t, err := time.ParseInLocation("20060102", v, time.Local)
                if err != nil {
                        return nil, ""
                }
                return &propTime{t: t, isDate: true}, ""
        }

        // DATE-TIME form. Strip a trailing Z (UTC) first; otherwise resolve
        // the TZID param (unknown zone → UTC with a warning) or fall back to
        // the server's local zone for floating times.
        loc := time.Local
        warn := ""
        utc := strings.HasSuffix(v, "Z") || strings.HasSuffix(v, "z")
        if utc {
                v = v[:len(v)-1]
                loc = time.UTC
        } else if tzid := cl.params["TZID"]; tzid != "" {
                l, err := time.LoadLocation(tzid)
                if err != nil {
                        loc = time.UTC
                        warn = fmt.Sprintf("unknown TZID %q interpreted as UTC", tzid)
                } else {
                        loc = l
                }
        }

        t, ok := parseDateTimeBody(v, loc)
        if !ok {
                return nil, ""
        }
        return &propTime{t: t}, warn
}

// parseDateTimeBody parses the body of a DATE-TIME value (Z stripped):
// YYYYMMDDTHHMMSS with optional fractional seconds, or the seconds-less
// lenient form some producers emit.
func parseDateTimeBody(v string, loc *time.Location) (time.Time, bool) {
        if t, err := time.ParseInLocation("20060102T150405", v, loc); err == nil {
                return t, true
        }
        if t, err := time.ParseInLocation("20060102T1504", v, loc); err == nil {
                return t, true
        }
        // Fractional seconds (RFC 5545 permits them; producers rarely send
        // them). Trim the fraction and retry rather than growing layouts.
        if i := strings.IndexAny(v, ".,"); i > 8 {
                if t, err := time.ParseInLocation("20060102T150405", v[:i], loc); err == nil {
                        return t, true
                }
        }
        return time.Time{}, false
}

// ParseDuration decodes an RFC 5545 DURATION (§3.3.6):
// [+-]P[nW][nD][T[nH][nM][nS]]. Durations are nominal — a week is 7 days,
// a day 24 hours — because a duration field carries no calendar context
// to consult.
func ParseDuration(s string) (time.Duration, error) {
        orig := s
        neg := false
        if len(s) > 0 && (s[0] == '+' || s[0] == '-') {
                neg = s[0] == '-'
                s = s[1:]
        }
        if len(s) < 2 || s[0] != 'P' {
                return 0, fmt.Errorf("calendar: invalid DURATION %q", orig)
        }
        s = s[1:]

        var d time.Duration
        inTime := false
        num := ""
        components := 0
        flush := func(unit time.Duration) error {
                if num == "" {
                        return fmt.Errorf("calendar: invalid DURATION %q", orig)
                }
                n, err := strconv.Atoi(num)
                if err != nil || n < 0 {
                        return fmt.Errorf("calendar: invalid DURATION %q", orig)
                }
                d += time.Duration(n) * unit
                num = ""
                components++
                return nil
        }
        for i := 0; i < len(s); i++ {
                c := s[i]
                switch {
                case c >= '0' && c <= '9':
                        num += string(c)
                case c == 'T':
                        if inTime || num != "" {
                                return 0, fmt.Errorf("calendar: invalid DURATION %q", orig)
                        }
                        inTime = true
                case c == 'W':
                        if inTime {
                                return 0, fmt.Errorf("calendar: invalid DURATION %q", orig)
                        }
                        if err := flush(7 * 24 * time.Hour); err != nil {
                                return 0, err
                        }
                case c == 'D':
                        if inTime {
                                return 0, fmt.Errorf("calendar: invalid DURATION %q", orig)
                        }
                        if err := flush(24 * time.Hour); err != nil {
                                return 0, err
                        }
                case c == 'H':
                        if !inTime {
                                return 0, fmt.Errorf("calendar: invalid DURATION %q", orig)
                        }
                        if err := flush(time.Hour); err != nil {
                                return 0, err
                        }
                case c == 'M':
                        if !inTime {
                                return 0, fmt.Errorf("calendar: months are not a fixed duration (%q)", orig)
                        }
                        if err := flush(time.Minute); err != nil {
                                return 0, err
                        }
                case c == 'S':
                        if !inTime {
                                return 0, fmt.Errorf("calendar: invalid DURATION %q", orig)
                        }
                        if err := flush(time.Second); err != nil {
                                return 0, err
                        }
                default:
                        return 0, fmt.Errorf("calendar: invalid DURATION %q", orig)
                }
        }
        if num != "" || components == 0 {
                return 0, fmt.Errorf("calendar: invalid DURATION %q", orig)
        }
        if neg {
                d = -d
        }
        return d, nil
}

// ---------- directory scanning ----------

// fileMemo is the per-file parse cache: a file whose mtime AND size are
// unchanged is not re-parsed. Both must match — either alone can be
// forged (touch, same-length rewrite), and the pair is cheap to check.
type fileMemo struct {
        mod      time.Time
        size     int64
        events   []Event
        skipped  int // SkippedLines for this file
        skippedE int // SkippedEvents for this file
}

var (
        scanMu   sync.Mutex
        scanMemo = map[string]*fileMemo{}
)

// Scan walks dir (recursively) and returns every event found, sorted by
// start instant (UID as the tiebreaker). A missing directory is an empty
// calendar, not an error — the dashboard's "not configured" vs "empty"
// distinction lives one level up, at the config dir setting. Unreadable
// or oversized files are counted in Stats and skipped.
func Scan(dir string) ([]Event, Stats, error) {
        scanMu.Lock()
        defer scanMu.Unlock()

        var st Stats
        var events []Event
        walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
                if err != nil {
                        if errors.Is(err, fs.ErrNotExist) && path == dir {
                                return nil // missing dir: empty calendar, keep walking nothing
                        }
                        st.SkippedFiles++
                        return nil // unreadable entry: skip, don't abort the walk
                }
                if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".ics") {
                        return nil
                }
                st.Files++
                fi, err := d.Info()
                if err != nil {
                        st.SkippedFiles++
                        return nil
                }
                if fi.Size() > maxFileBytes {
                        st.SkippedFiles++
                        return nil
                }
                if m, ok := scanMemo[path]; ok && m.mod.Equal(fi.ModTime()) && m.size == fi.Size() {
                        events = append(events, m.events...)
                        st.SkippedLines += m.skipped
                        st.SkippedEvents += m.skippedE
                        st.Events += len(m.events)
                        return nil
                }
                raw, err := os.ReadFile(path)
                if err != nil {
                        st.SkippedFiles++
                        return nil
                }
                evs, fs := Parse(string(raw))
                st.SkippedLines += fs.SkippedLines
                st.SkippedEvents += fs.SkippedEvents
                st.Events += len(evs)
                for i := range evs {
                        evs[i].File = path
                }
                scanMemo[path] = &fileMemo{mod: fi.ModTime(), size: fi.Size(), events: evs, skipped: fs.SkippedLines, skippedE: fs.SkippedEvents}
                events = append(events, evs...)
                return nil
        })
        if walkErr != nil {
                return nil, st, walkErr
        }

        sort.SliceStable(events, func(i, j int) bool {
                if !events[i].Start.Equal(events[j].Start) {
                        return events[i].Start.Before(events[j].Start)
                }
                return events[i].UID < events[j].UID
        })
        return events, st, nil
}

// Agenda returns the events overlapping the half-open window [from, to),
// sorted by start. An event with no end overlaps only if its start falls
// inside the window; an all-day event spans [its day, next midnight).
// Recurring events expand: every occurrence of a recur.go-subset rule
// that overlaps the window surfaces as its own Event (same UID, same
// fields, its own Start/End); a rule outside the subset keeps the
// first-occurrence behavior. The window bounds expansion, but COUNT and
// UNTIL bound the whole series — a series whose budget ran out before
// the window yields nothing.
func Agenda(dir string, from, to time.Time) ([]Event, Stats, error) {
        evs, st, err := Scan(dir)
        if err != nil {
                return nil, st, err
        }
        out := make([]Event, 0, len(evs))
        for _, ev := range evs {
                if ev.rule != nil {
                        for _, occ := range ev.rule.expandOccurrenceStarts(ev, from, to) {
                                clone := ev
                                clone.Start = occ
                                if !ev.End.IsZero() {
                                        clone.End = occ.Add(ev.End.Sub(ev.Start))
                                }
                                out = append(out, clone)
                        }
                        continue
                }
                if overlapsWindow(ev, ev.Start, from, to) {
                        out = append(out, ev)
                }
        }
        // Expansion interleaves occurrences with one-off events, so the
        // result re-sorts; UID as the tiebreak keeps a series' occurrences
        // in chronological order (they share the master's UID, and stable
        // sort preserves generation order).
        sort.SliceStable(out, func(i, j int) bool {
                if !out[i].Start.Equal(out[j].Start) {
                        return out[i].Start.Before(out[j].Start)
                }
                return out[i].UID < out[j].UID
        })
        return out, st, nil
}

// LocalMidnight truncates t to the start of its calendar day in t's own
// location — the anchor "today" the agenda and range surfaces use.
func LocalMidnight(t time.Time) time.Time {
        y, m, d := t.Date()
        return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
