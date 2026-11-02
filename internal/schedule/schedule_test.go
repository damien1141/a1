package schedule

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/sched.db")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestParseCron(t *testing.T) {
	valid := []string{
		"* * * * *",
		"*/15 * * * *",
		"30 3 * * *",
		"0 0 1 * *",
		"0 22 * * 1-5",
		"23 0-20/2 * * *",
		"5 4 * * sun",
		"0 0 29 2 *", // never fires — still parses
		"0 0 1 jan,dec *",
		"15 10 * * 7", // 7 = Sunday
		"0 0 * * mon-fri",
	}
	for _, expr := range valid {
		_, err := parseCron(expr)
		assert.NoError(t, err, expr)
	}
	invalid := []string{
		"",
		"* * *",
		"* * * * * *",
		"60 * * * *",
		"* 24 * * *",
		"0 0 0 * *",
		"0 0 32 * *",
		"0 0 * 13 *",
		"0 0 * * 8",
		"*/0 * * * *",
		"a * * * *",
		"5-1 * * * *",
	}
	for _, expr := range invalid {
		_, err := parseCron(expr)
		assert.Error(t, err, expr)
	}
}

func TestCronNext(t *testing.T) {
	base := time.Date(2026, 11, 3, 10, 7, 30, 0, time.UTC) // a Tuesday
	cases := []struct {
		expr string
		want time.Time
	}{
		{"* * * * *", time.Date(2026, 11, 3, 10, 8, 0, 0, time.UTC)},
		{"*/15 * * * *", time.Date(2026, 11, 3, 10, 15, 0, 0, time.UTC)},
		{"30 3 * * *", time.Date(2026, 11, 4, 3, 30, 0, 0, time.UTC)},
		{"0 0 1 * *", time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)},
		// Weekday-restricted: base is Tue Nov 3 10:07, so Tuesday noon
		// later today is the next fire.
		{"0 12 * * 2", time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)},
		// dom/dow both restricted: vixie OR — the 5th (Thursday) matches
		// dow before the 15th matches dom.
		{"0 0 5,15 * 4", time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)},
		// dom restricted, dow starred: the 15th.
		{"0 0 15 * *", time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)},
		{"23 0-20/2 * * *", time.Date(2026, 11, 3, 10, 23, 0, 0, time.UTC)},
		{"0 0 1 jan *", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
		// Feb 30 never fires within the scan bound.
		{"0 0 30 2 *", time.Time{}},
	}
	for _, tc := range cases {
		spec, err := parseCron(tc.expr)
		require.NoError(t, err, tc.expr)
		got, ok := spec.next(base)
		if tc.want.IsZero() {
			assert.False(t, ok, tc.expr)
			continue
		}
		require.True(t, ok, tc.expr)
		assert.Equal(t, tc.want, got, tc.expr)
	}
}

func TestParseTriggerValidation(t *testing.T) {
	_, err := ParseTrigger("", "", "")
	assert.Error(t, err)
	_, err = ParseTrigger("* * * * *", "15m", "")
	assert.Error(t, err) // two triggers
	_, err = ParseTrigger("bad", "", "")
	assert.Error(t, err)
	_, err = ParseTrigger("", "-5m", "")
	assert.Error(t, err)
	_, err = ParseTrigger("", "15", "")
	assert.Error(t, err) // not a duration
	_, err = ParseTrigger("", "", "2026-13-01T00:00:00Z")
	assert.Error(t, err)

	tr, err := ParseTrigger("", "15m", "")
	require.NoError(t, err)
	assert.Equal(t, KindFixedRate, tr.Kind())
	tr, err = ParseTrigger("", "", "2026-12-01T09:00:00Z")
	require.NoError(t, err)
	assert.Equal(t, KindOneTime, tr.Kind())
}

func TestTriggerNext(t *testing.T) {
	base := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)

	tr := Trigger{FixedRate: 15 * time.Minute}
	next, ok := tr.Next(base)
	require.True(t, ok)
	assert.Equal(t, base.Add(15*time.Minute), next)

	past := Trigger{OneTime: base.Add(-time.Hour)}
	_, ok = past.Next(base)
	assert.False(t, ok)

	future := Trigger{OneTime: base.Add(time.Hour)}
	next, ok = future.Next(base)
	require.True(t, ok)
	assert.Equal(t, future.OneTime, next)
}

