package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cronSpec is a parsed 5-field cron expression. Each field is a bitmask:
// bit i set means value i is allowed. Values are stored in their native
// ranges (minutes 0-59, hours 0-23, dom 1-31, months 1-12, dow 0-6 with
// 7 normalized to 0 — both mean Sunday).
type cronSpec struct {
	minutes uint64 // bits 0-59
	hours   uint64 // bits 0-23
	doms    uint64 // bits 1-31
	months  uint64 // bits 1-12
	dows    uint64 // bits 0-6
	// starDOM/starDOW record whether the dom/dow fields were unrestricted.
	// Vixie semantics: when BOTH are restricted, a minute matches if either
	// the date or the weekday matches (so "0 0 1 * 1" fires on the 1st and
	// on every Monday).
	starDOM bool
	starDOW bool
}

type cronField struct {
	name string
	lo   int
	hi   int
	// aliases maps names (jan, mon, …) to values; nil for numeric fields.
	aliases map[string]int
}

var cronFields = [5]cronField{
	{name: "minute", lo: 0, hi: 59},
	{name: "hour", lo: 0, hi: 23},
	{name: "day-of-month", lo: 1, hi: 31},
	{name: "month", lo: 1, hi: 12, aliases: map[string]int{
		"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
		"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
	}},
	{name: "day-of-week", lo: 0, hi: 7, aliases: map[string]int{
		"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
	}},
}

// parseCron parses a 5-field expression. Accepted per field: "*", "*/n",
// "a", "a-b", "a-b/n", "a/n", and comma lists of those. Names are
// case-insensitive where the field has aliases.
func parseCron(expr string) (cronSpec, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return cronSpec{}, fmt.Errorf("schedule: cron %q: want 5 fields (minute hour dom month dow), got %d", expr, len(fields))
	}
	var spec cronSpec
	masks := [5]*uint64{&spec.minutes, &spec.hours, &spec.doms, &spec.months, &spec.dows}
	star := [5]*bool{nil, nil, &spec.starDOM, nil, &spec.starDOW}
	for i, raw := range fields {
		mask, err := parseCronField(raw, cronFields[i])
		if err != nil {
			return cronSpec{}, fmt.Errorf("schedule: cron %q: field %d (%s): %w", expr, i+1, cronFields[i].name, err)
		}
		*masks[i] = mask
		if star[i] != nil {
			*star[i] = raw == "*"
		}
	}
	// Sunday can arrive as 7; fold it onto bit 0.
	if spec.dows&(1<<7) != 0 {
		spec.dows |= 1
		spec.dows &^= 1 << 7
	}
	return spec, nil
}

func parseCronField(raw string, f cronField) (uint64, error) {
	var mask uint64
	for _, part := range strings.Split(raw, ",") {
		m, err := parseCronPart(part, f)
		if err != nil {
			return 0, err
		}
		mask |= m
	}
	if mask == 0 {
		return 0, fmt.Errorf("empty field")
	}
	return mask, nil
}

func parseCronPart(part string, f cronField) (uint64, error) {
	rangePart, step := part, 1
	if s, rest, ok := strings.Cut(part, "/"); ok {
		rangePart = s
		n, err := strconv.Atoi(rest)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("step %q must be a positive integer", rest)
		}
		step = n
	}
	lo, hi := f.lo, f.hi
	switch {
	case rangePart == "*":
		// full range
	case strings.Contains(rangePart, "-"):
		a, b, ok := strings.Cut(rangePart, "-")
		if !ok {
			return 0, fmt.Errorf("bad range %q", rangePart)
		}
		var err error
		lo, err = cronValue(a, f)
		if err != nil {
			return 0, err
		}
		hi, err = cronValue(b, f)
		if err != nil {
			return 0, err
		}
		if lo > hi {
			return 0, fmt.Errorf("range %q: start exceeds end", rangePart)
		}
	default:
		v, err := cronValue(rangePart, f)
		if err != nil {
			return 0, err
		}
		if step > 1 {
			// "a/n" means a..max every n — the single-value form only
			// exists to anchor the sequence.
			hi = f.hi
		} else {
			lo, hi = v, v
		}
	}
	var mask uint64
	for v := lo; v <= hi; v += step {
		bit := v
		if bit == 7 && f.name == "day-of-week" {
			bit = 0 // Sunday, spelled the other way
		}
		mask |= 1 << bit
	}
	return mask, nil
}

func cronValue(s string, f cronField) (int, error) {
	if f.aliases != nil {
		if v, ok := f.aliases[strings.ToLower(s)]; ok {
			return v, nil
		}
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("value %q is not a number or name", s)
	}
	if v < f.lo || v > f.hi {
		return 0, fmt.Errorf("value %d out of range %d-%d", v, f.lo, f.hi)
	}
	return v, nil
}

// matches reports whether the minute containing t satisfies the spec.
func (c cronSpec) matches(t time.Time) bool {
	minute := t.Minute()
	hour := t.Hour()
	dom := t.Day()
	month := int(t.Month())
	dow := int(t.Weekday())
	if c.minutes&(1<<minute) == 0 || c.hours&(1<<hour) == 0 || c.months&(1<<month) == 0 {
		return false
	}
	domOK := c.doms&(1<<dom) != 0
	dowOK := c.dows&(1<<dow) != 0
	switch {
	case c.starDOM && c.starDOW:
		return true
	case c.starDOM:
		return dowOK
	case c.starDOW:
		return domOK
	default:
		return domOK || dowOK // vixie OR rule
	}
}

// cronScanLimit bounds the minute-granular forward scan: two years past
// `after` with no match means the expression never fires (e.g. Feb 30).
const cronScanLimit = 2 * 366 * 24 * 60

// next scans forward minute by minute for the first match after t. The scan
// is bounded; a miss returns (zero, false).
func (c cronSpec) next(t time.Time) (time.Time, bool) {
	// Candidate times are minute-aligned and strictly after t. Work in the
	// schedule's location so DST folds behave like the local cron(8).
	cand := t.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < cronScanLimit; i++ {
		if c.matches(cand) {
			return cand, true
		}
		cand = cand.Add(time.Minute)
	}
	return time.Time{}, false
}
