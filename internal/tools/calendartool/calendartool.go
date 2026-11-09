// Package calendartool exposes the personal calendar to the agent: one
// `personal_calendar` facade with an action enum (agenda | range | create |
// update), the scheduletool shape. It reads and writes the local .ics
// directory through internal/calendar and is governed by chunk 0's flags:
// the master switch (personal.enabled) plus the active model's
// personal_read / personal_write opt-ins.
//
// The flags are enforced TWICE, by design. The engine mounts the tool only
// when the switch is on and the model opted into reads; and every call
// re-reads the LIVE state — a mis-mounted tool (a stale mount after a
// config flip, a hand-built engine, a future wiring mistake) still cannot
// read or write anything the operator did not consent to.
package calendartool

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/calendar"
        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/tools/tooldef"
)

const calendarGuidance = `Read and manage the user's personal calendar (a local directory of .ics files — the same one the dashboard's agenda widget shows).

Pick ONE action:
- agenda: the next N days (default 14), today included, soonest first.
- range: events between two dates, e.g. from=2026-12-01&to=2026-12-31.
- create: add an event (summary + start required; end, all_day, location, description optional). Returns the new uid.
- update: patch an existing event by uid (any of summary, start, end, all_day, location, description). Untouched fields stay as they are.

Times: "2026-12-01T09:00:00Z" (an instant, RFC3339) or "2026-12-01" (a local date — for all-day events). A bare date as the start of a create makes the event all-day unless all_day says otherwise.

Reads need the model's personal_read; create/update need personal_write. If a call is refused, say so — do not retry with different arguments.`

// Deps wires the facade to the live configuration. All three funcs are read
// at CALL time (the mount-time check is a convenience, not the enforcement):
// CalendarDir returns the live directory ("" = unconfigured), and the two
// Allowed funcs fold the master switch together with the active model's
// opt-in flags. Nil funcs are treated as locked — the safe default.
type Deps struct {
        CalendarDir  func() string
        ReadAllowed  func() bool
        WriteAllowed func() bool
}

// CalendarTool returns the personal_calendar facade (nil CalendarDir → nil).
func CalendarTool(deps Deps) tooldef.Tool {
        if deps.CalendarDir == nil {
                return tooldef.Tool{}
        }
        readAllowed := deps.ReadAllowed
        if readAllowed == nil {
                readAllowed = func() bool { return false }
        }
        writeAllowed := deps.WriteAllowed
        if writeAllowed == nil {
                writeAllowed = func() bool { return false }
        }
        return tooldef.Tool{
                Definition: llm.ToolDefinition{
                        Name:        "personal_calendar",
                        Description: calendarGuidance,
                        Params: &llm.FunctionParameters{
                                Type: "object",
                                Properties: llm.Object{
                                        "action": llm.Object{
                                                "type":        "string",
                                                "description": "agenda | range | create | update",
                                                "enum":        []string{"agenda", "range", "create", "update"},
                                        },
                                        "days": llm.Object{
                                                "type":        "integer",
                                                "description": "agenda only: how many days to show, today included (default 14, clamped to 120).",
                                        },
                                        "from": llm.Object{
                                                "type":        "string",
                                                "description": "range only: window start, YYYY-MM-DD (an RFC3339 instant is accepted too).",
                                        },
                                        "to": llm.Object{
                                                "type":        "string",
                                                "description": "range only: window end, exclusive (same formats as from).",
                                        },
                                        "uid": llm.Object{
                                                "type":        "string",
                                                "description": "update only: the event's uid, as returned by agenda/create.",
                                        },
                                        "summary": llm.Object{
                                                "type":        "string",
                                                "description": "create (required) / update: the event title.",
                                        },
                                        "start": llm.Object{
                                                "type":        "string",
                                                "description": "create (required) / update: RFC3339 instant, or YYYY-MM-DD for all-day.",
                                        },
                                        "end": llm.Object{
                                                "type":        "string",
                                                "description": "create / update: the exclusive end (same formats; omitted = ends when it starts, or next midnight for all-day).",
                                        },
                                        "all_day": llm.Object{
                                                "type":        "boolean",
                                                "description": "create / update: force all-day (a bare-date start implies it).",
                                        },
                                        "location": llm.Object{
                                                "type":        "string",
                                                "description": "create / update: where (empty string on update clears it).",
                                        },
                                        "description": llm.Object{
                                                "type":        "string",
                                                "description": "create / update: free-text notes (empty string on update clears it).",
                                        },
                                },
                                Required: []string{"action"},
                        },
                },
                DetailFromArgs: calendarDetail,
                Run: func(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
                        var in calInput
                        if err := json.Unmarshal(input, &in); err != nil {
                                return tooldef.Result{}, err
                        }
                        switch strings.ToLower(strings.TrimSpace(in.Action)) {
                        case "agenda":
                                return runAgenda(deps.CalendarDir, readAllowed, in)
                        case "range":
                                return runRange(deps.CalendarDir, readAllowed, in)
                        case "create":
                                return runCreate(deps.CalendarDir, writeAllowed, in)
                        case "update":
                                return runUpdate(deps.CalendarDir, writeAllowed, in)
                        default:
                                return tooldef.Result{}, fmt.Errorf("personal_calendar: unknown action %q (want agenda, range, create, or update)", in.Action)
                        }
                },
        }
}

