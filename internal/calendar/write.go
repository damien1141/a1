// write.go is the calendar package's write side: text escaping (RFC 5545
// §3.3.11), line folding (§3.1), whole-file Create, and — the part that
// has to be surgical — Update, which patches a single property inside a
// user's .ics file while leaving every byte it does not understand alone.
package calendar

import (
        "crypto/rand"
        "encoding/hex"
        "errors"
        "fmt"
        "os"
        "path/filepath"
        "strings"
        "time"
)

// icsUTC is the DATE-TIME rendering for created events: UTC with a Z
// suffix, the one form every consumer understands.
const icsUTC = "20060102T150405Z"

// icsDate is the DATE rendering for all-day events.
const icsDate = "20060102"

// EscapeText escapes a TEXT property value per RFC 5545 §3.3.11:
// backslash, semicolon, comma, and line breaks. Bare CR is dropped —
// CRLF is the line delimiter, not content.
func EscapeText(s string) string {
        var b strings.Builder
        b.Grow(len(s))
        for _, r := range s {
                switch r {
                case '\\':
                        b.WriteString(`\\`)
                case ';':
                        b.WriteString(`\;`)
                case ',':
                        b.WriteString(`\,`)
                case '\n':
                        b.WriteString(`\n`)
                case '\r':
                        // dropped: fold/unfold owns CRLF
                default:
                        b.WriteRune(r)
                }
        }
        return b.String()
}

// UnescapeText reverses EscapeText. Unknown escapes keep both bytes —
// the input came from a producer this subset does not fully know, and
// destroying characters is worse than passing them through.
func UnescapeText(s string) string {
        var b strings.Builder
        b.Grow(len(s))
        for i := 0; i < len(s); i++ {
                c := s[i]
                if c != '\\' || i+1 >= len(s) {
                        b.WriteByte(c)
                        continue
                }
                i++
                switch s[i] {
                case 'n', 'N':
                        b.WriteByte('\n')
                case '\\', ';', ',':
                        b.WriteByte(s[i])
                default:
                        b.WriteByte('\\')
                        b.WriteByte(s[i])
                }
        }
        return b.String()
}

// foldLimit is the RFC 5545 §3.1 line-length cap in octets, excluding
// CRLF. Continuation lines spend one octet on the leading space.
const foldLimit = 75

// appendFolded appends one logical content line to dst as one or more
// physical CRLF-terminated lines, none over foldLimit octets. Cuts land
// only on rune boundaries, so multi-byte UTF-8 is never split.
func appendFolded(dst []byte, line string) []byte {
        continuation := false
        for len(line) > 0 {
                take := foldLimit
                if continuation {
                        take = foldLimit - 1 // room for the leading space
                }
                if take > len(line) {
                        take = len(line)
                }
                for take > 0 && take < len(line) && line[take]&0xC0 == 0x80 {
                        take-- // back off to a rune start
                }
                if take == 0 {
                        take = 1 // unreachable for valid UTF-8; never loop forever
                }
                if continuation {
                        dst = append(dst, ' ')
                }
                dst = append(dst, line[:take]...)
                dst = append(dst, '\r', '\n')
                line = line[take:]
                continuation = true
        }
        return dst
}

// renderEventLines renders one Event as the logical (unfolded) content
// lines of its VEVENT — no BEGIN/END wrappers, no folding. Timed events
// serialize as UTC (Z); all-day events as VALUE=DATE.
func renderEventLines(ev Event, withStamp bool) []string {
        var ls []string
        if ev.UID != "" {
                ls = append(ls, "UID:"+ev.UID)
        }
        if withStamp {
                ls = append(ls, "DTSTAMP:"+time.Now().UTC().Format(icsUTC))
        }
        if ev.AllDay {
                ls = append(ls, "DTSTART;VALUE=DATE:"+ev.Start.In(time.Local).Format(icsDate))
                if !ev.End.IsZero() {
                        ls = append(ls, "DTEND;VALUE=DATE:"+ev.End.In(time.Local).Format(icsDate))
                }
        } else {
                ls = append(ls, "DTSTART:"+ev.Start.UTC().Format(icsUTC))
                if !ev.End.IsZero() {
                        ls = append(ls, "DTEND:"+ev.End.UTC().Format(icsUTC))
                }
        }
        if s := strings.TrimSpace(ev.Summary); s != "" {
                ls = append(ls, "SUMMARY:"+EscapeText(s))
        }
        if s := strings.TrimSpace(ev.Location); s != "" {
                ls = append(ls, "LOCATION:"+EscapeText(s))
        }
        if s := strings.TrimSpace(ev.Description); s != "" {
                ls = append(ls, "DESCRIPTION:"+EscapeText(s))
        }
        return ls
}

