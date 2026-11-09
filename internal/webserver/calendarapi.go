package webserver

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/calendar"
)

// calendarapi.go serves the dashboard's agenda widget (chunk 2): GET
// /api/personal/calendar (?days=N — the next N days from today's local
// midnight, soonest first) and GET /api/personal/calendar/range
// (?from=&to= — the month-grid window). Both compose personalGate at
// registration: the master switch is enforced server-side, never just
// hidden in the UI. Reads only — the WEB surface never writes the
// calendar (the agent tool owns the write face, behind the model's
// personal_write opt-in); this file would not know how.
//
// Hostile inputs fall back, never error: the usage endpoint's doctrine
// (unparsable ⇒ default, oversize ⇒ clamp), because a widget's fetch is
// not a place to teach anyone a lesson. Degraded directories (unreadable
// files, malformed lines) answer 200 with honest stats — one bad .ics
// must not take the agenda down.

const (
	// calendarDefaultDays is the agenda card's window ("next 14 days").
	calendarDefaultDays = 14
	// calendarMaxDays bounds a hostile ?days=.
	calendarMaxDays = 90
	// calendarRangeMaxDays bounds a range window (~13 months).
	calendarRangeMaxDays = 400
)

// calendarEventJSON is one agenda event over the wire. Times are RFC3339
// instants; all-day events carry their local-midnight anchors.
type calendarEventJSON struct {
	UID         string   `json:"uid"`
	Summary     string   `json:"summary"`
	Location    string   `json:"location,omitempty"`
	Description string   `json:"description,omitempty"`
	Start       string   `json:"start"`
	End         string   `json:"end,omitempty"`
	AllDay      bool     `json:"allDay"`
	Recurring   bool     `json:"recurring"`
	Warnings    []string `json:"warnings,omitempty"`
}

// calendarStatsJSON is the honest accounting: what the scan saw, not just
// what it served. "degraded" is the one-line summary surfaces check.
type calendarStatsJSON struct {
	Files         int  `json:"files"`
	SkippedLines  int  `json:"skippedLines"`
	SkippedEvents int  `json:"skippedEvents"`
	SkippedFiles  int  `json:"skippedFiles"`
	Degraded      bool `json:"degraded"`
}

// calendarResponse is the shared payload of both endpoints.
type calendarResponse struct {
	Configured bool                `json:"configured"`
	From       string              `json:"from"`
	To         string              `json:"to"`
	Events     []calendarEventJSON `json:"events"`
	Stats      calendarStatsJSON   `json:"stats"`
}

// handleCalendarAgenda serves the widget's default window. days <= 0 or
// unparsable falls back to 14; > 90 clamps to 90.
func (s *Server) handleCalendarAgenda(w http.ResponseWriter, r *http.Request) {
	days := calendarDefaultDays
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			switch {
			case n <= 0:
				// hostile or sloppy: the default
			case n > calendarMaxDays:
				days = calendarMaxDays
			default:
				days = n
			}
		}
	}
	from := calendar.LocalMidnight(time.Now())
	s.serveCalendarWindow(w, from, from.AddDate(0, 0, days))
}

// handleCalendarRange serves an explicit [from, to) window. Malformed or
// missing bounds fall back (from ⇒ today, to ⇒ from + 30 days); inverted
// or oversize windows normalize to one day / the 400-day cap.
func (s *Server) handleCalendarRange(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from := calendar.LocalMidnight(time.Now())
	if t, ok := parseCalendarDay(q.Get("from")); ok {
		from = calendar.LocalMidnight(t)
	}
	to := from.AddDate(0, 0, 30)
	if t, ok := parseCalendarDay(q.Get("to")); ok {
		to = calendar.LocalMidnight(t)
	}
	if !to.After(from) {
		to = from.AddDate(0, 0, 1)
	}
	if to.Sub(from) > time.Duration(calendarRangeMaxDays)*24*time.Hour {
		to = from.AddDate(0, 0, calendarRangeMaxDays)
	}
	s.serveCalendarWindow(w, from, to)
}

// parseCalendarDay accepts YYYY-MM-DD or a full RFC3339 instant, day-wise.
func parseCalendarDay(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if len(s) == 10 {
		if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
			return t, true
		}
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// serveCalendarWindow is the shared body: scan, filter, marshal, honest
// stats. A missing or hostile directory is an empty calendar with the
// degraded flag — never a 500.
func (s *Server) serveCalendarWindow(w http.ResponseWriter, from, to time.Time) {
	dir := s.personalConfig().CalendarDir
	evs, st, err := calendar.Agenda(dir, from, to)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "calendar: "+err.Error())
		return
	}
	out := make([]calendarEventJSON, 0, len(evs))
	for _, ev := range evs {
		e := calendarEventJSON{
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
			e.End = ev.End.Format(time.RFC3339)
		}
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, calendarResponse{
		Configured: strings.TrimSpace(dir) != "",
		From:       from.Format(time.RFC3339),
		To:         to.Format(time.RFC3339),
		Events:     out,
		Stats: calendarStatsJSON{
			Files:         st.Files,
			SkippedLines:  st.SkippedLines,
			SkippedEvents: st.SkippedEvents,
			SkippedFiles:  st.SkippedFiles,
			Degraded:      st.Degraded(),
		},
	})
}
