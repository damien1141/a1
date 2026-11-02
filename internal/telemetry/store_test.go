package telemetry

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"database/sql"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "tel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestStoreRoundTrip(t *testing.T) {
	db := testDB(t)
	st, err := NewStore(db)
	require.NoError(t, err)

	st.Record(Event{Kind: EventToolCall, Tool: "bash", CallID: "c1", When: time.Now()})
	st.Record(Event{Kind: EventToolOK, Tool: "bash", CallID: "c1", When: time.Now(),
		DurationMS: 42, SessionID: "sess-1"})
	st.Record(Event{Kind: EventToolErr, Tool: "read", CallID: "c2", When: time.Now(),
		DurationMS: 7, Err: "boom", SessionID: "sess-1"})
	st.Close()

	aggs, err := ToolAggregates(db)
	require.NoError(t, err)
	require.Len(t, aggs, 2)
	// bash first: higher call count (the tool_call pre-event is not a row).
	assert.Equal(t, "bash", aggs[0].Tool)
	assert.Equal(t, 1, aggs[0].Calls)
	assert.Equal(t, 1, aggs[0].OK)
	assert.Equal(t, 0, aggs[0].Errors)
	assert.InDelta(t, 42.0, aggs[0].MeanLatency, 1e-9)
	assert.Equal(t, "read", aggs[1].Tool)
	assert.Equal(t, 1, aggs[1].Errors)

	errs, err := RecentErrors(db, 10)
	require.NoError(t, err)
	require.Len(t, errs, 1)
	assert.Equal(t, "read", errs[0].Tool)
	assert.Equal(t, "boom", errs[0].Err)

	runs, err := RecentToolRuns(db, 10)
	require.NoError(t, err)
	require.Len(t, runs, 2, "tool_call pre-events never persist")
	assert.Equal(t, "read", runs[0].Tool, "newest first")
}

func TestStoreCloseFlushes(t *testing.T) {
	db := testDB(t)
	st, err := NewStore(db)
	require.NoError(t, err)
	st.Record(Event{Kind: EventToolOK, Tool: "bash", CallID: "c1", When: time.Now(), DurationMS: 5})
	st.Close()
	runs, err := RecentToolRuns(db, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1, "queued events land before the writer exits")
	// Idempotent close.
	st.Close()
}

func TestMaterializeTurnsIdempotent(t *testing.T) {
	db := testDB(t)
	rows := []TurnRow{
		{Model: "m1", PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, At: "2026-02-01T10:00:00Z"},
		{Model: "m1", PromptTokens: 20, CompletionTokens: 5, TotalTokens: 25, At: "2026-02-01T10:01:00Z"},
	}
	require.NoError(t, MaterializeTurns(db, "sess-1", rows))
	require.NoError(t, MaterializeTurns(db, "sess-1", rows), "re-materializing replaces")

	summary, err := TurnSummary(db)
	require.NoError(t, err)
	require.Len(t, summary, 1)
	assert.Equal(t, "sess-1", summary[0].SessionID)
	assert.Equal(t, "m1", summary[0].Model)
	assert.Equal(t, 2, summary[0].Turns)
	assert.Equal(t, int64(40), summary[0].TotalTok)
}

func TestMaterializeTurnsDistinctSessions(t *testing.T) {
	db := testDB(t)
	require.NoError(t, MaterializeTurns(db, "a", []TurnRow{{Model: "m", TotalTokens: 5, At: "t1"}}))
	require.NoError(t, MaterializeTurns(db, "b", []TurnRow{{Model: "m", TotalTokens: 7, At: "t1"}}))
	summary, err := TurnSummary(db)
	require.NoError(t, err)
	require.Len(t, summary, 2)
}

// collectingSink captures every event handed to it (tests only).
type collectingSink struct {
	mu     sync.Mutex
	events []Event
}

func (c *collectingSink) Record(e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func TestRecorderForwardsToGlobalSink(t *testing.T) {
	collector := &collectingSink{}
	SetGlobalSink(collector)
	defer SetGlobalSink(nil)

	r := NewRecorder()
	r.Record(Event{Kind: EventToolOK, Tool: "bash", DurationMS: 3, SessionID: "s"})
	r.Record(Event{Kind: EventToolErr, Tool: "read", Err: "x"})

	collector.mu.Lock()
	defer collector.mu.Unlock()
	require.Len(t, collector.events, 2)
	assert.Equal(t, "s", collector.events[0].SessionID)
	assert.Equal(t, int64(3), collector.events[0].DurationMS)

	// The in-memory counters still work alongside the sink.
	assert.Equal(t, 1, r.ToolOK())
	assert.Equal(t, 1, r.ToolErrors())
}

func TestStoreDropsWhenFull(t *testing.T) {
	db := testDB(t)
	st, err := NewStore(db)
	require.NoError(t, err)
	defer st.Close()
	// Flood past the queue capacity without draining: Record must not
	// block and must count the drops.
	for i := 0; i < queueCapacity+100; i++ {
		st.Record(Event{Kind: EventToolOK, Tool: "bash", When: time.Now()})
	}
	assert.Positive(t, st.DropCount(), "overflow drops instead of blocking")
}

func TestNilHandlesAreSafe(t *testing.T) {
	var st *Store
	assert.NotPanics(t, func() { st.Record(Event{Kind: EventToolOK}) })
	assert.NotPanics(t, func() { st.Close() })
	assert.Equal(t, int64(0), st.DropCount())
	assert.NotPanics(t, func() {
		var r *Recorder
		r.Record(Event{})
	})
}
