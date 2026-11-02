// Package schedule owns the Clock's wake-up grammar (Bet 01): durable
// schedules with three trigger kinds — cron, fixed_rate, one_time — whose
// fires submit prompts to a host-supplied callback. In practice the callback
// spawns a job through the fleet's job manager, so wake-ups reuse its FIFO
// backpressure, timeouts, and persistence, and run as worker-role engines
// (headless-strict) unless a schedule says otherwise.
//
// Design notes:
//
//   - No new loop entry point: a fire is just a prompt submission. The
//     engine's Loop runs it through EmitUserInput like any user turn, so
//     extensions intercept wake-ups exactly as they do interactive input.
//   - Schedules persist as rows in the vault (the schedules table, created
//     lazily on the caller's *sql.DB handle — the same pattern the memstore
//     namespaces use), so the daemon reloads them on restart. config.yaml
//     `schedules:` entries are synced into the same table at boot and win
//     id conflicts: config is the authored source.
//   - Cron is a hand-rolled 5-field parser (minute hour dom month dow)
//     supporting *, */n, a, a-b, a-b/n, comma lists, and 3-letter
//     day/month names. The dom/dow pair keeps vixie semantics: when both
//     are restricted, either matching is enough.
package schedule

import (
	"fmt"
	"strings"
	"time"
)

// Trigger is when a schedule fires. Exactly one field is set.
type Trigger struct {
	// Cron is a 5-field expression (minute hour day-of-month month
	// day-of-week), minute granularity.
	Cron string
	// FixedRate fires every duration (aligned to the schedule's creation).
	FixedRate time.Duration
	// OneTime fires once at this instant, then the schedule disables itself.
	OneTime time.Time
}

// Trigger kinds.
const (
	KindCron      = "cron"
	KindFixedRate = "fixed_rate"
	KindOneTime   = "one_time"
)

// Kind reports which trigger field is set ("" when none).
func (t Trigger) Kind() string {
	switch {
	case t.Cron != "":
		return KindCron
	case t.FixedRate > 0:
		return KindFixedRate
	case !t.OneTime.IsZero():
		return KindOneTime
	default:
		return ""
	}
}

// Empty reports whether no trigger is configured.
func (t Trigger) Empty() bool { return t.Kind() == "" }

// ParseTrigger builds a Trigger from its three string forms: cron
// expression, duration string ("15m", "2h30m"), and RFC3339 timestamp.
// Exactly one must be non-empty. OneTime is accepted even in the past —
// Next then reports no future fire and the scheduler disables the schedule
// on its first pass.
func ParseTrigger(cron, fixedRate, oneTime string) (Trigger, error) {
	cron = strings.TrimSpace(cron)
	fixedRate = strings.TrimSpace(fixedRate)
	oneTime = strings.TrimSpace(oneTime)
	set := 0
	for _, s := range []string{cron, fixedRate, oneTime} {
		if s != "" {
			set++
		}
	}
	if set == 0 {
		return Trigger{}, fmt.Errorf("schedule: exactly one of cron, fixed_rate, one_time is required")
	}
	if set > 1 {
		return Trigger{}, fmt.Errorf("schedule: at most one of cron, fixed_rate, one_time may be set")
	}
	switch {
	case cron != "":
		if _, err := parseCron(cron); err != nil {
			return Trigger{}, err
		}
		return Trigger{Cron: cron}, nil
	case fixedRate != "":
		d, err := time.ParseDuration(fixedRate)
		if err != nil {
			return Trigger{}, fmt.Errorf("schedule: fixed_rate %q: %w", fixedRate, err)
		}
		if d <= 0 {
			return Trigger{}, fmt.Errorf("schedule: fixed_rate must be positive, got %s", fixedRate)
		}
		return Trigger{FixedRate: d}, nil
	default:
		ts, err := time.Parse(time.RFC3339, oneTime)
		if err != nil {
			return Trigger{}, fmt.Errorf("schedule: one_time %q: %w", oneTime, err)
		}
		return Trigger{OneTime: ts}, nil
	}
}

// Next computes the next fire time strictly after `after`. The second
// return is false when no future fire exists (a one_time in the past).
func (t Trigger) Next(after time.Time) (time.Time, bool) {
	switch t.Kind() {
	case KindCron:
		spec, err := parseCron(t.Cron)
		if err != nil {
			return time.Time{}, false
		}
		return spec.next(after)
	case KindFixedRate:
		return after.Add(t.FixedRate), true
	case KindOneTime:
		if t.OneTime.After(after) {
			return t.OneTime, true
		}
		return time.Time{}, false
	default:
		return time.Time{}, false
	}
}

// Summary renders the trigger for listings ("cron 30 3 * * *", "every 15m",
// "once 2026-12-01T09:00:00Z").
func (t Trigger) Summary() string {
	switch t.Kind() {
	case KindCron:
		return "cron " + t.Cron
	case KindFixedRate:
		return "every " + t.FixedRate.String()
	case KindOneTime:
		return "once " + t.OneTime.Format(time.RFC3339)
	default:
		return "no trigger"
	}
}
