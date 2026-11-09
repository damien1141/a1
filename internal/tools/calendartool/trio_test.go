package calendartool

import (
        "context"
        "encoding/json"
        "fmt"
        "os"
        "path/filepath"
        "strings"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/calendar"
)

// The trio for the personal_calendar facade. The unit under test is the
// FACADE, not the parser (internal/calendar owns that trio): these tests
// pin the call-time flag enforcement, the argument surface, and the
// facade-vs-library parity.

// fixtureDeps builds a tool over a temp calendar dir seeded with .ics
// content. allowed are live funcs so a test can flip consent mid-flight —
// exactly the mis-mount scenario the call-time gate exists for.
type fixtureDeps struct {
        dir         string
        readAllowed bool
        writeOn     bool
}

// seed writes one .ics file into the fixture dir.
func seed(t *testing.T, dir, name, content string) {
        t.Helper()
        require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

// icsEvent renders a minimal one-event calendar around the given
// start/end (already formatted as UTC Z values).
func icsEvent(uid, summary, start, end string) string {
        var b strings.Builder
        b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//x//EN\r\n")
        b.WriteString("BEGIN:VEVENT\r\n")
        b.WriteString("UID:" + uid + "\r\n")
        b.WriteString("DTSTAMP:20260101T000000Z\r\n")
        b.WriteString("DTSTART:" + start + "\r\n")
        if end != "" {
                b.WriteString("DTEND:" + end + "\r\n")
        }
        b.WriteString("SUMMARY:" + summary + "\r\n")
        b.WriteString("END:VEVENT\r\nEND:VCALENDAR\r\n")
        return b.String()
}

// runTool invokes the facade with the given JSON args.
func runTool(t *testing.T, f *fixtureDeps, args string) (map[string]any, error) {
        t.Helper()
        tool := CalendarTool(Deps{
                CalendarDir: func() string { return f.dir },
                ReadAllowed: func() bool { return f.readAllowed },
                WriteAllowed: func() bool { return f.writeOn },
        })
        res, err := tool.Run(context.Background(), json.RawMessage(args))
        if err != nil {
                return nil, err
        }
        var body map[string]any
        require.NoError(t, json.Unmarshal([]byte(res.Content), &body))
        return body, nil
}

// ---------- positive ----------

func TestCalendarToolAgendaListsEvents_Positive(t *testing.T) {
        dir := t.TempDir()
        f := &fixtureDeps{dir: dir, readAllowed: true}
        now := time.Now().UTC()
        today := now.Format("20060102T150405Z")
        later := now.Add(2 * time.Hour).Format("20060102T150405Z")
        tomorrow := now.AddDate(0, 0, 1).Format("20060102")
        seed(t, dir, "a.ics", icsEvent("uid-1@a1", "Standup", today, later))
        seed(t, dir, "b.ics", "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:uid-2@a1\r\nDTSTART;VALUE=DATE:"+tomorrow+"\r\nSUMMARY:Conference day\\, all of it\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")

        body, err := runTool(t, f, `{"action":"agenda","days":14}`)
        require.NoError(t, err)
        events := body["events"].([]any)
        require.Len(t, events, 2, "both the timed and the all-day event surface")

        first := events[0].(map[string]any)
        assert.Equal(t, "uid-1@a1", first["uid"])
        assert.Equal(t, "Standup", first["summary"])
        assert.Equal(t, false, first["all_day"])
        assert.NotContains(t, first, "file", "filesystem paths stay out of the model's view")

        second := events[1].(map[string]any)
        assert.Equal(t, true, second["all_day"], "VALUE=DATE is an all-day event")
        assert.Equal(t, "Conference day, all of it", second["summary"], "escaped comma unescaped for the model")
}

func TestCalendarToolCreateUpdateRoundTrip_Positive(t *testing.T) {
        dir := t.TempDir()
        f := &fixtureDeps{dir: dir, readAllowed: true, writeOn: true}

        created, err := runTool(t, f, `{"action":"create","summary":"Dentist","start":"2030-06-04T09:00:00Z","end":"2030-06-04T09:45:00Z","location":"Clinic","description":"Bring the card"}`)
        require.NoError(t, err)
        assert.Equal(t, true, created["created"])
        ev := created["event"].(map[string]any)
        uid, ok := ev["uid"].(string)
        require.True(t, ok, "create returns the minted uid")
        assert.Equal(t, "Dentist", ev["summary"])
        assert.Equal(t, "Clinic", ev["location"])
        assert.Equal(t, false, ev["all_day"])

        // The write landed as a real .ics file the library (and any other
        // consumer) can read back.
        files, err := filepath.Glob(filepath.Join(dir, "*.ics"))
        require.NoError(t, err)
        require.Len(t, files, 1)

        // Update patches exactly the given field and reports the re-parsed truth.
        updated, err := runTool(t, f, fmt.Sprintf(`{"action":"update","uid":%q,"summary":"Dentist (rescheduled)","location":""}`, uid))
        require.NoError(t, err)
        assert.Equal(t, true, updated["updated"])
        ev = updated["event"].(map[string]any)
        assert.Equal(t, "Dentist (rescheduled)", ev["summary"])
        assert.NotContains(t, ev, "location", "an explicit empty string clears the property")

        // And an all-day create from a bare date.
        allday, err := runTool(t, f, `{"action":"create","summary":"Hack day","start":"2030-06-07"}`)
        require.NoError(t, err)
        ev = allday["event"].(map[string]any)
        assert.Equal(t, true, ev["all_day"], "a bare-date start makes the event all-day")
        assert.Contains(t, ev["end"], "2030-06-08", "all-day events end at the following midnight")
}

// ---------- negative ----------

// TestCalendarToolRefusedWhenFlagsOff_Negative is the flagship: the flags
// are read at CALL time, so a tool that was mounted under one consent
// state refuses the moment the live state says no — and a refused write
// leaves the directory untouched.
func TestCalendarToolRefusedWhenFlagsOff_Negative(t *testing.T) {
        dir := t.TempDir()
        f := &fixtureDeps{dir: dir, readAllowed: true, writeOn: true}

        // Sanity: the tool works while consent holds.
        _, err := runTool(t, f, `{"action":"agenda"}`)
        require.NoError(t, err)

        // Consent flips (config save, model switch — any path). Reads refuse.
        f.readAllowed = false
        _, err = runTool(t, f, `{"action":"agenda"}`)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "personal_read")
        _, err = runTool(t, f, `{"action":"range","from":"2030-01-01","to":"2030-02-01"}`)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "personal_read")

        // Reads still work; writes refuse, and nothing is written.
        f.readAllowed = true
        f.writeOn = false
        _, err = runTool(t, f, `{"action":"create","summary":"Nope","start":"2030-01-01T10:00:00Z"}`)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "personal_write")
        entries, err := os.ReadDir(dir)
        require.NoError(t, err)
        assert.Empty(t, entries, "a refused create writes no file")
}