// NewUID mints a UID for a created event: nanosecond timestamp plus
// crypto-random hex, in the a1 namespace.
func NewUID() string {
        var b [4]byte
        if _, err := rand.Read(b[:]); err != nil {
                // crypto/rand failing means the OS entropy pool is gone; the
                // timestamp alone is still unique within this process.
                return fmt.Sprintf("a1-%d@a1", time.Now().UnixNano())
        }
        return fmt.Sprintf("a1-%d-%s@a1", time.Now().UnixNano(), hex.EncodeToString(b[:]))
}

// filenameFor maps a UID to a safe file name: [A-Za-z0-9._@-] kept,
// everything else folded to '_'.
func filenameFor(uid string) string {
        var b strings.Builder
        for _, r := range uid {
                switch {
                case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
                        b.WriteRune(r)
                case r == '.' || r == '_' || r == '@' || r == '-':
                        b.WriteRune(r)
                default:
                        b.WriteByte('_')
                }
        }
        s := b.String()
        s = strings.TrimLeft(s, ".')") // no dotfiles, no leading dots
        if s == "" {
                s = "event"
        }
        return s
}

// Create writes ev as a new one-event .ics file under dir and returns
// the event as stored (defaults applied, UID minted). One file per
// created event: the agent never rewrites a user's multi-event file to
// add to it — that is what Update is for, surgically.
func Create(dir string, ev Event) (Event, error) {
        if strings.TrimSpace(ev.Summary) == "" {
                return Event{}, errors.New("calendar: create requires a summary")
        }
        if ev.Start.IsZero() {
                return Event{}, errors.New("calendar: create requires a start")
        }
        if !ev.End.IsZero() && ev.End.Before(ev.Start) {
                return Event{}, errors.New("calendar: create end is before start")
        }
        // RFC 5545 §3.6.1: an absent DTEND means the event ends when it
        // starts (DATE) or spans no time (DATE-TIME). Make it explicit.
        if ev.End.IsZero() {
                if ev.AllDay {
                        ev.End = ev.Start.AddDate(0, 0, 1)
                } else {
                        ev.End = ev.Start
                }
        }
        if ev.UID == "" {
                ev.UID = NewUID()
        }
        ev.Warnings = nil

        if err := os.MkdirAll(dir, 0o755); err != nil {
                return Event{}, fmt.Errorf("calendar: create dir: %w", err)
        }
        name, err := uniqueName(dir, filenameFor(ev.UID)+".ics")
        if err != nil {
                return Event{}, err
        }
        ev.File = name

        var buf []byte
        buf = append(buf, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//a1//calendar//EN\r\nCALSCALE:GREGORIAN\r\nBEGIN:VEVENT\r\n"...)
        for _, ln := range renderEventLines(ev, true) {
                buf = appendFolded(buf, ln)
        }
        buf = append(buf, "END:VEVENT\r\nEND:VCALENDAR\r\n"...)

        if err := writeFileAtomic(name, buf); err != nil {
                return Event{}, err
        }
        return ev, nil
}

// uniqueName finds an unused file path in dir for the wanted base name,
// appending -2, -3, ... when taken.
func uniqueName(dir, base string) (string, error) {
        stem, ext := strings.TrimSuffix(base, ".ics"), ".ics"
        candidate := filepath.Join(dir, base)
        for n := 2; ; n++ {
                if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
                        return candidate, nil
                } else if err != nil {
                        return "", fmt.Errorf("calendar: stat %s: %w", candidate, err)
                }
                candidate = filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, n, ext))
        }
}

// writeFileAtomic writes via temp file + rename so a crash never leaves
// a half-written .ics behind.
func writeFileAtomic(path string, content []byte) error {
        tmp, err := os.CreateTemp(filepath.Dir(path), ".a1-cal-*")
        if err != nil {
                return fmt.Errorf("calendar: temp file: %w", err)
        }
        defer os.Remove(tmp.Name()) // no-op after a successful rename
        if _, err := tmp.Write(content); err != nil {
                tmp.Close()
                return fmt.Errorf("calendar: write: %w", err)
        }
        if err := tmp.Close(); err != nil {
                return fmt.Errorf("calendar: close: %w", err)
        }
        if err := os.Chmod(tmp.Name(), 0o644); err != nil {
                return fmt.Errorf("calendar: chmod: %w", err)
        }
        if err := os.Rename(tmp.Name(), path); err != nil {
                return fmt.Errorf("calendar: rename: %w", err)
        }
        // Our own writes must never be served stale from the scan memo —
        // rename preserves the temp file's mtime, and a same-size rewrite
        // inside one coarse-clock tick could otherwise look unchanged.
        invalidateMemo(path)
        return nil
}

// invalidateMemo drops the scan memo for a path this package just wrote.
func invalidateMemo(path string) {
        scanMu.Lock()
        delete(scanMemo, path)
        scanMu.Unlock()
}