func TestSchedulerUpsertListCancel(t *testing.T) {
	db := testDB(t)
	s, err := New(db, func(context.Context, Schedule) error { return nil })
	require.NoError(t, err)

	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	sc, err := s.Upsert(context.Background(), Schedule{
		Prompt:  "run the lints",
		Trigger: Trigger{Cron: "30 3 * * *"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, sc.ID)
	assert.Equal(t, "tool", sc.Source)
	assert.True(t, sc.NextRunAt.Equal(time.Date(2026, 11, 4, 3, 30, 0, 0, time.UTC)))

	list := s.List()
	require.Len(t, list, 1)

	_, err = s.Upsert(context.Background(), Schedule{Prompt: "no trigger"})
	assert.Error(t, err)
	_, err = s.Upsert(context.Background(), Schedule{Trigger: Trigger{Cron: "* * * * *"}})
	assert.Error(t, err) // prompt required

	cancelled, err := s.Cancel(sc.ID)
	require.NoError(t, err)
	assert.False(t, cancelled.Enabled)

	_, err = s.Cancel("nope")
	assert.Error(t, err)
}

func TestSchedulerFiresOneTimeAndDisables(t *testing.T) {
	db := testDB(t)
	var (
		mu    sync.Mutex
		fired []Schedule
	)
	s, err := New(db, func(_ context.Context, sc Schedule) error {
		mu.Lock()
		fired = append(fired, sc)
		mu.Unlock()
		return nil
	}, WithTick(2*time.Millisecond))
	require.NoError(t, err)

	fakeNow := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	s.now = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return fakeNow
	}

	sc, err := s.Upsert(context.Background(), Schedule{
		Prompt:  "one-shot",
		Trigger: Trigger{OneTime: fakeNow.Add(time.Second)},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)
	defer cancel()

	// Advance the fake clock past the fire time; the real ticker (2ms)
	// notices within a few ticks.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		clockMu.Lock()
		fakeNow = fakeNow.Add(500 * time.Millisecond)
		clockMu.Unlock()
		mu.Lock()
		n := len(fired)
		mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(3 * time.Millisecond)
	}
	mu.Lock()
	require.Len(t, fired, 1)
	mu.Unlock()

	got, ok := s.Get(sc.ID)
	require.True(t, ok)
	assert.False(t, got.Enabled, "one_time must disable after firing")
	assert.False(t, got.LastRunAt.IsZero())

	// Give the loop one more pass to prove it does not re-fire.
	clockMu.Lock()
	fakeNow = fakeNow.Add(time.Hour)
	clockMu.Unlock()
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	assert.Len(t, fired, 1)
	mu.Unlock()
}

func TestSchedulerPersistenceRoundTrip(t *testing.T) {
	db := testDB(t)
	s1, err := New(db, func(context.Context, Schedule) error { return nil })
	require.NoError(t, err)
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	s1.now = func() time.Time { return now }

	created, err := s1.Upsert(context.Background(), Schedule{
		Prompt:   "nightly lint",
		Role:     "worker",
		Contract: []string{"go-build", "go-test:..."},
		Timeout:  30 * time.Minute,
		Trigger:  Trigger{FixedRate: 6 * time.Hour},
	})
	require.NoError(t, err)
	_, err = s1.Upsert(context.Background(), Schedule{
		Prompt:  "disabled sibling",
		Trigger: Trigger{Cron: "* * * * *"},
	})
	require.NoError(t, err)
	_, err = s1.Cancel(created.ID)
	require.NoError(t, err)

	// A second scheduler over the same db reloads the persisted state.
	s2, err := New(db, func(context.Context, Schedule) error { return nil })
	require.NoError(t, err)
	list := s2.List()
	require.Len(t, list, 2)
	byID := map[string]Schedule{}
	for _, sc := range list {
		byID[sc.ID] = sc
	}
	got := byID[created.ID]
	assert.False(t, got.Enabled)
	assert.Equal(t, "nightly lint", got.Prompt)
	assert.Equal(t, "worker", got.Role)
	assert.Equal(t, []string{"go-build", "go-test:..."}, got.Contract)
	assert.Equal(t, 30*time.Minute, got.Timeout)
	assert.Equal(t, 6*time.Hour, got.Trigger.FixedRate)
}

func TestSyncConfigWinsAndDisablesOrphans(t *testing.T) {
	db := testDB(t)
	s, err := New(db, func(context.Context, Schedule) error { return nil })
	require.NoError(t, err)
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	// A tool-created schedule with an id the config will claim.
	_, err = s.Upsert(context.Background(), Schedule{
		ID:      "nightly",
		Prompt:  "tool-made version",
		Source:  "tool",
		Trigger: Trigger{Cron: "0 4 * * *"},
	})
	require.NoError(t, err)
	// A config schedule that later disappears.
	_, err = s.Upsert(context.Background(), Schedule{
		ID:      "gone-soon",
		Source:  "config",
		Prompt:  "temporary",
		Trigger: Trigger{Cron: "0 5 * * *"},
	})
	require.NoError(t, err)
	// A tool schedule Sync must never touch.
	toolOnly, err := s.Upsert(context.Background(), Schedule{
		Prompt:  "model-made",
		Source:  "tool",
		Trigger: Trigger{Cron: "0 6 * * *"},
	})
	require.NoError(t, err)

	err = s.Sync(context.Background(), []Schedule{{
		ID:      "nightly",
		Prompt:  "config version",
		Trigger: Trigger{Cron: "30 3 * * *"},
	}})
	require.NoError(t, err)

	got, ok := s.Get("nightly")
	require.True(t, ok)
	assert.Equal(t, "config version", got.Prompt)
	assert.Equal(t, "config", got.Source)
	assert.Equal(t, time.Date(2026, 11, 4, 3, 30, 0, 0, time.UTC), got.NextRunAt)

	// gone-soon was config-sourced and vanished → disabled. toolOnly stays.
	orph, ok := s.Get("gone-soon")
	require.True(t, ok)
	assert.False(t, orph.Enabled)
	kept, ok := s.Get(toolOnly.ID)
	require.True(t, ok)
	assert.True(t, kept.Enabled)
}