func TestCalendarToolUnconfiguredDir_Negative(t *testing.T) {
        f := &fixtureDeps{dir: "", readAllowed: true, writeOn: true}
        _, err := runTool(t, f, `{"action":"agenda"}`)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "calendar_dir")
}

func TestCalendarToolBadInputs_Negative(t *testing.T) {
        dir := t.TempDir()
        f := &fixtureDeps{dir: dir, readAllowed: true, writeOn: true}

        for _, tc := range []struct{ name, args, wantErr string }{
                {"unknown action", `{"action":"delete"}`, "unknown action"},
                {"create without summary", `{"action":"create","start":"2030-01-01T10:00:00Z"}`, "summary"},
                {"create without start", `{"action":"create","summary":"X"}`, "start"},
                {"create bad start", `{"action":"create","summary":"X","start":"tomorrow"}`, "RFC3339"},
                {"create end before start", `{"action":"create","summary":"X","start":"2030-01-02T10:00:00Z","end":"2030-01-01T10:00:00Z"}`, "before start"},
                {"range without from", `{"action":"range","to":"2030-02-01"}`, "from"},
                {"range inverted", `{"action":"range","from":"2030-02-01","to":"2030-01-01"}`, "strictly after"},
                {"range absurd span", `{"action":"range","from":"2000-01-01","to":"2100-01-01"}`, "capped"},
                {"update without uid", `{"action":"update","summary":"X"}`, "uid"},
                {"update empty patch", `{"action":"update","uid":"x@a1"}`, "changes nothing"},
                {"update unknown uid", `{"action":"update","uid":"ghost@a1","summary":"X"}`, "no event with UID"},
        } {
                t.Run(tc.name, func(t *testing.T) {
                        _, err := runTool(t, f, tc.args)
                        require.Error(t, err)
                        assert.Contains(t, err.Error(), tc.wantErr)
                })
        }
}

