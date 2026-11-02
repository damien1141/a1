package schedule

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"time"
)

// SubmitFunc receives each fire. The host wires it to whatever runs the
// wake-up — in practice a job.Manager spawn with the schedule's prompt,
// role, and contract, so wake-ups reuse the fleet's backpressure and
// persistence and enter the engine through Loop (EmitUserInput) like any
// user turn.
type SubmitFunc func(ctx context.Context, sc Schedule) error

// Scheduler owns the live schedule set: CRUD, persistence, and the ticker
// that fires due schedules. Safe for concurrent use.
type Scheduler struct {
	store  *store
	submit SubmitFunc

	mu     sync.Mutex
	scheds map[string]Schedule

	now  func() time.Time
	tick time.Duration
	log  func(format string, args ...any)
}

// Option configures a Scheduler.
type Option func(*Scheduler)

// WithTick sets the due-check cadence (default 10s; cron granularity is
// one minute, so a faster tick only sharpens fixed_rate/one_time fires).
func WithTick(d time.Duration) Option {
	return func(s *Scheduler) {
		if d > 0 {
			s.tick = d
		}
	}
}

// WithNow overrides the clock (tests). The ticker still runs in real time;
// due-ness is computed against this clock.
func WithNow(fn func() time.Time) Option {
	return func(s *Scheduler) { s.now = fn }
}

// WithLogger installs a diagnostics sink (fire/failure notices).
func WithLogger(fn func(format string, args ...any)) Option {
	return func(s *Scheduler) { s.log = fn }
}

// New builds a Scheduler over the persistence handle (nil db = memory
// only, the degraded mode when the vault failed to boot) and loads any
// persisted schedules.
func New(db *sql.DB, submit SubmitFunc, opts ...Option) (*Scheduler, error) {
	if submit == nil {
		return nil, fmt.Errorf("schedule: submit callback is required")
	}
	st, err := newStore(db)
	if err != nil {
		return nil, err
	}
	s := &Scheduler{
		store:  st,
		submit: submit,
		scheds: map[string]Schedule{},
		now:    time.Now,
		tick:   10 * time.Second,
		log:    func(string, ...any) {},
	}
	for _, opt := range opts {
		opt(s)
	}
	persisted, err := st.list()
	if err != nil {
		return nil, err
	}
	for _, sc := range persisted {
		s.scheds[sc.ID] = sc
	}
	return s, nil
}

// Upsert creates or updates a schedule. An empty ID is assigned one;
// fire times are (re)computed from the trigger against the current clock.
// The persisted row is written even when the db handle is nil (memory-only
// degraded mode keeps the schedule alive for this process).
func (s *Scheduler) Upsert(ctx context.Context, sc Schedule) (Schedule, error) {
	if err := ctx.Err(); err != nil {
		return Schedule{}, err
	}
	if sc.Prompt == "" {
		return Schedule{}, fmt.Errorf("schedule: prompt is required")
	}
	if sc.Trigger.Empty() {
		return Schedule{}, fmt.Errorf("schedule: trigger is required")
	}
	now := s.now()
	if sc.ID == "" {
		id, err := newScheduleID()
		if err != nil {
			return Schedule{}, err
		}
		sc.ID = id
		sc.CreatedAt = now
	}
	if sc.CreatedAt.IsZero() {
		sc.CreatedAt = now
	}
	if sc.Source == "" {
		sc.Source = "tool"
	}
	sc.UpdatedAt = now
	next, ok := sc.Trigger.Next(now)
	switch {
	case !ok:
		// A one_time already in the past: keep the row (history) but it
		// will disable itself on the scheduler's first pass.
		sc.NextRunAt = time.Time{}
	default:
		sc.NextRunAt = next
	}

	s.mu.Lock()
	old, existed := s.scheds[sc.ID]
	if existed {
		sc.CreatedAt = old.CreatedAt
		if sc.LastRunAt.IsZero() {
			sc.LastRunAt = old.LastRunAt
		}
	} else {
		// New schedules are born enabled — disabling is Cancel's job, so a
		// caller that forgets the flag cannot create a dead schedule. A
		// config entry declared enabled:false never reaches Upsert (the
		// converter drops it; Sync's orphan-disable retires any row it
		// previously owned).
		sc.Enabled = true
	}
	s.scheds[sc.ID] = sc
	s.mu.Unlock()

	if err := s.store.put(sc); err != nil {
		return Schedule{}, err
	}
	return sc, nil
}