// calInput is the superset of every action's arguments; each action reads
// the fields it owns and ignores the rest (the schema documents ownership).
// The three text fields are pointers so update can distinguish "absent
// (untouched)" from "present but empty (clear it)" — create just requires
// a non-nil, non-blank summary.
type calInput struct {
        Action      string  `json:"action"`
        Days        int     `json:"days"`
        From        string  `json:"from"`
        To          string  `json:"to"`
        UID         string  `json:"uid"`
        Summary     *string `json:"summary"`
        Start       string  `json:"start"`
        End         string  `json:"end"`
        AllDay      *bool   `json:"all_day"`
        Location    *string `json:"location"`
        Description *string `json:"description"`
}

// ---------- shared plumbing ----------

// requireRead is the call-time read gate.
func requireRead(dirFn func() string, allowed func() bool) (string, error) {
        if !allowed() {
                return "", errors.New("personal_calendar: reads are not enabled for this model (personal.enabled plus the model's personal_read flag in config.yaml)")
        }
        return requireDir(dirFn)
}

// requireWrite is the call-time write gate.
func requireWrite(dirFn func() string, allowed func() bool) (string, error) {
        if !allowed() {
                return "", errors.New("personal_calendar: writes are not enabled for this model (personal.enabled plus the model's personal_write flag in config.yaml)")
        }
        return requireDir(dirFn)
}

func requireDir(dirFn func() string) (string, error) {
        dir := strings.TrimSpace(dirFn())
        if dir == "" {
                return "", errors.New("personal_calendar: no calendar directory is configured (personal.calendar_dir in config.yaml)")
        }
        return dir, nil
}

// parseToolTime decodes a user/model-supplied time: RFC3339 instants, or
// bare YYYY-MM-DD dates (local midnight, all-day by nature). The second
// return says whether the value was date-only.
func parseToolTime(s string) (time.Time, bool, error) {
        s = strings.TrimSpace(s)
        if s == "" {
                return time.Time{}, false, errors.New("empty time")
        }
        if len(s) == 10 && s[4] == '-' && s[7] == '-' {
                t, err := time.ParseInLocation("2006-01-02", s, time.Local)
                if err != nil {
                        return time.Time{}, false, fmt.Errorf("%q is not a YYYY-MM-DD date", s)
                }
                return t, true, nil
        }
        t, err := time.Parse(time.RFC3339, s)
        if err != nil {
                return time.Time{}, false, fmt.Errorf("%q is neither RFC3339 nor YYYY-MM-DD", s)
        }
        return t, false, nil
}

// eventJSON is one event as the model sees it. File paths are deliberately
// omitted — the model operates on uids, not on the filesystem.
type eventJSON struct {
        UID         string   `json:"uid"`
        Summary     string   `json:"summary"`
        Location    string   `json:"location,omitempty"`
        Description string   `json:"description,omitempty"`
        Start       string   `json:"start"`
        End         string   `json:"end,omitempty"`
        AllDay      bool     `json:"all_day"`
        Recurring   bool     `json:"recurring"`
        Warnings    []string `json:"warnings,omitempty"`
}

