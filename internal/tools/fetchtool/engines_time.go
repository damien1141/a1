package fetchtool

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// engines_time.go — the time/date subtool.
//
// The wall clock used to ride the runtime-context rider: a fresh stamp
// every turn ("Current date and time: Monday, 2 June 2006 (15:04:05)…")
// persisted into history, where it stayed at cache-read price forever. As
// a fetch engine it costs zero until the model actually needs the clock —
// and the answer it gets is fresher than any injected stamp, because it is
// rendered at fetch time, not at turn start.
//
// Local queries ("what time is it") need no network at all. Zone queries
// ("what's the time in Tokyo") geocode the place through the shared
// Open-Meteo cache and render in the zone it returns; a geocode failure
// degrades to the local answer rather than erroring — half an answer beats
// none, and the router would otherwise drop the direct answer entirely.

// timeEngine answers current date/time queries.
type timeEngine struct{}

func newTimeEngine() *timeEngine { return &timeEngine{} }

func (*timeEngine) Name() string { return "time" }

// timeQueryRe is deliberately tight. The time engine always produces an
// answer, so a false positive is sticky — the router never falls through
// once this engine matches. Ambiguous shapes ("what is the time complexity
// of quicksort", "compile time in go") must NOT match, so the bare
// "what is the time/date" alternatives are end-anchored ($) and the
// "time in <place>" alternative requires the question head ("what time in
// Tokyo"). False negatives fall to web search, which is survivable.
var timeQueryRe = regexp.MustCompile(`(?i)` +
	`\bwhat time is it\b` +
	`|\bwhat(?:'s| is)? (?:the )?time\s*(?:now|right now)?\s*[?.!]*$` +
	`|\bwhat(?:'s| is)? (?:the |today'?s )?date\s*(?:today|now|is it)?\s*[?.!]*$` +
	`|\bwhat day is it\b` +
	`|\bwhat day of the week\b` +
	`|\bwhat (?:month|year|week) is it\b` +
	`|\btime now\b` +
	`|\bcurrent time\b` +
	`|\bcurrent date\b` +
	`|\blocal time\b` +
	`|\btoday'?s date\b` +
	`|\bdate today\b` +
	`|\bwhat(?:'s| is)?(?: the)? time in\s` +
	`|\biso week\b` +
	`|\butc (?:time|clock)\b`)

func (e *timeEngine) Matches(query string) bool {
	return timeQueryRe.MatchString(query)
}

// timeLocRe captures the place in "what's the time in Tokyo".
var timeLocRe = regexp.MustCompile(`(?i)\btime\s+(?:in|at|for)\s+([^?]+?)\s*[?.!]*$`)

// timeLocation extracts the place name from a time query; "" when the
// query names none (answer in the local zone).
func timeLocation(query string) string {
	m := timeLocRe.FindStringSubmatch(strings.TrimSpace(query))
	if len(m) != 2 {
		return ""
	}
	place := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(m[1]), "?.!"))
	for _, tail := range []string{"right now", "now", "please", "currently"} {
		if lower := strings.ToLower(place); strings.HasSuffix(lower, " "+tail) {
			place = strings.TrimSpace(place[:len(place)-len(tail)])
		}
	}
	if len(place) < 2 {
		return ""
	}
	if len(place) > 60 {
		return ""
	}
	return place
}

// Search renders the current date/time. Zone queries geocode the place and
// answer in its zone; the local zone answers everything else.
func (e *timeEngine) Search(ctx context.Context, query string, _ searchOptions) (*SearchResponse, error) {
	now := time.Now()
	answer := formatNow(now, "", "")
	if place := timeLocation(query); place != "" {
		if geo, err := geocodePlace(ctx, place); err == nil && geo.timezone != "" {
			if loc, lerr := time.LoadLocation(geo.timezone); lerr == nil {
				answer = formatNow(now.In(loc), geo.label, geo.timezone)
			}
		}
		// Geocode or zone-load failure: the local-zone answer above stands.
		// Erroring would drop the direct answer entirely — worse than
		// answering in the wrong zone with the zone named.
	}
	return &SearchResponse{
		Engine: e.Name(),
		Query:  query,
		Answer: answer,
	}, nil
}

// formatNow renders one clock reading. The line format is the one the old
// rider injection used (weekday spelled out because models misjudge
// day-of-week from a bare date; ISO week pins "this week" for relative
// phrasing; the zone named so the model knows which clock it read).
// place/timezone are set for zone queries ("Current date and time in
// Tokyo … zone JST (Asia/Tokyo)"); a non-zero UTC offset appends the UTC
// line so cross-zone math never needs a second fetch.
func formatNow(now time.Time, place, timezone string) string {
	year, week := now.ISOWeek()
	head := "Current date and time"
	if place != "" {
		head += " in " + place
	}
	zone := now.Format("MST")
	if timezone != "" {
		zone = fmt.Sprintf("%s (%s)", zone, timezone)
	}
	s := fmt.Sprintf(
		"%s: %s (%s), %s (ISO week %d-W%02d, zone %s).",
		head,
		now.Format("Monday, 2 January 2006"),
		now.Format("15:04:05"),
		now.Format("2006-01-02"),
		year, week,
		zone,
	)
	if _, offset := now.Zone(); offset != 0 {
		utc := now.UTC()
		s += fmt.Sprintf("\nUTC: %s (%s), %s.",
			utc.Format("Monday, 2 January 2006"),
			utc.Format("15:04:05 UTC"),
			utc.Format("2006-01-02"),
		)
	}
	return s
}
