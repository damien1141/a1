package sessionlist

import (
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/session"
)

func TestFormatRelative(t *testing.T) {
        now := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
        assert.Equal(t, "just now", FormatRelative(now.Add(-30*time.Second), now))
        assert.Equal(t, "5m ago", FormatRelative(now.Add(-5*time.Minute), now))
        assert.Equal(t, "3h ago", FormatRelative(now.Add(-3*time.Hour), now))
        assert.Equal(t, "yesterday", FormatRelative(now.Add(-25*time.Hour), now))
        assert.Equal(t, "3d ago", FormatRelative(now.Add(-3*24*time.Hour), now))
        assert.Equal(t, "Jan 2", FormatRelative(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), now))
        assert.Equal(t, "Dec 2025", FormatRelative(time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC), now))
        assert.Equal(t, "—", FormatRelative(time.Time{}, now))
}

func TestItemsMapsCurrentBadge(t *testing.T) {
        now := time.Now()
        items := Items([]session.SessionMeta{
                {ID: "aaaaaaaa-1111", Preview: "one", Mtime: now},
                {ID: "bbbbbbbb-2222", Preview: "two", Mtime: now},
        }, "bbbbbbbb-2222", now)
        // 2 sessions + 1 prepended "New session" row = 3 items.
        require.Len(t, items, 3)
        // The first item is the synthetic "New session" row.
        assert.Equal(t, NewSessionID, items[0].ID, "first row is the New session entry")
        assert.Equal(t, "New session", items[0].Primary)
        // The session rows follow; their badges are unchanged.
        assert.Empty(t, items[1].Badge)
        assert.Equal(t, "current", items[2].Badge)
        assert.Equal(t, "bbbbbbbb", items[2].Primary)
}

// TestItemsPrependsNewSessionRow verifies the "New session" row is
// always present, even when the session list is empty.
func TestItemsPrependsNewSessionRow(t *testing.T) {
        items := Items(nil, "", time.Now())
        require.Len(t, items, 1, "empty list still has the New session row")
        assert.Equal(t, NewSessionID, items[0].ID)
        assert.Equal(t, "New session", items[0].Primary)
        assert.Equal(t, "start an empty session", items[0].Detail)
}

// TestItemsNewSessionRowIsFilterable verifies the "New session" row
// matches a "new" filter so the user can find it by typing.
func TestItemsNewSessionRowIsFilterable(t *testing.T) {
        now := time.Now()
        items := Items(nil, "", now)
        require.Len(t, items, 1)
        assert.Contains(t, items[0].Keywords, "new",
                "New session row must match 'new' filter")
        assert.Contains(t, items[0].Keywords, "clear",
                "New session row must match 'clear' filter")
}