func eventToJSON(ev calendar.Event) eventJSON {
        out := eventJSON{
                UID:         ev.UID,
                Summary:     ev.Summary,
                Location:    ev.Location,
                Description: ev.Description,
                Start:       ev.Start.Format(time.RFC3339),
                AllDay:      ev.AllDay,
                Recurring:   ev.Recurring,
                Warnings:    ev.Warnings,
        }
        if !ev.End.IsZero() {
                out.End = ev.End.Format(time.RFC3339)
        }
        return out
}

func statsJSON(st calendar.Stats) map[string]any {
        return map[string]any{
                "files":          st.Files,
                "skipped_lines":  st.SkippedLines,
                "skipped_events": st.SkippedEvents,
                "skipped_files":  st.SkippedFiles,
                "degraded":       st.Degraded(),
        }
}

func jsonResult(body map[string]any, detail string) (tooldef.Result, error) {
        b, err := json.MarshalIndent(body, "", "  ")
        if err != nil {
                return tooldef.Result{}, err
        }
        return tooldef.Result{Content: string(b), Detail: detail, Output: string(b)}, nil
}

// ---------- reads ----------

// agendaDaysDefault / agendaDaysMax bound the agenda window.
const (
        agendaDaysDefault = 14
        agendaDaysMax     = 120
        // rangeMaxDays bounds a range window (~13 months): the facade answers
        // calendar questions, not filesystem dumps.
        rangeMaxDays = 400
)

func runAgenda(dirFn func() string, allowed func() bool, in calInput) (tooldef.Result, error) {
        dir, err := requireRead(dirFn, allowed)
        if err != nil {
                return tooldef.Result{}, err
        }
        days := in.Days
        if days <= 0 {
                days = agendaDaysDefault
        }
        if days > agendaDaysMax {
                days = agendaDaysMax
        }
        from := calendar.LocalMidnight(time.Now())
        to := from.AddDate(0, 0, days)
        return listEvents(dir, from, to, fmt.Sprintf("agenda %dd", days))
}

func runRange(dirFn func() string, allowed func() bool, in calInput) (tooldef.Result, error) {
        dir, err := requireRead(dirFn, allowed)
        if err != nil {
                return tooldef.Result{}, err
        }
        fromStr := strings.TrimSpace(in.From)
        if fromStr == "" {
                return tooldef.Result{}, errors.New("personal_calendar: range requires from (YYYY-MM-DD)")
        }
        from, _, err := parseToolTime(fromStr)
        if err != nil {
                return tooldef.Result{}, fmt.Errorf("personal_calendar: from: %w", err)
        }
        from = calendar.LocalMidnight(from)
        toStr := strings.TrimSpace(in.To)
        if toStr == "" {
                return tooldef.Result{}, errors.New("personal_calendar: range requires to (YYYY-MM-DD)")
        }
        to, _, err := parseToolTime(toStr)
        if err != nil {
                return tooldef.Result{}, fmt.Errorf("personal_calendar: to: %w", err)
        }
        to = calendar.LocalMidnight(to)
        if !to.After(from) {
                return tooldef.Result{}, errors.New("personal_calendar: range requires to strictly after from")
        }
        if to.Sub(from) > time.Duration(rangeMaxDays)*24*time.Hour {
                return tooldef.Result{}, fmt.Errorf("personal_calendar: range is capped at %d days", rangeMaxDays)
        }
        return listEvents(dir, from, to, fmt.Sprintf("range %s→%s", from.Format("2006-01-02"), to.Format("2006-01-02")))
}

// listEvents is the shared read path: window, marshal, honest stats.
func listEvents(dir string, from, to time.Time, detail string) (tooldef.Result, error) {
        evs, st, err := calendar.Agenda(dir, from, to)
        if err != nil {
                return tooldef.Result{}, err
        }
        out := make([]eventJSON, 0, len(evs))
        for _, ev := range evs {
                out = append(out, eventToJSON(ev))
        }
        body := map[string]any{
                "from":   from.Format(time.RFC3339),
                "to":     to.Format(time.RFC3339),
                "count":  len(out),
                "events": out,
                "stats":  statsJSON(st),
        }
        return jsonResult(body, detail)
}

// ---------- writes ----------