// ---------- differential ----------

// TestCalendarToolFacadeMatchesLibrary_Differential: the facade must be a
// faithful projection of the library, nothing more. Every event the tool
// reports (after a create and an update, so both write paths are in play)
// is compared field-by-field against calendar.Scan's independent view of
// the same directory — different code paths, same fixtures, one truth.
func TestCalendarToolFacadeMatchesLibrary_Differential(t *testing.T) {
        dir := t.TempDir()
        f := &fixtureDeps{dir: dir, readAllowed: true, writeOn: true}
        now := time.Now().UTC()
        seed(t, dir, "seed.ics",
                icsEvent("seed-1@a1", "Timed", now.Format("20060102T150405Z"), now.Add(time.Hour).Format("20060102T150405Z")))
        seed(t, dir, "allday.ics", "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:seed-2@a1\r\nDTSTART;VALUE=DATE:"+now.AddDate(0, 0, 3).Format("20060102")+"\r\nSUMMARY:All day thing\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")

        // Two writes through the facade (dates near now so one bounded range
        // window sees everything — the cap is part of the surface under test).
        soon := now.AddDate(0, 0, 5).Format("2006-01-02")
        created, err := runTool(t, f, fmt.Sprintf(`{"action":"create","summary":"Diff me","start":%q,"end":%q,"location":"Lab"}`, soon+"T08:30:00Z", soon+"T09:00:00Z"))
        require.NoError(t, err)
        createdUID := created["event"].(map[string]any)["uid"].(string)
        _, err = runTool(t, f, fmt.Sprintf(`{"action":"update","uid":%q,"summary":"Diff me harder"}`, "seed-1@a1"))
        require.NoError(t, err)

        // The facade's agenda over a wide-but-capped window...
        body, err := runTool(t, f, fmt.Sprintf(`{"action":"range","from":%q,"to":%q}`,
                now.AddDate(0, 0, -1).Format("2006-01-02"), now.AddDate(0, 0, 30).Format("2006-01-02")))
        require.NoError(t, err)
        rawEvents, err := json.Marshal(body["events"])
        require.NoError(t, err)
        var got []eventJSON
        require.NoError(t, json.Unmarshal(rawEvents, &got))

        // ...versus the library's independent scan of the same directory.
        libEvents, _, err := calendar.Scan(dir)
        require.NoError(t, err)
        require.Len(t, libEvents, 3)

        assert.Equal(t, len(libEvents), len(got), "facade and library agree on count")
        byUID := map[string]calendar.Event{}
        for _, ev := range libEvents {
                byUID[ev.UID] = ev
        }
        for _, g := range got {
                want, ok := byUID[g.UID]
                require.True(t, ok, "facade event %q exists in the library's view", g.UID)
                assert.Equal(t, want.Summary, g.Summary, g.UID)
                assert.Equal(t, want.Location, g.Location, g.UID)
                assert.Equal(t, want.Description, g.Description, g.UID)
                assert.Equal(t, want.AllDay, g.AllDay, g.UID)
                assert.Equal(t, want.Recurring, g.Recurring, g.UID)
                assert.True(t, want.Start.Equal(mustTime(t, g.Start)), "%s: start %s vs %s", g.UID, g.Start, want.Start)
                if want.End.IsZero() {
                        assert.Empty(t, g.End, g.UID)
                } else {
                        assert.True(t, want.End.Equal(mustTime(t, g.End)), "%s: end %s vs %s", g.UID, g.End, want.End)
                }
        }
        // The update and the create both landed where the library can see them.
        assert.Equal(t, "Diff me harder", byUID["seed-1@a1"].Summary)
        assert.Equal(t, "Diff me", byUID[createdUID].Summary)
}

func mustTime(t *testing.T, s string) time.Time {
        t.Helper()
        tm, err := time.Parse(time.RFC3339, s)
        require.NoError(t, err)
        return tm
}
