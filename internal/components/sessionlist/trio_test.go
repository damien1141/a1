package sessionlist

import (
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components/listpicker"
	"github.com/damien1141/a1/internal/session"
)

func TestFormatRelative_FutureClampAndBoundaries_Negative(t *testing.T) {
	now := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	// A future mtime (clock skew, mixed time zones) clamps to "just now"
	// instead of formatting a negative duration or jumping to a date.
	assert.Equal(t, "just now", FormatRelative(now.Add(time.Second), now), "one second ahead")
	assert.Equal(t, "just now", FormatRelative(now.Add(100*time.Hour), now), "far ahead")

	// Category boundaries land in the older bucket, never between buckets.
	assert.Equal(t, "1m ago", FormatRelative(now.Add(-60*time.Second), now), "exactly one minute")
	assert.Equal(t, "1h ago", FormatRelative(now.Add(-time.Hour), now), "exactly one hour")
	assert.Equal(t, "yesterday", FormatRelative(now.Add(-24*time.Hour), now), "exactly one day")
	assert.Equal(t, "2d ago", FormatRelative(now.Add(-48*time.Hour), now), "exactly two days")
	// Exactly 7 days leaves "Ndd ago" for the calendar fallback.
	assert.Equal(t, "Aug 25", FormatRelative(now.Add(-7*24*time.Hour), now), "exactly seven days")
}

func TestItems_HostileMetadata_Negative(t *testing.T) {
	now := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	items := Items([]session.SessionMeta{
		{ID: "", Preview: "", Mtime: now},         // fully blank row
		{ID: "12345678", Mtime: now},              // exactly at the 8-byte cut
		{ID: "123456789", Mtime: now},             // one byte past it
		{ID: "日本語日本語", Mtime: now.Add(time.Hour)}, // multibyte + future mtime
	}, "", now)
	require.Len(t, items, 5)

	assert.Equal(t, "", items[1].Primary, "empty id stays empty, no panic")
	assert.Equal(t, "(no preview)", items[1].Detail, "blank preview falls back to the placeholder")
	assert.Equal(t, " ", items[1].Keywords, "keywords stay id+' '+preview even when both are blank")
	// The blank row's empty ID compares equal to the empty currentID, so the
	// degenerate "current" match lands on the blank row. Pinned as-is.
	assert.Equal(t, "current", items[1].Badge, "empty currentID badges the empty-id row")

	assert.Equal(t, "12345678", items[2].Primary, "an 8-byte id is kept whole")
	assert.Equal(t, "12345678", items[3].Primary, "a 9-byte id is cut to 8")

	// shortID cuts bytes, not runes: the multibyte id yields its first 8 bytes,
	// splitting the third CJK rune mid-sequence. Pinned so a future switch to
	// rune-aware cutting is a conscious test change, not a silent pass.
	assert.Equal(t, string([]byte("日本語日本語")[:8]), items[4].Primary, "byte-based short id")
	assert.Equal(t, "just now", items[4].Leading, "future mtime clamps inside Items too")

	// A zero `now` is replaced by the wall clock, but a zero mtime still
	// renders the em dash regardless of when "now" resolves.
	zeroNow := Items([]session.SessionMeta{{ID: "x"}}, "", time.Time{})
	require.Len(t, zeroNow, 2)
	assert.Equal(t, "—", zeroNow[1].Leading, "zero mtime with a zero now")
}

// naiveRelative recomputes the compact-time spec with integer-second
// arithmetic and integer division — a units path independent of the
// time.Duration comparisons and float Minutes()/Hours() conversions used by
// FormatRelative.
func naiveRelative(t, now time.Time) string {
	if t.IsZero() {
		return "—"
	}
	secs := int(now.Sub(t) / time.Second)
	if secs < 0 {
		secs = 0
	}
	switch {
	case secs < 60:
		return "just now"
	case secs < 3600:
		return strconv.Itoa(secs/60) + "m ago"
	case secs < 86400:
		return strconv.Itoa(secs/3600) + "h ago"
	case secs < 172800:
		return "yesterday"
	case secs < 604800:
		return strconv.Itoa(secs/86400) + "d ago"
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2006")
}

// oracle: naive integer-seconds reference (above) on a boundary grid, random
// whole-second offsets, and day offsets that cross year boundaries — both
// implementations must agree on the bucket and the formatted value.
func TestFormatRelative_MatchesNaiveSecondsReference_Differential(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	corpus := []time.Time{}
	for _, secs := range []int{
		0, 1, 59, 60, 61, 3599, 3600, 3601, 86399, 86400, 90000,
		172799, 172800, 200000, 604799, 604800, 604801, 2 * 604800,
		-1, -3600, -100000, // future wall times
	} {
		corpus = append(corpus, now.Add(time.Duration(secs)*time.Second))
	}
	// Day offsets cross the 7-day line and the year line in both directions.
	for _, days := range []int{7, 8, 30, 200, 365, 366, 400, 500, -1, -300} {
		corpus = append(corpus, now.AddDate(0, 0, -days))
	}
	rng := rand.New(rand.NewSource(7))
	for range 250 {
		secs := rng.Intn(1_600_000) - 100_000
		corpus = append(corpus, now.Add(time.Duration(secs)*time.Second))
	}
	for _, at := range corpus {
		assert.Equal(t, naiveRelative(at, now), FormatRelative(at, now), "FormatRelative(%s)", at.Format(time.RFC3339))
	}
}

// oracle: hand-written golden rows — the complete expected []listpicker.Item
// slice for one fixed session list (synthetic row, relative times, short ids,
// preview fallback, current badge), written out literally rather than
// regenerated by Items.
func TestItems_MatchesGoldenRows_Differential(t *testing.T) {
	now := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	got := Items([]session.SessionMeta{
		{ID: "aaaaaaaa-1111-2222", Preview: "fix the login bug", Mtime: now.Add(-5 * time.Minute)},
		{ID: "short-id", Preview: "", Mtime: time.Time{}},
	}, "short-id", now)
	want := []listpicker.Item{
		{
			ID:       NewSessionID,
			Leading:  "new",
			Primary:  "New session",
			Detail:   "start an empty session",
			Keywords: "new clear fresh",
		},
		{
			ID:       "aaaaaaaa-1111-2222",
			Leading:  "5m ago",
			Primary:  "aaaaaaaa",
			Detail:   "fix the login bug",
			Keywords: "aaaaaaaa-1111-2222 fix the login bug",
		},
		{
			ID:       "short-id",
			Leading:  "—",
			Primary:  "short-id",
			Detail:   "(no preview)",
			Keywords: "short-id ",
			Badge:    "current",
		},
	}
	require.Len(t, got, 3)
	assert.Equal(t, want, got)
}