// Patch is a field-set for Update: nil fields are left untouched.
type Patch struct {
        Summary     *string
        Location    *string
        Description *string
        Start       *time.Time
        End         *time.Time
        AllDay      *bool
}

// IsEmpty reports whether the patch carries no field at all.
func (p Patch) IsEmpty() bool {
        return p.Summary == nil && p.Location == nil && p.Description == nil &&
                p.Start == nil && p.End == nil && p.AllDay == nil
}

// Update patches the event with the given UID in place — a line-level
// surgical rewrite of the file that holds it. Only the patched property
// lines change: every other line (RRULE, VALARM blocks, custom X-
// properties, other events in the same file) is re-emitted byte-for-
// byte modulo re-folding, which is presentation, not content. Returns
// the event as the REAL parser reads it back after the rewrite — the
// operation verifies itself.
func Update(dir, uid string, p Patch) (Event, error) {
        if strings.TrimSpace(uid) == "" {
                return Event{}, errors.New("calendar: update requires a uid")
        }
        evs, _, err := Scan(dir)
        if err != nil {
                return Event{}, err
        }
        var target *Event
        for i := range evs {
                if evs[i].UID == uid {
                        target = &evs[i]
                        break
                }
        }
        if target == nil {
                return Event{}, fmt.Errorf("calendar: no event with UID %q under %s", uid, dir)
        }

        raw, err := os.ReadFile(target.File)
        if err != nil {
                return Event{}, fmt.Errorf("calendar: read %s: %w", target.File, err)
        }
        lines, _ := unfold(string(raw)) // already parseable: no orphans by construction

        // Resolve the patch against the current event first — the rendering
        // of DTSTART/DTEND depends on the (possibly new) all-day-ness.
        merged := *target
        allDayNew := merged.AllDay
        if p.AllDay != nil {
                allDayNew = *p.AllDay
        }
        timeFormChanged := p.AllDay != nil && *p.AllDay != merged.AllDay

        if p.Summary != nil {
                merged.Summary = *p.Summary
        }
        if p.Location != nil {
                merged.Location = *p.Location
        }
        if p.Description != nil {
                merged.Description = *p.Description
        }
        if p.Start != nil {
                merged.Start = *p.Start
        }
        if p.End != nil {
                merged.End = *p.End
        }
        if merged.End.IsZero() && allDayNew {
                merged.End = merged.Start.AddDate(0, 0, 1)
        }
        if !merged.End.IsZero() && merged.End.Before(merged.Start) {
                return Event{}, errors.New("calendar: update would make end precede start")
        }
        merged.AllDay = allDayNew

        // The property lines to rewrite, rendered from the merged event. A
        // property is rewritten when patched; all-day-ness changing forces
        // both time lines to change form.
        rewrite := map[string]string{}
        if p.Summary != nil {
                s := strings.TrimSpace(merged.Summary)
                if s == "" {
                        rewrite["SUMMARY"] = "" // delete the property
                } else {
                        rewrite["SUMMARY"] = "SUMMARY:" + EscapeText(s)
                }
        }
        if p.Location != nil {
                s := strings.TrimSpace(merged.Location)
                if s == "" {
                        rewrite["LOCATION"] = ""
                } else {
                        rewrite["LOCATION"] = "LOCATION:" + EscapeText(s)
                }
        }
        if p.Description != nil {
                s := strings.TrimSpace(merged.Description)
                if s == "" {
                        rewrite["DESCRIPTION"] = ""
                } else {
                        rewrite["DESCRIPTION"] = "DESCRIPTION:" + EscapeText(s)
                }
        }
        if p.Start != nil || p.End != nil || timeFormChanged {
                if allDayNew {
                        rewrite["DTSTART"] = "DTSTART;VALUE=DATE:" + merged.Start.In(time.Local).Format(icsDate)
                        if !merged.End.IsZero() {
                                rewrite["DTEND"] = "DTEND;VALUE=DATE:" + merged.End.In(time.Local).Format(icsDate)
                        }
                } else {
                        rewrite["DTSTART"] = "DTSTART:" + merged.Start.UTC().Format(icsUTC)
                        if !merged.End.IsZero() {
                                rewrite["DTEND"] = "DTEND:" + merged.End.UTC().Format(icsUTC)
                        }
                }
        }

        // RFC 5545 §3.8.7.2: DTSTAMP SHOULD be bumped on modification — and
        // inserted when the event never carried one (it is REQUIRED).
        if len(rewrite) > 0 {
                rewrite["DTSTAMP"] = "DTSTAMP:" + time.Now().UTC().Format(icsUTC)
        }

        out, err := patchLines(lines, uid, rewrite)
        if err != nil {
                return Event{}, err
        }
        var buf []byte
        for _, ln := range out {
                buf = appendFolded(buf, ln)
        }
        if err := writeFileAtomic(target.File, buf); err != nil {
                return Event{}, err
        }

        // Self-verification: return what the real parser sees now. Scan's
        // memo is stale for this file (mtime changed), so this re-parses.
        updated, _, err := Scan(dir)
        if err != nil {
                return Event{}, err
        }
        for _, ev := range updated {
                if ev.UID == uid {
                        return ev, nil
                }
        }
        return Event{}, fmt.Errorf("calendar: update of %q succeeded but the event no longer parses", uid)
}