func runCreate(dirFn func() string, allowed func() bool, in calInput) (tooldef.Result, error) {
        dir, err := requireWrite(dirFn, allowed)
        if err != nil {
                return tooldef.Result{}, err
        }
        if in.Summary == nil || strings.TrimSpace(*in.Summary) == "" {
                return tooldef.Result{}, errors.New("personal_calendar: create requires a summary")
        }
        if strings.TrimSpace(in.Start) == "" {
                return tooldef.Result{}, errors.New("personal_calendar: create requires a start")
        }
        start, dateOnly, err := parseToolTime(in.Start)
        if err != nil {
                return tooldef.Result{}, fmt.Errorf("personal_calendar: start: %w", err)
        }
        ev := calendar.Event{
                Summary:     strings.TrimSpace(*in.Summary),
                Start:       start,
        }
        if in.Location != nil {
                ev.Location = strings.TrimSpace(*in.Location)
        }
        if in.Description != nil {
                ev.Description = strings.TrimSpace(*in.Description)
        }
        switch {
        case in.AllDay != nil:
                ev.AllDay = *in.AllDay
        case dateOnly:
                ev.AllDay = true // a bare-date start is an all-day event by nature
        }
        if endStr := strings.TrimSpace(in.End); endStr != "" {
                end, _, err := parseToolTime(endStr)
                if err != nil {
                        return tooldef.Result{}, fmt.Errorf("personal_calendar: end: %w", err)
                }
                if ev.AllDay {
                        // All-day ends are dates: snap the supplied instant to its day.
                        end = calendar.LocalMidnight(end)
                }
                ev.End = end
        }
        created, err := calendar.Create(dir, ev)
        if err != nil {
                return tooldef.Result{}, err
        }
        return jsonResult(map[string]any{
                "created": true,
                "event":   eventToJSON(created),
        }, "create "+truncate(created.Summary, 60))
}

func runUpdate(dirFn func() string, allowed func() bool, in calInput) (tooldef.Result, error) {
        dir, err := requireWrite(dirFn, allowed)
        if err != nil {
                return tooldef.Result{}, err
        }
        uid := strings.TrimSpace(in.UID)
        if uid == "" {
                return tooldef.Result{}, errors.New("personal_calendar: update requires a uid")
        }
        p := calendar.Patch{}
        // Presence vs emptiness: a nil field is untouched, a non-nil field is
        // written as-is (so an explicit "" clears a text property, matching
        // calendar.Update's delete-on-empty semantics).
        p.Summary = in.Summary
        p.Location = in.Location
        p.Description = in.Description
        if in.Start != "" {
                start, _, err := parseToolTime(in.Start)
                if err != nil {
                        return tooldef.Result{}, fmt.Errorf("personal_calendar: start: %w", err)
                }
                p.Start = &start
        }
        if in.End != "" {
                end, _, err := parseToolTime(in.End)
                if err != nil {
                        return tooldef.Result{}, fmt.Errorf("personal_calendar: end: %w", err)
                }
                p.End = &end
        }
        p.AllDay = in.AllDay
        if p.IsEmpty() {
                return tooldef.Result{}, errors.New("personal_calendar: update changes nothing (pass at least one of summary, start, end, all_day, location, description)")
        }
        updated, err := calendar.Update(dir, uid, p)
        if err != nil {
                return tooldef.Result{}, err
        }
        return jsonResult(map[string]any{
                "updated": true,
                "event":   eventToJSON(updated),
        }, "update "+truncate(updated.Summary, 60))
}

// ---------- UI detail ----------

func calendarDetail(input json.RawMessage) string {
        var in calInput
        _ = json.Unmarshal(input, &in)
        str := func(p *string) string {
                if p == nil {
                        return ""
                }
                return *p
        }
        switch strings.ToLower(strings.TrimSpace(in.Action)) {
        case "agenda":
                if in.Days > 0 {
                        return fmt.Sprintf("agenda %dd", in.Days)
                }
                return "agenda 14d"
        case "range":
                return fmt.Sprintf("range %s→%s", in.From, in.To)
        case "create":
                return "create " + truncate(str(in.Summary), 60)
        case "update":
                return "update " + truncate(in.UID, 40)
        default:
                return "personal_calendar"
        }
}

func truncate(s string, n int) string {
        if len(s) <= n {
                return s
        }
        return s[:n] + "…"
}