// List returns the live schedules sorted by next fire time (zero/never
// last).
func (s *Scheduler) List() []Schedule {
	s.mu.Lock()
	out := make([]Schedule, 0, len(s.scheds))
	for _, sc := range s.scheds {
		out = append(out, sc)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].NextRunAt, out[j].NextRunAt
		if a.IsZero() {
			return false
		}
		if b.IsZero() {
			return true
		}
		return a.Before(b)
	})
	return out
}

// Get returns one schedule by id.
func (s *Scheduler) Get(id string) (Schedule, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc, ok := s.scheds[id]
	return sc, ok
}

// Cancel disables a schedule (the row stays as history, like a terminal
// job — deletion is the store's business, not the tool's).
func (s *Scheduler) Cancel(id string) (Schedule, error) {
	s.mu.Lock()
	sc, ok := s.scheds[id]
	if ok {
		sc.Enabled = false
		sc.UpdatedAt = s.now()
		s.scheds[id] = sc
	}
	s.mu.Unlock()
	if !ok {
		return Schedule{}, fmt.Errorf("schedule: unknown id %s", id)
	}
	if err := s.store.put(sc); err != nil {
		return Schedule{}, err
	}
	return sc, nil
}

// Sync folds config-declared schedules into the live set: config entries
// are upserted (winning id conflicts — config is the authored source), and
// config-sourced rows whose ids disappeared from the config are disabled.
// Tool-created schedules are never touched.
func (s *Scheduler) Sync(ctx context.Context, fromConfig []Schedule) error {
	s.mu.Lock()
	seen := map[string]bool{}
	for _, sc := range fromConfig {
		seen[sc.ID] = true
	}
	var orphans []string
	for id, sc := range s.scheds {
		if sc.Source == "config" && !seen[id] {
			orphans = append(orphans, id)
		}
	}
	s.mu.Unlock()

	for _, sc := range fromConfig {
		sc.Source = "config"
		// Config entries that reach Sync are enabled by definition — the
		// converter drops enabled:false rows and the orphan pass below
		// retires whatever they previously owned.
		sc.Enabled = true
		if _, err := s.Upsert(ctx, sc); err != nil {
			return fmt.Errorf("schedule: sync %s: %w", sc.ID, err)
		}
	}
	for _, id := range orphans {
		if _, err := s.Cancel(id); err != nil {
			return fmt.Errorf("schedule: sync disable %s: %w", id, err)
		}
	}
	return nil
}

// Run fires due schedules until ctx is done. The ticker is only a wake-up
// signal; due-ness is computed against the scheduler clock, so tests can
// drive a fake clock past a next-run while the real ticker ticks fast.
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(s.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.fireDue(ctx)
		}
	}
}

// fireDue submits every enabled schedule whose next fire has arrived,
// then advances its bookkeeping: LastRun = now, NextRun recomputed, and a
// spent one_time disables itself. Submits run outside the lock — a host
// callback that re-enters the scheduler (List, Get) must not deadlock.
func (s *Scheduler) fireDue(ctx context.Context) {
	now := s.now()
	s.mu.Lock()
	var due []Schedule
	for id, sc := range s.scheds {
		if !sc.Enabled || sc.Trigger.Empty() {
			continue
		}
		next := sc.NextRunAt
		if next.IsZero() {
			if n, ok := sc.Trigger.Next(now); ok {
				next = n
			}
		}
		if next.IsZero() || next.After(now) {
			continue
		}
		sc.LastRunAt = now
		if n, ok := sc.Trigger.Next(now); ok {
			sc.NextRunAt = n
		} else {
			// The one_time fired (or was born spent): disable, keep the row.
			sc.NextRunAt = time.Time{}
			sc.Enabled = false
		}
		s.scheds[id] = sc
		due = append(due, sc)
	}
	s.mu.Unlock()

	for _, sc := range due {
		if err := s.submit(ctx, sc); err != nil {
			s.log("schedule: fire %s: %v", sc.ID, err)
		}
		if err := s.store.put(sc); err != nil {
			s.log("schedule: persist fire %s: %v", sc.ID, err)
		}
	}
}