// patchLines performs the line surgery: within the VEVENT carrying the
// UID, replace the first line of each patched property, drop duplicates,
// and insert missing ones just before END:VEVENT. Everything else —
// including nested components and every other event in the file — is
// copied verbatim. A present DURATION is replaced by DTEND whenever the
// end is rewritten (both would be contradictory).
func patchLines(lines []string, uid string, rewrite map[string]string) ([]string, error) {
        // Locate the target VEVENT: [begin, end) indices of the one carrying
        // the UID. A later VEVENT that does not match must not clobber an
        // earlier match, so the found range is latched in matchedBegin/End.
        begin, end := -1, -1 // the VEVENT currently being scanned
        matchedBegin, matchedEnd := -1, -1
        depth := 0
        for i, ln := range lines {
                cl, err := parseContentLine(ln)
                if err != nil {
                        continue
                }
                switch {
                case cl.name == "BEGIN" && strings.EqualFold(strings.TrimSpace(cl.value), "VEVENT"):
                        if depth == 0 {
                                begin = i
                        }
                        depth++
                case cl.name == "END" && strings.EqualFold(strings.TrimSpace(cl.value), "VEVENT"):
                        depth--
                        if depth == 0 && begin >= 0 {
                                if matchedBegin < 0 && hasUID(lines, begin, i, uid) {
                                        matchedBegin, matchedEnd = begin, i
                                }
                                begin = -1
                        }
                }
        }
        if matchedBegin < 0 {
                return nil, fmt.Errorf("calendar: UID %q not found in its own file", uid)
        }
        begin, end = matchedBegin, matchedEnd

        out := make([]string, 0, len(lines))
        out = append(out, lines[:begin+1]...) // through BEGIN:VEVENT itself

        // One pass over the event body: track nested components so a
        // VALARM's DESCRIPTION is never mistaken for the event's.
        done := map[string]bool{}
        sub := 0
        for i := begin + 1; i < end; i++ {
                ln := lines[i]
                cl, err := parseContentLine(ln)
                if err != nil || sub > 0 {
                        if err == nil {
                                switch {
                                case cl.name == "BEGIN":
                                        sub++
                                case cl.name == "END":
                                        sub--
                                }
                        }
                        out = append(out, ln)
                        continue
                }
                switch cl.name {
                case "BEGIN":
                        sub++
                        out = append(out, ln)
                case "END":
                        sub--
                        out = append(out, ln)
                default:
                        if replacement, patched := rewrite[cl.name]; patched {
                                if done[cl.name] {
                                        continue // duplicate property: drop (invalid ICS anyway)
                                }
                                done[cl.name] = true
                                if replacement != "" {
                                        out = append(out, replacement)
                                }
                                continue // empty replacement: property deleted
                        }
                        // A DURATION contradicts a rewritten DTEND: consume it.
                        if cl.name == "DURATION" {
                                if _, endRewritten := rewrite["DTEND"]; endRewritten {
                                        continue
                                }
                        }
                        out = append(out, ln)
                }
        }
        // Insertions (patched properties the event never had) land directly
        // before END:VEVENT (excluded from the body loop above), in the
        // conventional property order.
        for _, prop := range []string{"DTSTAMP", "DTSTART", "DTEND", "SUMMARY", "LOCATION", "DESCRIPTION"} {
                if replacement, patched := rewrite[prop]; patched && !done[prop] && replacement != "" {
                        out = append(out, replacement)
                        done[prop] = true
                }
        }
        out = append(out, lines[end])
        out = append(out, lines[end+1:]...)
        return out, nil
}

// hasUID reports whether the VEVENT spanning [begin, end) carries the
// exact UID (compared raw, like the parser keeps it).
func hasUID(lines []string, begin, end int, uid string) bool {
        depth := 0
        for i := begin + 1; i < end; i++ {
                cl, err := parseContentLine(lines[i])
                if err != nil {
                        continue
                }
                switch {
                case cl.name == "BEGIN":
                        depth++
                case cl.name == "END":
                        depth--
                case cl.name == "UID" && depth == 0:
                        return cl.value == uid
                }
        }
        return false
}
